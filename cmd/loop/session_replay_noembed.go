package main

import (
	"io/fs"
	"os"
	"path"
	"runtime"
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
