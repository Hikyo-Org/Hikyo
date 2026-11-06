//go:build windows

package app

import "errors"

func chownSQLiteFiles(string, int) error {
	return errors.New("native security rollback requires a Linux systemd host")
}
