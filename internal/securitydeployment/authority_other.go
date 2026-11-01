//go:build !linux && !darwin

package securitydeployment

import "os/exec"

func installedAuthority(string, bool, bool) error { return ErrUnsupported }

func configureCommand(*exec.Cmd) {}
