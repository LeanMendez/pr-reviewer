// Package execx runs external processes behind an injectable function type.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// waitDelay bounds how long Wait lingers on I/O after the process is killed.
const waitDelay = 5 * time.Second

// Cmd describes a process to run. Nil Stdin/Stdout/Stderr are discarded.
type Cmd struct {
	Name   string
	Args   []string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Exec runs a Cmd to completion.
type Exec func(ctx context.Context, cmd Cmd) error

// ExitError is returned when a process exits with a non-zero code.
type ExitError struct {
	Code   int
	Stderr string
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d: %s", e.Code, e.Stderr)
}

// Real returns an Exec backed by os/exec. Stdout and stderr are streamed
// straight to the caller's writers; stderr is also captured for ExitError.
func Real() Exec {
	return func(ctx context.Context, c Cmd) error {
		cmd := exec.CommandContext(ctx, c.Name, c.Args...)
		cmd.Dir = c.Dir
		// A killed process whose grandchildren keep the pipes open must not hang Wait.
		cmd.WaitDelay = waitDelay
		cmd.Stdin = c.Stdin
		cmd.Stdout = c.Stdout
		var captured bytes.Buffer
		if c.Stderr != nil {
			cmd.Stderr = io.MultiWriter(c.Stderr, &captured)
		} else {
			cmd.Stderr = &captured
		}
		err := cmd.Run()
		if err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s: %w", c.Name, ctxErr)
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return &ExitError{Code: ee.ExitCode(), Stderr: captured.String()}
		}
		return fmt.Errorf("%s: %w", c.Name, err)
	}
}

// Fake returns an Exec for tests: it calls fn, writes its stdout/stderr to
// the Cmd writers and returns an *ExitError when the code is non-zero.
func Fake(fn func(Cmd) (stdout string, stderr string, code int)) Exec {
	return func(ctx context.Context, c Cmd) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		stdout, stderr, code := fn(c)
		if c.Stdout != nil {
			if _, err := io.WriteString(c.Stdout, stdout); err != nil {
				return err
			}
		}
		if c.Stderr != nil {
			if _, err := io.WriteString(c.Stderr, stderr); err != nil {
				return err
			}
		}
		if code != 0 {
			return &ExitError{Code: code, Stderr: stderr}
		}
		return nil
	}
}
