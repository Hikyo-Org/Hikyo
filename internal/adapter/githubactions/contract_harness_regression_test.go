package githubactions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func syntheticContractEnvironment() map[string]string {
	values := map[string]string{
		"OWNER": "fixture-org", "REPOSITORY": "adapter-acceptance", "ENVIRONMENT": "contract",
		"WORKFLOW_PATH": ".github/workflows/hikyo-contract-consume.yml", "REF": "main",
		"WORKFLOW_SHA": strings.Repeat("a", 40), "CANDIDATE_SHA": strings.Repeat("b", 40),
		"AUTHORIZATION_REF": "synthetic-approval", "REPOSITORY_ID": "42", "ORGANIZATION_ID": "43", "ENVIRONMENT_ID": "44",
	}
	for _, name := range []string{"REPOSITORY_TOKEN", "ORGANIZATION_TOKEN", "ENVIRONMENT_TOKEN", "REPOSITORY_DENIED_TOKEN", "ORGANIZATION_DENIED_TOKEN", "ENVIRONMENT_DENIED_TOKEN", "HARNESS_TOKEN"} {
		values[name] = "github_pat_synthetic_" + name
	}
	return values
}

func TestContractConfigurationRejectsMissingPinsAndCredentialReuse(t *testing.T) {
	valid := syntheticContractEnvironment()
	parse := func(values map[string]string) error {
		_, err := parseGitHubContractConfig(func(name string) string { return values[strings.TrimPrefix(name, "HIKYO_GITHUB_CONTRACT_")] })
		return err
	}
	if err := parse(valid); err != nil {
		t.Fatal(err)
	}
	for name := range valid {
		t.Run("missing/"+name, func(t *testing.T) {
			values := syntheticContractEnvironment()
			delete(values, name)
			if err := parse(values); err == nil {
				t.Fatal("incomplete opt-in accepted")
			}
		})
	}
	tokens := []string{"REPOSITORY_TOKEN", "ORGANIZATION_TOKEN", "ENVIRONMENT_TOKEN", "REPOSITORY_DENIED_TOKEN", "ORGANIZATION_DENIED_TOKEN", "ENVIRONMENT_DENIED_TOKEN", "HARNESS_TOKEN"}
	for i, name := range tokens {
		for _, other := range tokens[:i] {
			t.Run("reuse/"+name+"/"+other, func(t *testing.T) {
				values := syntheticContractEnvironment()
				values[name] = values[other]
				err := parse(values)
				if err == nil || !strings.Contains(err.Error(), "distinct") {
					t.Fatalf("reuse refusal: %v", err)
				}
				if strings.Contains(err.Error(), values[name]) {
					t.Fatal("credential leaked in diagnostic")
				}
			})
		}
	}
	for _, row := range []struct{ name, value string }{
		{"REPOSITORY", "Hikyo"}, {"REPOSITORY", "other/production"}, {"REPOSITORY_ID", "0"},
		{"ENVIRONMENT_ID", "-1"}, {"ORGANIZATION_ID", "42.5"}, {"WORKFLOW_SHA", "main"},
		{"CANDIDATE_SHA", strings.Repeat("A", 40)}, {"REPOSITORY_TOKEN", "ghp_classic"},
		{"HARNESS_TOKEN", "github_pat_synthetic \t"}, {"REF", "main\n"},
	} {
		t.Run(row.name+"/malformed", func(t *testing.T) {
			values := syntheticContractEnvironment()
			values[row.name] = row.value
			if err := parse(values); err == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
}

func TestContractExplicitOptInCannotPassBySkipping(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		command := exec.CommandContext(t.Context(), binary, "-test.run=^TestGitHubDotComContract$", "-test.v")
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "HIKYO_GITHUB_CONTRACT") {
				command.Env = append(command.Env, value)
			}
		}
		if enabled {
			command.Env = append(command.Env, "HIKYO_GITHUB_CONTRACT=1")
		}
		output, err := command.CombinedOutput()
		if enabled {
			if err == nil || !strings.Contains(string(output), "--- FAIL: TestGitHubDotComContract") || strings.Contains(string(output), "--- SKIP:") {
				t.Fatalf("explicit opt-in must fail before network access: %s; %v", output, err)
			}
		} else if err != nil || !strings.Contains(string(output), "--- SKIP: TestGitHubDotComContract") {
			t.Fatalf("ordinary CI must retain explicit external skip: %s; %v", output, err)
		}
	}
}

func TestContractLostDispatchResponseStillCleansOnlyItsRun(t *testing.T) {
	var calls []string
	deleted := false
	harness := &contractHTTP{cfg: githubContractConfig{owner: "fixture", repository: "repo", repositoryID: 42}, client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls = append(calls, request.Method+" "+request.URL.RequestURI())
		switch {
		case strings.Contains(request.URL.Path, "/workflows/"):
			return response(200, `{"total_count":2,"workflow_runs":[{"id":7,"display_title":"hikyo-contract-owned","created_at":"2026-10-28T10:00:00Z","repository":{"id":42}},{"id":8,"display_title":"unrelated","created_at":"2026-10-28T10:00:00Z","repository":{"id":42}}]}`), nil
		case request.URL.Path == "/repos/fixture/repo/actions/runs/7":
			return response(200, `{"status":"completed"}`), nil
		case strings.HasSuffix(request.URL.Path, "/runs/7/artifacts"):
			if deleted {
				return response(200, `{"total_count":0,"artifacts":[]}`), nil
			}
			return response(200, `{"total_count":1,"artifacts":[{"id":71}]}`), nil
		case request.Method == "DELETE" && strings.HasSuffix(request.URL.Path, "/artifacts/71"):
			deleted = true
			return response(204, ""), nil
		default:
			return nil, fmt.Errorf("unexpected request %s", request.URL.Path)
		}
	})}}
	var evidence []string
	err := harness.cleanupDispatch(t.Context(), "consume.yml", "owned", time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC), map[int64]bool{}, func(id int64, kind string) { evidence = append(evidence, fmt.Sprintf("%s:%d", kind, id)) })
	if err != nil {
		t.Fatal(err)
	}
	if !deleted || !slices.Equal(evidence, []string{"owned_run:7", "cleaned_run:7"}) {
		t.Fatalf("cleanup/evidence: %v", evidence)
	}
	for _, call := range calls {
		if strings.Contains(call, "/runs/8") {
			t.Fatalf("unowned run touched: %s", call)
		}
	}
}

func TestContractCleanupRefusesIncompleteInventoryAndDeleteFailure(t *testing.T) {
	for _, row := range []struct {
		name, body   string
		deleteStatus int
	}{
		{"truncated", `{"total_count":1,"artifacts":[]}`, 204},
		{"missing", `{}`, 204},
		{"null", `{"total_count":0,"artifacts":null}`, 204},
		{"invalid-id", `{"total_count":1,"artifacts":[{"id":0}]}`, 204},
		{"delete-fails", `{"total_count":1,"artifacts":[{"id":71}]}`, 403},
	} {
		t.Run(row.name, func(t *testing.T) {
			harness := &contractHTTP{cfg: githubContractConfig{owner: "fixture", repository: "repo"}, client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "DELETE" {
					return response(row.deleteStatus, ""), nil
				}
				if strings.HasSuffix(r.URL.Path, "/artifacts") {
					return response(200, row.body), nil
				}
				return response(200, `{"status":"completed"}`), nil
			})}}
			if err := harness.cleanupWorkflowRun(t.Context(), 7); err == nil {
				t.Fatal("incomplete cleanup accepted")
			}
		})
	}
}

func TestContractDispatchListingFailureStillScrubsObservedRun(t *testing.T) {
	var cleaned []int64
	harness := &contractHTTP{cfg: githubContractConfig{owner: "fixture", repository: "repo"}, client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/workflows/") {
			return response(503, `{}`), nil
		}
		if strings.HasSuffix(r.URL.Path, "/artifacts") {
			return response(200, `{"total_count":0,"artifacts":[]}`), nil
		}
		return response(200, `{"status":"completed"}`), nil
	})}}
	err := harness.cleanupDispatch(t.Context(), "consume.yml", "owned", time.Now(), map[int64]bool{7: true}, func(id int64, kind string) {
		if kind == "cleaned_run" {
			cleaned = append(cleaned, id)
		}
	})
	if err == nil || !slices.Equal(cleaned, []int64{7}) {
		t.Fatalf("listing error lost known custody: cleaned=%v err=%v", cleaned, err)
	}
}

func TestContractPendingRunCancellationPrecedesArtifactScrub(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var calls []string
	canceled := false
	harness := &contractHTTP{cfg: githubContractConfig{owner: "fixture", repository: "repo"}, client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.Method == "POST" {
			canceled = true
			return response(202, ""), nil
		}
		if strings.HasSuffix(r.URL.Path, "/artifacts") {
			return response(200, `{"total_count":0,"artifacts":[]}`), nil
		}
		if canceled {
			return response(200, `{"status":"completed"}`), nil
		}
		return response(200, `{"status":"in_progress"}`), nil
	})}}
	if err := harness.cleanupWorkflowRun(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, []string{"GET /repos/fixture/repo/actions/runs/7", "POST /repos/fixture/repo/actions/runs/7/cancel", "GET /repos/fixture/repo/actions/runs/7", "GET /repos/fixture/repo/actions/runs/7/artifacts"}) {
		t.Fatalf("cleanup order: %v", calls)
	}
}

func TestContractArtifactRedirectDoesNotForwardCredential(t *testing.T) {
	for _, target := range []string{"https://signed-artifact.example/file", "https://api.github.com/file", "http://unsafe.example/file", "https://user:pass@unsafe.example/file"} {
		parsed, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		request := &http.Request{URL: parsed, Header: make(http.Header)}
		request.Header.Set("Authorization", "Bearer github_pat_synthetic")
		err = contractRedirect(request, nil)
		if request.Header.Get("Authorization") != "" {
			t.Fatal("credential forwarded")
		}
		if (parsed.Scheme != "https" || parsed.User != nil) != (err != nil) {
			t.Fatalf("redirect policy: %v", err)
		}
	}
	contacts := 0
	harness := newContractHTTP(githubContractConfig{})
	harness.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		contacts++
		if contacts == 1 {
			if request.Header.Get("Authorization") == "" {
				t.Fatal("initial API request lacks credential")
			}
			return response(http.StatusFound, "", http.Header{"Location": []string{"https://signed-artifact.example/file"}}), nil
		}
		if request.Header.Get("Authorization") != "" {
			t.Fatal("configured HTTP client forwarded credential to artifact storage")
		}
		return response(http.StatusOK, "hash-only artifact"), nil
	})
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.github.com/repos/fixture/repo/actions/artifacts/71/zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer github_pat_synthetic")
	result, err := harness.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = result.Body.Close()
	if contacts != 2 {
		t.Fatalf("artifact redirect contacts: %d", contacts)
	}
}

func TestContractNameCleanupOwnsLostMutationAndMissingPatchProbe(t *testing.T) {
	var deletes []string
	client, err := NewTestClient("https://api.github.com", "github_pat_synthetic", &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		deletes = append(deletes, r.Method+" "+r.URL.Path)
		return response(http.StatusNotFound, `{}`), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Forget()
	t.Run("lost-or-unexpected-response", func(t *testing.T) {
		// Registration occurs before the uncertain write, independent of success.
		ownContractName(t, client, adapter.Destination{Kind: adapter.Repository, Owner: "fixture", Name: "repo"}, adapter.Variable, "HIKYO_CONTRACT_MISSING")
	})
	if !slices.Equal(deletes, []string{"DELETE /repos/fixture/repo/actions/variables/HIKYO_CONTRACT_MISSING"}) {
		t.Fatalf("lost mutation not scrubbed: %v", deletes)
	}
}

// Exercise Fatal paths in a child test process so expected contract refusals
// cannot become passing subtests, and cleanup still runs through testing.T.
func TestContractFatalPathsPreserveCleanup(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"repository-moved", "ref-moved", "fixture-drift", "run-moved", "artifact-invalid", "dispatch-response-lost"} {
		t.Run(mode, func(t *testing.T) {
			command := exec.CommandContext(t.Context(), binary, "-test.run=^TestContractSyntheticFatalPath$", "-test.v")
			command.Env = append(os.Environ(), "HIKYO_CONTRACT_SYNTHETIC_CASE="+mode)
			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), "--- FAIL: TestContractSyntheticFatalPath") {
				t.Fatalf("failure disappeared: %v: %s", err, output)
			}
			if strings.HasPrefix(mode, "repository-") || mode == "ref-moved" || mode == "fixture-drift" {
				if strings.Contains(string(output), "SYNTHETIC POST") || strings.Contains(string(output), "SYNTHETIC DELETE") {
					t.Fatalf("preflight refusal mutated fixture: %s", output)
				}
			} else if !strings.Contains(string(output), "SYNTHETIC DELETE /repos/fixture/repo/actions/artifacts/71") || !strings.Contains(string(output), `"kind":"cleaned_run"`) {
				t.Fatalf("fatal path lost artifact cleanup: %s", output)
			}
		})
	}
}

func TestContractSyntheticFatalPath(t *testing.T) {
	mode := os.Getenv("HIKYO_CONTRACT_SYNTHETIC_CASE")
	if mode == "" {
		return
	}
	nonce := ""
	deleted := false
	sha := strings.Repeat("a", 40)
	harness := &contractHTTP{cfg: githubContractConfig{owner: "fixture", repository: "repo", repositoryID: 42, organizationID: 43, workflowPath: ".github/workflows/consume.yml", ref: "main", workflowSHA: sha}, client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		fmt.Println("SYNTHETIC " + r.Method + " " + r.URL.Path)
		switch {
		case r.URL.Path == "/repos/fixture/repo":
			if mode == "repository-moved" {
				return response(200, `{"id":99,"owner":{"id":43}}`), nil
			}
			return response(200, `{"id":42,"owner":{"id":43},"private":false,"archived":false}`), nil
		case strings.Contains(r.URL.Path, "/commits/"):
			if mode == "ref-moved" {
				return response(200, `{"sha":"wrong"}`), nil
			}
			return response(200, `{"sha":"`+sha+`"}`), nil
		case strings.Contains(r.URL.Path, "/contents/"):
			return response(200, `{"encoding":"base64","content":"`+base64.StdEncoding.EncodeToString([]byte("stale fixture"))+`"}`), nil
		case strings.HasSuffix(r.URL.Path, "/dispatches"):
			var payload struct {
				Inputs map[string]string `json:"inputs"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			nonce = payload.Inputs["nonce"]
			if mode == "dispatch-response-lost" {
				return nil, fmt.Errorf("synthetic lost response")
			}
			return response(204, ""), nil
		case strings.Contains(r.URL.Path, "/workflows/"):
			runSHA := sha
			if mode == "run-moved" {
				runSHA = "wrong"
			}
			return response(200, fmt.Sprintf(`{"total_count":1,"workflow_runs":[{"id":7,"display_title":"hikyo-contract-%s","created_at":%q,"head_sha":%q,"event":"workflow_dispatch","run_attempt":1,"status":"completed","conclusion":"success","repository":{"id":42}}]}`, nonce, time.Now().UTC().Format(time.RFC3339), runSHA)), nil
		case r.URL.Path == "/repos/fixture/repo/actions/runs/7":
			return response(200, `{"status":"completed"}`), nil
		case strings.HasSuffix(r.URL.Path, "/runs/7/artifacts"):
			if deleted {
				return response(200, `{"total_count":0,"artifacts":[]}`), nil
			}
			return response(200, fmt.Sprintf(`{"total_count":1,"artifacts":[{"id":71,"name":"hikyo-contract-%s"}]}`, nonce)), nil
		case r.Method == "DELETE" && strings.HasSuffix(r.URL.Path, "/artifacts/71"):
			deleted = true
			return response(204, ""), nil
		case strings.HasSuffix(r.URL.Path, "/artifacts/71/zip"):
			return response(200, "invalid archive"), nil
		default:
			return nil, fmt.Errorf("unexpected synthetic request %s", r.URL.Path)
		}
	})}}
	if mode == "repository-moved" || mode == "ref-moved" || mode == "fixture-drift" {
		harness.assertWorkflowFixture(t)
		return
	}
	harness.assertWorkflowValueHashes(t, adapter.Destination{Kind: adapter.Repository}, "SYNTHETIC_SECRET", "SYNTHETIC_VARIABLE", "secret", "variable")
}
