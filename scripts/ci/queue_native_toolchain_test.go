package ci_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Execute the actual workflow command and actual trusted helper. The shim models
// toolchain selection/download, including a wrong selected version and a hang.
func TestQueueNativeToolchainSetupSharesAbsoluteWorkCutoff(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-queue-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w queueWorkflowContract
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	commands := map[string]string{"direct": w.Jobs["publish"].Steps[len(w.Jobs["publish"].Steps)-2].Run}
	workerRaw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-queue-worker.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var worker queueWorkflowContract
	if err := yaml.Unmarshal(workerRaw, &worker); err != nil {
		t.Fatal(err)
	}
	commands["admission"] = worker.Jobs["publish"].Steps[3].Run
	commands["ambiguous"] = worker.Jobs["publish"].Steps[3].Run
	helper, err := os.ReadFile(filepath.Join(root, "scripts/ci/build-queue-publisher.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, module, bootstrap, selected string
		seconds                           int
		pass                              bool
		calls                             int
	}{
		{"exact selection", "go 1.27.2\n", "go1.21.0", "go1.27.2", 60, true, 3},
		{"matching toolchain", "go 1.27.2\ntoolchain go1.27.2\n", "go1.27.2", "go1.27.2", 60, true, 3},
		{"missing pin", "module example.org/m\n", "go1.27.2", "go1.27.2", 60, false, 0},
		{"duplicate pin", "go 1.27.2\ngo 1.27.2\n", "go1.27.2", "go1.27.2", 60, false, 0},
		{"malformed pin", "go 1.27\n", "go1.27.2", "go1.27.2", 60, false, 0},
		{"prerelease pin", "go 1.28rc1\n", "go1.27.2", "go1.27.2", 60, false, 0},
		{"contradictory toolchain", "go 1.27.2\ntoolchain go1.28.0\n", "go1.27.2", "go1.27.2", 60, false, 0},
		{"duplicate toolchain", "go 1.27.2\ntoolchain go1.27.2\ntoolchain go1.27.2\n", "go1.27.2", "go1.27.2", 60, false, 0},
		{"auto toolchain", "go 1.27.2\ntoolchain go1.27.2+auto\n", "go1.27.2", "go1.27.2", 60, false, 0},
		{"leading zero pin", "go 1.027.2\n", "go1.27.2", "go1.27.2", 60, false, 0},
		{"unsupported bootstrap", "go 1.27.2\n", "go1.20.14", "go1.27.2", 60, false, 0},
		{"development bootstrap", "go 1.27.2\n", "devel go1.28", "go1.27.2", 60, false, 1},
		{"missing cache", "go 1.27.2\n", "go1.21.0", "go1.27.2", 60, false, 0},
		{"incomplete cache", "go 1.27.2\n", "go1.21.0", "go1.27.2", 60, false, 0},
		{"cache version drift", "go 1.27.2\n", "go1.24.13", "go1.27.2", 60, false, 1},
		{"cache architecture drift", "go 1.27.2\n", "go1.21.0", "go1.27.2", 60, false, 1},
		{"unsupported runner architecture", "go 1.27.2\n", "go1.21.0", "go1.27.2", 60, false, 0},
		{"relative cache root", "go 1.27.2\n", "go1.21.0", "go1.27.2", 60, false, 0},
		{"missing cache root", "go 1.27.2\n", "go1.21.0", "go1.27.2", 60, false, 0},
		{"x64 bootstrap", "go 1.27.2\n", "go1.21.0", "go1.27.2", 60, true, 3},
		{"selected version drift", "go 1.27.2\n", "go1.21.0", "go1.28.0", 60, false, 2},
		{"download failure", "go 1.27.2\n", "go1.21.0", "failure", 60, false, 2},
		{"download exceeds work cutoff", "go 1.27.2\n", "go1.21.0", "hang", 16, false, 2},
		{"expired work cutoff", "go 1.27.2\n", "go1.21.0", "go1.27.2", 15, false, 0},
		{"second boundary consumes work cutoff", "go 1.27.2\n", "go1.21.0", "go1.27.2", 16, false, 0},
	} {
		for route, command := range commands {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				cacheRoot := filepath.Join(dir, "toolcache")
				cacheVersion := strings.TrimPrefix(tc.bootstrap, "go")
				if tc.name == "development bootstrap" || tc.name == "cache version drift" {
					cacheVersion = "1.26.9"
				}
				runnerArch, arch, hostArch := "ARM64", "arm64", "arm64"
				if tc.name == "x64 bootstrap" {
					runnerArch, arch, hostArch = "X64", "x64", "amd64"
				}
				if tc.name == "cache architecture drift" {
					hostArch = "amd64"
				}
				if tc.name == "unsupported runner architecture" {
					runnerArch = "RISCV64"
				}
				sdk := filepath.Join(cacheRoot, "go", cacheVersion, arch)
				bin := filepath.Join(sdk, "bin")
				ambient := filepath.Join(dir, "ambient")
				scripts := filepath.Join(dir, "scripts/ci")
				for _, p := range []string{bin, scripts, ambient} {
					if err := os.MkdirAll(p, 0700); err != nil {
						t.Fatal(err)
					}
				}
				for p, data := range map[string][]byte{filepath.Join(dir, "go.mod"): []byte(tc.module), filepath.Join(scripts, "build-queue-publisher.sh"): helper} {
					if err := os.WriteFile(p, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if tc.name != "missing cache" && tc.name != "incomplete cache" {
					if err := os.WriteFile(sdk+".complete", nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(ambient, "go"), []byte("#!/bin/sh\necho ambient >> \"$TRACE\"\nexit 99\n"), 0700); err != nil {
					t.Fatal(err)
				}
				// Anchor deadline arithmetic independently of process scheduling.
				// The one-second download budget still uses the real timeout and
				// hanging Go shim; only the workflow's wall-clock read is virtual.
				const clockStart int64 = 1_800_000_000
				clockNow := clockStart
				if tc.name == "second boundary consumes work cutoff" {
					clockNow++
				}
				clock := `#!/bin/sh
set -eu
test "$#" -eq 1
test "$1" = +%s
printf '%s\n' "$1" >> "$CLOCK_TRACE"
printf '%s\n' "$CLOCK_NOW"
`
				if err := os.WriteFile(filepath.Join(ambient, "date"), []byte(clock), 0700); err != nil {
					t.Fatal(err)
				}
				shim := `#!/usr/bin/env bash
set -eu
printf '%s|%s|%s|%s|%s|%s|%s|%s|%s\n' "$*" "${GOTOOLCHAIN-}" "${GOENV-}" "${GOWORK-}" "${GOSUMDB-}" "${GOPROXY-}" "${GOFLAGS-}" "${GONOSUMDB-}" "${GOPRIVATE-}" >> "$TRACE"
if [[ "$*" == 'env GOVERSION GOHOSTARCH' ]]; then
 [[ "$GOTOOLCHAIN" == local ]]
 printf '%s\n%s\n' "$BOOT" "$BOOT_ARCH"
elif [[ "$*" == 'env GOVERSION' ]]; then
 case "$SELECTED" in failure) exit 1;; hang) exec sleep 10;; *) printf '%s\n' "$SELECTED";; esac
elif [[ "$*" == "build -o $RUNNER_TEMP/queue-publisher ./scripts/ci/pr-gate" ]]; then
 printf '#!/bin/sh\nexit 0\n' > "$RUNNER_TEMP/queue-publisher"
else exit 1; fi
`
				if err := os.WriteFile(filepath.Join(bin, "go"), []byte(shim), 0700); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "bash", "-c", command)
				cmd.Dir = dir
				trace := filepath.Join(dir, "trace")
				clockTrace := filepath.Join(dir, "clock-trace")
				cmd.Env = append(os.Environ(), "PATH="+ambient+":"+os.Getenv("PATH"), "RUNNER_TEMP="+dir, "RUNNER_TOOL_CACHE="+cacheRoot, "RUNNER_ARCH="+runnerArch, "BOOT_ARCH="+hostArch, "KIND="+route, "TRACE="+trace, "BOOT="+tc.bootstrap, "SELECTED="+tc.selected, fmt.Sprintf("PR_GATE_DEADLINE=%d", clockStart+int64(tc.seconds)), fmt.Sprintf("CLOCK_NOW=%d", clockNow), "CLOCK_TRACE="+clockTrace, "GOENV=untrusted", "GOTOOLCHAIN=auto", "GOSUMDB=off", "GONOSUMDB=*", "GOPRIVATE=*", "GOFLAGS=-toolexec=untrusted")
				if tc.name == "relative cache root" {
					cmd.Env = append(cmd.Env, "RUNNER_TOOL_CACHE=relative")
				}
				if tc.name == "missing cache root" {
					cmd.Env = append(cmd.Env, "RUNNER_TOOL_CACHE=")
				}
				start := time.Now()
				output, err := cmd.CombinedOutput()
				if (err == nil) != tc.pass {
					t.Fatalf("pass=%v, err=%v, output=%s", tc.pass, err, output)
				}
				if ctx.Err() != nil {
					t.Fatalf("workflow exceeded bounded harness: %v", ctx.Err())
				}
				if tc.selected == "hang" && time.Since(start) > 3*time.Second {
					t.Fatal("native selection escaped the remaining absolute cutoff")
				}
				clockData, clockErr := os.ReadFile(clockTrace)
				if clockErr != nil || string(clockData) != "+%s\n" {
					t.Fatalf("workflow did not use the anchored wall clock exactly once: %q, %v", clockData, clockErr)
				}
				data, _ := os.ReadFile(trace)
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				if len(data) == 0 {
					lines = nil
				}
				if len(lines) != tc.calls {
					t.Fatalf("got %d go calls, want %d: %s", len(lines), tc.calls, data)
				}
				for i, line := range lines {
					want := "|go1.27.2|off|off|sum.golang.org|https://proxy.golang.org|||"
					if i == 0 {
						want = "|local|off|off|sum.golang.org|https://proxy.golang.org|||"
					}
					if !strings.HasSuffix(line, want) {
						t.Fatalf("unsafe toolchain environment: %s", line)
					}
				}
			})
		}
	}
}
