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
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/lightningnetwork/lnd/clock"
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

	if diff := cmp.Diff(recordedValue, actualValue); diff != "" {
		return fmt.Errorf(
			"grpc %s %s[%d] mismatch (-want +got):\n%s",
			method,
			event,
			idx,
			diff,
		)
	}
	return nil
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

			stderrUnhook, err := hookStderr(os.Stderr, nil, func(p []byte) {
				stderrBuf.Write(p)
			})
			require.NoErrorf(t, err, "hook stderr for %s", path)

			stdinUnhook, err := hookStdin(os.Stdin, bytes.NewBufferString(replay.stdin), nil)
			require.NoErrorf(t, err, "hook stdin for %s", path)

			prevTerm := term
			term = newTerminal(os.Stdin, os.Stdout, os.Stderr)
			defer func() { term = prevTerm }()

			restoreDialer := withClientDialer(&replayDialer{conn: replay.conn})
			defer restoreDialer()

			restoreEnv := applyEnv(replay.env)
			defer restoreEnv()

			restoreClock := withClock(
				clock.NewTestClock(time.Unix(sessionClockStartUnix, 0)),
			)
			defer restoreClock()

			sessionRec = nil

			cmd := newRootCommandForReplay()

			err = cmd.Run(context.Background(), replay.args)
			if err != nil {
				term.Errorf("[loop] %v\n", err)
			}

			require.NoErrorf(t, stdoutUnhook(), "unhook stdout for %s", path)
			require.NoErrorf(t, stderrUnhook(), "unhook stderr for %s", path)
			require.NoErrorf(t, stdinUnhook(), "unhook stdin for %s", path)

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

// newRootCommandForReplay returns a root command clone with fresh flag state.
func newRootCommandForReplay() *cli.Command {
	return cloneCommandForReplay(newRootCommand())
}

// cloneCommandForReplay deep-clones a command tree for deterministic replays.
func cloneCommandForReplay(cmd *cli.Command) *cli.Command {
	if cmd == nil {
		return nil
	}

	cloned := cloneCommandStruct(cmd)
	cloned.Flags, cloned.MutuallyExclusiveFlags = cloneFlagsWithGroups(
		cmd.Flags, cmd.MutuallyExclusiveFlags,
	)
	cloned.Arguments = cloneArguments(cmd.Arguments)
	cloned.Commands = cloneCommands(cmd.Commands)

	return cloned
}

// cloneCommandStruct copies exported fields of a command into a new instance.
func cloneCommandStruct(cmd *cli.Command) *cli.Command {
	if cmd == nil {
		return nil
	}

	src := reflect.ValueOf(cmd).Elem()
	dst := reflect.New(src.Type()).Elem()
	copyExportedFields(dst, src)
	return dst.Addr().Interface().(*cli.Command)
}

// cloneCommands clones a list of subcommands for replay.
func cloneCommands(cmds []*cli.Command) []*cli.Command {
	if len(cmds) == 0 {
		return nil
	}

	cloned := make([]*cli.Command, len(cmds))
	for i, cmd := range cmds {
		cloned[i] = cloneCommandForReplay(cmd)
	}
	return cloned
}

// cloneFlagsWithGroups clones flags and rebinds mutually exclusive groups.
func cloneFlagsWithGroups(flags []cli.Flag, groups []cli.MutuallyExclusiveFlags) ([]cli.Flag, []cli.MutuallyExclusiveFlags) {
	clonedFlags, clonedMap := cloneFlags(flags)
	clonedGroups := cloneMutuallyExclusiveFlags(groups, clonedMap)
	return clonedFlags, clonedGroups
}

// cloneFlags creates fresh flag instances and returns a map of originals to clones.
func cloneFlags(flags []cli.Flag) ([]cli.Flag, map[cli.Flag]cli.Flag) {
	if len(flags) == 0 {
		return nil, map[cli.Flag]cli.Flag{}
	}

	cloned := make([]cli.Flag, len(flags))
	clonedMap := make(map[cli.Flag]cli.Flag, len(flags))
	for i, flag := range flags {
		if flag == nil {
			continue
		}
		copy := cloneFlag(flag)
		cloned[i] = copy
		clonedMap[flag] = copy
	}
	return cloned, clonedMap
}

// cloneMutuallyExclusiveFlags clones flag groups using the provided flag map.
func cloneMutuallyExclusiveFlags(groups []cli.MutuallyExclusiveFlags, clonedMap map[cli.Flag]cli.Flag) []cli.MutuallyExclusiveFlags {
	if len(groups) == 0 {
		return nil
	}

	clonedGroups := make([]cli.MutuallyExclusiveFlags, len(groups))
	for i, group := range groups {
		clonedGroup := cli.MutuallyExclusiveFlags{
			Required: group.Required,
			Category: group.Category,
		}
		if len(group.Flags) > 0 {
			clonedGroup.Flags = make([][]cli.Flag, len(group.Flags))
			for j, option := range group.Flags {
				if len(option) == 0 {
					continue
				}
				clonedOption := make([]cli.Flag, len(option))
				for k, flag := range option {
					if flag == nil {
						continue
					}
					clone, ok := clonedMap[flag]
					if !ok {
						clone = cloneFlag(flag)
						clonedMap[flag] = clone
					}
					clonedOption[k] = clone
				}
				clonedGroup.Flags[j] = clonedOption
			}
		}
		clonedGroups[i] = clonedGroup
	}

	return clonedGroups
}

// cloneFlag clones a single flag by copying its exported fields.
func cloneFlag(flag cli.Flag) cli.Flag {
	if flag == nil {
		return nil
	}

	cloned, ok := cloneStructWithExportedFields(flag)
	if !ok {
		return flag
	}
	clonedFlag, ok := cloned.(cli.Flag)
	if !ok {
		return flag
	}
	return clonedFlag
}

// cloneArguments clones positional argument definitions.
func cloneArguments(args []cli.Argument) []cli.Argument {
	if len(args) == 0 {
		return nil
	}

	cloned := make([]cli.Argument, len(args))
	for i, arg := range args {
		cloned[i] = cloneArgument(arg)
	}
	return cloned
}

// cloneArgument clones a single argument by copying its exported fields.
func cloneArgument(arg cli.Argument) cli.Argument {
	if arg == nil {
		return nil
	}

	cloned, ok := cloneStructWithExportedFields(arg)
	if !ok {
		return arg
	}
	clonedArg, ok := cloned.(cli.Argument)
	if !ok {
		return arg
	}
	return clonedArg
}

// cloneStructWithExportedFields clones a pointer-to-struct by exported fields.
func cloneStructWithExportedFields(src interface{}) (interface{}, bool) {
	if src == nil {
		return nil, false
	}

	value := reflect.ValueOf(src)
	if value.Kind() != reflect.Ptr || value.Elem().Kind() != reflect.Struct {
		return nil, false
	}

	cloned := reflect.New(value.Elem().Type())
	copyExportedFields(cloned.Elem(), value.Elem())
	return cloned.Interface(), true
}

// copyExportedFields copies exported fields from src into dst.
func copyExportedFields(dst, src reflect.Value) {
	for i := 0; i < src.NumField(); i++ {
		field := src.Type().Field(i)
		if field.PkgPath != "" {
			continue
		}
		dstField := dst.Field(i)
		if !dstField.CanSet() {
			continue
		}
		dstField.Set(cloneValue(src.Field(i)))
	}
}

// cloneValue shallow-clones slices and maps while preserving other values.
func cloneValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}

	switch value.Kind() {
	case reflect.Slice:
		if value.IsNil() {
			return value
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		reflect.Copy(cloned, value)
		return cloned
	case reflect.Map:
		if value.IsNil() {
			return value
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		for _, key := range value.MapKeys() {
			cloned.SetMapIndex(key, value.MapIndex(key))
		}
		return cloned
	default:
		return value
	}
}

func requireTextEqual(t *testing.T, label, expected, actual string) {
	t.Helper()
	expected = normalizeRFC3339Timestamps(expected)
	actual = normalizeRFC3339Timestamps(actual)
	if diff := cmp.Diff(expected, actual); diff != "" {
		t.Fatalf("%s mismatch (-want +got):\n%s", label, diff)
	}
}

// rfc3339TimestampRegex matches RFC3339 timestamps embedded in CLI output.
var rfc3339TimestampRegex = regexp.MustCompile(
	`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`,
)

// normalizeRFC3339Timestamps rewrites RFC3339 timestamps to UTC to avoid
// environment-dependent timezone output during session replay.
func normalizeRFC3339Timestamps(text string) string {
	return rfc3339TimestampRegex.ReplaceAllStringFunc(text, func(ts string) string {
		parsed, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return ts
		}
		return parsed.UTC().Format(time.RFC3339Nano)
	})
}

// TestCloneCommandForReplayResetsFlagState verifies cloned commands reset flag state.
func TestCloneCommandForReplayResetsFlagState(t *testing.T) {
	originalFlag := &cli.StringFlag{
		Name:    "alpha",
		Usage:   "alpha usage",
		Aliases: []string{"a"},
	}
	require.NoError(t, originalFlag.Set("alpha", "value"))
	require.True(t, originalFlag.IsSet())

	sharedFlag := &cli.BoolFlag{Name: "shared"}
	require.NoError(t, sharedFlag.Set("shared", "true"))
	require.True(t, sharedFlag.IsSet())

	originalArg := &cli.StringArg{
		Name:      "arg",
		UsageText: "arg usage",
		Value:     "default",
	}
	_, err := originalArg.Parse([]string{"parsed"})
	require.NoError(t, err)
	require.Equal(t, "parsed", originalArg.Get())

	root := &cli.Command{
		Name:  "root",
		Flags: []cli.Flag{originalFlag, sharedFlag},
		MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{
			{
				Flags:    [][]cli.Flag{{originalFlag}, {sharedFlag}},
				Required: true,
				Category: "cat",
			},
		},
		Arguments: []cli.Argument{originalArg},
		Metadata: map[string]interface{}{
			"key": "value",
		},
		Commands: []*cli.Command{
			{
				Name:  "sub",
				Flags: []cli.Flag{sharedFlag},
			},
		},
	}

	cloned := cloneCommandForReplay(root)

	require.NotSame(t, root, cloned)
	require.Len(t, cloned.Flags, len(root.Flags))
	require.Len(t, cloned.Commands, len(root.Commands))
	require.Len(t, cloned.MutuallyExclusiveFlags, len(root.MutuallyExclusiveFlags))
	require.Len(t, cloned.Arguments, len(root.Arguments))

	clonedAlpha := findFlagByName(t, cloned.Flags, "alpha").(*cli.StringFlag)
	require.NotSame(t, originalFlag, clonedAlpha)
	require.False(t, clonedAlpha.IsSet())
	require.Equal(t, originalFlag.Name, clonedAlpha.Name)
	require.Equal(t, originalFlag.Usage, clonedAlpha.Usage)
	require.Equal(t, originalFlag.Aliases, clonedAlpha.Aliases)

	clonedAlpha.Aliases[0] = "b"
	require.Equal(t, []string{"a"}, originalFlag.Aliases)

	clonedShared := findFlagByName(t, cloned.Flags, "shared").(*cli.BoolFlag)
	require.NotSame(t, sharedFlag, clonedShared)
	require.False(t, clonedShared.IsSet())

	group := cloned.MutuallyExclusiveFlags[0]
	require.Same(t, clonedAlpha, group.Flags[0][0].(*cli.StringFlag))
	require.Same(t, clonedShared, group.Flags[1][0].(*cli.BoolFlag))

	clonedArg := cloned.Arguments[0].(*cli.StringArg)
	require.NotSame(t, originalArg, clonedArg)
	require.Equal(t, "default", clonedArg.Get())

	cloned.Metadata["key"] = "updated"
	require.Equal(t, "value", root.Metadata["key"])
}

// findFlagByName locates a flag by name or alias.
func findFlagByName(t *testing.T, flags []cli.Flag, name string) cli.Flag {
	t.Helper()
	for _, flag := range flags {
		if flag == nil {
			continue
		}
		for _, candidate := range flag.Names() {
			if candidate == name {
				return flag
			}
		}
	}
	t.Fatalf("flag %q not found", name)
	return nil
}
