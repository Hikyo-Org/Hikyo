package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocsBootstrapJQDependency(t *testing.T) {
	root := repositoryRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "scripts/ci/verify-docs.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"installed-local", "provisioned-ci", "local-missing", "nongithub-ci", "github-without-ci", "nonroot-ci", "not-container", "unsupported-os", "unsupported-version", "update-failure", "install-failure", "install-without-jq"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			scripts := filepath.Join(dir, "scripts/ci")
			pkg := filepath.Join(dir, "docs/site")
			for _, p := range []string{bin, scripts, pkg} {
				if err := os.MkdirAll(p, 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, body string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(body), 0700); err != nil {
					t.Fatal(err)
				}
			}
			// Map only fixed OS/container observations into fixtures, without adding
			// production override knobs or changing the executable bootstrap logic.
			source := strings.ReplaceAll(string(raw), "/.dockerenv", filepath.Join(dir, "container"))
			source = strings.ReplaceAll(source, "/etc/os-release", filepath.Join(dir, "os-release"))
			write(filepath.Join(scripts, "verify-docs.sh"), source)
			write(filepath.Join(dir, "container"), "")
			release := "ID=ubuntu\nVERSION_ID=\"24.04\"\n"
			if scenario == "unsupported-os" {
				release = "ID=alpine\nVERSION_ID=\"24.04\"\n"
			}
			if scenario == "unsupported-version" {
				release = "ID=ubuntu\nVERSION_ID=\"22.04\"\n"
			}
			write(filepath.Join(dir, "os-release"), release)
			if scenario == "not-container" {
				if err := os.Remove(filepath.Join(dir, "container")); err != nil {
					t.Fatal(err)
				}
			}
			for _, tool := range []string{"dirname", "grep", "cp"} {
				p, err := exec.LookPath(tool)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
					t.Fatal(err)
				}
			}
			write(filepath.Join(bin, "id"), "#!/bin/sh\nif [ \"$SCENARIO\" = nonroot-ci ]; then echo 1000; else echo 0; fi\n")
			jqReady := "#!/bin/sh\nexit 0\n"
			write(filepath.Join(dir, "jq-ready"), jqReady)
			if scenario == "installed-local" {
				write(filepath.Join(bin, "jq"), jqReady)
			}
			write(filepath.Join(bin, "apt-get"), `#!/bin/sh
set -eu
printf 'apt-get %s\n' "$*" >>"$FIXTURE/calls"
case "$SCENARIO:$1" in update-failure:update|install-failure:install) exit 43 ;; esac
if [ "$1" = install ] && [ "$SCENARIO" != install-without-jq ]; then cp "$FIXTURE/jq-ready" "$FIXTURE/bin/jq"; fi
`)
			write(filepath.Join(bin, "node"), `#!/bin/sh
printf 'node %s\n' "$*" >>"$FIXTURE/calls"
if [ "$1" = -p ]; then printf 'pnpm@11.24.0\n'; fi
`)
			for _, tool := range []string{"corepack", "pnpm"} {
				write(filepath.Join(bin, tool), "#!/bin/sh\nprintf '"+tool+" %s\\n' \"$*\" >>\"$FIXTURE/calls\"\n")
			}
			for _, name := range []string{"install-corepack.sh", "check-doc-status_test.sh", "check-oss-policy_test.sh", "check-docs-live_test.sh", "check-fallback-channel-test_test.sh"} {
				write(filepath.Join(scripts, name), "#!/bin/sh\nprintf '"+name+"\\n' >>\"$FIXTURE/calls\"\n")
			}
			ci := "true"
			if scenario == "installed-local" || scenario == "local-missing" || scenario == "github-without-ci" {
				ci = "false"
			}
			github := ci
			if scenario == "nongithub-ci" {
				github = "false"
			}
			if scenario == "github-without-ci" {
				github = "true"
			}
			env := append(os.Environ(), "PATH="+bin, "FIXTURE="+dir, "SCENARIO="+scenario, "CI="+ci, "GITHUB_ACTIONS="+github)
			cmd := exec.Command(filepath.Join(scripts, "verify-docs.sh"))
			cmd.Env = env
			out, err := cmd.CombinedOutput()
			pass := scenario == "installed-local" || scenario == "provisioned-ci"
			if pass != (err == nil) {
				t.Fatalf("unexpected result: %v %s", err, out)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			lines := strings.Split(strings.TrimSuffix(string(calls), "\n"), "\n")
			switch scenario {
			case "provisioned-ci":
				if len(lines) < 3 || lines[0] != "apt-get update" || lines[1] != "apt-get install --yes --no-install-recommends jq" || !strings.Contains(string(calls), "pnpm --dir "+pkg+" run verify") {
					t.Fatalf("provisioning must precede all bootstrap/verification: %s", calls)
				}
			case "installed-local":
				if strings.Contains(string(calls), "apt-get") || !strings.Contains(string(calls), "pnpm --dir "+pkg+" run verify") {
					t.Fatalf("existing jq must avoid install and preserve verification: %s", calls)
				}
			case "update-failure":
				if string(calls) != "apt-get update\n" {
					t.Fatalf("update failure did not stop before verification: %s", calls)
				}
			case "install-failure", "install-without-jq":
				if string(calls) != "apt-get update\napt-get install --yes --no-install-recommends jq\n" {
					t.Fatalf("install failure did not stop before verification: %s", calls)
				}
			default:
				if len(calls) != 0 || !strings.Contains(string(out), "jq is required") {
					t.Fatalf("unsupported/local absence must refuse without installs/bootstrap: %s %s", calls, out)
				}
			}
			// Even a recognized root CI context never installs from policy validation.
			if scenario != "installed-local" && scenario != "provisioned-ci" {
				if err := os.Remove(filepath.Join(dir, "calls")); err != nil && !os.IsNotExist(err) {
					t.Fatal(err)
				}
				cmd = exec.Command(filepath.Join(root, "scripts/release/check-main-policy.sh"), filepath.Join(root, "release/repository/main-ci-gate.json"))
				cmd.Env = env
				out, err = cmd.CombinedOutput()
				if err == nil || !strings.Contains(string(out), "jq is required") {
					t.Fatalf("pure validator missing-tool result: %v %s", err, out)
				}
				if _, err := os.Stat(filepath.Join(dir, "calls")); !os.IsNotExist(err) {
					t.Fatal("validator performed provisioning/bootstrap")
				}
			}
		})
	}
}
