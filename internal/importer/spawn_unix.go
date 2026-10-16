//go:build unix

package importer

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// processTree owns the helper's process group: cancellation and failed
// invocations kill descendants that remain in that group. This is lifecycle
// cleanup, not a sandbox for helpers that deliberately leave the group.
type processTree struct{ cmd *exec.Cmd }

// prepareProcessTree must run before Start. It places the child in a fresh
// process group and routes exec's context cancellation through kill.
func prepareProcessTree(cmd *exec.Cmd) *processTree {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	tree := &processTree{cmd: cmd}
	cmd.Cancel = tree.kill
	return tree
}

// adopt runs after Start; group membership is established by Setpgid at fork,
// so there is nothing left to claim on unix.
func (*processTree) adopt() error { return nil }

// kill terminates the whole process group.
func (t *processTree) kill() error {
	return syscall.Kill(-t.cmd.Process.Pid, syscall.SIGKILL)
}

// Successful Unix helpers may intentionally leave background agents running.
// Failure paths must call kill explicitly before releasing ownership.
func (*processTree) release() {}

func writeSubprocessOutput(stdout io.Writer, output []byte) (int, error) {
	// os.Stdout.Write otherwise exits the runner on a broken pipe before it
	// can clean up descendants. Notify makes that write return EPIPE instead.
	// Scope the registration to delivery, after the helper has already exited,
	// and Stop only our channel to preserve any other signal subscriptions.
	brokenPipe := make(chan os.Signal, 1)
	signal.Notify(brokenPipe, syscall.SIGPIPE)
	defer signal.Stop(brokenPipe)
	return stdout.Write(output)
}
