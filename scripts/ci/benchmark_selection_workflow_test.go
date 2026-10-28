package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBenchmarkSelectionAndTimingFailuresPropagate(t *testing.T) {
	for _, filename := range []string{"codspeed.yml", "pr-benchmark.yml"} {
		t.Run(filename, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", filename))
			if err != nil {
				t.Fatal(err)
			}
			var workflow struct {
				Jobs map[string]struct {
					If      string            `yaml:"if"`
					Outputs map[string]string `yaml:"outputs"`
					Steps   []struct {
						Uses string            `yaml:"uses"`
						With map[string]string `yaml:"with"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			if filename == "pr-benchmark.yml" {
				if !strings.Contains(workflow.Jobs["benchmarks"].If, "needs.request.outputs.go == 'true'") || !strings.Contains(workflow.Jobs["matrix"].If, "needs.request.outputs.browser == 'true'") {
					t.Fatal("each measurement runner must require its selected workload")
				}
				for _, output := range []string{"go", "browser"} {
					if workflow.Jobs["request"].Outputs[output] != "${{ steps.request.outputs."+output+" }}" {
						t.Fatalf("%s selection must come from the trusted request verifier", output)
					}
				}
			}
			var command string
			for _, step := range workflow.Jobs["benchmarks"].Steps {
				if strings.HasPrefix(step.Uses, "CodSpeedHQ/action@") {
					if step.With["mode"] != "walltime" || step.With["go-runner-version"] != "1.3.0" {
						t.Fatal("measurement must pin the qualified Go walltime integration")
					}
					command = step.With["run"]
				}
			}
			if !strings.Contains(command, "go run ./scripts/ci/codspeed-verify") {
				t.Fatal("upload requires validation of the actual timed boundaries")
			}
			if filename == "pr-benchmark.yml" {
				if !strings.Contains(command, "${{ steps.request.outputs.command }}") {
					t.Fatal("Go benchmarks must use the reverified exact-head workload commands")
				}
				command = strings.ReplaceAll(command, "${{ steps.request.outputs.command }}", "go test -bench='^BenchmarkValuesList$' ./internal/service/")
			} else if !strings.Contains(command, "go test -bench=. ./internal/crypto/ ./internal/dotenv/ ./internal/scanning/ ./internal/server/ ./internal/service/") {
				t.Fatal("main must retain complete diagnostic coverage")
			}
			for _, test := range []struct{ benchmark, verifier, want int }{{0, 0, 0}, {17, 0, 17}, {0, 19, 19}} {
				t.Run(strconv.Itoa(test.benchmark)+"-"+strconv.Itoa(test.verifier), func(t *testing.T) {
					dir := t.TempDir()
					capture := filepath.Join(dir, "calls")
					fakeGo := "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$*\" >>\"$BENCHMARK_CALLS\"\ncase \"$1\" in\n test) printf 'pkg: example/internal/service\\nBenchmarkValuesList-2 1 1000 ns/op\\n'; exit \"$BENCHMARK_EXIT\";;\n run) exit \"$VERIFIER_EXIT\";;\n *) exit 99;;\nesac\n"
					if err := os.WriteFile(filepath.Join(dir, "go"), []byte(fakeGo), 0755); err != nil {
						t.Fatal(err)
					}
					cmd := exec.Command("bash", "-c", command)
					cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "BENCHMARK_CALLS="+capture, "BENCHMARK_EXIT="+strconv.Itoa(test.benchmark), "VERIFIER_EXIT="+strconv.Itoa(test.verifier), "CODSPEED_PROFILE_FOLDER="+dir)
					out, err := cmd.CombinedOutput()
					status := 0
					if err != nil {
						if exit, ok := err.(*exec.ExitError); ok {
							status = exit.ExitCode()
						} else {
							t.Fatal(err)
						}
					}
					if status != test.want {
						t.Fatalf("exit=%d, want %d: %s", status, test.want, out)
					}
					calls, err := os.ReadFile(capture)
					if err != nil {
						t.Fatal(err)
					}
					if test.benchmark != 0 && strings.Contains(string(calls), "codspeed-verify") {
						t.Fatal("a failed benchmark must stop before verification/upload")
					}
				})
			}
		})
	}
}
