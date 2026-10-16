package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeAPI struct {
	run                             run
	reads, snapshots, refs, cancels int
	readOverride                    func(string, int) (any, bool)
	rolesOverride                   func(int) any
	refOverride                     func(int) (string, error)
	finish                          bool
	conclusion                      string
}

func decode(value, target any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func (a *fakeAPI) read(_ context.Context, path string, target any) error {
	if strings.Contains(path, "/actions/runs/") {
		a.reads++
	}
	if a.readOverride != nil {
		if value, ok := a.readOverride(path, a.reads); ok {
			return decode(value, target)
		}
	}
	if strings.HasPrefix(path, "graphql:") {
		a.snapshots++
		if a.rolesOverride != nil {
			return decode(a.rolesOverride(a.snapshots), target)
		}
		return decode(roleFixture(), target)
	}
	if strings.Contains(path, "/runs?") {
		runs := []run{}
		if strings.Contains(path, "status=queued&") {
			runs = append(runs, a.run)
		}
		return decode(map[string]any{"total_count": len(runs), "workflow_runs": runs}, target)
	}
	if strings.Contains(path, "/actions/runs/") {
		r := a.run
		if a.cancels > 0 && a.finish {
			r.Status = "completed"
			r.Conclusion = &a.conclusion
		}
		return decode(r, target)
	}
	return fmt.Errorf("unexpected fixture path: %s", path)
}

func (a *fakeAPI) cancel(_ context.Context, id int64) error {
	if id != a.run.ID {
		return errors.New("cancelled another run")
	}
	a.cancels++
	return nil
}
func (a *fakeAPI) ref(_ context.Context, _ string) (string, error) {
	a.refs++
	if a.refOverride != nil {
		return a.refOverride(a.refs)
	}
	return "", nil
}

func roleFixture() map[string]any {
	return map[string]any{"data": map[string]any{"repository": map[string]any{
		"databaseId":       7,
		"defaultBranchRef": map[string]any{"name": "main", "target": map[string]any{"oid": strings.Repeat("b", 40)}},
		"pullRequests":     map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}},
		"mergeQueue":       map[string]any{"id": "QUEUE", "entries": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}},
	}}}
}
func repoNode(f map[string]any) map[string]any {
	return f["data"].(map[string]any)["repository"].(map[string]any)
}
func queueEntries(f map[string]any) map[string]any {
	return repoNode(f)["mergeQueue"].(map[string]any)["entries"].(map[string]any)
}
func pullNodes(f map[string]any) map[string]any { return repoNode(f)["pullRequests"].(map[string]any) }

func fixture() (*sweeper, *fakeAPI) {
	a := &fakeAPI{finish: true, conclusion: "cancelled"}
	a.run = run{ID: 41, Attempt: 1, WorkflowID: 12, Name: "trusted-merge-ci", Path: workflowPath, Event: "merge_group", Head: strings.Repeat("a", 40), Branch: "gh-readonly-queue/main/pr-3-a", Status: "queued"}
	a.run.Repository.ID, a.run.Repository.Name = 7, "Hikyo-Org/Hikyo"
	a.run.HeadRepository = a.run.Repository
	s := &sweeper{api: a, repo: "Hikyo-Org/Hikyo", repoID: 7, branch: "main", workflowID: 12, wait: func(context.Context) error { return nil }}
	return s, a
}

func queueEntry(head string) map[string]any {
	return map[string]any{"id": "ENTRY", "position": 1, "state": "AWAITING_CHECKS", "headCommit": map[string]any{"oid": head}, "baseCommit": map[string]any{"oid": strings.Repeat("c", 40)}, "pullRequest": map[string]any{"number": 3, "headRefOid": strings.Repeat("d", 40), "baseRefName": "main"}}
}

func TestRetirementProtectsAllLiveRolesAndAuthenticatesObsoleteRun(t *testing.T) {
	for _, scenario := range []string{"obsolete", "moved-ref", "main", "open-pr", "queue-head", "queue-base", "queue-pr", "present-ref", "ref-restored", "role-restored", "dry-run"} {
		t.Run(scenario, func(t *testing.T) {
			s, a := fixture()
			a.rolesOverride = func(n int) any {
				f := roleFixture()
				switch scenario {
				case "main":
					repoNode(f)["defaultBranchRef"] = map[string]any{"name": "main", "target": map[string]any{"oid": a.run.Head}}
				case "open-pr":
					pullNodes(f)["nodes"] = []any{map[string]any{"number": 3, "headRefOid": a.run.Head, "baseRefName": "another-target"}}
				case "queue-head", "queue-base", "queue-pr", "role-restored":
					if scenario == "role-restored" && n == 1 {
						return f
					}
					e := queueEntry(a.run.Head)
					if scenario == "queue-base" {
						e["headCommit"] = map[string]any{"oid": strings.Repeat("e", 40)}
						e["baseCommit"] = map[string]any{"oid": a.run.Head}
					}
					if scenario == "queue-pr" {
						e["headCommit"] = map[string]any{"oid": strings.Repeat("e", 40)}
						e["pullRequest"] = map[string]any{"number": 3, "headRefOid": a.run.Head, "baseRefName": "main"}
					}
					queueEntries(f)["nodes"] = []any{e}
				}
				return f
			}
			a.refOverride = func(n int) (string, error) {
				if scenario == "present-ref" || (scenario == "ref-restored" && n > 1) {
					return a.run.Head, nil
				}
				if scenario == "moved-ref" {
					return strings.Repeat("e", 40), nil
				}
				return "", nil
			}
			s.dryRun = scenario == "dry-run"
			if err := s.sweep(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := 0
			if scenario == "obsolete" || scenario == "moved-ref" {
				want = 1
			}
			if a.cancels != want {
				t.Fatalf("cancels=%d want=%d", a.cancels, want)
			}
			if want == 1 && (a.snapshots != 2 || a.refs != 2 || a.reads != 3) {
				t.Fatalf("missing final authority or terminal readback: %+v", a)
			}
		})
	}
}

func TestRetirementRefusesForeignOrAmbiguousRunIdentity(t *testing.T) {
	for _, scenario := range []string{"workflow", "event", "path", "name", "repository", "head-repository", "branch", "sha", "attempt", "missing-count", "missing-runs", "too-many", "partial-page", "wrong-status", "has-conclusion", "same-status duplicate"} {
		t.Run(scenario, func(t *testing.T) {
			s, a := fixture()
			switch scenario {
			case "workflow":
				a.run.WorkflowID++
			case "event":
				a.run.Event = "push"
			case "path":
				a.run.Path = ".github/workflows/security-release.yml"
			case "name":
				a.run.Name = "ci"
			case "repository":
				a.run.Repository.ID++
			case "head-repository":
				a.run.HeadRepository.ID++
			case "branch":
				a.run.Branch = "main"
			case "sha":
				a.run.Head = "bad"
			case "attempt":
				a.run.Attempt = 0
			case "wrong-status":
				a.run.Status = "completed"
			case "has-conclusion":
				a.run.Conclusion = &a.conclusion
			default:
				a.readOverride = func(path string, _ int) (any, bool) {
					if !strings.Contains(path, "/runs?") {
						return nil, false
					}
					switch scenario {
					case "missing-count":
						return map[string]any{"workflow_runs": []run{}}, true
					case "missing-runs":
						return map[string]any{"total_count": 0}, true
					case "too-many":
						return map[string]any{"total_count": 1001, "workflow_runs": []run{}}, true
					case "partial-page":
						return map[string]any{"total_count": 2, "workflow_runs": []run{a.run}}, true
					case "same-status duplicate":
						return map[string]any{"total_count": 2, "workflow_runs": []run{a.run, a.run}}, true
					}
					return nil, false
				}
			}
			if err := s.sweep(context.Background()); err == nil {
				t.Fatal("ambiguous identity accepted")
			}
			if a.cancels != 0 {
				t.Fatal("foreign run cancelled")
			}
		})
	}
}

func TestRetirementRefusesIncompleteOrMalformedRoles(t *testing.T) {
	for _, scenario := range []string{"graphql-partial", "missing-repository", "wrong-repository", "missing-main", "missing-queue", "missing-pulls", "missing-entries", "missing-page-info", "missing-cursor", "repeated-cursor", "duplicate-pull", "duplicate-entry", "position-gap", "invalid-group", "invalid-state", "wrong-base", "role-churn"} {
		t.Run(scenario, func(t *testing.T) {
			s, a := fixture()
			a.rolesOverride = func(n int) any {
				f := roleFixture()
				switch scenario {
				case "graphql-partial":
					f["errors"] = []any{map[string]any{"message": "partial failure"}}
				case "missing-repository":
					f["data"] = map[string]any{}
				case "wrong-repository":
					repoNode(f)["databaseId"] = 8
				case "missing-main":
					repoNode(f)["defaultBranchRef"] = nil
				case "missing-queue":
					repoNode(f)["mergeQueue"] = nil
				case "missing-pulls":
					pullNodes(f)["nodes"] = nil
				case "missing-entries":
					queueEntries(f)["nodes"] = nil
				case "missing-page-info":
					pullNodes(f)["pageInfo"] = nil
				case "missing-cursor":
					pullNodes(f)["nodes"] = []any{map[string]any{"number": n, "headRefOid": a.run.Head, "baseRefName": "main"}}
					pullNodes(f)["pageInfo"] = map[string]any{"hasNextPage": true}
				case "repeated-cursor", "role-churn":
					pullNodes(f)["nodes"] = []any{map[string]any{"number": n, "headRefOid": a.run.Head, "baseRefName": "main"}}
					pullNodes(f)["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "repeat"}
					if scenario == "role-churn" && n > 1 {
						repoNode(f)["defaultBranchRef"] = map[string]any{"name": "main", "target": map[string]any{"oid": strings.Repeat("c", 40)}}
					}
				case "duplicate-pull":
					p := map[string]any{"number": 3, "headRefOid": a.run.Head, "baseRefName": "main"}
					pullNodes(f)["nodes"] = []any{p, p}
				default:
					e := queueEntry(a.run.Head)
					switch scenario {
					case "duplicate-entry":
						queueEntries(f)["nodes"] = []any{e, e}
						return f
					case "position-gap":
						e["position"] = 2
					case "invalid-group":
						e["headCommit"] = nil
					case "invalid-state":
						e["state"] = "UNKNOWN"
					case "wrong-base":
						e["pullRequest"] = map[string]any{"number": 3, "headRefOid": a.run.Head, "baseRefName": "other"}
					}
					queueEntries(f)["nodes"] = []any{e}
				}
				return f
			}
			if err := s.sweep(context.Background()); err == nil {
				t.Fatal("incomplete roles accepted")
			}
			if a.cancels != 0 {
				t.Fatal("cancelled without complete roles")
			}
		})
	}
}

func TestRetirementRechecksAttemptAndVerifiesTerminalReadback(t *testing.T) {
	for _, scenario := range []string{"rerun-before-roles", "rerun-before-post", "completed-before-post", "ref-error", "readback-still-active", "readback-rerun", "success-race"} {
		t.Run(scenario, func(t *testing.T) {
			s, a := fixture()
			if scenario == "ref-error" {
				a.refOverride = func(int) (string, error) { return "", errors.New("403") }
			}
			if scenario == "readback-still-active" {
				a.finish = false
			}
			if scenario == "success-race" {
				a.conclusion = "success"
			}
			a.readOverride = func(path string, n int) (any, bool) {
				if !strings.Contains(path, "/actions/runs/") {
					return nil, false
				}
				r := a.run
				if (scenario == "rerun-before-roles" && n == 1) || (scenario == "rerun-before-post" && n == 2) || (scenario == "readback-rerun" && n == 3) {
					r.Attempt++
					return r, true
				}
				if scenario == "completed-before-post" && n == 2 {
					r.Status = "completed"
					r.Conclusion = &a.conclusion
					return r, true
				}
				return nil, false
			}
			err := s.sweep(context.Background())
			wantErr := scenario != "completed-before-post" && scenario != "success-race"
			if (err != nil) != wantErr {
				t.Fatalf("error=%v wantError=%v", err, wantErr)
			}
			want := 0
			if strings.HasPrefix(scenario, "readback-") || scenario == "success-race" {
				want = 1
			}
			if a.cancels != want {
				t.Fatalf("cancels=%d want=%d", a.cancels, want)
			}
		})
	}
}

func TestRetirementWorkflowTrustAndIsolation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "ci-run-retirement.yml"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, required := range []string{"branches: [main]", "cron: '17,47 * * * *'", "cancel-in-progress: false", "github.ref == format('refs/heads/{0}', github.event.repository.default_branch)", "ref: ${{ github.sha }}", "persist-credentials: false", "actions: write", "pull-requests: read", "cache: false", "run: go run ./scripts/ci/retire-runs"} {
		if !strings.Contains(source, required) {
			t.Fatalf("missing trusted workflow boundary: %s", required)
		}
	}
	for _, forbidden := range []string{"pull_request:", "pull_request_target:", "merge_group:", "workflow_run:", "secrets.", "checks: write", "queue: max", "needs:", "inputs."} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("retirement must remain independent trusted housekeeping: %s", forbidden)
		}
	}
}

func TestRetirementCompleteRunPagination(t *testing.T) {
	for _, scenario := range []string{"complete", "duplicate", "count-changed", "truncated"} {
		t.Run(scenario, func(t *testing.T) {
			s, a := fixture()
			a.readOverride = func(path string, _ int) (any, bool) {
				if !strings.Contains(path, "status=queued&") {
					return nil, false
				}
				count := 101
				runs := []run{}
				if strings.HasSuffix(path, "page=1") {
					for id := int64(1); id <= 100; id++ {
						r := a.run
						r.ID = id
						runs = append(runs, r)
					}
				} else {
					r := a.run
					r.ID = 101
					if scenario == "duplicate" {
						r.ID = 1
					}
					if scenario == "count-changed" {
						count++
					}
					if scenario != "truncated" {
						runs = append(runs, r)
					}
				}
				return map[string]any{"total_count": count, "workflow_runs": runs}, true
			}
			runs, err := s.inventory(context.Background())
			if scenario == "complete" {
				if err != nil || len(runs) != 101 {
					t.Fatalf("complete pagination: %d runs, %v", len(runs), err)
				}
			} else if err == nil {
				t.Fatal("incomplete paginated inventory accepted")
			}
		})
	}
}

func TestRetirementCompleteRolePaginationAndUnmaterializedBase(t *testing.T) {
	s, a := fixture()
	a.rolesOverride = func(n int) any {
		f := roleFixture()
		if n == 1 {
			pullNodes(f)["nodes"] = []any{map[string]any{"number": 1, "headRefOid": strings.Repeat("d", 40), "baseRefName": "main"}}
			pullNodes(f)["pageInfo"] = map[string]any{"hasNextPage": true, "endCursor": "pull-page2"}
			e := queueEntry(strings.Repeat("e", 40))
			e["state"] = "QUEUED"
			e["headCommit"] = nil
			e["baseCommit"] = map[string]any{"oid": a.run.Head}
			queueEntries(f)["nodes"] = []any{e}
		} else {
			pullNodes(f)["nodes"] = []any{map[string]any{"number": 2, "headRefOid": strings.Repeat("f", 40), "baseRefName": "main"}}
		}
		return f
	}
	roles, err := s.roles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, head := range []string{a.run.Head, strings.Repeat("d", 40), strings.Repeat("f", 40)} {
		if !roles[head] {
			t.Fatalf("lost protected paginated SHA %s", head)
		}
	}
	if a.snapshots != 2 {
		t.Fatalf("pages=%d want=2", a.snapshots)
	}
}

func TestRetirementRefRequiresExact404OrAuthenticatedCommit(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s' \"$GH_FIXTURE_RESPONSE\"\nexit \"$GH_FIXTURE_EXIT\"\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	branch := "gh-readonly-queue/main/pr-3-a"
	for _, scenario := range []string{"404", "403", "500", "valid", "wrong-ref", "wrong-type", "malformed"} {
		t.Run(scenario, func(t *testing.T) {
			status, exit := "HTTP/2.0 200 OK", "0"
			body := fmt.Sprintf(`{"ref":%q,"object":{"type":"commit","sha":%q}}`, "refs/heads/"+branch, strings.Repeat("a", 40))
			switch scenario {
			case "404", "403", "500":
				status = "HTTP/2.0 " + scenario + " Failure"
				exit = "1"
			case "wrong-ref":
				body = strings.ReplaceAll(body, branch, "other")
			case "wrong-type":
				body = strings.ReplaceAll(body, "commit", "tag")
			case "malformed":
				body = "bad"
			}
			t.Setenv("GH_FIXTURE_RESPONSE", status+"\r\nContent-Type: application/json\r\n\r\n"+body)
			t.Setenv("GH_FIXTURE_EXIT", exit)
			a := githubAPI{repo: "Hikyo-Org/Hikyo", remaining: 1}
			ref, err := a.ref(context.Background(), branch)
			if scenario == "404" {
				if err != nil || ref != "" {
					t.Fatalf("404: ref=%s err=%v", ref, err)
				}
			} else if scenario == "valid" {
				if err != nil || !shaPattern.MatchString(ref) {
					t.Fatalf("valid ref=%s err=%v", ref, err)
				}
			} else if err == nil {
				t.Fatal("untrusted ref response accepted")
			}
			if a.remaining != 0 {
				t.Fatal("API request not charged")
			}
		})
	}
}

type fleetAPI struct {
	*fakeAPI
	runs          map[int64]run
	cancellations []int64
}

func (a *fleetAPI) read(ctx context.Context, path string, out any) error {
	if strings.Contains(path, "/runs?") {
		runs := []run{}
		if strings.Contains(path, "status=queued&") {
			for _, r := range a.runs {
				runs = append(runs, r)
			}
		}
		return decode(map[string]any{"total_count": len(runs), "workflow_runs": runs}, out)
	}
	if strings.Contains(path, "/actions/runs/") {
		var id int64
		if _, err := fmt.Sscanf(path, "repos/Hikyo-Org/Hikyo/actions/runs/%d", &id); err != nil {
			return err
		}
		return decode(a.runs[id], out)
	}
	return a.fakeAPI.read(ctx, path, out)
}
func (a *fleetAPI) cancel(_ context.Context, id int64) error {
	r := a.runs[id]
	r.Status = "completed"
	r.Conclusion = &a.conclusion
	a.runs[id] = r
	a.cancellations = append(a.cancellations, id)
	return nil
}

func TestRetirementBoundsCancellationCountAndAPICalls(t *testing.T) {
	s, f := fixture()
	a := &fleetAPI{fakeAPI: f, runs: map[int64]run{}}
	for id := int64(1); id <= maxCancels+1; id++ {
		r := f.run
		r.ID = id
		a.runs[id] = r
	}
	s.api = a
	if err := s.sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(a.cancellations) != maxCancels {
		t.Fatalf("cancelled=%d want=%d", len(a.cancellations), maxCancels)
	}
	live := githubAPI{remaining: 0}
	if _, err := live.command(context.Background(), "api", "not-executed"); err == nil {
		t.Fatal("API budget bypassed")
	}
}

func TestRetirementDeduplicatesSameAttemptStatusTransition(t *testing.T) {
	s, a := fixture()
	a.run.Status = "in_progress"
	a.readOverride = func(path string, _ int) (any, bool) {
		for _, status := range []string{"queued", "in_progress"} {
			if strings.Contains(path, "status="+status+"&") {
				observed := a.run
				observed.Status = status
				return map[string]any{"total_count": 1, "workflow_runs": []run{observed}}, true
			}
		}
		return nil, false
	}
	runs, err := s.inventory(context.Background())
	if err != nil || len(runs) != 1 || runs[0].ID != a.run.ID || runs[0].Attempt != a.run.Attempt {
		t.Fatalf("ordinary status transition aborted or duplicated inventory: %+v/%v", runs, err)
	}
	if err := s.sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.cancels != 1 || a.snapshots != 2 || a.refs != 2 || a.reads != 3 {
		t.Fatalf("transition bypassed fresh roles/ref/source checks or cancelled twice: %+v", a)
	}
}

func TestRetirementStatusTransitionStillRejectsChangedIdentityAndSameStatusDuplicates(t *testing.T) {
	for _, fault := range []string{"attempt", "head", "branch", "workflow", "path", "name", "event", "repository", "head repository", "conclusion", "same-status first", "same-status later"} {
		t.Run(fault, func(t *testing.T) {
			s, a := fixture()
			a.readOverride = func(path string, _ int) (any, bool) {
				if strings.Contains(path, "status=queued&") {
					runs := []run{a.run}
					if fault == "same-status first" {
						runs = append(runs, a.run)
					}
					return map[string]any{"total_count": len(runs), "workflow_runs": runs}, true
				}
				if strings.Contains(path, "status=in_progress&") {
					changed := a.run
					changed.Status = "in_progress"
					switch fault {
					case "attempt":
						changed.Attempt++
					case "head":
						changed.Head = strings.Repeat("e", 40)
					case "branch":
						changed.Branch = "gh-readonly-queue/main/pr-4-b"
					case "workflow":
						changed.WorkflowID++
					case "path":
						changed.Path = ".github/workflows/other.yml"
					case "name":
						changed.Name = "other"
					case "event":
						changed.Event = "push"
					case "repository":
						changed.Repository.ID++
					case "head repository":
						changed.HeadRepository.ID++
					case "conclusion":
						changed.Conclusion = &a.conclusion
					}
					runs := []run{changed}
					if fault == "same-status later" {
						runs = append(runs, changed)
					}
					return map[string]any{"total_count": len(runs), "workflow_runs": runs}, true
				}
				return nil, false
			}
			if err := s.sweep(context.Background()); err == nil || a.cancels != 0 {
				t.Fatalf("changed identity or same-status duplicate accepted: cancels=%d err=%v", a.cancels, err)
			}
		})
	}
}
