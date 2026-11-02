package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevProtectionRejectsWeakenedOrQueuePolicy(t *testing.T) {
	root := repositoryRoot(t)
	policy := filepath.Join(root, "release/repository/dev-ci-gate.json")
	checker := filepath.Join(root, "scripts/release/check-dev-policy.sh")
	if out, err := exec.Command(checker, policy).CombinedOutput(); err != nil {
		t.Fatalf("valid dev protection rejected: %v %s", err, out)
	}
	for name, mutation := range map[string]string{
		"main target":            `.conditions.ref_name.include=["refs/heads/main"]`,
		"queue":                  `.rules += [{type:"merge_queue",parameters:{}}]`,
		"main admission":         `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].context)="ci-queue-required"`,
		"native feedback":        `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].context)="feedback (go)"`,
		"missing signature rule": `.rules |= map(select(.type!="required_signatures"))`,
		"missing PR rule":        `.rules |= map(select(.type!="pull_request"))`,
		"force push allowed":     `.rules |= map(select(.type!="non_fast_forward"))`,
		"deletion allowed":       `.rules |= map(select(.type!="deletion"))`,
		"bypass":                 `.bypass_actors=[{actor_id:5,actor_type:"RepositoryRole",bypass_mode:"always"}]`,
		"hidden bypass":          `del(.bypass_actors)`,
		"stale approval":         `(.rules[]|select(.type=="pull_request").parameters.dismiss_stale_reviews_on_push)=false`,
		"squash destroys DCO":    `(.rules[]|select(.type=="pull_request").parameters.allowed_merge_methods)=["merge","squash"]`,
		"unresolved threads":     `(.rules[]|select(.type=="pull_request").parameters.required_review_thread_resolution)=false`,
		"bootstrap":              `(.rules[]|select(.type=="required_status_checks").parameters.do_not_enforce_on_create)=true`,
		"strict rebase":          `(.rules[]|select(.type=="required_status_checks").parameters.strict_required_status_checks_policy)=true`,
		"duplicate context":      `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks)+=[{context:"ci-dev-required"}]`,
		"multiple documents":     `., .`,
		"malformed rules":        `.rules={}`,
	} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("jq", mutation, policy)
			data, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "mutated.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(checker, path).CombinedOutput(); err == nil || !strings.Contains(string(out), "invalid protection contract") {
				t.Fatalf("unsafe dev policy accepted or wrong refusal: %v %s", err, out)
			}
		})
	}
}

func TestDevActivationAndDiscoveryFixtures(t *testing.T) {
	root := repositoryRoot(t)
	for _, path := range []string{"scripts/release/configure-dev_test.sh", "scripts/ci/discover-dev-prs_test.sh"} {
		t.Run(path, func(t *testing.T) {
			if out, err := exec.Command("bash", filepath.Join(root, path)).CombinedOutput(); err != nil {
				t.Fatalf("dev fixture failed: %v\n%s", err, out)
			}
		})
	}
}
