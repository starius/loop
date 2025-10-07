package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lightninglabs/loop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const (
	sessionEnvVar     = "LOOP_SESSION_RECORD"
	sessionDefaultDir = "cmd/loop/testdata/sessions"
	sessionFileExt    = ".json"
)

const (
	eventStdout = "stdout"
	eventStderr = "stderr"
	eventStdin  = "stdin"
	eventGrpc   = "grpc"
	eventExit   = "exit"
)

type sessionRecorder struct {
	mu       sync.Mutex
	started  time.Time
	filePath string
	slug     string

	metadata sessionMetadata
	events   []sessionEvent

	finalizeOnce sync.Once
	finalized    bool

	marshalOptions protojson.MarshalOptions
}

type sessionMetadata struct {
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	StartTime time.Time         `json:"start_time"`
	WorkDir   string            `json:"work_dir"`
	Version   string            `json:"version"`
	RunError  *string           `json:"run_error,omitempty"`
	Duration  *time.Duration    `json:"duration,omitempty"`
}

type sessionEvent struct {
	TimeMS int64           `json:"time_ms"`
	Kind   string          `json:"kind"`
	Data   json.RawMessage `json:"data"`
}

type textPayload struct {
	Text string `json:"text"`
}

type stdinPayload struct {
	Text string `json:"text"`
}

type grpcPayload struct {
	Method      string          `json:"method"`
	Event       string          `json:"event"`
	MessageType string          `json:"message_type,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Error       string          `json:"error,omitempty"`
}

type exitPayload struct {
	RunError *string `json:"run_error,omitempty"`
}

func newSessionRecorder(args []string) (*sessionRecorder, error) {
	destination := os.Getenv(sessionEnvVar)
	if destination == "" {
		return nil, nil
	}

	recorder := &sessionRecorder{
		started: time.Now(),
		marshalOptions: protojson.MarshalOptions{
			UseProtoNames:   true,
			EmitUnpopulated: true,
		},
	}

	recorder.slug = deriveSessionSlug(args)

	metadata := sessionMetadata{
		Args:      append([]string(nil), args...),
		Env:       collectSessionEnv(),
		StartTime: recorder.started,
		WorkDir:   getWorkingDir(),
		Version:   loop.RichVersion(),
	}
	recorder.metadata = metadata

	baseDir, fileName := recorder.resolveFilePath(destination)
	recorder.filePath = filepath.Join(baseDir, fileName)

	return recorder, nil
}

func collectSessionEnv() map[string]string {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		value := parts[1]
		if key == sessionEnvVar {
			continue
		}
		if strings.HasPrefix(key, "LOOPCLI_") {
			env[key] = value
		}
	}

	return env
}

func getWorkingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}

	return wd
}

func (r *sessionRecorder) resolveFilePath(dest string) (string, string) {
	timestamp := r.started.Format("20060102-150405")
	slug := r.slug
	if slug == "" {
		slug = "session"
	}

	parts := strings.Split(dest, "/")
	var subdir, action string
	if len(parts) == 2 {
		subdir, action = parts[0], parts[1]
	}

	nameParts := []string{
		"session",
		timestamp,
		slug,
	}
	if action != "" {
		nameParts = append(nameParts, action)
	}
	name := strings.Join(nameParts, "_") + ".json"

	baseDir := sessionDefaultDir
	if subdir != "" {
		baseDir = filepath.Join(baseDir, subdir)
	}

	return baseDir, name
}

func (r *sessionRecorder) logEvent(kind string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	event := sessionEvent{
		TimeMS: time.Since(r.started).Milliseconds(),
		Kind:   kind,
		Data:   data,
	}

	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *sessionRecorder) WrapWriter(kind string, writer io.Writer) io.Writer {
	return &recordingWriter{
		recorder: r,
		kind:     kind,
		inner:    writer,
	}
}

func (r *sessionRecorder) WrapReader(kind string, reader io.Reader) io.Reader {
	return &recordingReader{
		recorder: r,
		kind:     kind,
		inner:    reader,
	}
}

type recordingWriter struct {
	recorder *sessionRecorder
	kind     string
	inner    io.Writer
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	n, err := w.inner.Write(p)
	if n > 0 {
		payload := textPayload{Text: string(p[:n])}
		w.recorder.logEvent(w.kind, payload)
	}

	return n, err
}

type recordingReader struct {
	recorder *sessionRecorder
	kind     string
	inner    io.Reader
}

func (rdr *recordingReader) Read(p []byte) (int, error) {
	n, err := rdr.inner.Read(p)
	if n > 0 {
		rdr.recorder.logEvent(
			rdr.kind, stdinPayload{
				Text: string(p[:n]),
			},
		)
	}

	return n, err
}

func (r *sessionRecorder) logExit(runErr error) {
	var payload exitPayload
	if runErr != nil {
		msg := runErr.Error()
		payload.RunError = &msg
	}

	r.logEvent(eventExit, payload)

	duration := time.Since(r.started)
	r.mu.Lock()
	if runErr != nil {
		msg := runErr.Error()
		r.metadata.RunError = &msg
	} else {
		r.metadata.RunError = nil
	}
	r.metadata.Duration = &duration
	r.mu.Unlock()
}

func (r *sessionRecorder) finalize(runErr error) error {
	var finalizeErr error
	r.finalizeOnce.Do(func() {
		r.logExit(runErr)

		r.mu.Lock()
		metadata := r.metadata
		events := append([]sessionEvent(nil), r.events...)
		r.mu.Unlock()

		fileContent := struct {
			Metadata sessionMetadata `json:"metadata"`
			Events   []sessionEvent  `json:"events"`
		}{
			Metadata: metadata,
			Events:   events,
		}

		err := os.MkdirAll(filepath.Dir(r.filePath), 0o755)
		if err != nil {
			finalizeErr = err

			return
		}

		file, err := os.Create(r.filePath)
		if err != nil {
			finalizeErr = err

			return
		}
		defer file.Close()

		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(fileContent); err != nil {
			finalizeErr = err

			return
		}
	})

	return finalizeErr
}

func (r *sessionRecorder) Finalize(runErr error) error {
	return r.finalize(runErr)
}

func (r *sessionRecorder) UnaryInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply interface{},
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption) error {

		r.logGRPCMessage(method, "request", req, nil)

		err := invoker(ctx, method, req, reply, cc, opts...)
		if err != nil {
			r.logGRPCMessage(method, "error", nil, err)

			return err
		}

		r.logGRPCMessage(method, "response", reply, nil)

		return nil
	}
}

func (r *sessionRecorder) StreamInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc,
		cc *grpc.ClientConn, method string, streamer grpc.Streamer,
		opts ...grpc.CallOption) (grpc.ClientStream, error) {

		clientStream, err := streamer(ctx, desc, cc, method, opts...)
		if err != nil {
			r.logGRPCMessage(method, "error", nil, err)

			return nil, err
		}

		return &recordingClientStream{
			ClientStream: clientStream,
			recorder:     r,
			method:       method,
		}, nil
	}
}

type recordingClientStream struct {
	grpc.ClientStream
	recorder *sessionRecorder
	method   string
}

func (s *recordingClientStream) SendMsg(m interface{}) error {
	s.recorder.logGRPCMessage(s.method, "send", m, nil)
	err := s.ClientStream.SendMsg(m)
	if err != nil {
		s.recorder.logGRPCMessage(s.method, "error", nil, err)
	}
	return err
}

func (s *recordingClientStream) RecvMsg(m interface{}) error {
	err := s.ClientStream.RecvMsg(m)
	if err != nil {
		s.recorder.logGRPCMessage(s.method, "error", nil, err)
		return err
	}

	s.recorder.logGRPCMessage(s.method, "recv", m, nil)
	return nil
}

func (r *sessionRecorder) logGRPCMessage(method, event string, msg interface{},
	receptionErr error) {

	payload := grpcPayload{Method: method, Event: event}

	if receptionErr != nil {
		payload.Error = receptionErr.Error()
		r.logEvent(eventGrpc, payload)

		return
	}

	if msg != nil {
		if protoMsg, ok := msg.(proto.Message); ok {
			payload.MessageType = string(proto.MessageName(protoMsg))
			data, err := r.marshalOptions.Marshal(protoMsg)
			if err == nil {
				payload.Payload = data
			}
		} else {
			data, err := json.Marshal(msg)
			if err == nil {
				payload.Payload = data
			}
		}
	}

	r.logEvent(eventGrpc, payload)
}

func (r *sessionRecorder) InjectContext(ctx context.Context) context.Context {
	if r == nil {
		return ctx
	}

	return metadata.AppendToOutgoingContext(
		ctx, "loop-session", filepath.Base(r.filePath),
	)
}

func deriveSessionSlug(args []string) string {
	if len(args) == 0 {
		return ""
	}

	base := filepath.Base(args[0])
	tokens := []string{base}

	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, "-") {
			break
		}
		if arg == "" {
			continue
		}
		tokens = append(tokens, arg)
	}

	return sanitizeSlug(strings.Join(tokens, "-"))
}

func sanitizeSlug(value string) string {
	value = strings.ToLower(value)
	var builder strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			builder.WriteRune(r)
			lastDash = false
			continue
		}

		if !lastDash {
			builder.WriteRune('-')
			lastDash = true
		}
	}

	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "session"
	}
	return slug
}
