package securitydeployment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
)

type Command struct {
	Path  string
	Args  []string
	Input []byte
}

type Runner interface {
	Run(context.Context, Command) ([]byte, error)
}

type CommandRunner struct{}

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

func (CommandRunner) Run(ctx context.Context, command Command) ([]byte, error) {
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(child, command.Path, command.Args...)
	configureCommand(cmd)
	if command.Input != nil {
		cmd.Stdin = bytes.NewReader(command.Input)
	}
	out, stderr := &boundedOutput{cancel: cancel}, &boundedOutput{cancel: cancel}
	cmd.Stdout, cmd.Stderr = out, stderr
	err := cmd.Run()
	if out.exceeded || stderr.exceeded {
		return nil, errors.New("security deployment: command output exceeds limit")
	}
	if err != nil {
		// Tool output can contain environment values or credentials. Return only
		// the executable's exit/cancellation status, never stdout/stderr.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("security deployment: installed command exited with status %d", exit.ExitCode())
		}
		return nil, errors.New("security deployment: installed command could not start")
	}
	return append([]byte(nil), out.buffer.Bytes()...), nil
}
