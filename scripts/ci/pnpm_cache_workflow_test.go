package ci_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type pnpmReuseWorkflow struct {
	Jobs map[string]struct {
		Container struct {
			Image string `yaml:"image"`
		} `yaml:"container"`
		Steps []sharedGoReuseStep `yaml:"steps"`
	} `yaml:"jobs"`
}

func pnpmReuseErrors(raw []byte) []error {
	var workflow pnpmReuseWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return []error{err}
	}
	const keySuffix = "${{ runner.os }}-${{ runner.arch }}-${{ steps.pnpm-cache.outputs.abi }}-${{ hashFiles('.nvmrc', 'web/package.json', 'web/pnpm-lock.yaml', 'web/pnpm-workspace.yaml', 'clients/ts/package.json', 'clients/ts/pnpm-lock.yaml', 'clients/ts/pnpm-workspace.yaml') }}"
	const prefixSuffix = "${{ runner.os }}-${{ runner.arch }}-${{ steps.pnpm-cache.outputs.abi }}-"
	const guard = "success() && steps.pnpm-store.outputs.cache-hit != 'true' && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'"
	var problems []error
	readers, writers := make(map[string]int), make(map[string]int)
	containerImage := workflow.Jobs["storybook"].Container.Image
	if !strings.HasPrefix(containerImage, "mcr.microsoft.com/playwright:v") || !strings.Contains(containerImage, "@sha256:") || workflow.Jobs["web"].Container.Image != containerImage {
		problems = append(problems, fmt.Errorf("Playwright pnpm writer and readers must use the same digest-pinned image and compression tools"))
	}
	for name, job := range workflow.Jobs {
		group, writer := "host", "app-build"
		if name == "storybook" || name == "web" {
			group, writer = "playwright", "storybook"
		}
		key, prefix := "pnpm-store-v2-"+group+"-"+keySuffix, "pnpm-store-v2-"+group+"-"+prefixSuffix
		node, bootstrap, selector, install := -1, -1, -1, -1
		writerRestoreID := false
		for i, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/setup-node@") && step.With["node-version-file"] == ".nvmrc" {
				node = i
			}
			if step.Run == "./scripts/ci/install-corepack.sh" {
				bootstrap = i
			}
			if step.ID == "pnpm-cache" && step.Run == "./scripts/ci/export-pnpm-cache.sh" {
				selector = i
			}
			if strings.Contains(step.Run, "pnpm install") || strings.Contains(step.Run, "build-spa.sh") {
				install = i
			}
			if strings.HasPrefix(step.Uses, "actions/cache/restore@") && strings.HasPrefix(step.With["key"], "pnpm-store-") {
				readers[name]++
				writerRestoreID = step.ID == "pnpm-store"
				if (group == "host" && job.Container.Image != "") || step.With["path"] != ".ci-cache/pnpm/store" || step.With["key"] != key || strings.TrimSpace(step.With["restore-keys"]) != prefix {
					problems = append(problems, fmt.Errorf("%s: pnpm readers must share the explicit relative store path, Node/pnpm ABI and lock/config hashes", name))
				}
				if node < 0 || bootstrap <= node || selector <= bootstrap || i <= selector {
					problems = append(problems, fmt.Errorf("%s: pinned Node and pnpm setup plus verified store selection must precede restore", name))
				}
				if install >= 0 {
					problems = append(problems, fmt.Errorf("%s: dependency installation must follow cache restore", name))
				}
			}
			if strings.HasPrefix(step.Uses, "actions/cache/save@") && (strings.HasPrefix(step.With["key"], "pnpm-store-") || strings.Contains(step.With["key"], "steps.pnpm-store.outputs.")) {
				writers[group]++
				if name != writer || !writerRestoreID || step.With["path"] != ".ci-cache/pnpm/store" || step.With["key"] != "${{ steps.pnpm-store.outputs.cache-primary-key }}" || strings.Join(strings.Fields(step.If), " ") != guard {
					problems = append(problems, fmt.Errorf("%s: only successful trusted main %s may write its group's exact-miss pnpm primary key", name, writer))
				}
			}
		}
	}
	for _, name := range []string{"app-build", "web", "storybook", "release-snapshot"} {
		if readers[name] != 1 {
			problems = append(problems, fmt.Errorf("%s: want one compatible pnpm reader, got %d", name, readers[name]))
		}
	}
	for _, group := range []string{"host", "playwright"} {
		if writers[group] != 1 {
			problems = append(problems, fmt.Errorf("want one trusted pnpm writer for %s, got %d", group, writers[group]))
		}
	}
	return problems
}

func TestPnpmReuseWorkflow(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range pnpmReuseErrors(raw) {
		t.Error(problem)
	}
}

func TestPnpmReuseWorkflowRejectsRegressions(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"home path", "missing reader", "missing selector", "missing ABI", "missing policy hash", "restore after install", "PR writer", "duplicate writer", "different image", "wrong archive group", "host in container"} {
		t.Run(name, func(t *testing.T) {
			var workflow pnpmReuseWorkflow
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			job := workflow.Jobs["app-build"]
			if name == "different image" {
				other := workflow.Jobs["web"]
				other.Container.Image = "mcr.microsoft.com/playwright:v1.62.1-noble@sha256:other"
				workflow.Jobs["web"] = other
			}
			if name == "host in container" {
				job.Container.Image = workflow.Jobs["web"].Container.Image
			}
			if name == "restore after install" {
				job.Steps = append([]sharedGoReuseStep{{Run: "pnpm install --frozen-lockfile"}}, job.Steps...)
			}
			for i, step := range job.Steps {
				if strings.HasPrefix(step.With["key"], "pnpm-store-") {
					switch name {
					case "home path":
						step.With["path"] = "~/.local/share/pnpm/store"
					case "missing reader":
						step.Uses = ""
					case "missing ABI":
						step.With["key"] = strings.ReplaceAll(step.With["key"], "${{ steps.pnpm-cache.outputs.abi }}-", "")
					case "missing policy hash":
						step.With["key"] = strings.ReplaceAll(step.With["key"], ", 'web/pnpm-workspace.yaml'", "")
					case "wrong archive group":
						step.With["key"] = strings.ReplaceAll(step.With["key"], "-host-", "-playwright-")
					}
				}
				if step.ID == "pnpm-cache" && name == "missing selector" {
					step.Run = ""
				}
				if strings.HasPrefix(step.Uses, "actions/cache/save@") && strings.Contains(step.With["key"], "steps.pnpm-store.outputs.") {
					if name == "PR writer" {
						step.If = "success()"
					}
					if name == "duplicate writer" {
						job.Steps = append(job.Steps, step)
					}
				}
				job.Steps[i] = step
			}
			workflow.Jobs["app-build"] = job
			mutated, err := yaml.Marshal(workflow)
			if err != nil {
				t.Fatal(err)
			}
			if len(pnpmReuseErrors(mutated)) == 0 {
				t.Fatal("unsafe pnpm cache regression accepted")
			}
		})
	}
}

func TestPnpmReuseEnvironmentFixture(t *testing.T) {
	output, err := exec.Command("sh", "./export-pnpm-cache_test.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("pnpm cache environment fixture failed: %v\n%s", err, output)
	}
}
