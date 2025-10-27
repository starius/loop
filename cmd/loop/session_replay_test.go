package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
			require.NoError(t, err)

			var stdoutBuf bytes.Buffer
			var stderrBuf bytes.Buffer

			stdoutUnhook, err := hookStdout(os.Stdout, nil, func(p []byte) {
				stdoutBuf.Write(p)
			})
			require.NoError(t, err)
			defer func() {
				require.NoError(t, stdoutUnhook())
			}()

			stderrUnhook, err := hookStderr(os.Stderr, nil, func(p []byte) {
				stderrBuf.Write(p)
			})
			require.NoError(t, err)
			defer func() {
				require.NoError(t, stderrUnhook())
			}()

			stdinUnhook, err := hookStdin(os.Stdin, bytes.NewBufferString(replay.stdin), nil)
			require.NoError(t, err)
			defer func() {
				require.NoError(t, stdinUnhook())
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
				require.Equal(t, *replay.runError, err.Error(), "run error mismatch")
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, replay.stdout, stdoutBuf.String(), "stdout mismatch")

			require.Equal(t, replay.stderr, stderrBuf.String(), "stderr mismatch")
		})
	}
}
