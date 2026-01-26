package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
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

	hooksMu      sync.Mutex
	hooksStarted bool
	stdoutUnhook func() error
	stderrUnhook func() error
	stdinUnhook  func() error
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
	envValue, ok := os.LookupEnv(sessionEnvVar)
	if !ok {
		return nil, nil
	}

	enabled, err := strconv.ParseBool(envValue)
	if err != nil {
		return nil, fmt.Errorf("invalid %s value %q", sessionEnvVar, envValue)
	}
	if !enabled {
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

	baseDir, fileName, err := recorder.resolveFilePath()
	if err != nil {
		return nil, err
	}
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

func (r *sessionRecorder) resolveFilePath() (string, string, error) {
	counter, err := nextSessionCounter(sessionDefaultDir)
	if err != nil {
		return "", "", err
	}

	slug := r.slug
	if slug == "" {
		slug = "session"
	}

	name := fmt.Sprintf("%02d_%s%s", counter, slug, sessionFileExt)
	return sessionDefaultDir, name, nil
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

func (r *sessionRecorder) Start(stdinSource io.Reader, stdoutForward, stderrForward io.Writer) error {
	if r == nil {
		return nil
	}

	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()

	if r.hooksStarted {
		return nil
	}

	origStdout := os.Stdout
	if stdoutForward == nil {
		stdoutForward = origStdout
	}
	stdoutHook, err := hookStdout(origStdout, stdoutForward, func(p []byte) {
		r.logEvent(eventStdout, textPayload{Text: string(p)})
	})
	if err != nil {
		return err
	}

	origStderr := os.Stderr
	if stderrForward == nil {
		stderrForward = origStderr
	}
	stderrHook, err := hookStderr(origStderr, stderrForward, func(p []byte) {
		r.logEvent(eventStderr, textPayload{Text: string(p)})
	})
	if err != nil {
		_ = stdoutHook()
		return err
	}

	origStdin := os.Stdin
	if stdinSource == nil {
		stdinSource = origStdin
	}
	stdinHook, err := hookStdin(origStdin, stdinSource, func(p []byte) {
		r.logEvent(eventStdin, stdinPayload{Text: string(p)})
	})
	if err != nil {
		_ = stderrHook()
		_ = stdoutHook()
		return err
	}

	r.stdoutUnhook = stdoutHook
	r.stderrUnhook = stderrHook
	r.stdinUnhook = stdinHook
	r.hooksStarted = true

	return nil
}

func (r *sessionRecorder) stopHooks() error {
	r.hooksMu.Lock()
	defer r.hooksMu.Unlock()

	var firstErr error
	if r.stdoutUnhook != nil {
		if err := r.stdoutUnhook(); err != nil && firstErr == nil {
			firstErr = err
		}
		r.stdoutUnhook = nil
	}
	if r.stderrUnhook != nil {
		if err := r.stderrUnhook(); err != nil && firstErr == nil {
			firstErr = err
		}
		r.stderrUnhook = nil
	}
	if r.stdinUnhook != nil {
		if err := r.stdinUnhook(); err != nil && firstErr == nil {
			firstErr = err
		}
		r.stdinUnhook = nil
	}
	r.hooksStarted = false

	return firstErr
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
		if err := r.stopHooks(); err != nil {
			finalizeErr = err
			return
		}

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

func nextSessionCounter(baseDir string) (int, error) {
	maxCounter := 0
	walkErr := filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(d.Name()) != sessionFileExt {
			return nil
		}

		counter, ok := parseSessionCounter(d.Name())
		if !ok {
			return nil
		}
		if counter > maxCounter {
			maxCounter = counter
		}
		return nil
	})
	if errors.Is(walkErr, fs.ErrNotExist) {
		return 1, nil
	}
	if walkErr != nil {
		return 0, walkErr
	}
	return maxCounter + 1, nil
}

func parseSessionCounter(name string) (int, bool) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	if base == "" {
		return 0, false
	}

	var digits strings.Builder
	for _, r := range base {
		if r < '0' || r > '9' {
			break
		}
		digits.WriteRune(r)
	}
	if digits.Len() == 0 {
		return 0, false
	}

	value, err := strconv.Atoi(digits.String())
	if err != nil {
		return 0, false
	}

	return value, true
}
