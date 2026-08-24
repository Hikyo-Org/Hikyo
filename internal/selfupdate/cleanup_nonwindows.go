//go:build !windows

package selfupdate

// CleanupPrevious is a no-op where replacement is one atomic rename.
func CleanupPrevious() error { return nil }
