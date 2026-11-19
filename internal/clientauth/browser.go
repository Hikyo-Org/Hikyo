package clientauth

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// OpenBrowser launches the platform browser for the front channel.
func OpenBrowser(target string) error {
	command, err := browserCommand(target, runtime.GOOS)
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("starting the browser: %w", err)
	}
	_ = command.Process.Release()
	return nil
}

// Validate before invoking an OS URL handler, which may also open files or
// dispatch custom schemes to other applications. Do not echo handoff tokens.
func browserCommand(target, platform string) (*exec.Cmd, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, fmt.Errorf("browser URL must be an absolute http or https URL without credentials")
	}
	switch platform {
	case "darwin":
		return exec.Command("open", target), nil
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target), nil
	default:
		return exec.Command("xdg-open", target), nil
	}
}
