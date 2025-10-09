//go:build !noreplayembed

package main

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed testdata/sessions
var embeddedSessions embed.FS

var sessionsFS = func() fs.FS {
	sub, err := fs.Sub(embeddedSessions, "testdata/sessions")
	if err != nil {
		panic(fmt.Sprintf("fs.Sub failed: %v", err))
	}

	return sub
}()
