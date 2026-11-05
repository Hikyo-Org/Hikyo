//go:build !ios && !android

package main

import "os/exec"

func openURL(u string) error {
	cmd := exec.Command("open", u)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
