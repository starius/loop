package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var sessionsFS = func() fs.FS {
	// Find the location of cmd/loop source dir.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return emptyFS{}
	}
	loopDir := path.Dir(filename)

	cmdLoopFS := os.DirFS(loopDir)
	sub, err := fs.Sub(cmdLoopFS, "testdata/sessions")
	if err != nil {
		return emptyFS{}
	}

	return sub
}()

type emptyFS struct{}

func (emptyFS) Open(string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

type recordedSession struct {
	args     []string
	env      map[string]string
	stdin    string
	stdout   string
	stderr   string
	runError *string
	conn     *recordedClientConn
}

func loadRecordedSessionFS(fsys fs.FS, path string) (*recordedSession, error) {
	blob, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, err
	}
	return parseRecordedSession(blob)
}

func loadRecordedSessionPath(path string) (*recordedSession, error) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseRecordedSession(blob)
}

func parseRecordedSession(blob []byte) (*recordedSession, error) {
	var data struct {
		Metadata sessionMetadata `json:"metadata"`
		Events   []sessionEvent  `json:"events"`
	}

	if err := json.Unmarshal(blob, &data); err != nil {
		return nil, err
	}

	replay := &recordedSession{
		args:     append([]string(nil), data.Metadata.Args...),
		env:      data.Metadata.Env,
		runError: data.Metadata.RunError,
	}

	var stdoutBuilder strings.Builder
	var stderrBuilder strings.Builder
	var stdinBuilder strings.Builder

	conn, err := newRecordedClientConn(data.Events)
	if err != nil {
		return nil, err
	}
	replay.conn = conn

	for _, event := range data.Events {
		switch event.Kind {
		case eventStdout:
			var payload textPayload
			if err := json.Unmarshal(event.Data, &payload); err != nil {
				return nil, err
			}
			stdoutBuilder.WriteString(payload.Text)
		case eventStderr:
			var payload textPayload
			if err := json.Unmarshal(event.Data, &payload); err != nil {
				return nil, err
			}
			stderrBuilder.WriteString(payload.Text)
		case eventStdin:
			var payload stdinPayload
			if err := json.Unmarshal(event.Data, &payload); err != nil {
				return nil, err
			}
			stdinBuilder.WriteString(payload.Text)
		}
	}

	replay.stdin = stdinBuilder.String()
	replay.stdout = stdoutBuilder.String()
	replay.stderr = stderrBuilder.String()

	return replay, nil
}

func applyEnv(values map[string]string) func() {
	if len(values) == 0 {
		return func() {}
	}

	type previous struct {
		value string
		set   bool
	}

	prev := make(map[string]previous, len(values))
	for k, v := range values {
		curr, set := os.LookupEnv(k)
		prev[k] = previous{value: curr, set: set}
		_ = os.Setenv(k, v)
	}

	return func() {
		for k, p := range prev {
			if p.set {
				_ = os.Setenv(k, p.value)
				continue
			}
			_ = os.Unsetenv(k)
		}
	}
}

var protoMarshal = protojson.MarshalOptions{
	UseProtoNames:   true,
	EmitUnpopulated: true,
}

var protoUnmarshal = protojson.UnmarshalOptions{DiscardUnknown: true}

type replayDialer struct {
	conn *recordedClientConn
}

func (d *replayDialer) Dial(ctx context.Context, cmd *cli.Command) (grpc.ClientConnInterface, func(), error) {
	return d.conn, func() {}, nil
}

type recordedClientConn struct {
	events []grpcPayload
	idx    int
	mu     sync.Mutex
}

func newRecordedClientConn(events []sessionEvent) (*recordedClientConn, error) {
	var payloads []grpcPayload
	for _, event := range events {
		if event.Kind != eventGrpc {
			continue
		}
		var payload grpcPayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return nil, err
		}
		payloads = append(payloads, payload)
	}

	return &recordedClientConn{events: payloads}, nil
}

func (c *recordedClientConn) Invoke(ctx context.Context, method string, args interface{}, reply interface{}, opts ...grpc.CallOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	req, reqIdx, err := c.consume(method, "request")
	if err != nil {
		return err
	}

	if err := compareMessageWithContext(method, req.Event, reqIdx, args, req.Payload); err != nil {
		return err
	}

	resp, respIdx, err := c.consume(method, "response", "error")
	if err != nil {
		return err
	}

	if resp.Event == "error" {
		if resp.Error == io.EOF.Error() {
			return io.EOF
		}
		return errors.New(resp.Error)
	}

	if replyMsg, ok := reply.(proto.Message); ok {
		if resp.MessageType != "" {
			if got := string(proto.MessageName(replyMsg)); got != resp.MessageType {
				return fmt.Errorf("grpc %s response[%d] type mismatch: got %s want %s", method, respIdx, got, resp.MessageType)
			}
		}
		if len(resp.Payload) > 0 {
			if err := protoUnmarshal.Unmarshal(resp.Payload, replyMsg); err != nil {
				return fmt.Errorf("grpc %s response[%d] unmarshal: %w", method, respIdx, err)
			}
		}
		return nil
	}

	if len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, reply); err != nil {
			return fmt.Errorf("grpc %s response[%d] unmarshal: %w", method, respIdx, err)
		}
	}

	return nil
}

func (c *recordedClientConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return &replayStream{
		conn:   c,
		method: method,
		ctx:    ctx,
	}, nil
}

type replayStream struct {
	conn   *recordedClientConn
	method string
	ctx    context.Context
}

func (s *replayStream) Header() (metadata.MD, error) { return nil, nil }

func (s *replayStream) Trailer() metadata.MD { return nil }

func (s *replayStream) CloseSend() error { return nil }

func (s *replayStream) Context() context.Context { return s.ctx }

func (s *replayStream) SendMsg(m interface{}) error {
	s.conn.mu.Lock()
	defer s.conn.mu.Unlock()

	evt, evtIdx, err := s.conn.consumeLocked(s.method, "send")
	if err != nil {
		return err
	}
	return compareMessageWithContext(s.method, evt.Event, evtIdx, m, evt.Payload)
}

func (s *replayStream) RecvMsg(m interface{}) error {
	s.conn.mu.Lock()
	defer s.conn.mu.Unlock()

	evt, evtIdx, err := s.conn.consumeLocked(s.method, "recv", "error")
	if err != nil {
		return err
	}

	if evt.Event == "error" {
		if evt.Error == io.EOF.Error() {
			return io.EOF
		}
		return errors.New(evt.Error)
	}

	if msg, ok := m.(proto.Message); ok {
		if evt.MessageType != "" {
			if got := string(proto.MessageName(msg)); got != evt.MessageType {
				return fmt.Errorf("grpc %s recv[%d] type mismatch: got %s want %s", s.method, evtIdx, got, evt.MessageType)
			}
		}
		if err := protoUnmarshal.Unmarshal(evt.Payload, msg); err != nil {
			return fmt.Errorf("grpc %s recv[%d] unmarshal: %w", s.method, evtIdx, err)
		}
		return nil
	}

	if len(evt.Payload) > 0 {
		if err := json.Unmarshal(evt.Payload, m); err != nil {
			return fmt.Errorf("grpc %s recv[%d] unmarshal: %w", s.method, evtIdx, err)
		}
	}

	return nil
}

func (c *recordedClientConn) consume(method, expected string, alternatives ...string) (*grpcPayload, int, error) {
	return c.consumeLocked(method, expected, alternatives...)
}

func (c *recordedClientConn) consumeLocked(method, expected string, alternatives ...string) (*grpcPayload, int, error) {
	if c.idx >= len(c.events) {
		return nil, c.idx, fmt.Errorf("grpc %s event[%d] missing, expected %s", method, c.idx, expected)
	}

	idx := c.idx
	evt := c.events[c.idx]
	c.idx++

	if evt.Method != method {
		return nil, idx, fmt.Errorf("grpc event[%d] unexpected method %s, want %s", idx, evt.Method, method)
	}

	if evt.Event == expected || contains(alternatives, evt.Event) {
		return &evt, idx, nil
	}

	return nil, idx, fmt.Errorf("grpc %s event[%d] unexpected event %s, expected %s", method, idx, evt.Event, expected)
}

func compareMessageWithContext(method, event string, idx int, msg interface{}, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}

	switch typed := msg.(type) {
	case proto.Message:
		actual, err := protoMarshal.Marshal(typed)
		if err != nil {
			return err
		}
		return compareJSONWithContext(method, event, idx, actual, raw)
	default:
		actual, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		return compareJSONWithContext(method, event, idx, actual, raw)
	}
}

func compareJSONWithContext(method, event string, idx int, actual []byte, recorded json.RawMessage) error {
	var actualValue interface{}
	var recordedValue interface{}

	if err := json.Unmarshal(actual, &actualValue); err != nil {
		return fmt.Errorf("grpc %s %s[%d] unmarshal actual: %w", method, event, idx, err)
	}
	if err := json.Unmarshal(recorded, &recordedValue); err != nil {
		return fmt.Errorf("grpc %s %s[%d] unmarshal recorded: %w", method, event, idx, err)
	}

	if !reflect.DeepEqual(actualValue, recordedValue) {
		path, got, want := findJSONDiff(actualValue, recordedValue)
		return fmt.Errorf(
			"grpc %s %s[%d] mismatch at %s:\n got: %s\nwant: %s",
			method,
			event,
			idx,
			path,
			formatJSONValue(got),
			formatJSONValue(want),
		)
	}
	return nil
}

func findJSONDiff(actual, recorded interface{}) (string, interface{}, interface{}) {
	return findJSONDiffAt("$", actual, recorded)
}

func findJSONDiffAt(path string, actual, recorded interface{}) (string, interface{}, interface{}) {
	if reflect.DeepEqual(actual, recorded) {
		return "", nil, nil
	}
	if actual == nil || recorded == nil {
		return path, actual, recorded
	}

	switch typed := actual.(type) {
	case map[string]interface{}:
		other, ok := recorded.(map[string]interface{})
		if !ok {
			return path, actual, recorded
		}

		keys := make([]string, 0, len(typed)+len(other))
		seen := make(map[string]struct{}, len(typed)+len(other))
		for key := range typed {
			keys = append(keys, key)
			seen[key] = struct{}{}
		}
		for key := range other {
			if _, ok := seen[key]; !ok {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)

		for _, key := range keys {
			left, okLeft := typed[key]
			right, okRight := other[key]
			if !okLeft || !okRight {
				return path + "." + key, left, right
			}
			if !reflect.DeepEqual(left, right) {
				return findJSONDiffAt(path+"."+key, left, right)
			}
		}
		return path, actual, recorded

	case []interface{}:
		other, ok := recorded.([]interface{})
		if !ok {
			return path, actual, recorded
		}
		if len(typed) != len(other) {
			return path + ".length", len(typed), len(other)
		}
		for i := range typed {
			if !reflect.DeepEqual(typed[i], other[i]) {
				return findJSONDiffAt(fmt.Sprintf("%s[%d]", path, i), typed[i], other[i])
			}
		}
		return path, actual, recorded

	default:
		return path, actual, recorded
	}
}

func formatJSONValue(value interface{}) string {
	if value == nil {
		return "null"
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func contains(values []string, v string) bool {
	for _, value := range values {
		if value == v {
			return true
		}
	}
	return false
}

func TestRecordedSessions(t *testing.T) {
	if _, err := fs.ReadDir(sessionsFS, "."); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			t.Skip("no recorded sessions present")
		}
		require.NoError(t, err)
	}

	var sessionFiles []string
	walkErr := fs.WalkDir(sessionsFS, ".",
		func(path string, d fs.DirEntry, err error) error {

			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(d.Name(), sessionFileExt) {
				sessionFiles = append(sessionFiles, path)
			}

			return nil
		})
	require.NoError(t, walkErr)
	if len(sessionFiles) == 0 {
		t.Skip("no recorded sessions present")
	}

	for _, path := range sessionFiles {
		t.Run(path, func(t *testing.T) {
			prevDeterministic := forceDeterministicJSON
			forceDeterministicJSON = true
			defer func() { forceDeterministicJSON = prevDeterministic }()

			replay, err := loadRecordedSessionFS(sessionsFS, path)
			require.NoErrorf(t, err, "load session %s", path)

			var stdoutBuf bytes.Buffer
			var stderrBuf bytes.Buffer

			stdoutUnhook, err := hookStdout(os.Stdout, nil, func(p []byte) {
				stdoutBuf.Write(p)
			})
			require.NoErrorf(t, err, "hook stdout for %s", path)
			defer func() {
				require.NoErrorf(t, stdoutUnhook(), "unhook stdout for %s", path)
			}()

			stderrUnhook, err := hookStderr(os.Stderr, nil, func(p []byte) {
				stderrBuf.Write(p)
			})
			require.NoErrorf(t, err, "hook stderr for %s", path)
			defer func() {
				require.NoErrorf(t, stderrUnhook(), "unhook stderr for %s", path)
			}()

			stdinUnhook, err := hookStdin(os.Stdin, bytes.NewBufferString(replay.stdin), nil)
			require.NoErrorf(t, err, "hook stdin for %s", path)
			defer func() {
				require.NoErrorf(t, stdinUnhook(), "unhook stdin for %s", path)
			}()

			prevTerm := term
			term = newTerminal(os.Stdin, os.Stdout, os.Stderr)
			defer func() { term = prevTerm }()

			restoreDialer := withClientDialer(&replayDialer{conn: replay.conn})
			defer restoreDialer()

			restoreEnv := applyEnv(replay.env)
			defer restoreEnv()

			sessionRec = nil

			cmd := newRootCommand()

			err = cmd.Run(context.Background(), replay.args)
			if err != nil {
				term.Errorf("[loop] %v\n", err)
			}

			if replay.runError != nil {
				require.Error(t, err, "expected run error")
				require.Equalf(t, *replay.runError, err.Error(), "run error mismatch for %s", path)
			} else {
				require.NoErrorf(t, err, "command failed for %s", path)
			}

			requireTextEqual(t, "stdout", replay.stdout, stdoutBuf.String())

			requireTextEqual(t, "stderr", replay.stderr, stderrBuf.String())
		})
	}
}

func requireTextEqual(t *testing.T, label, expected, actual string) {
	t.Helper()
	if expected == actual {
		return
	}

	line, col, expLine, actLine := firstLineDiff(expected, actual)
	t.Fatalf(
		"%s mismatch at line %d, col %d:\nexpected: %q\nactual:   %q",
		label,
		line,
		col,
		expLine,
		actLine,
	)
}

func firstLineDiff(expected, actual string) (int, int, string, string) {
	expLines := strings.Split(expected, "\n")
	actLines := strings.Split(actual, "\n")
	maxLines := len(expLines)
	if len(actLines) > maxLines {
		maxLines = len(actLines)
	}

	for i := 0; i < maxLines; i++ {
		var expLine string
		var actLine string
		if i < len(expLines) {
			expLine = expLines[i]
		}
		if i < len(actLines) {
			actLine = actLines[i]
		}
		if expLine != actLine {
			col := firstDiffIndex(expLine, actLine) + 1
			return i + 1, col, expLine, actLine
		}
	}

	return 1, 1, "", ""
}

func firstDiffIndex(expected, actual string) int {
	maxLen := len(expected)
	if len(actual) < maxLen {
		maxLen = len(actual)
	}
	for i := 0; i < maxLen; i++ {
		if expected[i] != actual[i] {
			return i
		}
	}
	return maxLen
}
