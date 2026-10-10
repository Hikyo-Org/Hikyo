package ci_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// These declarations model workflow YAML, not runtime caches. Use reuse names
// so the invariant-12 declaration sweep still reserves cache names for stores
// with registered key construction and authorization boundaries.
type sharedGoReuseStep struct {
	ID   string            `yaml:"id,omitempty"`
	Uses string            `yaml:"uses,omitempty"`
	If   string            `yaml:"if,omitempty"`
	Run  string            `yaml:"run,omitempty"`
	With map[string]string `yaml:"with,omitempty"`
}

type sharedGoReuseWorkflow struct {
	Jobs map[string]struct {
		Steps []sharedGoReuseStep `yaml:"steps"`
	} `yaml:"jobs"`
}

// Consumers share an existing archive instead of adding another immutable
// family. Check the actual YAML and follow save keys back to their restore ID.
func sharedGoReuseErrors(raw []byte) []error {
	var workflow sharedGoReuseWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return []error{err}
	}
	const key = "go-test-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-${{ hashFiles('go.mod', 'go.sum', 'scripts/ci/go-tool-modules.txt') }}"
	const prefix = "go-test-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-"
	const moduleKey = "go-mod-v2-${{ runner.os }}-${{ hashFiles('go.mod', 'go.sum', 'scripts/ci/go-tool-modules.txt') }}"
	const guard = "success() && steps.test-go-cache.outputs.cache-hit != 'true' && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'"
	var problems []error
	readers := make(map[string]int)
	writers := 0
	for name, job := range workflow.Jobs {
		restoreIDs := make(map[string]bool)
		setup, abi, modules, compiled, composeProof := -1, -1, -1, -1, -1
		for i, step := range job.Steps {
			if name == "compose-demo" && step.Run == "./scripts/compose-demo.sh" {
				if composeProof >= 0 || step.If != "" {
					problems = append(problems, fmt.Errorf("compose-demo: the actual delivery proof must run once unconditionally"))
				}
				composeProof = i
			}
			for _, line := range strings.Split(step.Run, "\n") {
				if (name == "app-build" || name == "web-go" || name == "client" || name == "supply-chain-checks" || name == "compose-demo") && strings.Contains(line, "go test ") && !strings.Contains(line, "-count=1") && !strings.Contains(line, "go test -c ") {
					problems = append(problems, fmt.Errorf("%s: cached consumers must execute Go tests with -count=1", name))
				}
			}
			if strings.HasPrefix(step.Uses, "actions/setup-go@") && step.With["go-version-file"] == "go.mod" && step.With["cache"] == "false" {
				setup = i
			}
			if step.ID == "runner-cache-abi" && step.Run == "./scripts/ci/export-runner-cache-abi.sh" {
				abi = i
			}
			if strings.HasPrefix(step.Uses, "actions/cache/restore@") && step.With["path"] == "~/go/pkg/mod" && step.With["key"] == moduleKey {
				modules = i
			}
			if strings.HasPrefix(step.With["key"], "go-web-") || strings.HasPrefix(step.With["restore-keys"], "go-web-") {
				problems = append(problems, fmt.Errorf("%s: orphan go-web cache family", name))
			}
			if !strings.HasPrefix(step.Uses, "actions/cache/restore@") || !strings.HasPrefix(step.With["key"], "go-test-") {
				continue
			}
			compiled = i
			readers[name]++
			if step.ID != "" {
				restoreIDs[step.ID] = true
			}
			if step.With["path"] != "~/.cache/go-build" || step.With["key"] != key || strings.TrimSpace(step.With["restore-keys"]) != prefix {
				problems = append(problems, fmt.Errorf("%s: shared Go cache must use the writer's path, runner ABI and dependency hashes", name))
			}
			if setup < 0 || abi < 0 || modules < 0 || setup >= i || abi >= i || modules >= i {
				problems = append(problems, fmt.Errorf("%s: pinned Go setup, module restore and runner ABI must precede compiled cache restore", name))
			}
		}
		if name == "compose-demo" && (composeProof < 0 || compiled < 0 || composeProof <= compiled) {
			problems = append(problems, fmt.Errorf("compose-demo: compatible compiled cache restore must precede the fresh actual delivery proof"))
		}
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/cache/save@") {
				continue
			}
			linked := strings.HasPrefix(step.With["key"], "go-test-") || strings.Contains(step.With["key"], "steps.test-go-cache.outputs.")
			for id := range restoreIDs {
				linked = linked || strings.Contains(step.With["key"], "steps."+id+".outputs.")
			}
			if !linked {
				continue
			}
			writers++
			if name != "test_core" || !restoreIDs["test-go-cache"] || step.With["path"] != "~/.cache/go-build" || step.With["key"] != "${{ steps.test-go-cache.outputs.cache-primary-key }}" || strings.Join(strings.Fields(step.If), " ") != guard {
				problems = append(problems, fmt.Errorf("%s: only test_core may save its exact-miss primary key after successful trusted main validation", name))
			}
		}
	}
	for _, name := range []string{"test_core", "app-build", "web-go", "supply-chain-checks", "client", "compose-demo"} {
		if readers[name] != 1 {
			problems = append(problems, fmt.Errorf("%s: want one shared compiled Go cache reader, got %d", name, readers[name]))
		}
	}
	if writers != 1 {
		problems = append(problems, fmt.Errorf("want one existing shared Go cache writer, got %d", writers))
	}
	return problems
}

func TestSharedGoReuseWorkflow(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range sharedGoReuseErrors(raw) {
		t.Error(problem)
	}
	// This consumer invokes Go tests through a shell helper rather than YAML.
	client, err := os.ReadFile("check-client-skew.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(client), "go test -count=1 -json") {
		t.Fatal("client skew proof must execute fresh tests after restoring GOCACHE")
	}
	compose, err := os.ReadFile("../compose-demo.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, proof := range []string{`go build -o "$binary" ./cmd/hikyo`, `docker compose --project-directory "$project_dir" up --abort-on-container-exit`} {
		if !strings.Contains(string(compose), proof) {
			t.Fatalf("Compose must retain its fresh source build and actual delivery proof: %s", proof)
		}
	}
}

func TestSharedGoReuseWorkflowRejectsRegressions(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"missing writer", "missing consumer", "orphan family", "different path", "missing ABI", "missing tool lock", "PR writer", "OR bypass", "duplicate writer", "dangling writer", "restore before setup", "replayed test result", "missing compose cache", "compose before cache", "missing compose proof", "conditional compose proof"} {
		t.Run(name, func(t *testing.T) {
			var workflow sharedGoReuseWorkflow
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			jobName := "app-build"
			if strings.Contains(name, "compose") {
				jobName = "compose-demo"
			}
			if name == "missing writer" || name == "PR writer" || name == "OR bypass" || name == "duplicate writer" || name == "dangling writer" {
				jobName = "test_core"
			}
			if name == "replayed test result" {
				jobName = "supply-chain-checks"
			}
			job := workflow.Jobs[jobName]
			for i, step := range job.Steps {
				if strings.Contains(name, "compose") && step.Run == "./scripts/compose-demo.sh" {
					switch name {
					case "compose before cache":
						job.Steps = append([]sharedGoReuseStep{step}, append(job.Steps[:i], job.Steps[i+1:]...)...)
					case "missing compose proof":
						job.Steps[i].Run = "true"
					case "conditional compose proof":
						job.Steps[i].If = "steps.compiled-cache.outputs.cache-hit != 'true'"
					}
					if name != "missing compose cache" {
						break
					}
				}
				if name == "replayed test result" && strings.Contains(step.Run, "go test -count=1 ./scripts/release") {
					job.Steps[i].Run = strings.ReplaceAll(step.Run, "-count=1 ", "")
					break
				}
				reader := strings.HasPrefix(step.Uses, "actions/cache/restore@") && strings.HasPrefix(step.With["key"], "go-test-")
				writer := strings.HasPrefix(step.Uses, "actions/cache/save@") && strings.Contains(step.With["key"], "steps.test-go-cache.outputs.")
				if writer {
					switch name {
					case "missing writer":
						job.Steps = append(job.Steps[:i], job.Steps[i+1:]...)
					case "PR writer":
						job.Steps[i].If = strings.ReplaceAll(step.If, "'workflow_dispatch'", "'pull_request'")
					case "OR bypass":
						job.Steps[i].If += " || github.event_name == 'pull_request'"
					case "duplicate writer":
						job.Steps = append(job.Steps, step)
					case "dangling writer":
						job.Steps[i].With["key"] = "${{ steps.missing.outputs.cache-primary-key }}"
					}
					break
				}
				if !reader {
					continue
				}
				if jobName == "test_core" || name == "replayed test result" || (strings.Contains(name, "compose") && name != "missing compose cache") {
					continue
				}
				switch name {
				case "missing consumer", "missing compose cache":
					job.Steps = append(job.Steps[:i], job.Steps[i+1:]...)
				case "orphan family":
					job.Steps[i].With["key"] = strings.ReplaceAll(step.With["key"], "go-test-v2", "go-web-v2")
				case "different path":
					job.Steps[i].With["path"] = "~/go/pkg/mod"
				case "missing ABI":
					job.Steps[i].With["key"] = strings.ReplaceAll(step.With["key"], "${{ steps.runner-cache-abi.outputs.value }}-", "")
				case "missing tool lock":
					job.Steps[i].With["key"] = strings.ReplaceAll(step.With["key"], ", 'scripts/ci/go-tool-modules.txt'", "")
				case "restore before setup":
					job.Steps = append([]sharedGoReuseStep{step}, append(job.Steps[:i], job.Steps[i+1:]...)...)
				}
				break
			}
			workflow.Jobs[jobName] = job
			modified, err := yaml.Marshal(workflow)
			if err != nil {
				t.Fatal(err)
			}
			if problems := sharedGoReuseErrors(modified); len(problems) == 0 {
				t.Fatal("accepted cache policy regression")
			}
		})
	}
}
