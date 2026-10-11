package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func legacySnapshotBinding(s snapshot, repoID int64) legacyBinding {
	b := s.binding(repoID)
	return legacyBinding{RepoID: b.RepoID, PR: b.PR, Head: b.Head, Base: b.Base, BaseRef: s.PR.Base.Ref, Run: b.Run, Attempt: b.Attempt}
}

func TestCompactBindingBoundsAndFullIdentity(t *testing.T) {
	b := binding{RepoID: math.MaxInt64, PR: math.MaxInt, Head: strings.Repeat("a", 40), Base: strings.Repeat("b", 40), Run: math.MaxInt64, Attempt: math.MaxInt64}
	for _, ref := range []string{"main", "dependabot/go_modules/github.com/sigstore/sigstore-1.11.0", strings.Repeat("stack/秘密/🦦/", 1000)} {
		b.BaseRef = sha256.Sum256([]byte(ref))
		id := b.externalID()
		if len(id) > externalIDLimit {
			t.Fatalf("external_id has %d bytes", len(id))
		}
		decoded, err := decodeBinding(id)
		if err != nil || decoded.externalID() != id || decoded.BaseRef != b.BaseRef {
			t.Fatalf("binding failed round trip: %v", err)
		}
		changes := []binding{b, b, b, b, b, b, b}
		changes[0].RepoID--
		changes[1].PR--
		changes[2].Head = strings.Repeat("c", 40)
		changes[3].Base = strings.Repeat("c", 40)
		changes[4].BaseRef = sha256.Sum256([]byte(ref + "/changed"))
		changes[5].Run--
		changes[6].Attempt--
		for _, changed := range changes {
			if changed.externalID() == id {
				t.Fatal("changed authority shares the identifier")
			}
		}
	}
}

func TestCompactBindingRejectsMalformedAndNoncanonicalIDs(t *testing.T) {
	b := binding{RepoID: 10, PR: 7, Head: strings.Repeat("a", 40), Base: strings.Repeat("b", 40), BaseRef: sha256.Sum256([]byte("main")), Run: 100, Attempt: 1}
	id := b.externalID()
	// A 104-byte payload leaves two unused bits in its last base64 character.
	// Changing those bits preserves the decoded bytes but is noncanonical.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	last := strings.IndexByte(alphabet, id[len(id)-1])
	noncanonical := id[:len(id)-1] + string(alphabet[last+1])
	for _, value := range []string{externalPrefix + "bogus", noncanonical, id + ":extra", id[:len(id)-1], id + "="} {
		if _, err := decodeBinding(value); err == nil {
			t.Fatalf("accepted malformed binding %q", value)
		}
	}
	for _, changed := range []binding{
		{RepoID: 10, PR: 7, Head: b.Head, Base: b.Base, Run: 0, Attempt: 1},
		{RepoID: 10, PR: -7, Head: b.Head, Base: b.Base},
		{RepoID: -10, PR: 7, Head: b.Head, Base: b.Base},
	} {
		if _, err := decodeBinding(changed.externalID()); err == nil {
			t.Fatal("accepted invalid binding IDs")
		}
	}
}

func TestPublishRejectsInvalidBindingBeforeMutation(t *testing.T) {
	for _, field := range []string{"head", "base", "head case", "base length", "repo", "pr", "attempt"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t)
			b := (snapshot{PR: f.pr, Run: &f.source}).binding(10)
			switch field {
			case "head":
				b.Head = strings.Repeat("g", 40)
			case "base":
				b.Base = strings.Repeat("g", 40)
			case "head case":
				b.Head = strings.ToUpper(b.Head)
			case "base length":
				b.Base = b.Base[:len(b.Base)-1]
			case "repo":
				b.RepoID = -1
			case "pr":
				b.PR = 0
			case "attempt":
				b.Attempt = 0
			}
			if err := f.c.publish(context.Background(), f.pr, b, nil, pending("invalid binding")); err == nil {
				t.Fatal("invalid publication binding accepted")
			}
			if len(f.api.writes) != 0 {
				t.Fatal("invalid publication binding mutated checks")
			}
		})
	}
}

func TestLegacyBindingRemainsReadableAndMigrates(t *testing.T) {
	f := newFixture(t)
	b := (snapshot{PR: f.pr, Run: &f.source}).binding(10)
	legacy := legacyID(t, legacySnapshotBinding(snapshot{PR: f.pr, Run: &f.source}, 10))
	decoded, err := decodeBinding(legacy)
	if err != nil || decoded != b {
		t.Fatalf("legacy binding lost its full authority: %v", err)
	}
	v := check{ID: 99, Name: "ci-required", HeadSHA: f.pr.Head.SHA, ExternalID: legacy, Status: "completed", Conclusion: "success"}
	v.App.ID, v.App.Slug = actionsAppID, "github-actions"
	f.api.checks = []check{v}
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	requireConclusion(t, f, "completed", "success")
	if f.api.writes[0]["method"] != "POST" || f.api.writes[0]["status"] != "queued" {
		t.Fatal("legacy success was not invalidated by a new queued check")
	}
	for _, write := range f.api.writes {
		if !strings.HasPrefix(write["external_id"].(string), externalPrefix) {
			t.Fatal("migration wrote another legacy identifier")
		}
	}
}

func TestTruncatedLegacyOwnedCheckRepairsBeforePolicyReads(t *testing.T) {
	for _, ref := range []string{"dependabot/go_modules/github.com/sigstore/sigstore-1.11.0", strings.Repeat("stack/秘密/🦦/", 100)} {
		for _, failRead := range []bool{false, true} {
			f := newFixture(t)
			f.pr.Base.Ref = ref
			f.source.Pulls = []pull{f.pr}
			f.refresh()
			legacy := legacyID(t, legacySnapshotBinding(snapshot{PR: f.pr}, 10))
			if len(legacy) <= externalIDLimit {
				t.Fatal("fixture did not exceed provider limit")
			}
			v := check{ID: 99, Name: "ci-required", HeadSHA: f.pr.Head.SHA, ExternalID: legacy[:externalIDLimit], Status: "completed", Conclusion: "success"}
			v.App.ID, v.App.Slug = actionsAppID, "github-actions"
			f.api.checks = []check{v}
			if failRead {
				f.api.responses[f.c.endpoint("actions/runs/100")] = errors.New("source unavailable")
			}
			err := f.execute()
			if (err != nil) != failRead {
				t.Fatalf("repair error=%v, source read failure=%v", err, failRead)
			}
			if len(f.api.writes) == 0 || f.api.writes[0]["method"] != "POST" || f.api.writes[0]["status"] != "queued" {
				t.Fatal("truncated check did not receive a fresh queued barrier")
			}
			latest := f.api.checks[len(f.api.checks)-1]
			if failRead {
				if latest.Status != "queued" || latest.Conclusion != "" {
					t.Fatal("malformed legacy success survived a policy read failure")
				}
			} else {
				requireConclusion(t, f, "completed", "success")
			}
		}
	}
}

func TestTruncatedLegacyHostileIdentityCannotBeRepaired(t *testing.T) {
	for _, field := range []string{"repository", "invalid PR", "head", "app", "check head", "length", "encoding", "invalid suffix"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t)
			f.pr.Base.Ref = "dependabot/go_modules/github.com/sigstore/sigstore-1.11.0"
			f.source.Pulls = []pull{f.pr}
			f.refresh()
			b := legacySnapshotBinding(snapshot{PR: f.pr}, 10)
			switch field {
			case "repository":
				b.RepoID++
			case "invalid PR":
				b.PR = -7
			case "head":
				b.Head = strings.Repeat("c", 40)
			}
			legacy := legacyID(t, b)
			v := check{ID: 99, Name: "ci-required", HeadSHA: f.pr.Head.SHA, ExternalID: legacy[:externalIDLimit], Status: "queued"}
			v.App.ID, v.App.Slug = actionsAppID, "github-actions"
			switch field {
			case "app":
				v.App.ID++
			case "check head":
				v.HeadSHA = strings.Repeat("c", 40)
			case "length":
				v.ExternalID = v.ExternalID[:externalIDLimit-1]
			case "encoding":
				v.ExternalID = legacyExternalPrefix + strings.Repeat("!", externalIDLimit-len(legacyExternalPrefix))
			case "invalid suffix":
				data, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(v.ExternalID, legacyExternalPrefix))
				data[len(data)-1] = 1
				v.ExternalID = legacyExternalPrefix + base64.RawURLEncoding.EncodeToString(data)
			}
			f.api.checks = []check{v}
			if err := f.execute(); err == nil {
				t.Fatal("malformed foreign check accepted")
			}
			if len(f.api.writes) != 0 {
				t.Fatal("malformed foreign check mutated")
			}
		})
	}
}

func TestTruncatedLegacyStaleBindingIsSupersededBeforeReadFailure(t *testing.T) {
	for _, changed := range []string{"base SHA", "retarget", "prior PR"} {
		t.Run(changed, func(t *testing.T) {
			f := newFixture(t)
			f.pr.Base.Ref = "dependabot/go_modules/golang.org/x/tools-0.51.0"
			old := legacySnapshotBinding(snapshot{PR: f.pr, Run: &f.source}, 10)
			switch changed {
			case "base SHA":
				f.pr.Base.SHA = strings.Repeat("d", 40)
			case "retarget":
				f.pr.Base.Ref = "main"
			case "prior PR":
				old.PR = 8
			}
			f.source.Pulls = []pull{f.pr}
			f.refresh()
			id := legacyID(t, old)
			v := check{ID: 99, Name: "ci-required", HeadSHA: f.pr.Head.SHA, ExternalID: id[:externalIDLimit], Status: "completed", Conclusion: "success"}
			v.App.ID, v.App.Slug = actionsAppID, "github-actions"
			f.api.checks = []check{v}
			f.api.responses[f.c.endpoint("actions/runs/100")] = errors.New("source unavailable")
			if err := f.execute(); err == nil {
				t.Fatal("source failure ignored")
			}
			if len(f.api.writes) != 1 || f.api.writes[0]["status"] != "queued" || f.api.writes[0]["method"] != "POST" {
				t.Fatal("stale legacy success survived reassessment")
			}
			b, err := decodeBinding(f.api.checks[len(f.api.checks)-1].ExternalID)
			if err != nil || b.PR != f.pr.Number || b.Base != f.pr.Base.SHA || b.BaseRef != (snapshot{PR: f.pr}).binding(10).BaseRef || b.Run != 0 {
				t.Fatalf("repair did not bind the current PR: %v", err)
			}
		})
	}
}

func TestEveryCanonicalLegacyTruncationBoundaryCanBeInvalidated(t *testing.T) {
	f := newFixture(t)
	refs := []string{"a", "<", "秘密", "🦦", "\\", "\"", "\n", "\u2028", "&"}
	for _, text := range refs {
		for size := 1; size < 160; size++ {
			for _, run := range []int64{0, 1, 100, 38092256249, math.MaxInt64} {
				b := legacySnapshotBinding(snapshot{PR: f.pr}, 10)
				b.BaseRef = strings.Repeat(text, size)
				b.Run = run
				if run > 0 {
					b.Attempt = 1
				}
				id := legacyID(t, b)
				if len(id) <= externalIDLimit {
					continue
				}
				if !truncatedLegacyBinding(id[:externalIDLimit], 10, f.pr.Head.SHA) {
					t.Fatalf("ref=%q repeat=%d run=%d canonical truncation refused", text, size, run)
				}
			}
		}
	}
}

func TestMalformedLegacyStringPrefixesCannotBeRepaired(t *testing.T) {
	for _, value := range []string{`"bad\q`, `"bad\ud8`, `"bad\u00f`, `"raw<`, `"raw&`, "\"bad\x01", string([]byte{'"', 0xff})} {
		if canonicalStringPrefix([]byte(value)) {
			t.Fatalf("noncanonical legacy string prefix accepted: %q", value)
		}
	}
}

func TestVerifyPolicySubprocessContract(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("scripts/ci", 0o755); err != nil {
		t.Fatal(err)
	}
	const script = `#!/bin/sh
set -eu
test "$PR_NUMBER" = 877
test "$HEAD_SHA" = aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
test "$PR_GATE_ONCE" = 1
test "$GH_REPO" = Hikyo-Org/Hikyo
test "$GH_TOKEN" = controlled-test-token
exit "$POLICY_FIXTURE_EXIT"
`
	if err := os.WriteFile(filepath.Join("scripts", "ci", "check-fork-validation.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PR_NUMBER", "wrong-pr")
	t.Setenv("HEAD_SHA", "wrong-head")
	t.Setenv("PR_GATE_ONCE", "0")
	t.Setenv("GH_REPO", "Hikyo-Org/Hikyo")
	t.Setenv("GH_TOKEN", "controlled-test-token")
	for _, code := range []int{0, 1, 75, 2, 127} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Setenv("POLICY_FIXTURE_EXIT", strconv.Itoa(code))
			got, err := verifyPolicy(context.Background(), 877, strings.Repeat("a", 40))
			if code == 0 || code == 1 || code == 75 {
				if err != nil || got != code {
					t.Fatalf("mapped exit=%d error=%v, want %d", got, err, code)
				}
			} else if err == nil {
				t.Fatalf("unexpected exit %d was accepted as %d", code, got)
			}
		})
	}
	if err := os.Remove(filepath.Join("scripts", "ci", "check-fork-validation.sh")); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyPolicy(context.Background(), 877, strings.Repeat("a", 40)); err == nil {
		t.Fatal("missing trusted verifier accepted")
	}
}

type fakeAPI struct {
	responses map[string]any
	readHook  func(string, int)
	reads     map[string]int
	writes    []map[string]any
	writeErr  error
	checks    []check
	nextID    int64
}

func (f *fakeAPI) read(_ context.Context, path string, result any) error {
	f.reads[path]++
	if f.readHook != nil {
		f.readHook(path, f.reads[path])
	}
	var response any
	if strings.Contains(path, "/check-runs?") {
		u, err := url.Parse(path)
		if err != nil {
			return err
		}
		latest := make(map[int64]check)
		for _, v := range f.checks {
			if v.Name != u.Query().Get("check_name") {
				continue
			}
			if prior, ok := latest[v.App.ID]; !ok || v.ID > prior.ID {
				latest[v.App.ID] = v
			}
		}
		filtered := []check{}
		for _, v := range latest {
			filtered = append(filtered, v)
		}
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
		page, err := strconv.Atoi(u.Query().Get("page"))
		if err != nil {
			return err
		}
		start := (page - 1) * 100
		end := min(start+100, len(filtered))
		if start > len(filtered) {
			return errors.New("invalid check page")
		}
		response = map[string]any{"total_count": len(filtered), "check_runs": filtered[start:end]}
	} else {
		response = f.responses[path]
	}
	if response == nil {
		return fmt.Errorf("unexpected API read %s", path)
	}
	if err, ok := response.(error); ok {
		return err
	}
	data, err := json.Marshal(response)
	if err != nil {
		return err
	}
	return decode(data, result)
}
func (f *fakeAPI) write(_ context.Context, method, path string, payload, result any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var mutation map[string]any
	if err := decode(data, &mutation); err != nil {
		return err
	}
	mutation["method"], mutation["path"] = method, path
	for _, key := range []string{"conclusion", "completed_at"} {
		if value, exists := mutation[key]; exists && value == nil {
			return fmt.Errorf("Checks API field %s is not nullable", key)
		}
	}
	f.writes = append(f.writes, mutation)
	if f.writeErr != nil {
		return f.writeErr
	}
	var created check
	if err := decode(data, &created); err != nil {
		return err
	}
	if f.nextID == 0 {
		f.nextID = 900
	}
	created.ID = f.nextID
	if method == "POST" {
		f.nextID++
	}
	created.App.ID, created.App.Slug = actionsAppID, "github-actions"
	// Match the deployed Checks API boundary, including silent truncation.
	if len(created.ExternalID) > 255 {
		created.ExternalID = created.ExternalID[:255]
	}
	if method == "PATCH" {
		for _, prior := range f.checks {
			if path == fmt.Sprintf("repos/Hikyo-Org/Hikyo/check-runs/%d", prior.ID) {
				created.ID, created.HeadSHA = prior.ID, prior.HeadSHA
			}
		}
	}
	if method == "PATCH" {
		found := false
		for i, prior := range f.checks {
			if prior.ID == created.ID {
				f.checks[i] = created
				found = true
			}
		}
		if !found {
			return errors.New("PATCH targeted missing check")
		}
	} else {
		f.checks = append(f.checks, created)
	}
	response, err := json.Marshal(created)
	if err != nil {
		return err
	}
	return decode(response, result)
}

type fixture struct {
	c           *controller
	api         *fakeAPI
	pr          pull
	source      run
	event       event
	verifyCode  int
	verifyCalls int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	repo := repository{ID: 10, FullName: "Hikyo-Org/Hikyo", DefaultBranch: "main"}
	yes := true
	f.pr = pull{Number: 7, State: "open", Head: reference{SHA: strings.Repeat("a", 40), Ref: "feature", Repo: repo}, Base: reference{SHA: strings.Repeat("b", 40), Ref: "main", Repo: repo}, MergeCommitSHA: strings.Repeat("c", 40), Mergeable: &yes}
	f.pr.User.ID, f.pr.User.Login = 12, "valentinhl"
	f.source = run{ID: 100, Attempt: 1, WorkflowID: 20, Path: workflowPath, Name: "fork-ci", DisplayTitle: "fork-ci #7", Event: "pull_request", HeadSHA: f.pr.Head.SHA, Status: "completed", Conclusion: "success", Repository: repo, HeadRepository: repo, Pulls: []pull{f.pr}}
	f.source.References = append(f.source.References, struct {
		Path string `json:"path"`
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
	}{Path: repo.FullName + "/.github/workflows/ci.yml@" + f.pr.MergeCommitSHA, Ref: "refs/pull/7/merge", SHA: f.pr.MergeCommitSHA})
	f.api = &fakeAPI{responses: make(map[string]any), reads: make(map[string]int), checks: []check{}}
	f.c = &controller{api: f.api, repoName: repo.FullName, checkName: "ci-required", output: &bytes.Buffer{}}
	f.c.verify = func(context.Context, int, string) (int, error) { f.verifyCalls++; return f.verifyCode, nil }
	f.api.responses["repos/"+repo.FullName] = repo
	f.api.responses[f.c.endpoint("collaborators/valentinhl/permission")] = map[string]any{"permission": "read", "user": map[string]int{"id": 12}}
	f.api.responses[f.c.endpoint("contents/.github/VOUCHED.td?ref=main")] = map[string]any{"type": "file", "encoding": "base64", "path": ".github/VOUCHED.td", "content": base64.StdEncoding.EncodeToString([]byte("github:valentinhl\n"))}
	f.api.responses[f.c.endpoint("actions/workflows/ci-fork.yml")] = workflow{ID: 20, Path: workflowPath, Name: "fork-ci"}
	f.event.Repository = repo
	f.event.Inputs.PR = "7"
	f.refresh()
	return f
}
func (f *fixture) refresh() {
	f.api.responses[f.c.endpoint("pulls/7")] = f.pr
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr}
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/workflows/ci-fork.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))] = map[string]any{"total_count": 1, "workflow_runs": []run{f.source}}
	f.api.responses[f.c.endpoint("actions/runs/100")] = f.source
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/100/attempts/%d/jobs?per_page=100&page=1", f.source.Attempt))] = map[string]any{"total_count": 1, "jobs": []job{{ID: 200, RunID: 100, Attempt: f.source.Attempt, Name: "validation / ci-required", Status: "completed", Conclusion: "success"}}}
	f.api.responses[f.c.endpoint("git/commits/"+f.pr.MergeCommitSHA)] = map[string]any{"sha": f.pr.MergeCommitSHA, "parents": []map[string]string{{"sha": f.pr.Base.SHA}, {"sha": f.pr.Head.SHA}}}
}
func (f *fixture) execute() error {
	return f.c.execute(context.Background(), "workflow_dispatch", f.event)
}
func requireConclusion(t *testing.T, f *fixture, status, conclusion string) {
	t.Helper()
	if len(f.api.writes) < 1 || len(f.api.writes) > 2 {
		t.Fatalf("want pending barrier and one final result, got %d", len(f.api.writes))
	}
	last := f.api.writes[len(f.api.writes)-1]
	if last["status"] != status || last["conclusion"] != conclusion && !(conclusion == "" && last["conclusion"] == nil) {
		t.Fatalf("wrong outcome: %#v", last)
	}
}
func TestDelayedValidationDoesNotOccupyRunner(t *testing.T) {
	f := newFixture(t)
	f.source.Status = "queued"
	f.source.Conclusion = ""
	f.verifyCode = 75
	f.refresh()
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	requireConclusion(t, f, "queued", "")
	if f.verifyCalls != 1 {
		t.Fatalf("controller polled policy %d times", f.verifyCalls)
	}
	// No deadline or created-at age affects admission, including >90 minutes.
	if len(f.api.reads) > 12 {
		t.Fatalf("unexpected polling: %d API endpoints", len(f.api.reads))
	}
}
func TestSuccessAndDuplicateCallbacks(t *testing.T) {
	f := newFixture(t)
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	requireConclusion(t, f, "completed", "success")
	f.event.Run = f.source
	if err := f.c.execute(context.Background(), "workflow_run", f.event); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != 4 || f.api.writes[2]["status"] != "queued" || f.api.writes[3]["conclusion"] != "success" || f.api.writes[3]["method"] != "PATCH" {
		t.Fatal("duplicate callback must invalidate then reauthorize its own result")
	}
}
func TestCompletedSourceFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*fixture)
	}{
		{"upstream failure", func(f *fixture) { f.source.Conclusion = "failure"; f.refresh() }},
		{"missing aggregate", func(f *fixture) {
			f.api.responses[f.c.endpoint("actions/runs/100/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 0, "jobs": []job{}}
		}},
		{"duplicate aggregate", func(f *fixture) {
			j := job{ID: 200, RunID: 100, Attempt: 1, Name: "validation / ci-required", Status: "completed", Conclusion: "success"}
			k := j
			k.ID++
			f.api.responses[f.c.endpoint("actions/runs/100/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 2, "jobs": []job{j, k}}
		}},
		{"policy refusal", func(f *fixture) { f.verifyCode = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.modify(f)
			if err := f.execute(); err != nil {
				t.Fatal(err)
			}
			requireConclusion(t, f, "completed", "failure")
		})
	}
}
func TestStaleCallbackHeadBaseAndAttempt(t *testing.T) {
	for _, field := range []string{"head", "base", "attempt"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t)
			f.event.Run = f.source
			switch field {
			case "head":
				f.pr.Head.SHA = strings.Repeat("d", 40)
			case "base":
				f.pr.Base.Ref = "other"
			case "attempt":
				f.source.Attempt = 2
			}
			f.refresh()
			if err := f.c.execute(context.Background(), "workflow_run", f.event); err != nil {
				t.Fatal(err)
			}
			if field == "head" && len(f.api.writes) != 0 {
				t.Fatal("stale head callback created a check for another head")
			}
			if field == "base" && f.api.checks[len(f.api.checks)-1].Conclusion == "success" {
				t.Fatal("stale base authorized success")
			}
		})
	}
}
func TestForgedCallbackWorkflowAndAssociation(t *testing.T) {
	for _, field := range []string{"workflow", "repo", "association"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t)
			f.event.Run = f.source
			switch field {
			case "workflow":
				f.event.Run.WorkflowID = 99
			case "repo":
				f.event.Run.Repository.ID = 99
			case "association":
				other := f.pr
				other.Number = 999
				f.source.Pulls = []pull{other}
				f.refresh()
			}
			if err := f.c.execute(context.Background(), "workflow_run", f.event); err == nil {
				t.Fatal("forged callback accepted")
			}
			for _, v := range f.api.writes {
				if v["conclusion"] == "success" {
					t.Fatal("forged callback authorized success")
				}
			}
		})
	}
}
func TestSourceRunNeedsAPIAssociation(t *testing.T) {
	f := newFixture(t)
	f.source.Pulls = []pull{}
	f.source.References = nil
	f.refresh()
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	requireConclusion(t, f, "queued", "")
}
func TestEmptyForkAssociationUsesGitHubOwnedMergeProof(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*fixture)
		want   string
	}{
		{"genuine fork", func(*fixture) {}, "success"},
		{"forged PR title", func(f *fixture) { f.source.DisplayTitle = "fork-ci #999" }, ""},
		{"forged merge ref", func(f *fixture) { f.source.References[0].Ref = "refs/pull/999/merge" }, ""},
		{"wrong immutable merge", func(f *fixture) {
			f.source.References[0].SHA = strings.Repeat("d", 40)
			f.source.References[0].Path = f.c.repoName + "/.github/workflows/ci.yml@" + f.source.References[0].SHA
		}, ""},
		{"references not available yet", func(f *fixture) {
			f.source.References = nil
			f.source.Status = "queued"
			f.source.Conclusion = ""
			f.verifyCode = 75
		}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.pr.Head.Repo = repository{ID: 11, FullName: "fork/Hikyo"}
			f.source.HeadRepository = f.pr.Head.Repo
			f.source.Pulls = []pull{}
			tc.modify(f)
			f.refresh()
			f.api.responses[f.c.endpoint("actions/runs/100/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 2, "jobs": []job{{ID: 200, RunID: 100, Attempt: 1, Name: "validation / ci-required", Status: "completed", Conclusion: "success"}, {ID: 201, RunID: 100, Attempt: 1, Name: "vouch", Status: "completed", Conclusion: "success"}}}
			f.event.Run = f.source
			if err := f.c.execute(context.Background(), "workflow_run", f.event); err != nil {
				t.Fatal(err)
			}
			status := "queued"
			if tc.want != "" {
				status = "completed"
			}
			requireConclusion(t, f, status, tc.want)
		})
	}
}

func TestOmittedForkAssociationJSONStillNeedsImmutableProof(t *testing.T) {
	for _, representation := range []string{"omitted", "null"} {
		for _, proved := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/proved=%t", representation, proved), func(t *testing.T) {
				f := newFixture(t)
				f.pr.Head.Repo = repository{ID: 11, FullName: "fork/Hikyo"}
				f.source.HeadRepository = f.pr.Head.Repo
				f.source.Pulls = nil
				if !proved {
					f.source.References[0].Ref = "refs/pull/999/merge"
				}
				f.refresh()
				raw, err := json.Marshal(f.source)
				if err != nil {
					t.Fatal(err)
				}
				var source map[string]json.RawMessage
				if err := decode(raw, &source); err != nil {
					t.Fatal(err)
				}
				if representation == "omitted" {
					delete(source, "pull_requests")
				}
				// Both callback and REST source/list are actual JSON with this shape.
				f.api.responses[f.c.endpoint("actions/runs/100")] = source
				f.api.responses[f.c.endpoint(fmt.Sprintf("actions/workflows/ci-fork.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))] = map[string]any{"total_count": 1, "workflow_runs": []any{source}}
				f.api.responses[f.c.endpoint("actions/runs/100/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 2, "jobs": []job{{ID: 200, RunID: 100, Attempt: 1, Name: "validation / ci-required", Status: "completed", Conclusion: "success"}, {ID: 201, RunID: 100, Attempt: 1, Name: "vouch", Status: "completed", Conclusion: "success"}}}
				payload, err := json.Marshal(map[string]any{"repository": f.event.Repository, "workflow_run": source})
				if err != nil {
					t.Fatal(err)
				}
				var callback event
				if err := decode(payload, &callback); err != nil {
					t.Fatal(err)
				}
				if callback.Run.Pulls != nil {
					t.Fatal("fixture did not exercise nil associations")
				}
				if err := f.c.execute(context.Background(), "workflow_run", callback); err != nil {
					t.Fatal(err)
				}
				if proved {
					requireConclusion(t, f, "completed", "success")
				} else {
					requireConclusion(t, f, "queued", "")
				}
			})
		}
	}
}

func setOpenPRPages(f *fixture, pulls []pull) {
	for page, offset := 1, 0; ; page, offset = page+1, offset+100 {
		end := min(offset+100, len(pulls))
		f.api.responses[f.c.endpoint(fmt.Sprintf("pulls?state=open&per_page=100&page=%d", page))] = pulls[offset:end]
		if end-offset < 100 {
			return
		}
	}
}

func manyOpenPRs(f *fixture, count int) []pull {
	pulls := []pull{f.pr}
	for i := 1; i < count; i++ {
		p := f.pr
		p.Number = 1000 + i
		p.Head.SHA = fmt.Sprintf("%040x", i)
		pulls = append(pulls, p)
	}
	return pulls
}

func TestOpenPRPaginationBeyondTwoPagesAndFreshCollisionScan(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := newFixture(t)
			pulls := manyOpenPRs(f, 251)
			if collision {
				pulls[250].Head.SHA = f.pr.Head.SHA
			}
			setOpenPRPages(f, pulls)
			if err := f.execute(); err != nil {
				t.Fatal(err)
			}
			want := "success"
			if collision {
				want = "failure"
			}
			requireConclusion(t, f, "completed", want)
			for page := 1; page <= 3; page++ {
				if f.api.reads[f.c.endpoint(fmt.Sprintf("pulls?state=open&per_page=100&page=%d", page))] < 2 {
					t.Fatalf("page %d was not refreshed before publication", page)
				}
			}
		})
	}
}

func TestOpenPRPaginationRejectsInconsistencies(t *testing.T) {
	for _, name := range []string{"duplicate", "invalid", "null page", "transport failure"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			pulls := manyOpenPRs(f, 101)
			setOpenPRPages(f, pulls)
			path := f.c.endpoint("pulls?state=open&per_page=100&page=2")
			switch name {
			case "duplicate":
				f.api.responses[path] = []pull{pulls[0]}
			case "invalid":
				pulls[100].State = "closed"
			case "null page":
				f.api.responses[path] = []pull(nil)
			case "transport failure":
				f.api.responses[path] = errors.New("transport failure")
			}
			if err := f.execute(); err == nil {
				t.Fatal("incomplete or inconsistent collision list accepted")
			}
			requireConclusion(t, f, "queued", "")
		})
	}
}
func TestImmutableMergeCandidateProof(t *testing.T) {
	for _, tc := range []struct {
		name   string
		modify func(*fixture)
		want   string
	}{
		{"source candidate differs", func(f *fixture) {
			f.source.References[0].SHA = strings.Repeat("d", 40)
			f.source.References[0].Path = f.c.repoName + "/.github/workflows/ci.yml@" + strings.Repeat("d", 40)
			f.refresh()
		}, ""},
		{"missing referenced workflow", func(f *fixture) { f.source.References = nil; f.refresh() }, ""},
		{"stacked synthetic base parent", func(f *fixture) {
			f.api.responses[f.c.endpoint("git/commits/"+f.pr.MergeCommitSHA)] = map[string]any{"sha": f.pr.MergeCommitSHA, "parents": []map[string]string{{"sha": strings.Repeat("e", 40)}, {"sha": f.pr.Head.SHA}}}
		}, "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.modify(f)
			if err := f.execute(); err != nil {
				t.Fatal(err)
			}
			status := "queued"
			if tc.want != "" {
				status = "completed"
			}
			requireConclusion(t, f, status, tc.want)
		})
	}
}
func TestRevalidateImmediatelyBeforeMutation(t *testing.T) {
	for _, field := range []string{"head", "base", "attempt", "merge candidate"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t)
			f.api.readHook = func(path string, n int) {
				if path == f.c.endpoint("pulls/7") && n == 4 {
					switch field {
					case "head":
						f.pr.Head.SHA = strings.Repeat("d", 40)
					case "base":
						f.pr.Base.SHA = strings.Repeat("d", 40)
					case "attempt":
						f.source.Attempt = 2
					case "merge candidate":
						f.pr.MergeCommitSHA = strings.Repeat("d", 40)
					}
					f.refresh()
				}
			}
			if err := f.execute(); err == nil {
				t.Fatal("changed state accepted")
			}
			for _, v := range f.api.writes {
				if v["conclusion"] == "success" {
					t.Fatal("changed state authorized success")
				}
			}
		})
	}
}
func TestAPIErrorAndMalformedDataFailClosed(t *testing.T) {
	for _, response := range []any{errors.New("transport unavailable"), map[string]string{"id": "not an integer"}} {
		f := newFixture(t)
		f.api.responses[f.c.endpoint("actions/runs/100")] = response
		if err := f.execute(); err == nil {
			t.Fatal("bad API response accepted")
		}
		for _, v := range f.api.writes {
			if v["conclusion"] == "success" {
				t.Fatal("API failure authorized success")
			}
		}
	}
	var value repository
	for _, data := range []string{"", "null", "{}{}", "<html>proxy</html>"} {
		if err := decode([]byte(data), &value); err == nil && data != "null" {
			t.Fatalf("accepted malformed JSON %q", data)
		}
	}
}
func TestForeignControllerCheckIdentityRejected(t *testing.T) {
	for _, field := range []string{"app", "external id", "unrelated actions check"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t)
			v := check{ID: 99, Name: "ci-required", HeadSHA: f.pr.Head.SHA, ExternalID: (snapshot{PR: f.pr}).binding(10).externalID()}
			v.App.ID = actionsAppID
			v.App.Slug = "github-actions"
			switch field {
			case "app":
				v.App.ID = 66
			case "external id":
				v.ExternalID = externalPrefix + "bogus"
			case "unrelated actions check":
				v.ExternalID = ""
			}
			f.api.checks = []check{v}
			if err := f.execute(); err == nil {
				t.Fatal("untrusted/ambiguous check accepted")
			}
			if len(f.api.writes) != 0 {
				t.Fatal("foreign check mutated")
			}
		})
	}
}
func TestSameHeadDifferentBaseRefusesBothCandidates(t *testing.T) {
	f := newFixture(t)
	other := f.pr
	other.Number = 8
	other.Base.Ref = "release"
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr, other}
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	requireConclusion(t, f, "completed", "failure")
}
func TestRerunRevokesPriorSuccessAndScheduleRepairsIt(t *testing.T) {
	f := newFixture(t)
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	f.source.Attempt = 2
	f.source.Status = "in_progress"
	f.source.Conclusion = ""
	f.verifyCode = 75
	f.refresh()
	if err := f.c.execute(context.Background(), "schedule", f.event); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != 4 || f.api.writes[2]["method"] != "POST" || f.api.writes[2]["status"] != "queued" || f.api.writes[3]["status"] != "queued" || f.api.writes[3]["conclusion"] != nil {
		t.Fatal("missed rerun did not revoke old success")
	}
}
func TestScheduleRepairsMissingInitialCheck(t *testing.T) {
	f := newFixture(t)
	if err := f.c.execute(context.Background(), "schedule", f.event); err != nil {
		t.Fatal(err)
	}
	requireConclusion(t, f, "completed", "success")
}
func TestForkVouchRequired(t *testing.T) {
	for _, vouched := range []bool{false, true} {
		t.Run(fmt.Sprint(vouched), func(t *testing.T) {
			f := newFixture(t)
			f.pr.Head.Repo = repository{ID: 11, FullName: "fork/Hikyo"}
			f.source.HeadRepository = f.pr.Head.Repo
			f.source.Pulls = []pull{f.pr}
			f.refresh()
			if vouched {
				f.api.responses[f.c.endpoint("actions/runs/100/attempts/1/jobs?per_page=100&page=1")] = map[string]any{"total_count": 2, "jobs": []job{{ID: 200, RunID: 100, Attempt: 1, Name: "validation / ci-required", Status: "completed", Conclusion: "success"}, {ID: 201, RunID: 100, Attempt: 1, Name: "vouch", Status: "completed", Conclusion: "success"}}}
			}
			if err := f.execute(); err != nil {
				t.Fatal(err)
			}
			want := "failure"
			if vouched {
				want = "success"
			}
			requireConclusion(t, f, "completed", want)
		})
	}
}

func forkRerunFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.pr.Head.Repo = repository{ID: 11, FullName: "fork/Hikyo"}
	f.source.HeadRepository = f.pr.Head.Repo
	f.source.Pulls = []pull{f.pr}
	f.source.Attempt = 2
	f.refresh()
	// GitHub's current execution includes the successful admission dependency
	// reused from attempt one, while only the aggregate ran in attempt two.
	f.api.responses[f.c.endpoint("actions/runs/100/jobs?filter=latest&per_page=100&page=1")] = map[string]any{"total_count": 2, "jobs": []map[string]any{
		{"id": 200, "run_id": 100, "run_attempt": 2, "head_sha": f.pr.Head.SHA, "name": "validation / ci-required", "status": "completed", "conclusion": "success"},
		{"id": 201, "run_id": 100, "run_attempt": 1, "head_sha": f.pr.Head.SHA, "name": "vouch", "status": "completed", "conclusion": "success"},
	}}
	return f
}

func TestForkRerunReusesOnlyAPICurrentSuccessfulVouch(t *testing.T) {
	f := forkRerunFixture(t)
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	requireConclusion(t, f, "completed", "success")
}

func TestForkRerunRefusesUnprovedOrRevokedVouch(t *testing.T) {
	for _, name := range []string{"missing", "failed", "wrong head", "wrong run", "future attempt", "zero attempt", "duplicate", "revoked", "API failure", "failed current admission"} {
		t.Run(name, func(t *testing.T) {
			f := forkRerunFixture(t)
			path := f.c.endpoint("actions/runs/100/jobs?filter=latest&per_page=100&page=1")
			response := f.api.responses[path].(map[string]any)
			entries := response["jobs"].([]map[string]any)
			vouch := entries[1]
			switch name {
			case "missing":
				response["jobs"], response["total_count"] = entries[:1], 1
			case "failed":
				vouch["conclusion"] = "failure"
			case "wrong head":
				vouch["head_sha"] = strings.Repeat("d", 40)
			case "wrong run":
				vouch["run_id"] = 101
			case "future attempt":
				vouch["run_attempt"] = 3
			case "zero attempt":
				vouch["run_attempt"] = 0
			case "duplicate":
				response["jobs"], response["total_count"] = append(entries, map[string]any{"id": 202, "run_id": 100, "run_attempt": 1, "head_sha": f.pr.Head.SHA, "name": "vouch", "status": "completed", "conclusion": "success"}), 3
			case "revoked":
				f.api.responses[f.c.endpoint("contents/.github/VOUCHED.td?ref=main")] = map[string]any{"type": "file", "encoding": "base64", "path": ".github/VOUCHED.td", "content": base64.StdEncoding.EncodeToString([]byte("-github:valentinhl revoked\n"))}
			case "API failure":
				f.api.responses[path] = errors.New("transport failure")
			case "failed current admission":
				f.api.responses[f.c.endpoint("actions/runs/100/attempts/2/jobs?per_page=100&page=1")] = map[string]any{"total_count": 2, "jobs": []job{{ID: 200, RunID: 100, Attempt: 2, Name: "validation / ci-required", Status: "completed", Conclusion: "success"}, {ID: 203, RunID: 100, Attempt: 2, Name: "vouch", Status: "completed", Conclusion: "failure"}}}
			}
			err := f.execute()
			if err != nil {
				requireConclusion(t, f, "queued", "")
				return
			}
			requireConclusion(t, f, "completed", "failure")
		})
	}
}
func TestMetadataOnlyRunDoesNotSupersedeValidation(t *testing.T) {
	f := newFixture(t)
	metadata := f.source
	metadata.ID = 101
	metadata.DisplayTitle = "pr-metadata #7"
	metadata.Conclusion = "success"
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/workflows/ci-fork.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=1", f.pr.Head.SHA))] = map[string]any{"total_count": 2, "workflow_runs": []run{metadata, f.source}}
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	requireConclusion(t, f, "completed", "success")
}
func TestDryRunAndObservationName(t *testing.T) {
	f := newFixture(t)
	f.c.dryRun = true
	f.c.checkName = "ci-required-next"
	var out bytes.Buffer
	f.c.output = &out
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != 0 || f.verifyCalls != 2 || !strings.Contains(out.String(), "ci-required-next completed (success)") {
		t.Fatalf("bad dry run %s", out.String())
	}
	f.c.checkName = "arbitrary"
	if err := f.execute(); err == nil {
		t.Fatal("arbitrary context accepted")
	}
}
func TestNoMutationRetry(t *testing.T) {
	f := newFixture(t)
	f.api.writeErr = errors.New("response lost")
	if err := f.execute(); err == nil {
		t.Fatal("write failure ignored")
	}
	if len(f.api.writes) != 1 {
		t.Fatal("mutation retried")
	}
}
func TestPreviousSuccessIsSupersededBeforeSourceReadFailure(t *testing.T) {
	f := newFixture(t)
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	f.api.responses[f.c.endpoint("actions/runs/100")] = errors.New("source API unavailable")
	if err := f.execute(); err == nil {
		t.Fatal("read failure ignored")
	}
	if len(f.api.checks) != 2 || f.api.checks[1].Status != "queued" || f.api.checks[1].Conclusion != "" || f.api.checks[1].ID <= f.api.checks[0].ID {
		t.Fatal("previous success remains latest after source failure")
	}
}
func TestSameHeadSameBaseHasIndependentPRAuthority(t *testing.T) {
	f := newFixture(t)
	other := f.pr
	other.Number = 8
	other.User.ID = 99
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr, other}
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	requireConclusion(t, f, "completed", "failure")
}
func TestMetadataCallbackPreservesCompletedCheck(t *testing.T) {
	f := newFixture(t)
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	f.event.Run = f.source
	f.event.Run.DisplayTitle = "pr-metadata #7"
	if err := f.c.execute(context.Background(), "workflow_run", f.event); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != 2 {
		t.Fatal("metadata callback revoked successful validation")
	}
}
func TestLateOldAttemptReauthorizesCurrentCheck(t *testing.T) {
	f := newFixture(t)
	old := f.source
	f.source.Attempt = 2
	f.refresh()
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	f.event.Run = old
	if err := f.c.execute(context.Background(), "workflow_run", f.event); err != nil {
		t.Fatal(err)
	}
	if len(f.api.checks) != 2 || f.api.checks[1].Conclusion != "success" {
		t.Fatal("old callback stranded current check pending")
	}
}
func TestTrustdownPinnedSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, contents string
		want           bool
		bad            bool
	}{
		{"vouched", "github:ValentinHL reason\n", true, false},
		{"platformless", "VALENTINHL\n", true, false},
		{"denounced first", "-github:valentinhl reason\ngithub:valentinhl\n", false, false},
		{"vouched first", "github:valentinhl\n-github:valentinhl later\n", true, false},
		{"other platform", "gitlab:valentinhl\n", false, false},
		{"comments", " # ignore\n\ngithub:valentinhl details with spaces\n", true, false},
		{"unsupported", "github:valentinhl:bad\n", false, true},
		{"revoked", "# empty list\n", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := trustdownVouched(tc.contents, "valentinhl")
			if (err != nil) != tc.bad || got != tc.want {
				t.Fatalf("got %v %v", got, err)
			}
		})
	}
}
func TestCurrentForkAdmissionPrecedenceAndRevocation(t *testing.T) {
	for _, tc := range []struct {
		name, login, permission, contents string
		want                              bool
		bad                               bool
	}{
		{"vouched", "valentinhl", "read", "github:valentinhl", true, false},
		{"revoked", "valentinhl", "read", "# removed", false, false},
		{"denounced", "valentinhl", "read", "-github:valentinhl reason", false, false},
		{"collaborator precedes denial", "valentinhl", "write", "-github:valentinhl reason", true, false},
		{"admin", "valentinhl", "admin", "", true, false},
		{"maintain is not upstream short circuit", "valentinhl", "maintain", "", false, false},
		{"literal bot", "dependabot[bot]", "", "", true, false},
		{"API unavailable", "valentinhl", "error", "github:valentinhl", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if err := f.c.init(context.Background()); err != nil {
				t.Fatal(err)
			}
			f.pr.User.Login = tc.login
			f.api.responses[f.c.endpoint("collaborators/"+tc.login+"/permission")] = map[string]any{"permission": tc.permission, "user": map[string]int{"id": 12}}
			if tc.permission == "error" {
				f.api.responses[f.c.endpoint("collaborators/"+tc.login+"/permission")] = errors.New("unavailable")
			}
			f.api.responses[f.c.endpoint("contents/.github/VOUCHED.td?ref=main")] = map[string]any{"type": "file", "encoding": "base64", "path": ".github/VOUCHED.td", "content": base64.StdEncoding.EncodeToString([]byte(tc.contents))}
			got, err := f.c.currentVouch(context.Background(), f.pr)
			if got != tc.want || (err != nil) != tc.bad {
				t.Fatalf("got %v %v", got, err)
			}
		})
	}
}
func TestFinalPolicyRevocationPublishesRefusal(t *testing.T) {
	for _, code := range []int{1, 75} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			f := newFixture(t)
			count := 0
			f.c.verify = func(context.Context, int, string) (int, error) {
				count++
				if count == 2 {
					return code, nil
				}
				return 0, nil
			}
			if err := f.execute(); err != nil {
				t.Fatal(err)
			}
			status, conclusion := "completed", "failure"
			if code == 75 {
				status, conclusion = "queued", ""
			}
			requireConclusion(t, f, status, conclusion)
		})
	}
}
func TestClosedPRAuthorityIsSupersededForNewPRBeforeReadError(t *testing.T) {
	f := newFixture(t)
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	prior, err := decodeBinding(f.api.checks[0].ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	prior.PR = 8
	f.api.checks[0].ExternalID = prior.externalID()
	f.api.responses[f.c.endpoint("actions/runs/100")] = errors.New("source unavailable")
	if err := f.execute(); err == nil {
		t.Fatal("source error ignored")
	}
	latest := f.api.checks[len(f.api.checks)-1]
	current, err := decodeBinding(latest.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Status != "queued" || latest.Conclusion != "" || current.PR != 7 || current.Run != 0 || current.Base != f.pr.Base.SHA {
		t.Fatal("closed PR's success was not replaced by current PR pending barrier")
	}
}
func TestInitialPendingIsVisibleBeforeSourceReadFailure(t *testing.T) {
	f := newFixture(t)
	f.api.responses[f.c.endpoint("actions/runs/100")] = errors.New("source unavailable")
	if err := f.execute(); err == nil {
		t.Fatal("source error ignored")
	}
	if len(f.api.checks) != 1 || f.api.checks[0].Status != "queued" {
		t.Fatal("missing initial event left no pending check")
	}
}
func TestHistoricalChecksDoNotExhaustCurrentIdentityLookup(t *testing.T) {
	f := newFixture(t)
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1500; i++ {
		f.api.checks = append(f.api.checks, check{ID: int64(2000 + i), Name: fmt.Sprintf("unrelated %d", i), HeadSHA: f.pr.Head.SHA})
	}
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	last := f.api.checks[len(f.api.checks)-1]
	if last.Conclusion != "success" {
		t.Fatal("unrelated historical checks prevented current success")
	}
	for path := range f.api.reads {
		if strings.Contains(path, "/check-runs?") && (!strings.Contains(path, "filter=latest") || !strings.Contains(path, "check_name=ci-required")) {
			t.Fatal("lookup scans unbounded historical/unrelated checks")
		}
	}
}
func TestLatestCheckPaginationExactMultiple(t *testing.T) {
	f := newFixture(t)
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 99; i++ {
		v := check{ID: int64(2000 + i), Name: "ci-required", HeadSHA: f.pr.Head.SHA}
		v.App.ID = int64(1000 + i)
		v.App.Slug = "independent-provider"
		f.api.checks = append(f.api.checks, v)
	}
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	for path := range f.api.reads {
		if strings.Contains(path, "/check-runs?") && strings.Contains(path, "page=2") {
			t.Fatal("exact 100 results unnecessarily fetched page 2")
		}
	}
}

func TestPublisherAcceptsLongBaseReference(t *testing.T) {
	f := newFixture(t)
	f.pr.Base.Ref = "dependabot/go_modules/" + strings.Repeat("long-module-name/", 20)
	f.source.Pulls = []pull{f.pr}
	f.refresh()
	if err := f.execute(); err != nil {
		t.Fatal(err)
	}
	requireConclusion(t, f, "completed", "success")
	for _, c := range f.api.checks {
		if len(c.ExternalID) > 255 {
			t.Fatal("identifier exceeds GitHub limit")
		}
	}
}

func legacyID(t *testing.T, b legacyBinding) string {
	t.Helper()
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return legacyExternalPrefix + base64.RawURLEncoding.EncodeToString(data)
}

func TestCompactBindingRoundTripAndLegacyCompatibility(t *testing.T) {
	const maxID = int64(1<<63 - 1)
	old := legacyBinding{RepoID: maxID, PR: int(maxID), Head: strings.Repeat("a", 40), Base: strings.Repeat("b", 40), BaseRef: strings.Repeat("branch/", 100), Run: maxID, Attempt: maxID}
	b, err := decodeBinding(legacyID(t, old))
	if err != nil {
		t.Fatal(err)
	}
	if b.BaseRef != sha256.Sum256([]byte(old.BaseRef)) {
		t.Fatal("full base ref was not bound")
	}
	if len(b.externalID()) != 156 {
		t.Fatalf("unexpected length %d", len(b.externalID()))
	}
	got, err := decodeBinding(b.externalID())
	if err != nil || got != b {
		t.Fatalf("round trip: %v, %#v", err, got)
	}
	old.BaseRef += "different"
	other, err := decodeBinding(legacyID(t, old))
	if err != nil || other.externalID() == b.externalID() {
		t.Fatal("base reference collision")
	}
	for _, invalid := range []string{b.externalID()[:100], b.externalID() + "=", externalPrefix + strings.Repeat("A", 139)} {
		if _, err := decodeBinding(invalid); err == nil {
			t.Fatal("malformed binding accepted")
		}
	}
	for _, invalid := range []binding{
		{RepoID: -1, PR: 1}, {RepoID: 1, PR: -1}, {RepoID: 1, PR: 1, Run: -1, Attempt: -1}, {RepoID: 1, PR: 1, Run: 1, Attempt: 0},
	} {
		if _, err := decodeBinding(invalid.externalID()); err == nil {
			t.Fatal("invalid IDs accepted")
		}
	}
}

func TestTruncatedLegacyRecordIsSupersededBeforeSourceRead(t *testing.T) {
	for _, status := range []string{"queued", "completed"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t)
			old := legacyBinding{RepoID: 10, PR: 8, Head: f.pr.Head.SHA, Base: strings.Repeat("d", 40), BaseRef: "old/" + strings.Repeat("branch/", 50), Run: 50, Attempt: 1}
			id := legacyID(t, old)[:255]
			v := check{ID: 99, Name: f.c.checkName, HeadSHA: f.pr.Head.SHA, ExternalID: id, Status: status}
			if status == "completed" {
				v.Conclusion = "success"
			}
			v.App.ID, v.App.Slug = actionsAppID, "github-actions"
			f.api.checks = []check{v}
			f.api.responses[f.c.endpoint("actions/runs/100")] = errors.New("source unavailable")
			if err := f.execute(); err == nil {
				t.Fatal("source failure ignored")
			}
			if len(f.api.writes) != 1 || f.api.writes[0]["status"] != "queued" || f.api.writes[0]["method"] != "POST" {
				t.Fatal("legacy result was not replaced by fresh pending barrier")
			}
			current := f.api.checks[len(f.api.checks)-1]
			b, err := decodeBinding(current.ExternalID)
			if err != nil || b.PR != f.pr.Number || b.Base != f.pr.Base.SHA || b.Run != 0 {
				t.Fatal("replacement does not bind current candidate")
			}
			f.refresh()
			f.api.writes = nil
			if err := f.execute(); err != nil {
				t.Fatal(err)
			}
			requireConclusion(t, f, "completed", "success")
		})
	}
}

func TestLegacyRecoveryRejectsForeignOrMalformedMetadata(t *testing.T) {
	for _, field := range []string{"repo", "head", "app", "short", "malformed", "overflow"} {
		t.Run(field, func(t *testing.T) {
			f := newFixture(t)
			old := legacyBinding{RepoID: 10, PR: 7, Head: f.pr.Head.SHA, Base: f.pr.Base.SHA, BaseRef: strings.Repeat("branch/", 50)}
			if field == "repo" {
				old.RepoID = 11
			}
			if field == "head" {
				old.Head = strings.Repeat("d", 40)
			}
			id := legacyID(t, old)[:255]
			if field == "short" {
				id = id[:254]
			}
			if field == "malformed" {
				id = legacyExternalPrefix + strings.Repeat("x", 255-len(legacyExternalPrefix))
			}
			if field == "overflow" {
				id = legacyExternalPrefix + base64.RawURLEncoding.EncodeToString([]byte(`{"repo":10,"pr":999999999999999999999999,"head":"`+f.pr.Head.SHA+`","base_ref":"`+strings.Repeat("b", 100)))
				id = id[:255]
			}
			v := check{ID: 99, Name: f.c.checkName, HeadSHA: f.pr.Head.SHA, ExternalID: id, Status: "completed", Conclusion: "success"}
			v.App.ID, v.App.Slug = actionsAppID, "github-actions"
			if field == "app" {
				v.App.ID = 66
			}
			f.api.checks = []check{v}
			if err := f.execute(); err == nil {
				t.Fatal("foreign or malformed metadata accepted")
			}
			if len(f.api.writes) != 0 {
				t.Fatal("foreign metadata mutated")
			}
		})
	}
}
