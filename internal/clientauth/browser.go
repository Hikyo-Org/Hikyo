package clientauth

import (
	"context"
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
	// Background: the launcher is released after Start and must outlive the caller.
	switch platform {
	case "darwin":
		return exec.CommandContext(context.Background(), "open", target), nil //nolint:gosec // target is validated above as an absolute http or https URL without credentials
	case "windows":
		return exec.CommandContext(context.Background(), "rundll32", "url.dll,FileProtocolHandler", target), nil //nolint:gosec // target is validated above as an absolute http or https URL without credentials
	default:
		return exec.CommandContext(context.Background(), "xdg-open", target), nil //nolint:gosec // target is validated above as an absolute http or https URL without credentials
	}
}
