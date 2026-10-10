package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Cache-writable main contexts must never select a PR-controlled checkout.
func TestValidationCheckoutWorkflowPolicy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var ci mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &ci); err != nil {
		t.Fatal(err)
	}
	fullHistory := map[string]bool{"changes": true, "preflight": true, "client": true, "analysis_shards": true, "race_shard": true, "ci-required": true}
	for name, job := range ci.Jobs {
		if job.Uses != "" {
			continue
		}
		var checkouts []mainValidationStep
		var refusal mainValidationStep
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/checkout@") {
				checkouts = append(checkouts, step)
			}
			if step.Name == "Refuse unsupported validation context" {
				refusal = step
			}
		}
		if len(checkouts) != 2 {
			t.Fatalf("%s must separate PR and trusted-source checkouts, got %d", name, len(checkouts))
		}
		pr, trusted := checkouts[0], checkouts[1]
		if pr.If != "github.event_name == 'pull_request'" || pr.With["ref"] != "${{ github.event.pull_request.head.sha }}" || trusted.With["ref"] != "${{ github.sha }}" {
			t.Fatalf("%s must keep exact PR and non-PR revisions separate", name)
		}
		prOptions, trustedOptions := make(map[string]string), make(map[string]string)
		for key, value := range pr.With {
			if key != "ref" {
				prOptions[key] = value
			}
		}
		for key, value := range trusted.With {
			if key != "ref" {
				trustedOptions[key] = value
			}
		}
		if !reflect.DeepEqual(prOptions, trustedOptions) || prOptions["persist-credentials"] != "false" || pr.Uses != trusted.Uses {
			t.Fatalf("%s checkouts must preserve the same pinned action and credential/history options", name)
		}
		if fullHistory[name] && prOptions["fetch-depth"] != "0" {
			t.Fatalf("%s must retain complete history", name)
		}
		if name == "changes" || name == "ci-required" {
			if refusal.Run == "" || len(job.Steps) == 0 || job.Steps[0].Name != refusal.Name {
				t.Fatalf("%s must refuse unsupported contexts before any checkout or execution", name)
			}
		}
		for _, event := range []string{"pull_request", "pull_request_target", "merge_group", "workflow_dispatch", "schedule", "repository_dispatch", "push", "workflow_run"} {
			for _, ref := range []string{"refs/heads/main", "refs/heads/feature"} {
				t.Run(name+"/"+event+"/"+ref, func(t *testing.T) {
					// Evaluate the actual YAML conditions with metadata in the
					// environment, never interpolating an executable event value.
					matches := func(condition string) bool {
						t.Helper()
						condition = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(condition), "${{"), "}}"))
						condition = strings.NewReplacer("github.event_name", `"${FIXTURE_EVENT}"`, "github.ref", `"${FIXTURE_REF}"`).Replace(condition)
						cmd := exec.Command("bash", "-e", "-c", "if [[ "+condition+" ]]; then exit 0; else exit 1; fi")
						cmd.Env = append(os.Environ(), "FIXTURE_EVENT="+event, "FIXTURE_REF="+ref)
						out, err := cmd.CombinedOutput()
						if err != nil && len(out) > 0 {
							t.Fatalf("invalid checkout condition: %v %s", err, out)
						}
						return err == nil
					}
					prRuns, trustedRuns := matches(pr.If), matches(trusted.If)
					wantPR := event == "pull_request"
					wantTrusted := event == "merge_group" || (ref == "refs/heads/main" && (event == "workflow_dispatch" || event == "schedule" || event == "repository_dispatch"))
					if prRuns != wantPR || trustedRuns != wantTrusted || (prRuns && trustedRuns) {
						t.Fatalf("checkout selection PR=%v trusted=%v; want PR=%v trusted=%v", prRuns, trustedRuns, wantPR, wantTrusted)
					}
					if refusal.Run != "" {
						if matches(refusal.If) != (!wantPR && !wantTrusted) {
							t.Fatal("unsupported-context refusal disagrees with selected checkout")
						}
						if !wantPR && !wantTrusted {
							cmd := exec.Command("bash", "-e", "-c", refusal.Run)
							if cmd.Run() == nil {
								t.Fatal("unsupported-context refusal did not fail before execution")
							}
						}
					}
				})
			}
		}
	}
}
