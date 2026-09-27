package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/cli"
)

var plantedPAT = "ghp_" + strings.Repeat("A0b1C2d3E4", 3) + "ZZZZZZ"

func scanIO(t *testing.T, files map[string]string) (cli.IO, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	ios, stdout, stderr := testIO(t, nil)
	for rel, content := range files {
		p := filepath.Join(ios.Workdir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ios, stdout, stderr
}

// TestScanExitContract pins the verb's documented codes: findings exit 1 as a
// verdict (no `hikyo:` diagnostic), a clean or fully suppressed tree exits 0,
// and a refused scan exits 4, never 0.
func TestScanExitContract(t *testing.T) {
	t.Run("findings", func(t *testing.T) {
		ios, stdout, stderr := scanIO(t, map[string]string{"app.env": "TOKEN=" + plantedPAT + "\n"})
		if code := cli.Run(t.Context(), ios, []string{"scan"}); code != 1 {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
		if strings.Contains(stderr.String(), "hikyo:") {
			t.Fatalf("a findings verdict printed an error diagnostic: %q", stderr)
		}
		if !strings.Contains(stdout.String(), "app.env:1") || !strings.Contains(stdout.String(), "github-pat") {
			t.Fatalf("table = %q", stdout)
		}
		if !strings.Contains(stderr.String(), "1 finding(s)") {
			t.Fatalf("summary = %q", stderr)
		}
	})
	t.Run("inline suppressed is clean", func(t *testing.T) {
		ios, stdout, _ := scanIO(t, map[string]string{"app.env": plantedPAT + " # hikyo-scan:ignore github-pat\n"})
		if code := cli.Run(t.Context(), ios, []string{"scan"}); code != 0 {
			t.Fatalf("exit %d", code)
		}
		if stdout.Len() != 0 {
			t.Fatalf("a clean table scan printed %q", stdout)
		}
	})
	t.Run("excluded rule is clean", func(t *testing.T) {
		ios, _, _ := scanIO(t, map[string]string{"app.env": plantedPAT + "\n"})
		if code := cli.Run(t.Context(), ios, []string{"scan", "--exclude-rule", "github-pat"}); code != 0 {
			t.Fatalf("exit %d", code)
		}
	})
	t.Run("runtime budget refuses", func(t *testing.T) {
		ios, _, stderr := scanIO(t, map[string]string{"app.env": plantedPAT + "\n"})
		if code := cli.Run(t.Context(), ios, []string{"scan", "--timeout", "1ns"}); code != cli.ExitRefused {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
	})
	t.Run("git unavailable", func(t *testing.T) {
		ios, _, stderr := scanIO(t, nil)
		t.Setenv("PATH", t.TempDir())
		if code := cli.Run(t.Context(), ios, []string{"scan", "--staged"}); code != cli.ExitUnavailable {
			t.Fatalf("exit %d, stderr %q", code, stderr)
		}
	})
}

// TestScanOutputsAreRedacted is the SS4 canary discipline applied to the
// local verb: no output format carries the planted credential.
func TestScanOutputsAreRedacted(t *testing.T) {
	for _, format := range []string{"table", "json", "sarif"} {
		t.Run(format, func(t *testing.T) {
			ios, stdout, stderr := scanIO(t, map[string]string{"a/b.txt": "x\n" + plantedPAT + "\n"})
			if code := cli.Run(t.Context(), ios, []string{"scan", "-o", format}); code != 1 {
				t.Fatalf("exit %d, stderr %q", code, stderr)
			}
			for _, out := range []string{stdout.String(), stderr.String()} {
				if strings.Contains(out, plantedPAT) || strings.Contains(out, plantedPAT[4:16]) {
					t.Fatalf("%s output carries the planted credential", format)
				}
			}
			if !strings.Contains(stdout.String(), "a/b.txt") {
				t.Fatalf("%s output lacks the location: %q", format, stdout)
			}
		})
	}
}

func TestScanTableQuotesUntrustedNames(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("Windows file names cannot carry control characters")
	}
	ios, stdout, _ := scanIO(t, map[string]string{"x\nFORGED:1\tgithub-pat.txt": plantedPAT})
	cli.Run(t.Context(), ios, []string{"scan"})
	if strings.Contains(stdout.String(), "\nFORGED") {
		t.Fatalf("a file name forged a table row: %q", stdout)
	}
	if !strings.Contains(stdout.String(), `"x\nFORGED:1\tgithub-pat.txt":1`) {
		t.Fatalf("table = %q", stdout)
	}
}
