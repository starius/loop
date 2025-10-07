package main

import (
	"fmt"
	"io"
)

type terminal struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func newTerminal(stdin io.Reader, stdout, stderr io.Writer) *terminal {
	return &terminal{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
}

func (t *terminal) Reader() io.Reader {
	return t.stdin
}

func (t *terminal) Writer() io.Writer {
	return t.stdout
}

func (t *terminal) ErrorWriter() io.Writer {
	return t.stderr
}

func (t *terminal) Print(args ...interface{}) {
	fmt.Fprint(t.stdout, args...)
}

func (t *terminal) Println(args ...interface{}) {
	fmt.Fprintln(t.stdout, args...)
}

func (t *terminal) Printf(format string, args ...interface{}) {
	fmt.Fprintf(t.stdout, format, args...)
}

func (t *terminal) Error(args ...interface{}) {
	fmt.Fprint(t.stderr, args...)
}

func (t *terminal) Errorln(args ...interface{}) {
	fmt.Fprintln(t.stderr, args...)
}

func (t *terminal) Errorf(format string, args ...interface{}) {
	fmt.Fprintf(t.stderr, format, args...)
}

func (t *terminal) Scanln(args ...interface{}) error {
	_, err := fmt.Fscanln(t.stdin, args...)
	return err
}

func (t *terminal) Write(data []byte) (int, error) {
	return t.stdout.Write(data)
}
