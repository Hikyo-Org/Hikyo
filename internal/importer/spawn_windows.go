//go:build windows

package importer

import (
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processTree owns a helper and every descendant it spawns through a job
// object with KILL_ON_JOB_CLOSE: terminating or closing the job ends the whole
// tree, so a helper's grandchildren cannot outlive a failed import.
type processTree struct {
	cmd *exec.Cmd
	mu  sync.Mutex
	job windows.Handle
}

// prepareProcessTree must run before Start. It routes exec's context
// cancellation through kill; the job itself is created by adopt because a
// process can only be assigned once it exists.
func prepareProcessTree(cmd *exec.Cmd) *processTree {
	tree := &processTree{cmd: cmd}
	cmd.Cancel = tree.kill
	return tree
}

// adopt runs right after Start and places the child in a kill-on-close job.
// ponytail: the child runs unassigned between Start and adopt; a descendant
// spawned in that window escapes the job. Closing it needs CREATE_SUSPENDED
// plus ResumeThread, which os/exec does not expose.
func (t *processTree) adopt() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(t.cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	defer func() { _ = windows.CloseHandle(process) }()
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	t.mu.Lock()
	t.job = job
	t.mu.Unlock()
	return nil
}

// kill terminates the job, or the direct child when the job does not exist
// yet (cancellation between Start and adopt).
func (t *processTree) kill() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job == 0 {
		return t.cmd.Process.Kill()
	}
	return windows.TerminateJobObject(t.job, 1)
}

// release closes the job handle, which also terminates any survivor.
func (t *processTree) release() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}
