package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestTransitRejectsUnusedAADFlag(t *testing.T) {
	for _, verb := range []string{"sign", "verify", "hmac", "hmac-verify"} {
		t.Run(verb, func(t *testing.T) {
			var out bytes.Buffer
			err := runTransitData(t.Context(), IO{Stdout: &out, Stderr: &out, Stdin: strings.NewReader("message")}, verb, []string{"key", "--stdin", "--aad-file", "must-not-be-read"})
			var cliErr *Error
			if !errors.As(err, &cliErr) || cliErr.Code != ExitUsage || !strings.Contains(err.Error(), "aad-file") {
				t.Fatalf("got %v, want unsupported aad-file usage error", err)
			}
		})
	}
}
