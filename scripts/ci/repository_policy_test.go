package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMainPolicyContract(t *testing.T) {
	root := repositoryRoot(t)
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatal("policy validation requires jq")
	}
	queue, err := os.ReadFile(filepath.Join(root, "release/repository/main-ci-gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"merge-linear-history": `.rules += [{type:"required_linear_history"}]`,
		"integration-string":   `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].integration_id)="15368"`,
		"integration-negative": `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].integration_id)=-1`,
		"integration-fraction": `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].integration_id)=1.5`,
		"check-extra-field":    `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].ignored)=true`,
		"rules-object":         `.rules |= (to_entries | map(.key |= tostring) | from_entries)`,
		"multiple-documents":   `(. | .rules |= map(select(.type!="required_signatures"))), .`,
		"root-array":           `[.]`,
		"rules-null":           `.rules=null`,
		"rule-scalar":          `.rules += ["required_signatures"]`,
		"legacy-context":       `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].context)="ci-required-next"`,
		"feedback-context":     `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].context)="feedback"`,
		"strict":               `(.rules[]|select(.type=="required_status_checks").parameters.strict_required_status_checks_policy)=true`,
		"bypass":               `.bypass_actors=[{actor_id:5,actor_type:"RepositoryRole",bypass_mode:"always"}]`,
		"redacted-bypass":      `del(.bypass_actors)`,
		"disabled":             `.enforcement="disabled"`,
		"wrong-ref":            `.conditions.ref_name.include=["refs/heads/other"]`,
		"no-signatures":        `.rules|=map(select(.type!="required_signatures"))`,
		"no-deletion":          `.rules|=map(select(.type!="deletion"))`,
		"no-force-protection":  `.rules|=map(select(.type!="non_fast_forward"))`,
		"unresolved":           `(.rules[]|select(.type=="pull_request").parameters.required_review_thread_resolution)=false`,
		"stale-reviews":        `(.rules[]|select(.type=="pull_request").parameters.dismiss_stale_reviews_on_push)=false`,
		"missing-queue":        `.rules|=map(select(.type!="merge_queue"))`,
		"duplicate-queue":      `.rules += [.rules[]|select(.type=="merge_queue")]`,
		"headgreen":            `(.rules[]|select(.type=="merge_queue").parameters.grouping_strategy)="HEADGREEN"`,
		"concurrency":          `(.rules[]|select(.type=="merge_queue").parameters.max_entries_to_build)=2`,
		"batch-size":           `(.rules[]|select(.type=="merge_queue").parameters.max_entries_to_merge)=2`,
		"min-size":             `(.rules[]|select(.type=="merge_queue").parameters.min_entries_to_merge)=2`,
		"wait":                 `(.rules[]|select(.type=="merge_queue").parameters.min_entries_to_merge_wait_minutes)=1`,
		"timeout":              `(.rules[]|select(.type=="merge_queue").parameters.check_response_timeout_minutes)=5`,
		"method":               `(.rules[]|select(.type=="merge_queue").parameters.merge_method)="SQUASH"`,
		"create-exemption":     `(.rules[]|select(.type=="required_status_checks").parameters.do_not_enforce_on_create)=true`,
		"extra-context":        `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks)+=[{context:"ci-required-next"}]`,
	}
	validate := filepath.Join(root, "scripts/release/check-main-policy.sh")
	for _, args := range [][]string{{filepath.Join(root, "release/repository/main-ci-gate.json")}, {"--legacy", filepath.Join(root, "release/repository/main-ci-gate-legacy.json")}} {
		if out, err := exec.Command(validate, args...).CombinedOutput(); err != nil {
			t.Fatalf("positive contract: %v %s", err, out)
		}
	}
	for name, filter := range cases {
		t.Run(name, func(t *testing.T) {
			c := exec.Command("jq", filter)
			c.Stdin = strings.NewReader(string(queue))
			mutated, err := c.Output()
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "policy.json")
			if err := os.WriteFile(file, mutated, 0600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(validate, file).CombinedOutput()
			if err == nil || !strings.Contains(string(out), "invalid queue protection contract") {
				t.Fatalf("mutation accepted or wrong refusal: %v %s", err, out)
			}
		})
	}
	// Malformed shape/stream protection also owns the legacy rollback path.
	legacy, err := os.ReadFile(filepath.Join(root, "release/repository/main-ci-gate-legacy.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rules-object", "multiple-documents", "root-array", "rules-null", "rule-scalar"} {
		c := exec.Command("jq", cases[name])
		c.Stdin = strings.NewReader(string(legacy))
		mutated, err := c.Output()
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "legacy.json")
		if err := os.WriteFile(file, mutated, 0600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(validate, "--legacy", file).CombinedOutput(); err == nil {
			t.Fatalf("malformed legacy %s accepted: %s", name, out)
		}
	}

}

// Run the actual configuration script with an API double that permits only
// owning-main reads and one atomic PUT. Any tag/security/release/git route is red.
func TestMainOnlyConfiguration(t *testing.T) {
	root := repositoryRoot(t)
	for _, scenario := range []string{"legacy", "queue", "drift", "redacted", "duplicate", "missing", "put-failure", "multiple-before", "multiple-readback", "malformed-before", "multiple-list", "integration-positive", "integration-zero", "integration-null", "readback-null", "later-page", "duplicate-later-page", "page-limit", "malformed-page", "binding-drift", "legacy-linear-history", "queue-linear-history"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			before := filepath.Join(dir, "before.json")
			policy := "main-ci-gate-legacy.json"
			if scenario == "queue" || scenario == "queue-linear-history" {
				policy = "main-ci-gate.json"
			}
			raw, err := os.ReadFile(filepath.Join(root, "release/repository", policy))
			if err != nil {
				t.Fatal(err)
			}
			// This stricter unrelated rule and PR parameter must survive unchanged.
			c := exec.Command("jq", `.rules += [{type:"commit_message_pattern",parameters:{name:"Block CI skip",negate:true,operator:"contains",pattern:"[skip ci]"}}] | (.rules[]|select(.type=="pull_request").parameters.required_approving_review_count)=2`)
			c.Stdin = strings.NewReader(string(raw))
			raw, err = c.Output()
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(scenario, "linear-history") {
				c = exec.Command("jq", `.rules += [{type:"required_linear_history"}]`)
				c.Stdin = strings.NewReader(string(raw))
				raw, err = c.Output()
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "legacy-linear-history" {
					control := filepath.Join(dir, "legacy-source.json")
					if err := os.WriteFile(control, raw, 0600); err != nil {
						t.Fatal(err)
					}
					if out, err := exec.Command(filepath.Join(root, "scripts/release/check-main-policy.sh"), "--legacy", control).CombinedOutput(); err != nil {
						t.Fatalf("valid legacy linear-history source refused: %v %s", err, out)
					}
				}
			}

			if strings.HasPrefix(scenario, "integration-") || scenario == "binding-drift" {
				id := "15368"
				if scenario == "integration-zero" {
					id = "0"
				}
				if scenario == "integration-null" {
					id = "null"
				}
				c = exec.Command("jq", `(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].integration_id)=`+id)
				c.Stdin = strings.NewReader(string(raw))
				raw, err = c.Output()
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "malformed-before" {
				c = exec.Command("jq", `.rules |= (to_entries | map(.key |= tostring) | from_entries)`)
				c.Stdin = strings.NewReader(string(raw))
				raw, err = c.Output()
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "multiple-before" {
				raw = append(raw, raw...)
			}
			if scenario == "redacted" {
				c = exec.Command("jq", "del(.bypass_actors)")
				c.Stdin = strings.NewReader(string(raw))
				raw, err = c.Output()
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(before, raw, 0600); err != nil {
				t.Fatal(err)
			}
			mock := filepath.Join(dir, "gh")
			script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >>"$FIXTURE/calls"
case "$*" in
 'api repos/example/repository/rulesets?per_page=100&page='*)
  page=${2##*=}
  case "$SCENARIO:$page" in
   later-page:1|duplicate-later-page:1|page-limit:*)
    if [ "$SCENARIO" = duplicate-later-page ]; then
     jq -n '[{name:"main requires PR and release CI",id:1}] + [range(1;100)|{name:"other",id:(. + 10)}]'
    else jq -n '[range(1;101)|{name:"other",id:(. + 10)}]'; fi ;;
   later-page:2) printf '[{"name":"main requires PR and release CI","id":1}]' ;;
   duplicate-later-page:2) printf '[{"name":"main requires PR and release CI","id":2}]' ;;
   multiple-list:*) printf '[]\n[{"name":"main requires PR and release CI","id":1}]' ;;
   malformed-page:*) printf '{"unexpected":"object"}' ;;
   duplicate:*) printf '[{"name":"main requires PR and release CI","id":1},{"name":"main requires PR and release CI","id":2}]' ;;
   missing:*) printf '[]' ;;
   *) printf '[{"name":"main requires PR and release CI","id":1}]' ;;
  esac ;;
 'api repos/example/repository/rulesets')
  case "$SCENARIO" in
   later-page) printf '[]' ;;
   multiple-list) printf '[]\n[{"name":"main requires PR and release CI","id":1}]' ;;
   duplicate) printf '[{"name":"main requires PR and release CI","id":1},{"name":"main requires PR and release CI","id":2}]' ;;
   missing) printf '[]' ;;
   *) printf '[{"name":"main requires PR and release CI","id":1}]' ;;
  esac ;;
 'api repos/example/repository/rulesets/1')
  if [ -f "$FIXTURE/submitted.json" ]; then
   if [ "$SCENARIO" = multiple-readback ]; then cat "$FIXTURE/submitted.json" "$FIXTURE/submitted.json"
   elif [ "$SCENARIO" = binding-drift ]; then jq '(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].integration_id)=0' "$FIXTURE/submitted.json"
   elif [ "$SCENARIO" = readback-null ]; then jq '(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0].integration_id)=null' "$FIXTURE/submitted.json"
   elif [ "$SCENARIO" = drift ]; then jq '(.rules[]|select(.type=="pull_request").parameters.required_approving_review_count)=0' "$FIXTURE/submitted.json"
   else cat "$FIXTURE/submitted.json"; fi
  else cat "$FIXTURE/before.json"; fi ;;
 'api --method PUT repos/example/repository/rulesets/1 --input '*)
  [ "$SCENARIO" != put-failure ] || exit 1
  cp "$6" "$FIXTURE/submitted.json" ;;
 *) printf 'unexpected API route: %s\n' "$*" >&2; exit 1 ;;
esac
`
			if err := os.WriteFile(mock, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(filepath.Join(root, "scripts/release/configure-repository.sh"), "--main-only", "example/repository")
			cmd.Env = append(os.Environ(), "GH_BIN="+mock, "FIXTURE="+dir, "SCENARIO="+scenario)
			out, err := cmd.CombinedOutput()
			pass := scenario == "legacy" || scenario == "queue" || strings.HasPrefix(scenario, "integration-") || scenario == "readback-null" || scenario == "later-page"
			if pass != (err == nil) {
				t.Fatalf("scenario result %v: %s", err, out)
			}
			submitted, readErr := os.ReadFile(filepath.Join(dir, "submitted.json"))
			if pass {
				if readErr != nil {
					t.Fatal(readErr)
				}
				check := exec.Command("jq", "-e", "--slurpfile", "before", before, `
					(.rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0]) as $check |
					$check == ($before[0].rules[]|select(.type=="required_status_checks").parameters.required_status_checks[0]|.context="ci-queue-required") and
					(.rules|any(.type=="commit_message_pattern" and .parameters=={name:"Block CI skip",negate:true,operator:"contains",pattern:"[skip ci]"}) and any(.type=="pull_request" and .parameters.required_approving_review_count==2))`)
				check.Stdin = strings.NewReader(string(submitted))
				if out, err := check.CombinedOutput(); err != nil {
					t.Fatalf("unrelated protection lost: %v %s", err, out)
				}
				calls, err := os.ReadFile(filepath.Join(dir, "calls"))
				if err != nil {
					t.Fatal(err)
				}
				got := strings.Split(strings.TrimSuffix(string(calls), "\n"), "\n")
				want := []string{"api repos/example/repository/rulesets?per_page=100&page=1"}
				if scenario == "later-page" {
					want = append(want, "api repos/example/repository/rulesets?per_page=100&page=2")
				}
				get := "api repos/example/repository/rulesets/1"
				put := "api --method PUT repos/example/repository/rulesets/1 --input "
				if len(got) != len(want)+3 {
					t.Fatalf("unexpected route count: %s", calls)
				}
				payload, found := strings.CutPrefix(got[len(want)+1], put)
				if !found || filepath.Base(payload) != "proposed.json" {
					t.Fatalf("invalid atomic PUT route: %s", calls)
				}
				want = append(want, get, put+payload, get)
				if !slices.Equal(got, want) {
					t.Fatalf("unexpected ordered mutation/readback routes: %s", calls)
				}

			} else if scenario != "drift" && scenario != "multiple-readback" && scenario != "binding-drift" && readErr == nil {
				t.Fatal("unsafe source reached policy write")
			}
		})
	}
}
