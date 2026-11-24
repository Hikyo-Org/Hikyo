//go:build !darwin

// The desktop shell is macOS only (ADR D13). The pure logic in
// internal/shell builds and tests everywhere; this stub keeps `go build ./...`
// and `go vet ./...` working on other hosts.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "Hikyo Desktop runs on macOS only.")
	os.Exit(1)
}
