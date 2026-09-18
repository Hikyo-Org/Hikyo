//go:build unix

package importer

import (
	"os/exec"
	"syscall"
)

// processTree owns a helper and every descendant it spawns: the child starts
// in its own process group, so cancellation signals the group and a helper's
// grandchildren cannot outlive a failed import.
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

func (*processTree) release() {}
