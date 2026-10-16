package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func (f *fixture) repairWork() reportWork {
	return reportWork{Head: f.parents[1]["sha"], Run: f.r.ID, Attempt: f.r.Attempt}
}

func TestReplayRepairsStrandedCompletionWithActualSourceOutcome(t *testing.T) {
	for _, conclusion := range []string{"success", "failure", "cancelled", "timed_out"} {
		t.Run(conclusion, func(t *testing.T) {
			f := newFixture(t)
			f.existing("in_progress")
			f.r.Conclusion = conclusion
			f.prHead = strings.Repeat("e", 40) // Historical report stays on its captured head.
			if err := f.c.replay(context.Background(), f.e.Repository, f.repairWork()); err != nil {
				t.Fatal(err)
			}
			if len(f.writes) != 1 || f.writeMethods[0] != "PATCH" || f.writePaths[0] != "check-runs/777" || f.writes[0]["conclusion"] != conclusion {
				t.Fatalf("did not recover real source outcome: %+v", f.writes)
			}
			if _, ok := f.writes[0]["head_sha"]; ok {
				t.Fatal("repair attempted to move historical check")
			}
			if err := f.c.replay(context.Background(), f.e.Repository, f.repairWork()); err != nil {
				t.Fatal(err)
			}
			if len(f.writes) != 1 {
				t.Fatal("repair rewrote terminal report")
			}
		})
	}
}

func TestReplayNeverCreatesOrFinalizesActiveSource(t *testing.T) {
	for _, scenario := range []string{"missing", "terminal", "active source", "foreign binding", "same SHA active PR"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			switch scenario {
			case "foreign binding":
				f.existing("in_progress")
				f.checks[0].ExternalID += "foreign"
			case "terminal":
				f.existing("completed")
			case "active source":
				f.existing("in_progress")
				f.r.Status = "in_progress"
				f.r.Conclusion = ""
			case "same SHA active PR":
				f.existing("in_progress")
			}
			if err := f.c.replay(context.Background(), f.e.Repository, f.repairWork()); err != nil {
				t.Fatal(err)
			}
			expected := 0
			// This is informational source truth, never a required queue verdict. The
			// same PR head staying active does not change this completed run's result.
			if scenario == "same SHA active PR" {
				expected = 1
			}
			if len(f.writes) != expected {
				t.Fatalf("unexpected writes: %+v", f.writes)
			}
		})
	}
}

func TestReplayRejectsRoutingAndFreshIdentityChanges(t *testing.T) {
	for _, scenario := range []string{"head", "run", "attempt", "foreign app", "foreign URL", "unknown status", "nonterminal conclusion", "duplicate history", "source changes", "record changes", "record completes", "foreign terminal app", "foreign terminal binding", "wrong receipt", "wrong readback"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.existing("in_progress")
			work := f.repairWork()
			sourceReads, recordReads := 0, 0
			switch scenario {
			case "head":
				work.Head = strings.Repeat("e", 40)
			case "run":
				work.Run++
			case "attempt":
				work.Attempt++
			case "foreign app":
				f.checks[0].App.ID++
			case "foreign URL":
				f.checks[0].DetailsURL = "https://evil.test"
			case "unknown status":
				f.checks[0].Status = "unknown"
			case "nonterminal conclusion":
				f.checks[0].Conclusion = "success"
			case "duplicate history":
				f.checks = append(f.checks, f.checks[0])
			case "source changes":
				f.mutate = func(path string, v any) any {
					if path == fmt.Sprintf("actions/runs/%d/attempts/%d", f.r.ID, f.r.Attempt) {
						sourceReads++
						if sourceReads == 3 {
							r := v.(run)
							r.Conclusion = "success"
							return r
						}
					}
					return v
				}
			case "record changes", "record completes", "foreign terminal app", "foreign terminal binding", "wrong readback":
				f.mutate = func(path string, v any) any {
					if path == "check-runs/777" {
						recordReads++
						x := v.(check)
						if scenario == "record changes" {
							x.ExternalID += "changed"
						}
						if scenario == "record completes" || strings.HasPrefix(scenario, "foreign terminal") {
							x.Status = "completed"
							x.Conclusion = "failure"
							if scenario == "foreign terminal app" {
								x.App.ID++
							}
							if scenario == "foreign terminal binding" {
								x.ExternalID += "changed"
							}
						}
						if scenario == "wrong readback" && recordReads == 2 {
							x.Conclusion = "success"
						}
						return x
					}
					return v
				}
			case "wrong receipt":
				f.writeMutate = func(x check) check { x.App.ID++; return x }
			}
			// Route mismatches must fail before requesting a different source endpoint.
			if scenario == "run" || scenario == "attempt" {
				if err := f.c.publishReport(context.Background(), f.e, &work); err == nil {
					t.Fatal("accepted mismatched route")
				}
				return
			}
			err := f.c.replay(context.Background(), f.e.Repository, work)
			if scenario == "record completes" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("accepted unauthenticated repair")
			}
			expected := 0
			if scenario == "wrong receipt" || scenario == "wrong readback" {
				expected = 1
			}
			if len(f.writes) != expected {
				t.Fatalf("unexpected mutation: %+v", f.writes)
			}
		})
	}
}

func TestDecodeReportWorkRequiresOneExactTypedRoute(t *testing.T) {
	valid := fmt.Sprintf(`{"head":%q,"run":50,"attempt":1}`, strings.Repeat("c", 40))
	if _, err := decodeReportWork(valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", valid + ` {}`, strings.Replace(valid, `"run":50`, `"run":"50"`, 1), strings.Replace(valid, `"attempt":1`, `"attempt":0`, 1), strings.Replace(valid, `"run":50`, `"run":-50`, 1), strings.Replace(valid, strings.Repeat("c", 40), "main", 1), strings.Replace(valid, `"run":50`, `"run":50,"other":1`, 1)} {
		if _, err := decodeReportWork(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}

func TestReplayAcceptsOnlyExactCanonicalCheckURL(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	f.checks[0].DetailsURL = "https://github.com/owner/repo/runs/777"
	if err := f.c.replay(context.Background(), f.e.Repository, f.repairWork()); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 {
		t.Fatal("canonical Actions URL prevented stranded completion repair")
	}
}

type repairTestTransport struct {
	base        http.RoundTripper
	target      *url.URL
	loseReceipt bool
}

func (transport repairTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	endpoint := *req.URL
	if transport.target != nil {
		endpoint.Scheme = transport.target.Scheme
		endpoint.Host = transport.target.Host
	}
	clone.URL = &endpoint
	response, err := transport.base.RoundTrip(clone)
	if err == nil && transport.loseReceipt && req.Method == http.MethodPatch {
		response.Body.Close()
		return nil, errors.New("lost PATCH receipt")
	}
	return response, err
}

func TestReplayNeverRetriesAnUncertainPatch(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	f.c.http = &http.Client{Transport: repairTestTransport{base: http.DefaultTransport, loseReceipt: true}}
	if err := f.c.replay(context.Background(), f.e.Repository, f.repairWork()); err == nil {
		t.Fatal("lost receipt was reported successful")
	}
	if len(f.writes) != 1 {
		t.Fatalf("uncertain mutation retried: %+v", f.writes)
	}
	if err := f.c.replay(context.Background(), f.e.Repository, f.repairWork()); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) != 1 {
		t.Fatal("later repair rewrote the completed mutation")
	}
}

func TestRepairCLIRequiresTrustedRetirementContext(t *testing.T) {
	for _, scenario := range []string{"schedule", "manual", "wrong event", "wrong workflow", "wrong execution ref", "wrong dispatch ref", "invalid route", "unexpected argument"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.existing("in_progress")
			t.Setenv("GITHUB_EVENT_NAME", "schedule")
			t.Setenv("GITHUB_REF", "refs/heads/main")
			t.Setenv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/ci-check-retirement.yml@refs/heads/main")
			t.Setenv("GH_TOKEN", "test")
			t.Setenv("GH_REPO", "owner/repo")
			e := f.e
			e.Ref = "refs/heads/main"
			work, err := json.Marshal(f.repairWork())
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"--repair-work", string(work)}
			switch scenario {
			case "manual":
				t.Setenv("GITHUB_EVENT_NAME", "workflow_dispatch")
			case "wrong event":
				t.Setenv("GITHUB_EVENT_NAME", "workflow_run")
			case "wrong workflow":
				t.Setenv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/other.yml@refs/heads/main")
			case "wrong execution ref":
				t.Setenv("GITHUB_REF", "refs/heads/feature")
			case "wrong dispatch ref":
				t.Setenv("GITHUB_EVENT_NAME", "workflow_dispatch")
				e.Ref = "refs/heads/feature"
			case "invalid route":
				args[1] = `{"head":"main","run":50,"attempt":1}`
			case "unexpected argument":
				args = append(args, "extra")
			}
			raw, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "event.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GITHUB_EVENT_PATH", path)
			target, err := url.Parse(f.c.base)
			if err != nil {
				t.Fatal(err)
			}
			original := http.DefaultTransport
			http.DefaultTransport = repairTestTransport{base: original, target: target}
			defer func() { http.DefaultTransport = original }()
			err = execute(args...)
			if scenario == "schedule" || scenario == "manual" {
				if err != nil {
					t.Fatal(err)
				}
				if len(f.writes) != 1 || f.writes[0]["conclusion"] != "failure" {
					t.Fatalf("trusted repair failed: %+v", f.writes)
				}
			} else {
				if err == nil {
					t.Fatal("untrusted repair invocation accepted")
				}
				if len(f.requests) != 0 || len(f.writes) != 0 {
					t.Fatal("untrusted invocation reached network or mutation")
				}
			}
		})
	}
}
