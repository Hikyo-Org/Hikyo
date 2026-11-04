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
	"sync"
	"testing"
)

func legacySnapshotBinding(s snapshot, repoID int64) legacyBinding {
	b := s.binding(repoID)
	return legacyBinding{RepoID: b.RepoID, PR: b.PR, Head: b.Head, Base: b.Base, BaseRef: s.PR.Base.Ref, Run: b.Run, Attempt: b.Attempt}
}

func legacyExternalID(t *testing.T, b binding, ref string) string {
	old := legacyBinding{RepoID: b.RepoID, PR: b.PR, Head: b.Head, Base: b.Base, BaseRef: ref, Run: b.Run, Attempt: b.Attempt}
	t.Helper()
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	return legacyExternalPrefix + base64.RawURLEncoding.EncodeToString(data)
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
	invalid := []string{externalPrefix + "bogus", id + "=", noncanonical, id + ":extra", id[:len(id)-1]}
	for _, mutation := range []func(*binding){func(b *binding) { b.RepoID = -1 }, func(b *binding) { b.PR = -7 }, func(b *binding) { b.Run = 0 }, func(b *binding) { b.Attempt = -1 }} {
		changed := b
		mutation(&changed)
		invalid = append(invalid, changed.externalID())
	}
	for _, value := range invalid {
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
test "$PR_GATE_POLICY_ONLY" = 1
test "$GH_REPO" = Hikyo-Org/Hikyo
test "$GH_TOKEN" = controlled-test-token
printf '{"workflow_change_authorized":false}\n'
exit "$POLICY_FIXTURE_EXIT"
`
	if err := os.WriteFile(filepath.Join("scripts", "ci", "check-fork-validation.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PR_NUMBER", "wrong-pr")
	t.Setenv("HEAD_SHA", "wrong-head")
	t.Setenv("PR_GATE_ONCE", "0")
	t.Setenv("PR_GATE_POLICY_ONLY", "0")
	t.Setenv("GH_REPO", "Hikyo-Org/Hikyo")
	t.Setenv("GH_TOKEN", "controlled-test-token")
	for _, code := range []int{0, 1, 75, 2, 127} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Setenv("POLICY_FIXTURE_EXIT", strconv.Itoa(code))
			got, err := verifyPolicy(context.Background(), 877, strings.Repeat("a", 40))
			if code == 0 || code == 1 || code == 75 {
				if err != nil || got.Code != code {
					t.Fatalf("mapped decision=%v error=%v, want %d", got, err, code)
				}
			} else if err == nil {
				t.Fatalf("unexpected exit %d was accepted as %v", code, got)
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
	mu                sync.Mutex
	scopeChecksToHead bool
	responses         map[string]any
	readHook          func(string, int)
	reads             map[string]int
	writes            []map[string]any
	writeErr          error
	writeHook         func(context.Context, map[string]any) error
	checks            []check
	nextID            int64
}

func (f *fakeAPI) read(_ context.Context, path string, result any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads[path]++
	if f.readHook != nil {
		f.readHook(path, f.reads[path])
	}
	// Dev is absent unless a fixture installs it, so main admission stays unrouted.
	if _, ok := f.responses[path]; !ok && strings.HasSuffix(path, "/git/ref/heads/dev") {
		return &githubReadError{path: path, status: 404}
	}
	var response any
	if strings.Contains(path, "/check-runs?") {
		checks := f.checks
		if f.scopeChecksToHead {
			checks = nil
			for _, v := range f.checks {
				if strings.Contains(path, "/commits/"+v.HeadSHA+"/") {
					checks = append(checks, v)
				}
			}
		}
		u, err := url.Parse(path)
		if err != nil {
			return err
		}
		latest := make(map[int64]check)
		for _, v := range checks {
			if v.Name != u.Query().Get("check_name") {
				continue
			}
			if prior, ok := latest[v.App.ID]; !ok || v.ID > prior.ID {
				latest[v.App.ID] = v
			}
		}
		filtered := []check{}
		if u.Query().Get("filter") == "all" {
			for _, v := range checks {
				if v.Name == u.Query().Get("check_name") {
					filtered = append(filtered, v)
				}
			}
		} else {
			for _, v := range latest {
				filtered = append(filtered, v)
			}
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
	} else if strings.Contains(path, "/check-runs/") {
		for _, v := range f.checks {
			if strings.HasSuffix(path, fmt.Sprintf("/check-runs/%d", v.ID)) {
				response = v
				break
			}
		}
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
func (f *fakeAPI) write(ctx context.Context, method, path string, payload, result any) error {
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
			return errors.New("nullable check fields are unsupported")
		}
	}
	f.writes = append(f.writes, mutation)
	if f.writeErr != nil {
		return f.writeErr
	}
	if f.writeHook != nil {
		if err := f.writeHook(ctx, mutation); err != nil {
			return err
		}
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
	c                  *controller
	api                *fakeAPI
	pr                 pull
	source             run
	event              event
	verifyCode         int
	workflowAuthorized bool
	verifyCalls        int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	repo := repository{ID: 10, FullName: "Hikyo-Org/Hikyo", DefaultBranch: "main"}
	yes := true
	f.pr = pull{Number: 7, State: "open", Head: reference{SHA: strings.Repeat("a", 40), Ref: "feature", Repo: repo}, Base: reference{SHA: strings.Repeat("b", 40), Ref: "main", Repo: repo}, MergeCommitSHA: strings.Repeat("c", 40), Mergeable: &yes}
	f.pr.User.ID, f.pr.User.Login = 12, "valentinhl"
	f.source = run{ID: 100, Attempt: 1, WorkflowID: 20, Path: queueWorkflowPath, Name: "trusted-merge-ci", Event: "merge_group", HeadSHA: f.pr.Head.SHA, Status: "completed", Conclusion: "success", Repository: repo, HeadRepository: repo, Pulls: []pull{f.pr}}
	f.source.References = append(f.source.References, struct {
		Path string `json:"path"`
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
	}{Path: repo.FullName + "/.github/workflows/ci.yml@" + f.pr.MergeCommitSHA, Ref: "refs/pull/7/merge", SHA: f.pr.MergeCommitSHA})
	f.api = &fakeAPI{responses: make(map[string]any), reads: make(map[string]int), checks: []check{}}
	f.c = &controller{api: f.api, repoName: repo.FullName, checkName: "ci-required-next", output: &bytes.Buffer{}}
	f.c.verify = func(context.Context, int, string) (policyDecision, error) {
		f.verifyCalls++
		return policyDecision{Code: f.verifyCode, WorkflowChangeAuthorized: f.workflowAuthorized}, nil
	}
	f.api.responses["repos/"+repo.FullName] = repo
	f.api.responses[f.c.endpoint("collaborators/valentinhl/permission")] = map[string]any{"permission": "read", "user": map[string]int{"id": 12}}
	f.api.responses[f.c.endpoint("contents/.github/VOUCHED.td?ref=main")] = map[string]any{"type": "file", "encoding": "base64", "path": ".github/VOUCHED.td", "content": base64.StdEncoding.EncodeToString([]byte("github:valentinhl\n"))}
	f.event.Repository = repo
	f.event.Inputs.PR = "7"
	f.refresh()
	return f
}
func (f *fixture) refresh() {
	f.api.responses[f.c.endpoint("pulls/7")] = f.pr
	f.api.responses[f.c.endpoint("git/ref/heads/main")] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": f.pr.Base.SHA}}
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []pull{f.pr}
	f.api.responses[f.c.endpoint("actions/runs/100")] = f.source
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/100/attempts/%d/jobs?per_page=100&page=1", f.source.Attempt))] = map[string]any{"total_count": 2, "jobs": []job{{ID: 200, RunID: 100, Attempt: f.source.Attempt, Name: "validation / ci-required", Status: "completed", Conclusion: "success"}, {ID: 250, RunID: 100, Attempt: f.source.Attempt, HeadSHA: f.source.HeadSHA, Name: "candidate-proof", Status: "completed", Conclusion: "success"}}}
	f.api.responses[f.c.endpoint("git/commits/"+f.pr.MergeCommitSHA)] = map[string]any{"sha": f.pr.MergeCommitSHA, "parents": []map[string]string{{"sha": f.pr.Base.SHA}, {"sha": f.pr.Head.SHA}}}
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

func TestPolicyDecisionRequiresMachineReadableAuthority(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("scripts/ci", 0o755); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{"", "{}", "not-json", `{"workflow_change_authorized":"true"}`, `{"workflow_change_authorized":true} {}`} {
		if err := os.WriteFile("scripts/ci/check-fork-validation.sh", []byte("#!/bin/sh\nprintf '%s' '"+output+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := verifyPolicy(context.Background(), 7, strings.Repeat("a", 40)); err == nil {
			t.Fatalf("malformed workflow authority accepted: %q", output)
		}
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
			if err := f.c.initRepository(context.Background()); err != nil {
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

func TestOpenPRPaginationBeyondTwoPages(t *testing.T) {
	f := ownedCheckFixture(t)
	setOpenPRPages(f, manyOpenPRs(f, 251))
	pulls, err := f.c.openPRs(context.Background())
	if err != nil || len(pulls) != 251 {
		t.Fatalf("got %d PRs: %v", len(pulls), err)
	}
}

func TestOpenPRPaginationRejectsInconsistencies(t *testing.T) {
	for _, name := range []string{"duplicate", "invalid", "null page", "transport failure"} {
		t.Run(name, func(t *testing.T) {
			f := ownedCheckFixture(t)
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
			if _, err := f.c.openPRs(context.Background()); err == nil {
				t.Fatal("incomplete or inconsistent PR list accepted")
			}
		})
	}
}

func TestAPIDecodingRejectsMalformedData(t *testing.T) {
	var value repository
	for _, data := range []string{"", "null", "{}{}", "<html>proxy</html>"} {
		if err := decode([]byte(data), &value); err == nil && data != "null" {
			t.Fatalf("accepted malformed JSON %q", data)
		}
	}
}

// These read helpers are shared by the queue, dev and retirement writers.
func ownedCheckFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.c.repo = f.pr.Base.Repo
	return f
}

func TestForeignControllerCheckIdentityRejected(t *testing.T) {
	for _, field := range []string{"app", "external id", "unrelated actions check"} {
		t.Run(field, func(t *testing.T) {
			f := ownedCheckFixture(t)
			v := check{ID: 99, Name: f.c.checkName, HeadSHA: f.pr.Head.SHA, ExternalID: (snapshot{PR: f.pr}).binding(10).externalID()}
			v.App.ID, v.App.Slug = actionsAppID, "github-actions"
			switch field {
			case "app":
				v.App.ID = 66
			case "external id":
				v.ExternalID = externalPrefix + "bogus"
			case "unrelated actions check":
				v.ExternalID = ""
			}
			f.api.checks = []check{v}
			if _, err := f.c.listOwnedChecks(context.Background(), f.pr.Head.SHA); err == nil {
				t.Fatal("untrusted or ambiguous check accepted")
			}
		})
	}
}

func TestTruncatedLegacyHostileIdentityCannotBeRepaired(t *testing.T) {
	for _, field := range []string{"repository", "invalid PR", "head", "app", "check head", "length", "encoding", "invalid suffix", "overflow"} {
		t.Run(field, func(t *testing.T) {
			f := ownedCheckFixture(t)
			f.pr.Base.Ref = "dependabot/go_modules/github.com/sigstore/sigstore-1.11.0"
			b := legacySnapshotBinding(snapshot{PR: f.pr}, 10)
			switch field {
			case "repository":
				b.RepoID++
			case "invalid PR":
				b.PR = -7
			case "head":
				b.Head = strings.Repeat("c", 40)
			}
			v := check{ID: 99, Name: f.c.checkName, HeadSHA: f.pr.Head.SHA, ExternalID: legacyID(t, b)[:externalIDLimit], Status: "queued"}
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
			case "overflow":
				v.ExternalID = (legacyExternalPrefix + base64.RawURLEncoding.EncodeToString([]byte(`{"repo":10,"pr":999999999999999999999999,"head":"`+f.pr.Head.SHA+`","base_ref":"`+strings.Repeat("b", 200))))[:externalIDLimit]
			}
			f.api.checks = []check{v}
			if _, err := f.c.listOwnedChecks(context.Background(), f.pr.Head.SHA); err == nil {
				t.Fatal("malformed foreign check accepted")
			}
		})
	}
}

func TestHistoricalChecksDoNotExhaustCurrentIdentityLookup(t *testing.T) {
	f := ownedCheckFixture(t)
	for i := 0; i < 1500; i++ {
		f.api.checks = append(f.api.checks, check{ID: int64(2000 + i), Name: fmt.Sprintf("unrelated %d", i), HeadSHA: f.pr.Head.SHA})
	}
	if _, err := f.c.listOwnedChecks(context.Background(), f.pr.Head.SHA); err != nil {
		t.Fatal(err)
	}
	for path := range f.api.reads {
		if strings.Contains(path, "/check-runs?") && (!strings.Contains(path, "filter=all") || !strings.Contains(path, "check_name="+f.c.checkName)) {
			t.Fatal("lookup scans unbounded historical or unrelated checks")
		}
	}
}

func TestLatestCheckPaginationExactMultiple(t *testing.T) {
	f := ownedCheckFixture(t)
	for i := 0; i < 100; i++ {
		v := check{ID: int64(2000 + i), Name: f.c.checkName, HeadSHA: f.pr.Head.SHA}
		v.App.ID, v.App.Slug = int64(1000+i), "independent-provider"
		f.api.checks = append(f.api.checks, v)
	}
	if _, err := f.c.listOwnedChecks(context.Background(), f.pr.Head.SHA); err != nil {
		t.Fatal(err)
	}
	for path := range f.api.reads {
		if strings.Contains(path, "/check-runs?") && strings.Contains(path, "page=2") {
			t.Fatal("exact 100 results unnecessarily fetched page 2")
		}
	}
}

func TestCheckListingRefusesImpossibleCount(t *testing.T) {
	f := ownedCheckFixture(t)
	for i := 0; i < 1001; i++ {
		f.api.checks = append(f.api.checks, check{ID: int64(i + 1), Name: f.c.checkName})
	}
	if _, err := f.c.listOwnedChecks(context.Background(), f.pr.Head.SHA); err == nil {
		t.Fatal("unbounded historical listing accepted")
	}
}

func TestLiveMainRefOverridesStalePullMetadata(t *testing.T) {
	f := ownedCheckFixture(t)
	path := f.c.endpoint("git/ref/heads/main")
	live := strings.Repeat("d", 40)
	f.api.responses[path] = map[string]any{"ref": "refs/heads/main", "object": map[string]string{"type": "commit", "sha": live}}
	p, err := f.c.readPR(context.Background(), 7)
	if err != nil || p.Base.SHA != live {
		t.Fatalf("base %s did not bind the live main tip: %v", p.Base.SHA, err)
	}
	f.api.responses[path] = errors.New("branch ref temporarily unavailable")
	if _, err := f.c.readPR(context.Background(), 7); err == nil {
		t.Fatal("live ref failure ignored")
	}
	f.api.responses[path] = map[string]any{"ref": "refs/heads/other", "object": map[string]string{"type": "commit", "sha": live}}
	if _, err := f.c.readPR(context.Background(), 7); err == nil {
		t.Fatal("foreign live ref accepted")
	}
}

func TestPublishRefusesEveryContextOutsideQueueAndDev(t *testing.T) {
	f := ownedCheckFixture(t)
	if err := f.c.publish(context.Background(), f.pr, (snapshot{PR: f.pr}).binding(10), nil, pending("legacy")); err == nil || len(f.api.writes) != 0 {
		t.Fatalf("legacy context published: %v %v", err, f.api.writes)
	}
}
