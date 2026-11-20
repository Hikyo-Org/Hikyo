//go:build !darwin

package shell

// setQuarantine is a no-op off macOS: the shell ships on macOS only, and the
// pure Go logic is tested elsewhere.
func setQuarantine(string) error { return nil }

// Quarantine is a no-op off macOS.
func Quarantine(string) error { return nil }
