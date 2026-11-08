package securitydeployment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"

	"github.com/Hikyo-Org/hikyo/internal/securitystatus"
)

type Command struct {
	Path  string
	Args  []string
	Input []byte
	// Env adds variables to the coordinator's own environment; nil inherits it unchanged.
	Env []string
}

type Runner interface {
	Run(context.Context, Command) ([]byte, error)
}

// CommandRunner runs installed tools. Secrets lists exact values to remove from
// diagnostics, read before a command runs and again when it fails, so a value
// rotated while it ran is covered either way. An error from either read withholds
// the diagnostics, because they could then carry a value.
type CommandRunner struct {
	Secrets func() ([]string, error)
}

func (r CommandRunner) diagnostics(raw []byte, before []string, beforeErr error) string {
	if r.Secrets == nil {
		return stderrTail(raw, nil)
	}
	after, err := r.Secrets()
	if err != nil || beforeErr != nil {
		return ": diagnostics withheld, the installation's protected values could not be read"
	}
	return stderrTail(raw, append(before, after...))
}

type boundedOutput struct {
	buffer   bytes.Buffer
	cancel   context.CancelFunc
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if len(p) > maxRecordBytes-b.buffer.Len() {
		b.exceeded = true
		b.cancel()
		return 0, errors.New("command output exceeds limit")
	}
	return b.buffer.Write(p)
}

const stderrTailBytes = 4096

func printableText(raw string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || unicode.IsPrint(r) {
			return r
		}
		return -1
	}, strings.ToValidUTF8(raw, ""))
}

// stderrTail returns the final complete lines as an error suffix. Scrubbing covers
// the whole output before the cut, so a value crossing the boundary cannot survive.
// Exact values run on both sides of the patterns. A short plain value could be part
// of a label a pattern needs, so it waits. A long value, or one with characters the
// patterns would split on, goes first, or a pattern would leave part of it behind.
func stderrTail(raw []byte, secrets []string) string {
	text := printableText(string(raw))
	values := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		value := printableText(secret)
		if value == "" {
			// Replacing "" would put a marker between every rune.
			continue
		}
		values = append(values, value)
		if len(value) >= 8 || strings.ContainsAny(value, " \"'=:/") {
			text = strings.ReplaceAll(text, value, "[redacted]")
		}
	}
	text = securitystatus.Redact(text)
	for _, value := range values {
		text = strings.ReplaceAll(text, value, "[redacted]")
	}
	if len(text) > stderrTailBytes {
		// A partial first line is dropped: it starts mid-sentence, and a scrubbed
		// marker could be split. No complete line means nothing readable remains.
		_, rest, found := strings.Cut(text[len(text)-stderrTailBytes:], "\n")
		if !found {
			return ""
		}
		text = rest
	}
	if text = strings.TrimSpace(text); text == "" {
		return ""
	}
	return ": " + text
}

func (r CommandRunner) Run(ctx context.Context, command Command) ([]byte, error) {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(child, command.Path, command.Args...)
	configureCommand(cmd)
	if command.Env != nil {
		cmd.Env = append(os.Environ(), command.Env...)
	}
	if command.Input != nil {
		cmd.Stdin = bytes.NewReader(command.Input)
	}
	out, stderr := &boundedOutput{cancel: cancel}, &boundedOutput{cancel: cancel}
	cmd.Stdout, cmd.Stderr = out, stderr
	var before []string
	var beforeErr error
	if r.Secrets != nil {
		before, beforeErr = r.Secrets()
	}
	err := cmd.Run()
	if out.exceeded || stderr.exceeded {
		return nil, errors.New("security deployment: command output exceeds limit")
	}
	if err != nil {
		// Stdout carries structured workload data and is never returned. Stderr
		// reaches the journal, so only its scrubbed final lines are.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w%s", ctx.Err(), r.diagnostics(stderr.buffer.Bytes(), before, beforeErr))
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("security deployment: installed command exited with status %d%s", exit.ExitCode(), r.diagnostics(stderr.buffer.Bytes(), before, beforeErr))
		}
		if cmd.Process == nil {
			return nil, errors.New("security deployment: installed command could not start")
		}
		// It ran, so it may have had an effect (exec.ErrWaitDelay after a held pipe, for one).
		return nil, fmt.Errorf("security deployment: installed command ended abnormally: %w%s", err, r.diagnostics(stderr.buffer.Bytes(), before, beforeErr))
	}
	return append([]byte(nil), out.buffer.Bytes()...), nil
}
