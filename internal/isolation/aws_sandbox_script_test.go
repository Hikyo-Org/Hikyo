package isolation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The owner runs docs/testing/aws-sandbox/smoke-sandbox.sh as the account root
// user, so its refusals must hold before any AWS call. `aws` is a stub that
// records its argv and fails, so no case can reach a provider.
const awsStubScript = `#!/bin/sh
printf '%s\n' "$*" >> "$STUB_LOG"
if [ -n "$STUB_PROVISION" ]; then
	case "$*" in
	*"sts get-caller-identity"*) echo "arn:aws:iam::123456789012:root"; exit 0 ;;
	*"kms list-aliases"*) echo None; exit 0 ;;
	esac
fi
exit 1
`

func runSandboxScript(t *testing.T, stubProvision bool, args ...string) (stderr string, calls []string, err error) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if mkErr := os.Mkdir(bin, 0o755); mkErr != nil {
		t.Fatal(mkErr)
	}
	if wErr := os.WriteFile(filepath.Join(bin, "aws"), []byte(awsStubScript), 0o755); wErr != nil {
		t.Fatal(wErr)
	}
	script, absErr := filepath.Abs("../../docs/testing/aws-sandbox/smoke-sandbox.sh")
	if absErr != nil {
		t.Fatal(absErr)
	}
	log := filepath.Join(dir, "calls.log")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + dir, "STUB_LOG=" + log}
	if stubProvision {
		cmd.Env = append(cmd.Env, "STUB_PROVISION=1")
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("script timed out: %v\n%s", ctx.Err(), errBuf.String())
	}
	if data, readErr := os.ReadFile(log); readErr == nil {
		calls = strings.Split(strings.TrimSpace(string(data)), "\n")
	} else if !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal(readErr)
	}
	return errBuf.String(), calls, err
}

func requireSandboxTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
}

func TestAWSSandboxScriptRefusals(t *testing.T) {
	requireSandboxTools(t)
	refused := []struct {
		name string
		args []string
		want string
	}{
		{"no subcommand", nil, "usage:"},
		{"unknown subcommand", []string{"bogus"}, "unknown subcommand"},
		{"teardown without RUN", []string{"teardown"}, "teardown needs RUN"},
		{"provision RUN not hex", []string{"provision", "nothexnothe"}, "RUN must be 10 lowercase hex digits"},
		{"provision RUN uppercase", []string{"provision", "ABCDEF0123"}, "RUN must be 10 lowercase hex digits"},
		{"provision RUN too short", []string{"provision", "abc"}, "RUN must be 10 lowercase hex digits"},
		{"teardown RUN with suffix", []string{"teardown", "0123456789r1"}, "RUN must be 10 lowercase hex digits"},
		{"ca-bundle without endpoint", []string{"--ca-bundle", "ca.pem", "check"}, "--ca-bundle needs --endpoint-url"},
	}
	for _, endpoint := range []string{
		"https://secretsmanager.eu-central-1.amazonaws.com",
		"https://a:b@secretsmanager.eu-central-1.amazonaws.com",
		"HTTPS://X.AMAZONAWS.COM",
		"https://sts.cn-north-1.amazonaws.com.cn",
		"https://example.com",
		"https://8.8.8.8",
		"https://172.32.0.1",
		"https://172.15.0.1",
		"https://127.0.0.1@example.com",
		"https://example.com/@127.0.0.1",
		"https://example.com#@127.0.0.1",
		"https://127.0.0.1.example.com",
		"https://localhost.example.com",
		"https://010.0.0.1",
		"https://256.0.0.1",
		"https://[::2]:4566",
		"127.0.0.1:4566",
		"ftp://127.0.0.1",
		"https://127.0.0.1\\@example.com",
	} {
		refused = append(refused, struct {
			name string
			args []string
			want string
		}{"endpoint " + endpoint, []string{"--endpoint-url", endpoint, "check"}, "--endpoint-url must be an emulator"})
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			stderr, calls, err := runSandboxScript(t, false, tc.args...)
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				t.Fatalf("want exit 1, got %v\n%s", err, stderr)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Fatalf("stderr lacks %q:\n%s", tc.want, stderr)
			}
			if len(calls) != 0 {
				t.Fatalf("aws was called before the refusal: %q", calls)
			}
		})
	}
}

func TestAWSSandboxScriptAcceptsEmulatorEndpoints(t *testing.T) {
	requireSandboxTools(t)
	for _, endpoint := range []string{
		"https://127.0.0.1:4566",
		"https://localhost:4566",
		"https://10.1.2.3",
		"https://192.168.1.2:4566",
		"https://172.16.0.1",
		"https://172.31.255.255",
		"https://[::1]:4566",
		"https://user:pw@127.0.0.1:5444/path",
	} {
		t.Run(endpoint, func(t *testing.T) {
			stderr, calls, err := runSandboxScript(t, false, "--endpoint-url", endpoint, "check")
			if err == nil {
				t.Fatalf("the failing stub must abort check (a failed listing must not read as empty):\n%s", stderr)
			}
			if !strings.Contains(stderr, "DRY RUN") || len(calls) == 0 {
				t.Fatalf("guard did not pass to the stub; calls=%q\n%s", calls, stderr)
			}
			if !strings.Contains(calls[0], "--endpoint-url "+endpoint) {
				t.Fatalf("first call lacks the endpoint: %q", calls[0])
			}
		})
	}
}

// Provision announces RUN and the exact teardown command before its first
// create, and again when it fails midway.
func TestAWSSandboxScriptProvisionPrintsRecovery(t *testing.T) {
	requireSandboxTools(t)
	stderr, calls, err := runSandboxScript(t, true, "--endpoint-url", "https://127.0.0.1:4566", "provision", "0123456789")
	if err == nil {
		t.Fatalf("create-key must fail on the stub:\n%s", stderr)
	}
	if got := strings.Count(stderr, " teardown 0123456789"); got < 2 {
		t.Fatalf("want the teardown command before the first create and after the failure, got %d:\n%s", got, stderr)
	}
	if !strings.Contains(stderr, "--endpoint-url https://127.0.0.1:4566 teardown 0123456789") {
		t.Fatalf("teardown command must keep the emulator flags:\n%s", stderr)
	}
	if last := calls[len(calls)-1]; !strings.Contains(last, "kms create-key") {
		t.Fatalf("want create-key as the failing call, got %q", last)
	}
}
