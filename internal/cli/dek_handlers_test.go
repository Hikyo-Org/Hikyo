package cli

import (
	"context"
	"io"
	"testing"
)

// Call the handlers themselves: --help bypasses them and cannot detect a
// duplicate flag registration. Unknown flags must fail before state or HTTP.
func TestDEKHandlersRejectUnknownFlags(t *testing.T) {
	for name, handler := range map[string]func(context.Context, IO, []string) error{
		"rotate-dek": runRotateDEK,
		"reencrypt":  runReencrypt,
	} {
		t.Run(name, func(t *testing.T) {
			ios := IO{Stdout: io.Discard, Stderr: io.Discard, Env: Env{Getenv: func(string) string {
				t.Fatal("invalid syntax reached state resolution")
				return ""
			}}}
			if code := Report(io.Discard, handler(t.Context(), ios, []string{"--not-a-flag"})); code != ExitUsage {
				t.Fatalf("exit %d, want usage", code)
			}
		})
	}
}
