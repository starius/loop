//go:build !noreplayembed

package main

import (
	"embed"
	"io/fs"
)

//go:embed testdata/sessions
var embeddedSessions embed.FS

var sessionsFS fs.FS = embeddedSessions

const sessionsRootDir = "testdata/sessions"
