//go:build !unix

package main

import "os/exec"

// The protected publisher runs on Ubuntu. Preserve local non-Unix CLI builds;
// targeted execution refuses this platform rather than claiming tree cleanup.
func prepareGateCommand(*exec.Cmd) {}
