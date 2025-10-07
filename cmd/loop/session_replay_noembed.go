//go:build noreplayembed

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"runtime"
)

var sessionsFS = func() fs.FS {
	// Find the location of cmd/loop source dir.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		panic("No caller information")
	}
	loopDir := path.Dir(filename)

	cmdLoopFS := os.DirFS(loopDir)
	sub, err := fs.Sub(cmdLoopFS, "testdata/sessions")
	if err != nil {
		panic(fmt.Sprintf("fs.Sub failed: %v", err))
	}

	return sub
}()
