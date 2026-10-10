package ci_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type webFlowMatrixWorkflow struct {
	Jobs map[string]struct {
		Strategy struct {
			FailFast bool `yaml:"fail-fast"`
			Matrix   struct {
				Projects []string `yaml:"project"`
				Groups   []int    `yaml:"group"`
				Includes []struct {
					Group   int    `yaml:"group"`
					Project string `yaml:"project"`
					Specs   string `yaml:"specs"`
				} `yaml:"include"`
				Excludes []any `yaml:"exclude"`
			} `yaml:"matrix"`
		} `yaml:"strategy"`
		Steps []sharedGoCacheStep `yaml:"steps"`
	} `yaml:"jobs"`
}

func webFlowMatrixErrors(raw []byte, inventory []string) []error {
	var workflow webFlowMatrixWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return []error{err}
	}
	web := workflow.Jobs["web"]
	matrix := web.Strategy.Matrix
	var problems []error
	if web.Strategy.FailFast || !slices.Equal(matrix.Projects, []string{"desktop", "mobile"}) || len(matrix.Groups) != 4 || len(matrix.Excludes) != 0 {
		problems = append(problems, fmt.Errorf("web flow matrix must retain both viewports, four independent groups and no excluded legs"))
	}
	groups := make(map[int]bool)
	for _, group := range matrix.Groups {
		if group <= 0 || groups[group] {
			problems = append(problems, fmt.Errorf("invalid or duplicate web group %d", group))
		}
		groups[group] = true
	}
	actual := make(map[string]int)
	defined := make(map[int]bool)
	for _, entry := range matrix.Includes {
		if !groups[entry.Group] || defined[entry.Group] || entry.Project != "" || len(strings.Fields(entry.Specs)) == 0 {
			problems = append(problems, fmt.Errorf("web group %d must define one complete positional spec list for both viewports", entry.Group))
		}
		defined[entry.Group] = true
		for _, spec := range strings.Fields(entry.Specs) {
			actual[spec]++
		}
	}
	for group := range groups {
		if !defined[group] {
			problems = append(problems, fmt.Errorf("web group %d has no spec list", group))
		}
	}
	for _, spec := range inventory {
		if actual[spec] != 1 {
			problems = append(problems, fmt.Errorf("%s must execute exactly once on each viewport, appears %d times", spec, actual[spec]))
		}
		delete(actual, spec)
	}
	for spec := range actual {
		problems = append(problems, fmt.Errorf("web matrix contains an unknown spec or filter: %s", spec))
	}
	flows, epoch := 0, 0
	for _, step := range web.Steps {
		if strings.Contains(step.Run, "playwright test --project=") {
			flows++
			if step.Run != "pnpm exec playwright test --project=${{ matrix.project }} ${{ matrix.specs }}" {
				problems = append(problems, fmt.Errorf("browser legs must execute their complete positional spec list"))
			}
		}
		if step.Run == "pnpm exec playwright test --config e2e/session-epoch.config.ts" {
			epoch++
			if step.If != "matrix.project == 'desktop' && matrix.group == 1" {
				problems = append(problems, fmt.Errorf("cross-tab session epoch regression must remain on one desktop leg"))
			}
		}
	}
	if flows != 1 || epoch != 1 {
		problems = append(problems, fmt.Errorf("web matrix must retain one flow command and one cross-tab epoch command"))
	}
	return problems
}

func webFlowInventory(t *testing.T) []string {
	t.Helper()
	var inventory []string
	// Playwright's flow testDir is separate from the epoch, Storybook and
	// performance configs. Discover runnable files recursively rather than
	// pinning the current twelve specs or one particular balanced partition.
	pattern := regexp.MustCompile(`\.(?:spec|test)\.(?:[cm]?[jt]s)x?$`)
	err := filepath.WalkDir("../../web/e2e/flows", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && pattern.MatchString(entry.Name()) {
			inventory = append(inventory, strings.TrimPrefix(filepath.ToSlash(path), "../../web/"))
		}
		return nil
	})
	if err != nil || len(inventory) == 0 {
		t.Fatalf("flow inventory unavailable: %v", err)
	}
	return inventory
}

func TestWebFlowMatrixWorkflow(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range webFlowMatrixErrors(raw, webFlowInventory(t)) {
		t.Error(problem)
	}
	config, err := os.ReadFile("../../web/playwright.config.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, requirement := range []string{"testDir: './e2e/flows'", "fullyParallel: false", "retries: 0", "workers: 1"} {
		if !strings.Contains(string(config), requirement) {
			t.Errorf("flow harness must retain %s", requirement)
		}
	}
}

func TestWebFlowMatrixWorkflowRejectsCoverageRegression(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	inventory := webFlowInventory(t)
	for _, name := range []string{"omitted spec", "duplicate spec", "unknown spec", "new source spec", "omitted viewport", "extra jobs", "excluded leg", "filtered suite", "missing epoch"} {
		t.Run(name, func(t *testing.T) {
			var workflow webFlowMatrixWorkflow
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			web := workflow.Jobs["web"]
			wantInventory := inventory
			switch name {
			case "omitted spec":
				web.Strategy.Matrix.Includes[0].Specs = strings.Join(strings.Fields(web.Strategy.Matrix.Includes[0].Specs)[1:], " ")
			case "duplicate spec":
				web.Strategy.Matrix.Includes[1].Specs += " " + strings.Fields(web.Strategy.Matrix.Includes[0].Specs)[0]
			case "unknown spec":
				web.Strategy.Matrix.Includes[0].Specs += " e2e/flows/unknown.spec.ts"
			case "new source spec":
				wantInventory = append(slices.Clone(inventory), "e2e/flows/new.spec.ts")
			case "omitted viewport":
				web.Strategy.Matrix.Projects = []string{"desktop"}
			case "extra jobs":
				web.Strategy.Matrix.Groups = append(web.Strategy.Matrix.Groups, 5)
			case "excluded leg":
				web.Strategy.Matrix.Excludes = []any{map[string]any{"project": "mobile", "group": 1}}
			case "filtered suite", "missing epoch":
				for i, step := range web.Steps {
					if name == "filtered suite" && strings.Contains(step.Run, "playwright test --project=") {
						web.Steps[i].Run += " --grep only-one-flow"
					}
					if name == "missing epoch" && strings.Contains(step.Run, "session-epoch.config.ts") {
						web.Steps[i].Run = ""
					}
				}
			}
			workflow.Jobs["web"] = web
			mutated, err := yaml.Marshal(workflow)
			if err != nil {
				t.Fatal(err)
			}
			if len(webFlowMatrixErrors(mutated, wantInventory)) == 0 {
				t.Fatal("browser coverage regression accepted")
			}
		})
	}
}
