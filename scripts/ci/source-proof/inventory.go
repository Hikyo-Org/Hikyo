package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

var regexpQueue = regexp.MustCompile(`^gh-readonly-queue/main/pr-[1-9][0-9]*-[0-9a-f]{40}$`)

type step struct {
	Name        string `json:"name"`
	Number      int    `json:"number"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

type job struct {
	ID          int64  `json:"id"`
	RunID       int64  `json:"run_id"`
	Attempt     int64  `json:"run_attempt"`
	HeadSHA     string `json:"head_sha"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	RunnerName  string `json:"runner_name"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	Steps       []step `json:"steps"`
}

func (r *resolver) jobs(ctx context.Context, source run) ([]job, error) {
	var all []job
	seen := map[int64]bool{}
	total := -1
	for page := 1; page <= maxPages; page++ {
		var response struct {
			Total int   `json:"total_count"`
			Jobs  []job `json:"jobs"`
		}
		endpoint := fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=%d", source.ID, source.Attempt, page)
		if err := r.api.read(ctx, r.endpoint(endpoint), &response); err != nil {
			return nil, err
		}
		if response.Jobs == nil || response.Total < 0 || response.Total > maxPages*100 || len(response.Jobs) > 100 || (total >= 0 && total != response.Total) {
			return nil, errors.New("incomplete or changing full-validation job pagination")
		}
		total = response.Total
		for _, current := range response.Jobs {
			if current.ID <= 0 || seen[current.ID] || current.RunID != source.ID || current.Attempt != source.Attempt || current.HeadSHA != source.HeadSHA {
				return nil, errors.New("job identity does not bind the selected source run and attempt")
			}
			seen[current.ID] = true
			all = append(all, current)
		}
		if len(all) > total || (len(response.Jobs) < 100 && len(all) != total) {
			return nil, errors.New("job listing does not conserve total_count")
		}
		if len(all) == total {
			return all, nil
		}
	}
	return nil, errors.New("full-validation job history exceeds complete discovery bound")
}

var fullDomains = []string{
	"client", "compose-demo", "docs", "floor-bench", "freeze-guard", "fuzz",
	"generated", "headline-guarantee", "k8s-e2e", "lint", "race", "release-snapshot",
	"supply-chain-checks", "test", "web", "web-go",
}

var singletonJobs = map[string][]string{
	"changes":                  {"Classify changed paths"},
	"lint":                     {"Shellcheck (pinned archive)", "Actionlint", "Trusted release admission contract fixtures"},
	"preflight":                {"Format", "Go import formatting", "Generated code is fresh", "Operator CRDs and deepcopy are fresh"},
	"supply-chain-checks":      {"Release trust and refusal fixtures", "CI job registry and workflow bijection fixture", "Nightly release fixture"},
	"app-build":                {"Verify and build the SPA", "Build the release-shaped app"},
	"release-snapshot":         {"Verify every native package payload", "Smoke the exact candidate image", "Classify complete snapshot manifest"},
	"client":                   {"Typecheck", "Frozen generated client against current server, both engines"},
	"docs":                     {"Typecheck, build, and assert O4-O6 policy content"},
	"compose-demo":             {"Real Compose delivery round-trip"},
	"web-closure":              {"Enforce flow-registry closure over the merged log"},
	"storybook":                {"Storybook story tests (dark, the default)", "Storybook story tests (light)", "Storybook production build"},
	"web-go":                   {"govulncheck (vulnerabilities reachable from the release-shaped binary)", "Go tests (transport, serving rules, browser session)", "Boot+idle originates zero outbound connections"},
	"source-artifact-security": {"Native artifact adversarial campaign on both datastores"},
	"test_core":                {"Build", "Vet", "Test all packages except the sharded isolation suite", "Exercise every benchmark once", "MCP 2026-07-28 upstream conformance"},
	"k8s-e2e":                  {"Operator kind e2e"},
	"analysis_shards":          {"Plan complete test, race, and fuzz shards"},
	"headline-guarantee":       {"Container round-trip, stanza exclusivity and truncation refusal"},
	"ci-required":              {"Require every validation job"},
}

func validateRegistry(data []byte) error {
	var registry struct {
		Version  int                 `json:"version"`
		Workflow string              `json:"workflow"`
		Classes  map[string][]string `json:"path_classes"`
		Jobs     map[string]struct {
			Gate string   `json:"required_gate"`
			Plan []string `json:"plan_jobs"`
		} `json:"jobs"`
	}
	if err := decode(data, &registry); err != nil {
		return err
	}
	if registry.Version != 2 || registry.Workflow != ".github/workflows/ci.yml" || !reflect.DeepEqual(registry.Classes["full"], fullDomains) {
		return errors.New("unsupported full-validation registry schema or domains")
	}
	expected := make([]string, 0, len(singletonJobs)+6)
	for name := range singletonJobs {
		expected = append(expected, name)
	}
	expected = append(expected, "floor-bench", "web", "scan-xplat", "isolation_shard", "race_shard", "fuzz_shard")
	actual := make([]string, 0, len(registry.Jobs))
	for name, rule := range registry.Jobs {
		actual = append(actual, name)
		if rule.Gate == "" {
			return errors.New("incomplete validation registry gate")
		}
	}
	sort.Strings(expected)
	sort.Strings(actual)
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("unsupported full-validation job inventory")
	}
	return nil
}

type expectedJob struct {
	Guard string
	Steps []string
}

func expectedJobs(selected selectedRun) map[string]expectedJob {
	prefix := "validation / "
	if selected.producer.Legacy {
		prefix = ""
	}
	wanted := map[string]expectedJob{}
	guard := "Validate the immutable caller revision"
	for name, steps := range singletonJobs {
		wanted[prefix+name] = expectedJob{Guard: guard, Steps: steps}
	}
	wanted[prefix+"floor-bench / measure"] = expectedJob{Guard: "Validate the immutable caller or tag revision", Steps: []string{"Measure and enforce the declared floor"}}
	for _, platform := range []string{"macos-latest", "windows-latest"} {
		wanted[prefix+"scan-xplat ("+platform+")"] = expectedJob{Guard: guard, Steps: []string{"Scanner tests"}}
	}
	for _, project := range []string{"desktop", "mobile"} {
		for group := 1; group <= 4; group++ {
			steps := []string{fmt.Sprintf("Flow suite (%s group %d)", project, group)}
			if project == "desktop" && group == 1 {
				steps = append(steps, "Cross-tab session ownership regression", "Authenticated browser security campaign and oracle controls")
			}
			wanted[fmt.Sprintf("%sweb (%s, %d)", prefix, project, group)] = expectedJob{Guard: guard, Steps: steps}
		}
	}
	for shard := 0; shard < 3; shard++ {
		wanted[fmt.Sprintf("%sisolation shard (%d)", prefix, shard)] = expectedJob{Guard: guard, Steps: []string{"Test assigned isolation cases on SQLite and PostgreSQL"}}
		wanted[fmt.Sprintf("%sfuzz shard (%d)", prefix, shard)] = expectedJob{Guard: guard, Steps: []string{"Fuzz every target for bounded work"}}
	}
	for shard := 0; shard < 12; shard++ {
		wanted[fmt.Sprintf("%srace shard (%d)", prefix, shard)] = expectedJob{Guard: guard, Steps: []string{"Race detector (all packages except the isolation suite)"}}
	}
	if selected.producer.Queue {
		wanted["merge-group-proof"] = expectedJob{Steps: []string{"Require trusted validation"}}
	}
	return wanted
}

func validateJobs(selected selectedRun, jobs []job) error {
	wanted := expectedJobs(selected)
	seen := map[string]bool{}
	for _, current := range jobs {
		if (current.Name == "ci-queue-required" || current.Name == "ci-dev-required") && current.RunnerName == "" && len(current.Steps) == 0 {
			// The Actions API can include the trusted publisher's synthetic
			// record in this check suite. It supplies no execution evidence.
			// A backmerge publishes the dev check on main's tip.
			continue
		}
		expected, ok := wanted[current.Name]
		if !ok || seen[current.Name] {
			return fmt.Errorf("unexpected or duplicated full-validation job %q", current.Name)
		}
		seen[current.Name] = true
		if current.Status != "completed" || current.Conclusion != "success" || strings.TrimSpace(current.RunnerName) == "" || !validInterval(current.StartedAt, current.CompletedAt) {
			return fmt.Errorf("full-validation job did not execute successfully: %s", current.Name)
		}
		executed := map[string]bool{}
		names := map[string]bool{}
		numbers := map[int]bool{}
		for _, step := range current.Steps {
			if step.Number <= 0 || numbers[step.Number] || step.Name == "" || names[step.Name] {
				return errors.New("invalid or duplicate source job step")
			}
			names[step.Name] = true
			numbers[step.Number] = true
			if step.Status != "completed" || (step.Conclusion != "success" && step.Conclusion != "skipped") {
				return errors.New("incomplete or failed full-validation step")
			}
			if step.Conclusion == "success" {
				if !validInterval(step.StartedAt, step.CompletedAt) {
					return errors.New("successful job step lacks execution timestamps")
				}
				executed[step.Name] = true
			}
		}
		steps := append([]string{}, expected.Steps...)
		if expected.Guard != "" {
			steps = append(steps, expected.Guard)
		}
		for _, required := range steps {
			if !executed[required] {
				return fmt.Errorf("full-validation execution is missing %s / %s", current.Name, required)
			}
		}
	}
	if len(seen) != len(wanted) {
		return errors.New("full-validation job closure is incomplete; selective proof cannot qualify")
	}
	return nil
}

func validInterval(start, end string) bool {
	a, err := time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return false
	}
	b, err := time.Parse(time.RFC3339Nano, end)
	return err == nil && !a.IsZero() && !b.IsZero() && !b.Before(a)
}
