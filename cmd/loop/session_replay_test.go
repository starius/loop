package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

//go:embed testdata/sessions
var embeddedSessions embed.FS

func TestRecordedSessions(t *testing.T) {
	const sessionsRoot = "testdata/sessions"

	if _, err := fs.ReadDir(embeddedSessions, sessionsRoot); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			t.Skip("no recorded sessions present")
		}
		t.Fatalf("read sessions dir: %v", err)
	}

	var sessionFiles []string
	err := fs.WalkDir(embeddedSessions, sessionsRoot, func(path string, d fs.DirEntry, err error) error {
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
	if err != nil {
		t.Fatalf("walk sessions: %v", err)
	}
	if len(sessionFiles) == 0 {
		t.Skip("no recorded sessions present")
	}

	for _, path := range sessionFiles {
		path := path
		relName := strings.TrimPrefix(path, sessionsRoot+"/")
		if relName == "" {
			relName = filepath.Base(path)
		}

		t.Run(relName, func(t *testing.T) {
			replay, err := loadRecordedSessionFS(embeddedSessions, path)
			if err != nil {
				t.Fatalf("load session: %v", err)
			}

			stdinBuf := bytes.NewBufferString(replay.stdin)
			var stdoutBuf bytes.Buffer
			var stderrBuf bytes.Buffer

			prevTerm := term
			term = newTerminal(stdinBuf, &stdoutBuf, &stderrBuf)
			defer func() { term = prevTerm }()

			restoreDialer := withClientDialer(&replayDialer{conn: replay.conn})
			defer restoreDialer()

			restoreEnv := applyEnv(replay.env)
			defer restoreEnv()

			sessionRec = nil

			cmd := newRootCommand()

			err = cmd.Run(context.Background(), replay.args)
			exitCode := 0
			if err != nil {
				exitCode = 1
				term.Errorf("[loop] %v\n", err)
			}

			if replay.exitCode != nil && exitCode != *replay.exitCode {
				t.Fatalf("unexpected exit code %d, want %d", exitCode, *replay.exitCode)
			}

			if stdoutBuf.String() != replay.stdout {
				t.Fatalf("stdout mismatch\n--- got ---\n%s\n--- want ---\n%s", stdoutBuf.String(), replay.stdout)
			}

			if stderrBuf.String() != replay.stderr {
				t.Fatalf("stderr mismatch\n--- got ---\n%s\n--- want ---\n%s", stderrBuf.String(), replay.stderr)
			}
		})
	}
}
