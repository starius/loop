//go:build noreplayembed

package main

import (
	"io/fs"
	"os"
)

var sessionsFS fs.FS = os.DirFS("cmd/loop")

const sessionsRootDir = "testdata/sessions"
