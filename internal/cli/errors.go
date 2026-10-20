package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rossedman/muscles/internal/source"
)

// Exit codes. They are part of the interface: never renumber them.
const (
	ExitOK          = 0
	ExitFailure     = 1   // a runtime error
	ExitUsage       = 2   // the command line or config file is wrong
	ExitAuth        = 3   // registry authentication failed
	ExitNotFound    = 4   // the image doesn't exist
	ExitCheckFailed = 5   // images check: a budget or rule failed
	ExitInterrupted = 130 // Ctrl-C
)

const exitCodesHelp = "Exit codes: 0 ok, 1 error, 2 usage, 3 auth, 4 not found, 5 check failed, 130 interrupted."

// exitError carries an exit code, a stable machine code and the command
// that would fix it.
type exitError struct {
	code   int
	kind   string // AUTH_REQUIRED, NOT_FOUND, ...
	err    error
	run    string // a command that fixes it, if any
	cmd    *cobra.Command
	silent bool // the command already printed its outcome
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func (e *exitError) withCmd(cmd *cobra.Command) *exitError {
	e.cmd = cmd
	return e
}

func usageErrorf(format string, args ...any) *exitError {
	return &exitError{code: ExitUsage, kind: "USAGE", err: fmt.Errorf(format, args...)}
}

// imageError classifies a failure to open an image.
func imageError(ref string, err error) error {
	switch {
	case errors.Is(err, source.ErrAuth):
		return &exitError{code: ExitAuth, kind: "AUTH_REQUIRED", err: err, run: "docker login " + registryOf(ref)}
	case errors.Is(err, source.ErrNotFound):
		return &exitError{code: ExitNotFound, kind: "NOT_FOUND", err: err}
	case strings.Contains(err.Error(), "invalid image reference"):
		return &exitError{code: ExitUsage, kind: "USAGE", err: err}
	}
	return err
}

// registryOf guesses the registry host of a reference, for `docker login`.
func registryOf(ref string) string {
	ref = strings.TrimPrefix(ref, "registry://")
	if host, _, ok := strings.Cut(ref, "/"); ok && (strings.ContainsAny(host, ".:") || host == "localhost") {
		return host
	}
	return "docker.io"
}

// classify maps any error to its exit code and machine code.
func classify(err error) *exitError {
	var ee *exitError
	switch {
	case errors.As(err, &ee):
		return ee
	case errors.Is(err, context.Canceled):
		return &exitError{code: ExitInterrupted, kind: "INTERRUPTED", err: errors.New("interrupted")}
	case errors.Is(err, context.DeadlineExceeded):
		return &exitError{code: ExitFailure, kind: "TIMEOUT", err: errors.New("timed out (raise --timeout, or set it to 0 for no limit)")}
	}
	return &exitError{code: ExitFailure, kind: "FAILED", err: err}
}

// printError writes err on stderr: one sentence first, then what to run.
// Under --json it is an object instead.
func (a *app) printError(err error) int {
	a.status.clear()
	e := classify(err)
	if e.silent {
		return e.code
	}
	if a.jsonOutput() {
		body := map[string]any{"code": e.kind, "message": e.Error()}
		if e.run != "" {
			body["run"] = e.run
		}
		enc := jsonEncoder(a.opts.Stderr)
		_ = enc.Encode(map[string]any{"error": body})
		return e.code
	}
	fmt.Fprintf(a.errw, "%s %s\n", a.eth.Paint(a.eth.Err, "Error:"), e.Error())
	if e.run != "" {
		fmt.Fprintf(a.errw, "run: %s\n", e.run)
	}
	if e.code == ExitUsage && e.cmd != nil {
		fmt.Fprintf(a.errw, "See `%s --help` for usage.\n", e.cmd.CommandPath())
	}
	return e.code
}
