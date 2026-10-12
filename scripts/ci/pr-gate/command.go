package main

import (
	"context"
	"os/exec"
	"time"
)

// Cancellation must release inherited output pipes and the publication reserve,
// not just kill a shell while its Git/gh descendants continue running.
func gateCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	prepareGateCommand(cmd)
	return cmd
}
