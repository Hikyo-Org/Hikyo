package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// Live GitHub payloads (Hikyo-Org/Hikyo, 2026-11-07): GET pulls/1245, a layer of
// stack #1248 whose base is the lower layer's branch, and the pull_requests of
// ci-feedback run 42402392631 for the same layer, which carries the real base and
// no stack field. Only identities the fixture owns are patched in.
func livePayload(t *testing.T, name string) any {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func patchLive(m map[string]any, f *queueFixture) map[string]any {
	m["number"] = float64(f.pr.Number)
	head, base := m["head"].(map[string]any), m["base"].(map[string]any)
	head["sha"] = f.pr.Head.SHA
	for _, side := range []struct {
		m    map[string]any
		repo repository
	}{{head, f.pr.Head.Repo}, {base, f.pr.Base.Repo}} {
		repo := side.m["repo"].(map[string]any)
		repo["id"] = float64(side.repo.ID)
		if _, ok := repo["full_name"]; ok {
			repo["full_name"] = side.repo.FullName
		}
	}
	return m
}

// stackedFixture serves PR 7 as the live layer, with the fixture's dev tip.
func stackedFixture(t *testing.T, stackBase string) (*queueFixture, string) {
	t.Helper()
	f := newDevFixture(t)
	tip := f.pr.Base.SHA
	layer := patchLive(livePayload(t, "live-pull-1245.json").(map[string]any), f)
	layer["stack"].(map[string]any)["base"].(map[string]any)["ref"] = stackBase
	layer["user"] = map[string]any{"id": float64(f.pr.User.ID), "login": f.pr.User.Login}
	f.api.responses[f.c.endpoint("pulls/7")] = layer
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []any{layer}
	var served pull
	data, _ := json.Marshal(layer)
	if err := decode(data, &served); err != nil {
		t.Fatal(err)
	}
	f.pr = served
	runs := livePayload(t, "live-run-42402392631-pull_requests.json").([]any)
	patchLive(runs[0].(map[string]any), f)
	data, _ = json.Marshal(runs)
	if err := decode(data, &f.feedbackRun.Pulls); err != nil {
		t.Fatal(err)
	}
	feedbackLanes(f)
	f.api.responses[f.c.endpoint(compareRange(tip, f.pr.Head.SHA))] = map[string]any{"status": "ahead", "files": []map[string]string{{"filename": "internal/app/app.go"}}}
	return f, tip
}

// feedbackLanes serves the run association in f.feedbackRun with every lane green.
func feedbackLanes(f *queueFixture) {
	f.feedbackRefresh()
	r := f.feedbackRun
	f.api.responses[f.c.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d/jobs?per_page=100&page=1", r.ID, r.Attempt))] = map[string]any{"total_count": 3, "jobs": []job{
		{ID: 201, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (go)", Status: "completed", Conclusion: "success"},
		{ID: 202, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (frontend)", Status: "completed", Conclusion: "success"},
		{ID: 203, RunID: r.ID, Attempt: r.Attempt, HeadSHA: r.HeadSHA, Name: "feedback (tooling)", Status: "completed", Conclusion: "success"},
	}}
}

func compareRange(base, head string) string { return "compare/" + base + "..." + head }

func executeStacked(f *queueFixture) error {
	return f.q.executeDev(context.Background(), "schedule", f.event, f.pr.Number)
}

func TestDevAdmissionPublishesForPullRequestStackedOnDev(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	f, tip := stackedFixture(t, "dev")
	var policyBase any
	verify := f.c.verify
	f.c.verify = func(ctx context.Context, n int, head string) (policyDecision, error) {
		policyBase = ctx.Value(policyFilesBase{})
		return verify(ctx, n, head)
	}
	f.q.dco = func(_ context.Context, p pull) (int, error) {
		if a := p.admission(); a.Ref != "dev" || a.SHA != tip {
			t.Fatalf("DCO scope is not the live dev tip: %+v", a)
		}
		return 0, nil
	}
	if err := executeStacked(f); err != nil {
		t.Fatal(err)
	}
	if len(f.api.checks) != 1 || f.api.checks[0].Name != devCheckName || f.api.checks[0].Conclusion != "success" {
		t.Fatalf("a layer of a stack on dev was not admitted: %+v", f.api.checks)
	}
	live := f.pr
	live.DevTip = tip
	bound, err := decodeBinding(f.api.checks[0].ExternalID)
	if err != nil || bound != newBinding(f.c.repo.ID, live, &f.feedbackRun) {
		t.Fatalf("stacked result is not bound to its real base and dev's live tip: %v", err)
	}
	if policyBase != tip {
		t.Fatalf("the shell verifier was not handed the resolved dev tip: %v", policyBase)
	}
	if f.api.reads[f.c.endpoint(compareRange(tip, f.pr.Head.SHA))] == 0 || f.api.reads[f.c.endpoint(devFilesPage(f, 1))] != 0 {
		t.Fatalf("stacked file policy did not read dev...head: %v", f.api.reads)
	}
}

// The live feedback run lists the layer's real base and no stack. The PR keeps its
// real base for that association, so it still matches.
func TestStackedLayerKeepsItsRealBaseForRunAssociation(t *testing.T) {
	f, _ := stackedFixture(t, "dev")
	if f.pr.Base.Ref != "feat/security-maintenance-foundations" || f.pr.Stack.Base.Ref != "dev" || f.pr.admission().Ref != "dev" {
		t.Fatalf("decoded layer: base=%+v stack=%+v", f.pr.Base, f.pr.Stack)
	}
	if len(f.feedbackRun.Pulls) != 1 || f.feedbackRun.Pulls[0].Stack.Base.Ref != "" || f.feedbackRun.Pulls[0].Base.Ref != f.pr.Base.Ref {
		t.Fatalf("fixture lost the live run association: %+v", f.feedbackRun.Pulls)
	}
	f.c.expectedHead = f.pr.Head.SHA
	got, err := f.q.feedback(context.Background(), f.pr)
	if err != nil || got == nil {
		t.Fatalf("feedback rejected the live run association: %v", err)
	}
}

func TestDevAdmissionLeavesStacksOnOtherBasesAlone(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	for _, base := range []string{"main", "feat/other"} {
		t.Run(base, func(t *testing.T) {
			f, _ := stackedFixture(t, base)
			err := executeStacked(f)
			var stale *staleHeadWork
			if !errors.As(err, &stale) {
				t.Fatalf("a stack on %s was assessed by dev admission: %v", base, err)
			}
			// Like any PR outside dev, it is retired to a pending record, never admitted.
			for _, c := range f.api.checks {
				if c.Conclusion != "" {
					t.Fatalf("dev admission concluded for a stack on %s: %+v", base, c)
				}
			}
		})
	}
}

// Only a PR to main has a queue record. The main gate sees the layer's real base.
func TestQueueGateLeavesStackedPullRequestsAlone(t *testing.T) {
	for _, base := range []string{"dev", "main", "feat/other"} {
		t.Run(base, func(t *testing.T) {
			f, _ := retirementFixture(t, queueCheckName)
			f.pr.Base.Ref = "feat/lower"
			f.pr.Stack.Base.Ref, f.pr.Stack.Base.SHA = base, f.pr.Base.SHA
			f.refresh()
			if err := f.q.admit(context.Background(), f.pr.Number); err != nil {
				t.Fatal(err)
			}
			if len(f.api.checks) != 1 || f.api.checks[0].Status != "completed" || f.api.checks[0].Conclusion != "cancelled" {
				t.Fatalf("queue record on a layer of a stack on %s was not retired: %+v", base, f.api.checks)
			}
		})
	}
}

// A malformed stack on any open PR must not stall the main queue, which never
// reads it. Dev admission refuses the PR itself.
func TestMalformedStackDoesNotStallMainQueue(t *testing.T) {
	f := newQueueFixture(t)
	bad := patchLive(livePayload(t, "live-pull-1245.json").(map[string]any), f)
	bad["number"], bad["stack"] = float64(8), map[string]any{"base": map[string]any{"ref": "dev", "sha": "abc"}}
	bad["head"].(map[string]any)["sha"] = strings.Repeat("7", 40)
	data, _ := json.Marshal(f.pr)
	var good map[string]any
	_ = json.Unmarshal(data, &good)
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []any{good, bad}
	if _, err := f.q.openPRs(context.Background()); err != nil {
		t.Fatalf("a malformed stack stalled the open PR list in main mode: %v", err)
	}
}

func TestMalformedPullStackFailsClosedForDev(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	sha := strings.Repeat("a", 40)
	for name, bad := range map[string]string{
		"string":      `"dev"`,
		"array":       `[]`,
		"empty":       `{}`,
		"null base":   `{"base":null}`,
		"ref number":  `{"base":{"ref":5,"sha":"` + sha + `"}}`,
		"empty ref":   `{"base":{"ref":"","sha":"` + sha + `"}}`,
		"short sha":   `{"base":{"ref":"dev","sha":"abc"}}`,
		"upper sha":   `{"base":{"ref":"dev","sha":"` + strings.ToUpper(sha) + `"}}`,
		"missing sha": `{"base":{"ref":"dev"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			var p pull
			if err := decode([]byte(`{"number":7,"base":{"ref":"feat/lower","sha":"`+sha+`"},"stack":`+bad+`}`), &p); err != nil || !p.StackInvalid || p.stackedOnDev() {
				t.Fatalf("malformed stack not flagged: %v %+v", err, p)
			}
			f, _ := stackedFixture(t, "dev")
			var parsed any
			_ = json.Unmarshal([]byte(bad), &parsed)
			f.api.responses[f.c.endpoint("pulls/7")].(map[string]any)["stack"] = parsed
			if err := executeStacked(f); err == nil {
				t.Fatal("dev admission continued on a malformed stack")
			}
			f.assertNoSuccess(t)
		})
	}
}

// devPublisherContract only runs for a PR whose admission base is dev, so a layer
// that skipped it would be admitted on a candidate-controlled publisher.
func TestDevPublisherContractRunsForStackedPullRequest(t *testing.T) {
	f, _ := stackedFixture(t, "dev")
	headCopy := f.c.endpoint("contents/" + devGateWorkflowPath + "?ref=" + f.pr.Head.SHA)
	f.api.responses[headCopy] = map[string]string{"type": "file", "path": devGateWorkflowPath, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte("candidate-controlled publisher"))}
	_ = f.q.admit(context.Background(), f.pr.Number)
	f.assertNoSuccess(t)
	if f.api.reads[headCopy] == 0 {
		t.Fatal("the publisher contract never read the stacked head's workflow copy")
	}
}

// The layer's own file list starts at the layer below. A lower layer's unapproved
// CI definition edit is in dev...head, so the layer above must be refused for it.
func TestStackedLayerIsRefusedForLowerLayerCIEdit(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	f, tip := stackedFixture(t, "dev")
	f.api.responses[f.c.endpoint(compareRange(tip, f.pr.Head.SHA))] = map[string]any{"status": "ahead", "files": []map[string]string{{"filename": "internal/app/app.go"}, {"filename": ".github/workflows/ci.yml", "status": "modified"}}}
	// pulls/7/files, the layer's own diff, is clean.
	f.api.responses[f.c.endpoint(devFilesPage(f, 1))] = []map[string]string{{"filename": "internal/app/app.go"}}
	ci := ".github/workflows/ci.yml"
	enc := func(s string) map[string]string {
		return map[string]string{"type": "file", "path": ci, "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(s))}
	}
	f.api.responses[f.c.endpoint("contents/"+ci+"?ref="+strings.Repeat("e", 40))] = enc("trusted ci")
	f.api.responses[f.c.endpoint("contents/"+ci+"?ref="+f.pr.Head.SHA)] = enc("lower layer edit")
	if err := executeStacked(f); err != nil {
		t.Fatal(err)
	}
	if len(f.api.checks) != 1 || f.api.checks[0].Conclusion != "failure" || !strings.Contains(f.api.checks[0].Output.Summary, ci) {
		t.Fatalf("a lower layer's CI edit was not refused: %+v", f.api.checks)
	}
}

func TestStackedFileListFailsClosedOnIncompleteCompare(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	for name, response := range map[string]map[string]any{
		"at the limit":  {"status": "ahead", "files": make([]map[string]string, compareFileLimit)},
		"behind":        {"status": "behind", "files": []map[string]string{}},
		"files omitted": {"status": "ahead"},
		"files null":    {"status": "ahead", "files": nil},
	} {
		t.Run(name, func(t *testing.T) {
			f, tip := stackedFixture(t, "dev")
			f.api.responses[f.c.endpoint(compareRange(tip, f.pr.Head.SHA))] = response
			_ = executeStacked(f)
			f.assertNoSuccess(t)
		})
	}
}

// A stacked layer and the same layer retargeted to dev are different tuples, as
// are layers over different real bases.
func TestStackedBindingDiffersFromRetargetedDirectPullRequest(t *testing.T) {
	f, tip := stackedFixture(t, "dev")
	stacked := f.pr
	stacked.DevTip = tip
	direct := stacked
	direct.Base.Ref, direct.Base.SHA, direct.Stack, direct.DevTip = "dev", tip, stack{}, ""
	moved := stacked
	moved.Base.Ref = "feat/another-layer"
	a, b, c := newBinding(f.c.repo.ID, stacked, nil), newBinding(f.c.repo.ID, direct, nil), newBinding(f.c.repo.ID, moved, nil)
	if a.Base != tip || b.Base != tip {
		t.Fatal("bindings do not carry dev's tip")
	}
	if a == b || a == c {
		t.Fatal("a stacked binding is interchangeable with a retargeted or re-based one")
	}
	if sameRolePull(direct, stacked) {
		t.Fatal("a retargeted PR matched its stacked role")
	}
}

// One commit cannot borrow dev admission: a stacked PR sharing its head with any
// other PR is ambiguous, whatever the other PR targets.
func TestDevAdmissionRefusesStackedHeadSharedWithAnotherPullRequest(t *testing.T) {
	for _, base := range []string{"dev", "main"} {
		t.Run(base, func(t *testing.T) {
			f, _ := stackedFixture(t, "dev")
			other := f.pr
			other.Number, other.Base.Ref, other.Stack = 8, base, stack{}
			f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = []any{f.api.responses[f.c.endpoint("pulls/7")], other}
			_ = f.q.admit(context.Background(), f.pr.Number)
			f.assertNoSuccess(t)
		})
	}
}

func TestPullStackDecodesBesideTheRealBase(t *testing.T) {
	sha, tip := strings.Repeat("a", 40), strings.Repeat("b", 40)
	base := func(ref string) string {
		return `"base":{"ref":"` + ref + `","sha":"` + sha + `","repo":{"id":3,"full_name":"o/r"}}`
	}
	stackOn := func(ref string) string {
		return `"stack":{"base":{"ref":"` + ref + `","sha":"` + tip + `"},"position":2}`
	}
	for name, tc := range map[string]struct {
		body, real string
		stacked    bool
		admission  string
	}{
		"stack on dev":         {`{` + base("feat/lower") + `,` + stackOn("dev") + `}`, "feat/lower", true, "dev"},
		"bottom layer":         {`{` + base("dev") + `,` + stackOn("dev") + `}`, "dev", false, "dev"},
		"stack on main":        {`{` + base("feat/lower") + `,` + stackOn("main") + `}`, "feat/lower", false, "feat/lower"},
		"stack on other":       {`{` + base("feat/lower") + `,` + stackOn("feat/x") + `}`, "feat/lower", false, "feat/lower"},
		"null stack":           {`{` + base("feat/lower") + `,"stack":null}`, "feat/lower", false, "feat/lower"},
		"no stack":             {`{` + base("dev") + `}`, "dev", false, "dev"},
		"no stack, other base": {`{` + base("feat/lower") + `}`, "feat/lower", false, "feat/lower"},
	} {
		t.Run(name, func(t *testing.T) {
			var p pull
			if err := decode([]byte(tc.body), &p); err != nil {
				t.Fatal(err)
			}
			if p.Base.Ref != tc.real || p.Base.SHA != sha || p.stackedOnDev() != tc.stacked || p.admission().Ref != tc.admission || p.StackInvalid {
				t.Fatalf("pull = %+v admission=%+v", p, p.admission())
			}
		})
	}
}

// A bottom layer is a plain dev PR, so a malformed stack on it is refused by the
// stack check itself, not by falling out of dev admission.
func TestMalformedStackOnBottomLayerIsRefused(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	f, _ := stackedFixture(t, "dev")
	layer := f.api.responses[f.c.endpoint("pulls/7")].(map[string]any)
	layer["base"].(map[string]any)["ref"] = "dev"
	layer["stack"] = map[string]any{"base": map[string]any{"ref": "dev", "sha": "abc"}}
	f.feedbackRun.Pulls[0].Base.Ref = "dev"
	feedbackLanes(f)
	if err := executeStacked(f); err == nil {
		t.Fatal("dev admission continued on a bottom layer with a malformed stack")
	}
	f.assertNoSuccess(t)
}

// Movement is judged on the whole base tuple: a stacked layer's dev tip, and the
// stack it belongs to.
func TestStackedMovementIsDetected(t *testing.T) {
	f, tip := stackedFixture(t, "dev")
	a := f.pr
	a.DevTip = tip
	b := a
	b.DevTip = strings.Repeat("f", 40)
	if a.sameBase(b) || !a.sameBase(a) {
		t.Fatal("a moved dev tip went unnoticed")
	}
	unstacked := a
	unstacked.Stack = stack{}
	if sameRolePull(unstacked, a) {
		t.Fatal("a layer that left its stack still matches its stacked role")
	}
}

// stack.base.sha follows the trunk tip, which Base.SHA is already exempt from here.
// Only the stack's presence and branch identify the role.
func TestRoleMatchToleratesTrunkMovementUnderAStack(t *testing.T) {
	f, tip := stackedFixture(t, "dev")
	proved := f.pr
	proved.DevTip = tip
	current := proved
	current.Stack.Base.SHA = strings.Repeat("e", 40)
	if !sameRolePull(current, proved) {
		t.Fatal("a moved trunk tip under an unchanged stack made the role stale")
	}
	for name, mutate := range map[string]func(*pull){
		"stack left":      func(p *pull) { p.Stack = stack{} },
		"stack rebased":   func(p *pull) { p.Stack.Base.Ref = "main" },
		"stack malformed": func(p *pull) { p.StackInvalid = true },
	} {
		changed := proved
		mutate(&changed)
		if sameRolePull(changed, proved) {
			t.Fatalf("%s still matches the proved role", name)
		}
	}
}

// A direct dev PR never enters stacked file-list mode: the verifier strips any
// inherited PR_GATE_FILES_BASE and sets it only from the controller's own context.
func TestVerifyPolicyStripsInheritedFilesBase(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/scripts/ci", 0o755); err != nil {
		t.Fatal(err)
	}
	seen := dir + "/seen"
	script := "#!/bin/sh\nprintf '%s' \"${PR_GATE_FILES_BASE:-unset}\" >\"" + seen + "\"\nprintf '{\"workflow_change_authorized\":false}\\n'\n"
	if err := os.WriteFile(dir+"/scripts/ci/check-fork-validation.sh", []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("PR_GATE_FILES_BASE", strings.Repeat("a", 40))
	head := strings.Repeat("b", 40)
	read := func() string {
		data, err := os.ReadFile(seen)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if _, err := verifyPolicy(context.Background(), 7, head); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "unset" {
		t.Fatalf("a direct PR inherited PR_GATE_FILES_BASE=%s", got)
	}
	tip := strings.Repeat("c", 40)
	ctx := context.WithValue(context.Background(), policyFilesBase{}, tip)
	if _, err := verifyPolicy(ctx, 7, head); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != tip {
		t.Fatalf("the stacked verifier saw base %s, want %s", got, tip)
	}
}
