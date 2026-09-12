package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestFederationCAFlagsRefuseBeforeAuthentication(t *testing.T) {
	for _, args := range [][]string{
		{"add", "--issuer", "https://issuer.test", "--type", "kubernetes", "--refuse-audience", "api", "--jwks", "discovery", "--ca-bundle-file", "/absent-ca-file"},
		{"update", "--id", "fis_test", "--refuse-audience", "api", "--jwks", "discovery", "--ca-bundle-file", "/absent-ca-file", "--clear-ca-bundle"},
		{"update", "--id", "fis_test", "--refuse-audience", "api", "--jwks", "static", "--ca-bundle-file", "/absent-ca-file"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			err := runFederationIssuer(t.Context(), IO{Stderr: &out, Stdout: &out, Env: Env{Getenv: func(string) string { return "" }}}, args)
			if err == nil {
				t.Fatal("invalid CA flags accepted")
			}
			if Report(io.Discard, err) != ExitUsage {
				t.Fatalf("error = %v, want usage", err)
			}
		})
	}
}
