package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Pinned release files (scripts/ci/pinned-tools/<name>.pin) are restored from
// a read-only Actions entry that only ci-tool-cache.yml writes, and the helper
// verifies their checksum again before use. The restore path and key must be
// byte-identical in every reader and the seed: the entry version derives from
// the path string, so any drift is a permanent miss.
func pinnedToolReuseStep(name, tool, pinPrefix string) map[string]any {
	return map[string]any{
		"name": name,
		"uses": reuseRestoreAction,
		"with": map[string]any{
			"path": "~/.cache/hikyo-pinned-tools/" + tool,
			"key":  "pinned-tool-v1-${{ runner.os }}-" + tool + "-${{ hashFiles('" + pinPrefix + "scripts/ci/pinned-tools/" + tool + ".pin') }}",
		},
	}
}

const (
	shellcheckNewDownload = `archive="$RUNNER_TEMP/shellcheck.tar.xz"
./scripts/ci/fetch-pinned-tool.sh shellcheck-linux-x86_64 "$archive"
shellcheck_dir="$RUNNER_TEMP/shellcheck"
mkdir "$shellcheck_dir"
tar -xJf "$archive" -C "$shellcheck_dir" --strip-components=1
`
	shellcheckFrozenDownload = `archive="$RUNNER_TEMP/shellcheck.tar.xz"
curl --fail --location --proto '=https' --tlsv1.2 --silent --show-error \
  --retry 4 --retry-delay 2 --retry-all-errors \
  "https://github.com/koalaman/shellcheck/releases/download/$SHELLCHECK_VERSION/shellcheck-$SHELLCHECK_VERSION.linux.x86_64.tar.xz" \
  --output "$archive"
printf '%s  %s\n' "$SHELLCHECK_SHA256" "$archive" | sha256sum --check --strict
tar -xJf "$archive" -C "$RUNNER_TEMP"
shellcheck_dir="$RUNNER_TEMP/shellcheck-$SHELLCHECK_VERSION"
`
	kindNewDownload    = "./scripts/ci/fetch-pinned-tool.sh kind-linux-amd64 \"$binary\"\n"
	kindFrozenDownload = `curl --fail --location --proto '=https' --tlsv1.2 --silent --show-error \
  --retry 4 --retry-delay 2 --retry-all-errors \
  "https://github.com/kubernetes-sigs/kind/releases/download/$KIND_VERSION/kind-linux-amd64" \
  --output "$binary"
printf '%s  %s\n' "$KIND_SHA256" "$binary" | sha256sum --check --strict
`
	helmNewInstall = `helm_dir="$RUNNER_TEMP/helm"
mkdir -p "$helm_dir"
./scripts/ci/fetch-pinned-tool.sh helm-linux-amd64 "$RUNNER_TEMP/helm.tar.gz"
tar -xzf "$RUNNER_TEMP/helm.tar.gz" -C "$helm_dir" --strip-components=1
echo "$helm_dir" >>"$GITHUB_PATH"
"$helm_dir/helm" version
`
	cosignNewInstall = `mkdir -p "$HOME/.cosign"
./scripts/ci/fetch-pinned-tool.sh cosign-linux-amd64 "$HOME/.cosign/cosign"
chmod +x "$HOME/.cosign/cosign"
echo "$HOME/.cosign" >>"$GITHUB_PATH"
"$HOME/.cosign/cosign" version
`
)

// The frozen steps below restore literal versions. Lock the pin files to the
// same values first, so a changed pin cannot hide behind the restored step.
var frozenToolPins = map[string][2]string{
	"shellcheck-linux-x86_64": {"https://github.com/koalaman/shellcheck/releases/download/v0.11.0/shellcheck-v0.11.0.linux.x86_64.tar.xz", "8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198"},
	"kind-linux-amd64":        {"https://github.com/kubernetes-sigs/kind/releases/download/v0.32.0/kind-linux-amd64", "50030de23cf40a18505f20426f6a8506bedf13c6e509244bd1fa9463721b0f54"},
	"cosign-linux-amd64":      {"https://github.com/sigstore/cosign/releases/download/v3.1.3/cosign-linux-amd64", "4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71"},
	"helm-linux-amd64":        {"https://get.helm.sh/helm-v4.2.3-linux-amd64.tar.gz", "e9b88b4ee95b18c706839c28d3a0220e5bc470e9cd9262410c90793c45ff8b7c"},
}

func requirePinnedToolContent(name string) error {
	return requirePinnedToolContentIn("pinned-tools", name)
}

func requirePinnedToolContentIn(dir, name string) error {
	want, ok := frozenToolPins[name]
	if !ok {
		return fmt.Errorf("no frozen pin for %s", name)
	}
	raw, err := os.ReadFile(filepath.Join(dir, name+".pin"))
	if err != nil {
		return err
	}
	if string(raw) != "url="+want[0]+"\nsha256="+want[1]+"\n" {
		return fmt.Errorf("the %s pin must remain %s with its published checksum", name, want[0])
	}
	return nil
}

// normalizePinnedToolReuse requires the exact restore steps and helper calls
// that replaced the unretried frozen downloads in ci.yml, then removes the
// restores and restores the frozen step content so every other setting of the
// lint, supply-chain-checks and k8s-e2e jobs stays fingerprinted.
func normalizePinnedToolReuse(jobs map[string]map[string]any) error {
	for name := range frozenToolPins {
		if err := requirePinnedToolContent(name); err != nil {
			return err
		}
	}
	type edit struct {
		job      string
		restores []map[string]any
		added    []map[string]any
		rewrite  func(step map[string]any) (bool, error)
	}
	edits := []edit{
		{"lint", []map[string]any{pinnedToolReuseStep("Restore pinned ShellCheck", "shellcheck-linux-x86_64", "")}, nil, func(step map[string]any) (bool, error) {
			if step["name"] != "Shellcheck (pinned archive)" {
				return false, nil
			}
			run, _ := step["run"].(string)
			if _, hasEnv := step["env"]; hasEnv || strings.Count(run, shellcheckNewDownload) != 1 {
				return true, fmt.Errorf("lint ShellCheck must fetch its pinned archive through the verifying helper")
			}
			step["env"] = map[string]any{"SHELLCHECK_VERSION": "v0.11.0", "SHELLCHECK_SHA256": "8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198"}
			step["run"] = strings.Replace(run, shellcheckNewDownload, shellcheckFrozenDownload, 1)
			return true, nil
		}},
		{"supply-chain-checks", []map[string]any{pinnedToolReuseStep("Restore pinned Cosign", "cosign-linux-amd64", ""), pinnedToolReuseStep("Restore pinned Helm archive", "helm-linux-amd64", "")},
			[]map[string]any{{"name": "Pinned tool fetch fixture", "run": "./scripts/ci/fetch-pinned-tool_test.sh"}}, func(step map[string]any) (bool, error) {
				var frozen map[string]any
				switch step["name"] {
				case "Install pinned Cosign":
					if !reflect.DeepEqual(step, map[string]any{"name": "Install pinned Cosign", "run": cosignNewInstall}) {
						return true, fmt.Errorf("supply-chain-checks must install Cosign through the verifying helper")
					}
					frozen = map[string]any{"uses": "sigstore/cosign-installer@6f9f17788090df1f26f669e9d70d6ae9567deba6", "with": map[string]any{"cosign-release": "v3.1.3"}}
				case "Install pinned Helm":
					if !reflect.DeepEqual(step, map[string]any{"name": "Install pinned Helm", "run": helmNewInstall}) {
						return true, fmt.Errorf("supply-chain-checks must install Helm through the verifying helper")
					}
					frozen = map[string]any{"uses": "azure/setup-helm@9bc31f4ebc9c6b171d7bfbaa5d006ae7abdb4310", "with": map[string]any{"version": "v4.2.3"}}
				default:
					return false, nil
				}
				for key := range step {
					delete(step, key)
				}
				for key, value := range frozen {
					step[key] = value
				}
				return true, nil
			}},
		{"k8s-e2e", []map[string]any{
			pinnedToolReuseStep("Restore pinned kind", "kind-linux-amd64", ""),
			pinnedToolReuseStep("Restore pinned Node archive", "node-linux-x64", ""),
			pinnedToolReuseStep("Restore pinned Helm archive", "helm-linux-amd64", ""),
			pinnedToolReuseStep("Restore pinned Calico manifest", "calico-manifest", ""),
		}, nil, func(step map[string]any) (bool, error) {
			if step["name"] != "Install kind (pinned archive)" {
				return false, nil
			}
			run, _ := step["run"].(string)
			if _, hasEnv := step["env"]; hasEnv || strings.Count(run, kindNewDownload) != 1 {
				return true, fmt.Errorf("k8s-e2e kind must be fetched through the verifying helper")
			}
			step["env"] = map[string]any{"KIND_VERSION": "v0.32.0", "KIND_SHA256": "50030de23cf40a18505f20426f6a8506bedf13c6e509244bd1fa9463721b0f54"}
			step["run"] = strings.Replace(run, kindNewDownload, kindFrozenDownload, 1)
			return true, nil
		}},
	}
	for _, e := range edits {
		steps, ok := jobs[e.job]["steps"].([]any)
		if !ok {
			return fmt.Errorf("%s lost its steps", e.job)
		}
		kept := make([]any, 0, len(steps))
		restored, rewritten, added := 0, 0, 0
		for _, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("%s has malformed step configuration", e.job)
			}
			isRestore := false
			for _, want := range e.restores {
				if step["name"] == want["name"] {
					if !reflect.DeepEqual(step, want) {
						return fmt.Errorf("%s pinned-tool restore %q drifted from the seed path and key", e.job, step["name"])
					}
					isRestore = true
					restored++
				}
			}
			if isRestore {
				continue
			}
			isAddition := false
			for _, want := range e.added {
				if step["name"] == want["name"] {
					if !reflect.DeepEqual(step, want) {
						return fmt.Errorf("%s fixture step %q must be unconditional and fail closed", e.job, step["name"])
					}
					isAddition = true
					added++
				}
			}
			if isAddition {
				continue
			}
			done, err := e.rewrite(step)
			if err != nil {
				return err
			}
			if done {
				rewritten++
			}
			kept = append(kept, step)
		}
		wantRewrites := 1
		if e.job == "supply-chain-checks" {
			wantRewrites = 2
		}
		if restored != len(e.restores) || rewritten != wantRewrites || added != len(e.added) {
			return fmt.Errorf("%s requires its exact pinned-tool restores, fixture and one helper install, got %d, %d and %d", e.job, restored, added, rewritten)
		}
		jobs[e.job]["steps"] = kept
	}
	return nil
}

func ciJobsForPinnedTools(t *testing.T, mutate func(map[string]map[string]any)) error {
	t.Helper()
	_, raw := readWorkflowFile(t, "ci.yml")
	var workflow struct {
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(workflow.Jobs)
	}
	return normalizePinnedToolReuse(workflow.Jobs)
}

func TestPinnedToolReuseNormalizationRequiresExactSteps(t *testing.T) {
	if err := ciJobsForPinnedTools(t, nil); err != nil {
		t.Fatal(err)
	}
	stepNamed := func(jobs map[string]map[string]any, job, name string) map[string]any {
		for _, value := range jobs[job]["steps"].([]any) {
			if step := value.(map[string]any); step["name"] == name {
				return step
			}
		}
		t.Fatalf("%s has no step %q", job, name)
		return nil
	}
	for name, mutate := range map[string]func(map[string]map[string]any){
		"restore key drift": func(j map[string]map[string]any) {
			stepNamed(j, "lint", "Restore pinned ShellCheck")["with"].(map[string]any)["key"] = "pinned-tool-v2"
		},
		"restore path drift": func(j map[string]map[string]any) {
			stepNamed(j, "k8s-e2e", "Restore pinned kind")["with"].(map[string]any)["path"] = "~/.cache/kind"
		},
		"restore fallback added": func(j map[string]map[string]any) {
			stepNamed(j, "supply-chain-checks", "Restore pinned Cosign")["with"].(map[string]any)["restore-keys"] = "pinned-tool-v1-"
		},
		"restore removed": func(j map[string]map[string]any) {
			steps := j["k8s-e2e"]["steps"].([]any)
			kept := steps[:0:0]
			for _, value := range steps {
				if value.(map[string]any)["name"] != "Restore pinned Helm archive" {
					kept = append(kept, value)
				}
			}
			j["k8s-e2e"]["steps"] = kept
		},
		"shellcheck skips the helper": func(j map[string]map[string]any) {
			step := stepNamed(j, "lint", "Shellcheck (pinned archive)")
			step["run"] = strings.Replace(step["run"].(string), "./scripts/ci/fetch-pinned-tool.sh shellcheck-linux-x86_64", "curl -o", 1)
		},
		"cosign skips the helper": func(j map[string]map[string]any) {
			step := stepNamed(j, "supply-chain-checks", "Install pinned Cosign")
			step["run"] = strings.Replace(step["run"].(string), "./scripts/ci/fetch-pinned-tool.sh cosign-linux-amd64", "curl -o", 1)
		},
		"kind extra environment": func(j map[string]map[string]any) {
			stepNamed(j, "k8s-e2e", "Install kind (pinned archive)")["env"] = map[string]any{"X": "1"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			if ciJobsForPinnedTools(t, mutate) == nil {
				t.Fatal("unsafe pinned-tool configuration escaped the normalizer")
			}
		})
	}
}

// Every reader and the single seed writer must agree on the entry identity, and
// no workflow may keep a hand-rolled fixed-delay download or the retired action.
func TestPinnedToolReuseDrift(t *testing.T) {
	root := repositoryRoot(t)
	pins, err := filepath.Glob(filepath.Join(root, "scripts/ci/pinned-tools/*.pin"))
	if err != nil || len(pins) == 0 {
		t.Fatalf("no pin files: %v", err)
	}
	pinNames := map[string]bool{}
	for _, pin := range pins {
		name := strings.TrimSuffix(filepath.Base(pin), ".pin")
		raw, err := os.ReadFile(pin)
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^url=https://[^\s]+\nsha256=[0-9a-f]{64}\n$`).Match(raw) {
			t.Errorf("%s is not exactly an https url line and a lowercase sha256 line", pin)
		}
		pinNames[name] = true
	}

	seedWorkflow, _ := readWorkflowFile(t, "ci-tool-cache.yml")
	seedJob := seedWorkflow["jobs"].(map[string]any)["seed"].(map[string]any)
	seeded := map[string]bool{}
	for _, tool := range seedJob["strategy"].(map[string]any)["matrix"].(map[string]any)["tool"].([]any) {
		seeded[tool.(string)] = true
		if !pinNames[tool.(string)] {
			t.Errorf("seed matrix names %v, which has no pin file", tool)
		}
	}
	seedPath, seedKey := "~/.cache/hikyo-pinned-tools/${{ matrix.tool }}", "pinned-tool-v1-${{ runner.os }}-${{ matrix.tool }}-${{ hashFiles(format('scripts/ci/pinned-tools/{0}.pin', matrix.tool)) }}"
	var lookup, save int
	for _, value := range seedJob["steps"].([]any) {
		step := value.(map[string]any)
		uses, _ := step["uses"].(string)
		with, _ := step["with"].(map[string]any)
		switch {
		case strings.HasPrefix(uses, "actions/cache/restore@"):
			lookup++
			if with["path"] != seedPath || with["key"] != seedKey || with["lookup-only"] != true || with["restore-keys"] != nil {
				t.Errorf("seed lookup drifted: %v", with)
			}
		case strings.HasPrefix(uses, "actions/cache/save@"):
			save++
			if with["path"] != seedPath || with["key"] != "${{ steps.tool-seed.outputs.cache-primary-key }}" {
				t.Errorf("seed save drifted: %v", with)
			}
		}
	}
	if lookup != 1 || save != 1 {
		t.Errorf("seed needs exactly one lookup and one save, got %d and %d", lookup, save)
	}

	workflows, err := filepath.Glob(filepath.Join(root, ".github/workflows/*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	consumed := map[string]bool{}
	restoreName := regexp.MustCompile(`^~/\.cache/hikyo-pinned-tools/([a-z0-9][a-z0-9._-]*)$`)
	for _, file := range workflows {
		base := filepath.Base(file)
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Contains(text, "sigstore/cosign-installer@") || strings.Contains(text, "azure/setup-helm@") || strings.Contains(text, "--retry-delay") {
			t.Errorf("%s keeps a fixed-delay download, the cosign installer or setup-helm; use scripts/ci/fetch-pinned-tool.sh", base)
		}
		for _, name := range regexp.MustCompile(`fetch-pinned-tool\.sh"? (?:--prefetch )?([a-z0-9][a-z0-9._-]*)`).FindAllStringSubmatch(text, -1) {
			if !pinNames[name[1]] {
				t.Errorf("%s fetches %s, which has no pin file", base, name[1])
			}
			consumed[name[1]] = true
		}
		var workflow struct {
			Jobs map[string]struct {
				Steps []struct {
					Name string            `yaml:"name"`
					Uses string            `yaml:"uses"`
					Run  string            `yaml:"run"`
					With map[string]string `yaml:"with"`
				} `yaml:"steps"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		for jobName, job := range workflow.Jobs {
			runs := make([]string, len(job.Steps))
			for i, step := range job.Steps {
				runs[i] = step.Run
			}
			for idx, step := range job.Steps {
				pinnedKey := strings.HasPrefix(step.With["key"], "pinned-tool-")
				if strings.HasPrefix(step.Uses, "actions/cache/save@") && pinnedKey && base != "ci-tool-cache.yml" {
					t.Errorf("%s/%s saves a pinned-tool entry; ci-tool-cache.yml is the only writer", base, jobName)
				}
				if strings.HasPrefix(step.Uses, "actions/cache@") && pinnedKey {
					t.Errorf("%s/%s uses the combined cache action for a pinned tool", base, jobName)
				}
				if base == "ci-tool-cache.yml" || !strings.HasPrefix(step.Uses, "actions/cache/restore@") || !strings.Contains(step.With["path"], "hikyo-pinned-tools") {
					continue
				}
				m := restoreName.FindStringSubmatch(step.With["path"])
				if m == nil {
					t.Errorf("%s/%s restores a malformed pinned-tool path %q", base, jobName, step.With["path"])
					continue
				}
				tool := m[1]
				if err := requireRestoreBeforeConsumer(root, tool, idx, runs); err != nil {
					t.Errorf("%s/%s: %v", base, jobName, err)
				}
				pinPrefix := ""
				if base == "ci-feedback.yml" {
					pinPrefix = "feedback-tools/"
				}
				want := "pinned-tool-v1-${{ runner.os }}-" + tool + "-${{ hashFiles('" + pinPrefix + "scripts/ci/pinned-tools/" + tool + ".pin') }}"
				if step.With["key"] != want || len(step.With) != 2 {
					t.Errorf("%s/%s pinned-tool restore for %s drifted from the seed key: %v", base, jobName, tool, step.With)
				}
				if !pinNames[tool] || !seeded[tool] {
					t.Errorf("%s/%s restores %s, which is not pinned and seeded", base, jobName, tool)
				}
			}
		}
	}
	for name := range pinNames {
		if !consumed[name] && !scriptConsumesPin(t, root, name) {
			t.Errorf("pin %s is not consumed by any workflow or script", name)
		}
	}
	// These are fetched only by lanes that forbid shared caches, so they have
	// no restore step and must not be seeded.
	for _, name := range []string{"kind-linux-arm64", "helm-linux-arm64"} {
		if seeded[name] {
			t.Errorf("%s is used only by lanes that forbid shared caches; do not seed it", name)
		}
	}
}

func scriptConsumesPin(t *testing.T, root, name string) bool {
	t.Helper()
	scripts, err := filepath.Glob(filepath.Join(root, "scripts/ci/*.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range scripts {
		if filepath.Base(script) == "fetch-pinned-tool.sh" || strings.HasSuffix(script, "_test.sh") {
			continue
		}
		raw, err := os.ReadFile(script)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "fetch-pinned-tool.sh "+name+" ") {
			return true
		}
	}
	return false
}

// A restore only helps if it runs before the first step that fetches the tool,
// either directly through the helper or through k8s-e2e.sh and chart-kind.sh.
func requireRestoreBeforeConsumer(root, tool string, restoreIdx int, runs []string) error {
	direct := regexp.MustCompile(`fetch-pinned-tool\.sh"? ` + regexp.QuoteMeta(tool) + `[ "]`)
	scripted := false
	for _, script := range []string{"k8s-e2e.sh", "chart-kind.sh"} {
		raw, err := os.ReadFile(filepath.Join(root, "scripts/ci", script))
		if err != nil {
			return err
		}
		scripted = scripted || strings.Contains(string(raw), "fetch-pinned-tool.sh "+tool+" ")
	}
	for i, run := range runs {
		if direct.MatchString(run) || (scripted && strings.Contains(run, "scripts/ci/k8s-e2e.sh")) {
			if restoreIdx > i {
				return fmt.Errorf("restore of %s (step %d) runs after its first consumer (step %d)", tool, restoreIdx, i)
			}
			return nil
		}
	}
	return fmt.Errorf("restore of %s has no consuming step in the job", tool)
}

func TestPinnedToolReuseRestoreOrderingAndPinsAreEnforced(t *testing.T) {
	root := repositoryRoot(t)
	runs := []string{"echo", "./scripts/ci/fetch-pinned-tool.sh kind-linux-amd64 \"$b\""}
	if err := requireRestoreBeforeConsumer(root, "kind-linux-amd64", 0, runs); err != nil {
		t.Fatal(err)
	}
	if requireRestoreBeforeConsumer(root, "kind-linux-amd64", 2, runs) == nil {
		t.Error("a restore after its consumer was accepted")
	}
	if requireRestoreBeforeConsumer(root, "node-linux-x64", 0, []string{"echo"}) == nil {
		t.Error("a restore without a consumer was accepted")
	}
	if requireRestoreBeforeConsumer(root, "node-linux-x64", 3, []string{"echo", "echo", "./scripts/ci/k8s-e2e.sh"}) == nil {
		t.Error("a script-consumed restore after k8s-e2e.sh was accepted")
	}
	// Changing any frozen pin's URL or checksum must fail.
	for name := range frozenToolPins {
		raw, err := os.ReadFile(filepath.Join("pinned-tools", name+".pin"))
		if err != nil {
			t.Fatal(err)
		}
		for label, mutate := range map[string]func(string) string{
			"checksum": func(v string) string { return strings.Replace(v, "sha256=", "sha256=0", 1)[:len(v)-1] + "\n" },
			"version":  func(v string) string { return strings.Replace(v, "https://", "https://mirror.example/", 1) },
		} {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, name+".pin"), []byte(mutate(string(raw))), 0o600); err != nil {
				t.Fatal(err)
			}
			if requirePinnedToolContentIn(dir, name) == nil {
				t.Errorf("%s pin with changed %s was accepted", name, label)
			}
		}
		if err := requirePinnedToolContentIn("pinned-tools", name); err != nil {
			t.Error(err)
		}
	}
}
