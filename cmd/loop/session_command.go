package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

var sessionCommand = &cli.Command{
	Name:  "session",
	Usage: "work with recorded CLI sessions",
	Commands: []*cli.Command{
		sessionPlayCommand,
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

func playSession(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return showCommandHelp(ctx, cmd)
	}

	path := cmd.Args().Get(0)
	if !filepath.IsAbs(path) {
		path = filepath.Join(sessionDefaultDir, path)
	}

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
		term.Printf("[%s] exit code=%d\n", timestamp, payload.Code)
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
