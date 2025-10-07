package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/urfave/cli/v3"
)

var sessionCommand = &cli.Command{
	Name:   "session",
	Usage:  "work with recorded CLI sessions",
	Hidden: true,
	Commands: []*cli.Command{
		sessionUpdateCommand,
	},
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
			destAbs, err := filepath.Abs(destPath)
			if err != nil {
				return err
			}
			destPath = destAbs
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
	var stdinUnhook func() error

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
		destPath = destAbs
	}

	if sessionRec != nil {
		if err := sessionRec.Start(stdinBuf, stdoutSink, stderrSink); err != nil {
			return err
		}
		stdoutSink = os.Stdout
		stderrSink = os.Stderr
	} else {
		var err error
		stdinUnhook, err = hookStdin(os.Stdin, stdinBuf, nil)
		if err != nil {
			return err
		}
		defer func() {
			if stdinUnhook != nil {
				_ = stdinUnhook()
			}
		}()
	}

	termStdout := stdoutSink
	termStderr := stderrSink
	if sessionRec != nil {
		termStdout = os.Stdout
		termStderr = os.Stderr
	}
	term = newTerminal(os.Stdin, termStdout, termStderr)

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
	if runErr != nil {
		term.Errorf("[loop] %v\n", runErr)
	}

	var runErrMsg *string
	if runErr != nil {
		msg := runErr.Error()
		runErrMsg = &msg
	}

	switch {
	case replay.runError == nil && runErrMsg != nil:
		msg := fmt.Sprintf("run error changed from <nil> to %q", *runErrMsg)
		if stdoutOnly {
			term.Println(msg)
		} else {
			return errors.New(msg)
		}
	case replay.runError != nil && runErrMsg == nil:
		msg := fmt.Sprintf("run error changed from %q to <nil>", *replay.runError)
		if stdoutOnly {
			term.Println(msg)
		} else {
			return errors.New(msg)
		}
	case replay.runError != nil && runErrMsg != nil && *replay.runError != *runErrMsg:
		msg := fmt.Sprintf("run error changed from %q to %q", *replay.runError, *runErrMsg)
		if stdoutOnly {
			term.Println(msg)
		} else {
			return errors.New(msg)
		}
	}

	if sessionRec != nil {
		if err := sessionRec.Finalize(runErr); err != nil {
			return fmt.Errorf("finalize session: %w", err)
		}
		prevTerm.Printf("updated session written to %s\n", destPath)
	}

	return runErr
}
