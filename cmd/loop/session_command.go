package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

var sessionCommand = &cli.Command{
	Name:   "session",
	Usage:  "work with recorded CLI sessions",
	Hidden: true,
	Commands: []*cli.Command{
		sessionPlayCommand,
		sessionUpdateCommand,
	},
}

var sessionPlayCommand = &cli.Command{
	Name:      "play",
	Usage:     "play back a recorded session as a timeline",
	ArgsUsage: "<session-file>",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "realtime",
			Usage: "sleep between events to mimic original timing",
		},
	},
	Action: playSession,
}

var sessionUpdateCommand = &cli.Command{
	Name:      "update",
	Usage:     "replay a session with the current CLI and write a fresh recording",
	ArgsUsage: "<session-file>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "output",
			Aliases: []string{"o"},
			Usage:   "write updated session to this path (defaults to overwriting input)",
		},
		&cli.BoolFlag{
			Name:  "stdout",
			Usage: "mirror stdout/stderr while replaying",
		},
		&cli.BoolFlag{
			Name:  "all",
			Usage: "update all session recordings under testdata/sessions",
		},
	},
	Action: updateSession,
}

func playSession(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return showCommandHelp(ctx, cmd)
	}

	path := cmd.Args().Get(0)
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	var recording struct {
		Metadata sessionMetadata `json:"metadata"`
		Events   []sessionEvent  `json:"events"`
	}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&recording); err != nil {
		return err
	}

	realtime := cmd.Bool("realtime")
	var lastTS time.Duration

	for _, event := range recording.Events {
		ts := time.Duration(event.TimeMS) * time.Millisecond
		if realtime {
			if delta := ts - lastTS; delta > 0 {
				time.Sleep(delta)
			}
		}
		lastTS = ts

		if err := displayEvent(ts, event); err != nil {
			return err
		}
	}

	return nil
}

func displayEvent(ts time.Duration, event sessionEvent) error {
	timestamp := formatTimestamp(ts)

	switch event.Kind {
	case eventStdout:
		var payload textPayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		emitTextEvent(timestamp, "<", payload.Text)
	case eventStderr:
		var payload textPayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		emitTextEvent(timestamp, "!", payload.Text)
	case eventStdin:
		var payload stdinPayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		emitTextEvent(timestamp, ">", payload.Text)
	case eventGrpc:
		var payload grpcPayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		emitGRPCEvent(timestamp, payload)
	case eventExit:
		var payload exitPayload
		if err := json.Unmarshal(event.Data, &payload); err != nil {
			return err
		}
		term.Printf("[%s] exit failed=%t\n", timestamp, payload.Failed)
	default:
		return errors.New("unknown event kind: " + event.Kind)
	}

	return nil
}

func formatTimestamp(d time.Duration) string {
	totalMillis := d.Milliseconds()
	minutes := totalMillis / 60_000
	seconds := (totalMillis % 60_000) / 1000
	millis := totalMillis % 1000
	return fmt.Sprintf("%02d:%02d.%03d", minutes, seconds, millis)
}

func emitTextEvent(ts, prefix, text string) {
	if strings.HasSuffix(text, "\n") {
		term.Printf("[%s] %s %s", ts, prefix, text)
		return
	}
	term.Printf("[%s] %s %s\n", ts, prefix, text)
}

func emitGRPCEvent(ts string, payload grpcPayload) {
	var builder strings.Builder
	builder.WriteString("[")
	builder.WriteString(ts)
	builder.WriteString("] ⇄ ")
	builder.WriteString(payload.Method)
	builder.WriteString(" ")
	builder.WriteString(payload.Event)

	if payload.MessageType != "" {
		builder.WriteString(" type=")
		builder.WriteString(payload.MessageType)
	}

	if payload.Error != "" {
		builder.WriteString(" error=")
		builder.WriteString(payload.Error)
	}

	if len(payload.Payload) > 0 {
		pretty := prettifyJSON(payload.Payload)
		builder.WriteString(" payload=")
		builder.WriteString(pretty)
	}

	term.Println(builder.String())
}

func prettifyJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

func updateSession(ctx context.Context, cmd *cli.Command) error {
	if cmd.Bool("all") {
		if cmd.NArg() != 0 {
			return errors.New("--all cannot be combined with positional arguments")
		}
		if cmd.Bool("stdout") || cmd.IsSet("output") {
			return errors.New("--all cannot be combined with --stdout or --output")
		}
		return updateAllSessions(ctx, cmd)
	}

	if cmd.NArg() != 1 {
		return showCommandHelp(ctx, cmd)
	}

	srcArg := cmd.Args().Get(0)
	srcPath, err := resolveSessionPath(srcArg)
	if err != nil {
		return err
	}
	srcAbs, err := filepath.Abs(srcPath)
	if err != nil {
		return err
	}

	stdoutMode := cmd.Bool("stdout")
	if stdoutMode && cmd.IsSet("output") {
		return errors.New("--stdout cannot be combined with --output")
	}

	destPath := srcAbs
	if cmd.IsSet("output") {
		destPath = cmd.String("output")
		if filepath.Ext(destPath) == "" {
			destPath += sessionFileExt
		}
		if !filepath.IsAbs(destPath) {
			destPath, err = filepath.Abs(destPath)
			if err != nil {
				return err
			}
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
	}

	if stdoutMode {
		destPath = ""
	}

	return performSessionUpdate(ctx, cmd, srcAbs, destPath, stdoutMode)
}

func resolveSessionPath(arg string) (string, error) {
	candidates := []string{arg}
	if !filepath.IsAbs(arg) {
		candidates = append(candidates, filepath.Join(sessionDefaultDir, arg))
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}

		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		return candidate, nil
	}

	return "", fmt.Errorf("session file %s not found", arg)
}

func updateAllSessions(ctx context.Context, cmd *cli.Command) error {
	var files []string
	err := filepath.WalkDir(sessionDefaultDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != sessionFileExt {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return err
	}

	sort.Strings(files)
	if len(files) == 0 {
		term.Println("no session files found")
		return nil
	}

	for _, path := range files {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if err := performSessionUpdate(ctx, cmd, absPath, absPath, false); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}

	return nil
}

func performSessionUpdate(ctx context.Context, cmd *cli.Command, srcPath, destPath string, stdoutOnly bool) error {
	srcAbs, err := filepath.Abs(srcPath)
	if err != nil {
		return err
	}

	replay, err := loadRecordedSessionPath(srcAbs)
	if err != nil {
		return err
	}

	restoreEnv := applyEnv(replay.env)
	defer restoreEnv()

	prevDialer := withClientDialer(&replayDialer{conn: replay.conn})
	defer prevDialer()

	prevDeterministic := forceDeterministicJSON
	forceDeterministicJSON = true
	defer func() { forceDeterministicJSON = prevDeterministic }()

	stdinBuf := bytes.NewBufferString(replay.stdin)
	stdoutSink := io.Discard
	stderrSink := io.Discard

	prevTerm := term
	prevSession := sessionRec
	defer func() {
		term = prevTerm
		sessionRec = prevSession
	}()

	if stdoutOnly {
		stdoutSink = os.Stdout
		stderrSink = os.Stderr
		sessionRec = nil
	} else {
		destAbs, err := filepath.Abs(destPath)
		if err != nil {
			return err
		}
		prevRecordEnv, prevRecordSet := os.LookupEnv(sessionEnvVar)
		if err := os.Setenv(sessionEnvVar, destAbs); err != nil {
			return err
		}
		recorder, err := newSessionRecorder(replay.args)
		if !prevRecordSet {
			_ = os.Unsetenv(sessionEnvVar)
		} else {
			_ = os.Setenv(sessionEnvVar, prevRecordEnv)
		}
		if err != nil {
			return err
		}
		sessionRec = recorder
		stdoutSink = sessionRec.WrapWriter(eventStdout, stdoutSink)
		stderrSink = sessionRec.WrapWriter(eventStderr, stderrSink)
	}

	reader := io.Reader(stdinBuf)
	if sessionRec != nil {
		reader = sessionRec.WrapReader(eventStdin, reader)
	}

	term = newTerminal(reader, stdoutSink, stderrSink)

	runCtx := ctx
	if sessionRec != nil {
		runCtx = sessionRec.InjectContext(runCtx)
	}

	rootCmd := cmd.Root()
	if rootCmd == nil {
		return errors.New("session update requires root command context")
	}
	root := cloneCommand(rootCmd, 0)
	runErr := root.Run(runCtx, replay.args)
	failed := runErr != nil
	if failed {
		term.Errorf("[loop] %v\n", runErr)
	}

	if replay.failed != nil && failed != *replay.failed {
		msg := fmt.Sprintf("failure flag changed from %t to %t", *replay.failed, failed)
		if stdoutOnly {
			term.Println(msg)
		} else {
			return errors.New(msg)
		}
	}

	if sessionRec != nil {
		if err := sessionRec.Finalize(failed); err != nil {
			return fmt.Errorf("finalize session: %w", err)
		}
		prevTerm.Printf("updated session written to %s\n", destPath)
	}

	return runErr
}
