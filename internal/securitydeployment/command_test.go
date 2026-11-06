package securitydeployment

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCommandHelperProcess(t *testing.T) {
	index := slices.Index(os.Args, "--")
	if index < 0 {
		return
	}
	switch os.Args[index+1] {
	case "failure":
		fmt.Fprintln(os.Stdout, "CANARY_SECRET_STDOUT")
		fmt.Fprintln(os.Stderr, "stage refused: visible diagnostic")
		fmt.Fprintln(os.Stderr, `line 4: unterminated quoted value "CANARY_SECRET_QUOTED`)
		fmt.Fprintln(os.Stderr, `invalid value 'CANARY_SECRET_SINGLE' was refused`)
		fmt.Fprintln(os.Stderr, "dial postgres://hikyo:CANARY_SECRET_URL@db.example/hikyo failed")
		fmt.Fprintln(os.Stderr, "HIKYO_DB=CANARY_SECRET_ASSIGNED and password: CANARY_SECRET_LABELED")
		fmt.Fprintln(os.Stderr, "Authorization: Bearer CANARY_SECRET_BEARER")
		fmt.Fprintln(os.Stderr, "credential CANARYSECRETTOKEN0123456789abcdef0123456789 rejected")
		os.Exit(3)
	case "noisy":
		fmt.Fprint(os.Stderr, "CANARY_DISCARDED_HEAD\n"+strings.Repeat("noise\n", 4096)+"final \x1b[31mcause\n")
		os.Exit(4)
	case "large":
		fmt.Fprint(os.Stdout, strings.Repeat("x", maxRecordBytes+1024))
		os.Exit(0)
	case "success":
		fmt.Fprint(os.Stdout, `{"result":"ok"}`)
		os.Exit(0)
	}
	os.Exit(2)
}

func TestCommandRunnerBoundsOutputAndScrubsToolFailures(t *testing.T) {
	runner := CommandRunner{}
	for _, kind := range []string{"failure", "large", "success"} {
		t.Run(kind, func(t *testing.T) {
			out, err := runner.Run(t.Context(), Command{Path: os.Args[0], Args: []string{"-test.run=^TestCommandHelperProcess$", "--", kind}})
			if kind == "success" {
				if err != nil || string(out) != `{"result":"ok"}` {
					t.Fatalf("successful output changed: %s %v", out, err)
				}
				return
			}
			if err == nil || len(out) != 0 {
				t.Fatal("failure or oversized output admitted")
			}
			if strings.Contains(err.Error(), "CANARY") {
				t.Fatalf("tool failure exposed secret output: %v", err)
			}
			if kind == "failure" && (!strings.Contains(err.Error(), "status 3") || !strings.Contains(err.Error(), "stage refused: visible diagnostic")) {
				t.Fatalf("tool failure lost its diagnostic: %v", err)
			}
		})
	}
}

func TestCommandRunnerKeepsOnlyABoundedFinalStderrTail(t *testing.T) {
	_, err := CommandRunner{}.Run(t.Context(), Command{Path: os.Args[0], Args: []string{"-test.run=^TestCommandHelperProcess$", "--", "noisy"}})
	if err == nil || !strings.Contains(err.Error(), "final [31mcause") || strings.Contains(err.Error(), "\x1b") || strings.Contains(err.Error(), "CANARY") || len(err.Error()) > stderrTailBytes+128 {
		t.Fatalf("stderr tail unbounded or missing: %d %q", len(fmt.Sprint(err)), err)
	}
}

func TestStderrTailRemovesExactInstallationValues(t *testing.T) {
	// Short and unquoted, so only exact matching can catch it.
	tail := stderrTail([]byte("compose: invalid value hunter2 for service\n"), []string{"hunter2"})
	if strings.Contains(tail, "hunter2") || !strings.Contains(tail, "invalid value") {
		t.Fatalf("exact value survived or reason lost: %q", tail)
	}
	// An exact value colliding with a label must not defeat the label's pattern.
	if tail := stderrTail([]byte("refused token=abc123\n"), []string{"oken"}); strings.Contains(tail, "abc123") {
		t.Fatalf("exact value broke a pattern scrubber: %q", tail)
	}
	// Values the patterns would split, or whose printed form differs, must go whole.
	for _, secret := range []string{"horse battery staple", `pa"ss-tail`, "tab\tseparated", "a=b/c:d"} {
		raw := "HIKYO_ADMIN_PASSWORD=" + secret + " refused\n"
		if tail := stderrTail([]byte(raw), []string{secret}); strings.Contains(tail, "battery") || strings.Contains(tail, "tail") || strings.Contains(tail, "separated") || strings.Contains(tail, "b/c") {
			t.Fatalf("part of %q survived: %q", secret, tail)
		}
	}
	// A value in the part cut away must not leave a fragment in what remains.
	raw := append([]byte("first boundary-value\n"), bytes.Repeat([]byte("x\n"), stderrTailBytes/2-3)...)
	if tail := stderrTail(raw, []string{"boundary-value"}); strings.Contains(tail, "value") {
		t.Fatalf("value fragment survived the tail cut: %q", tail)
	}
}

func TestCommandDiagnosticsWithheldWithoutProtectedValues(t *testing.T) {
	runner := CommandRunner{Secrets: func() ([]string, error) { return nil, os.ErrPermission }}
	_, err := runner.Run(t.Context(), Command{Path: "/bin/sh", Args: []string{"-c", "echo short-secret >&2; exit 3"}})
	if err == nil || strings.Contains(err.Error(), "short-secret") || !strings.Contains(err.Error(), "withheld") {
		t.Fatalf("diagnostics not withheld: %v", err)
	}
}

// The added value wins over the coordinator's own, which is what the Compose image probe relies on.
func TestCommandEnvOverridesTheInheritedEnvironment(t *testing.T) {
	t.Setenv("HIKYO_SERVER_IMAGE", "inherited")
	out, err := CommandRunner{}.Run(t.Context(), Command{Path: "/bin/sh", Args: []string{"-c", `printf %s "$HIKYO_SERVER_IMAGE"`}, Env: []string{"HIKYO_SERVER_IMAGE=probe"}})
	if err != nil || string(out) != "probe" {
		t.Fatalf("added environment not applied: %q %v", out, err)
	}
}

func TestComposeEnvironmentValuesCoverDotenvForms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install.env")
	content := "# NOTE=comment\nexport A=plainvalue\nB=\"quoted value\"\nC=\nD=abc\nE=inline-value # note\nF: colon-value\nG=\"esc\\\"aped\"\nH=\"first\nsecond-line\"\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	values, err := composeEnvironmentValues(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"plainvalue", "quoted value", "inline-value", "colon-value", `esc"aped`, "first", "second-line"} {
		if !slices.Contains(values, want) {
			t.Fatalf("missing %q in %q", want, values)
		}
	}
	for _, skipped := range []string{"comment", "", "abc"} {
		if slices.Contains(values, skipped) {
			t.Fatalf("kept %q in %q", skipped, values)
		}
	}
	for i := 1; i < len(values); i++ {
		if len(values[i]) > len(values[i-1]) {
			t.Fatalf("not longest first: %q", values)
		}
	}
	if _, err := composeEnvironmentValues(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("absent file reported no error")
	}
}
