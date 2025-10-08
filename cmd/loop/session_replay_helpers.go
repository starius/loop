package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/urfave/cli/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type recordedSession struct {
	args   []string
	env    map[string]string
	stdin  string
	stdout string
	stderr string
	failed *bool
	conn   *recordedClientConn
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
		args:   append([]string(nil), data.Metadata.Args...),
		env:    data.Metadata.Env,
		failed: data.Metadata.Failed,
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

	req, err := c.consume(method, "request")
	if err != nil {
		return err
	}

	if err := compareMessage(args, req.Payload); err != nil {
		return err
	}

	resp, err := c.consume(method, "response", "error")
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
				return fmt.Errorf("response type mismatch: got %s want %s", got, resp.MessageType)
			}
		}
		if len(resp.Payload) > 0 {
			if err := protoUnmarshal.Unmarshal(resp.Payload, replyMsg); err != nil {
				return err
			}
		}
		return nil
	}

	if len(resp.Payload) > 0 {
		return json.Unmarshal(resp.Payload, reply)
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

	evt, err := s.conn.consumeLocked(s.method, "send")
	if err != nil {
		return err
	}
	return compareMessage(m, evt.Payload)
}

func (s *replayStream) RecvMsg(m interface{}) error {
	s.conn.mu.Lock()
	defer s.conn.mu.Unlock()

	evt, err := s.conn.consumeLocked(s.method, "recv", "error")
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
				return fmt.Errorf("stream response type mismatch: got %s want %s", got, evt.MessageType)
			}
		}
		return protoUnmarshal.Unmarshal(evt.Payload, msg)
	}

	if len(evt.Payload) > 0 {
		return json.Unmarshal(evt.Payload, m)
	}

	return nil
}

func (c *recordedClientConn) consume(method, expected string, alternatives ...string) (*grpcPayload, error) {
	return c.consumeLocked(method, expected, alternatives...)
}

func (c *recordedClientConn) consumeLocked(method, expected string, alternatives ...string) (*grpcPayload, error) {
	if c.idx >= len(c.events) {
		return nil, fmt.Errorf("no more grpc events, expected %s", expected)
	}

	evt := c.events[c.idx]
	c.idx++

	if evt.Method != method {
		return nil, fmt.Errorf("unexpected method %s, want %s", evt.Method, method)
	}

	if evt.Event == expected || contains(alternatives, evt.Event) {
		return &evt, nil
	}

	return nil, fmt.Errorf("unexpected event %s, expected %s", evt.Event, expected)
}

func compareMessage(msg interface{}, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}

	switch typed := msg.(type) {
	case proto.Message:
		actual, err := protoMarshal.Marshal(typed)
		if err != nil {
			return err
		}
		return compareJSON(actual, raw)
	default:
		actual, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		return compareJSON(actual, raw)
	}
}

func compareJSON(actual []byte, recorded json.RawMessage) error {
	var actualValue interface{}
	var recordedValue interface{}

	if err := json.Unmarshal(actual, &actualValue); err != nil {
		return err
	}
	if err := json.Unmarshal(recorded, &recordedValue); err != nil {
		return err
	}

	if !reflect.DeepEqual(actualValue, recordedValue) {
		return fmt.Errorf("request mismatch: got %s want %s", actual, recorded)
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
