package main

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

const testMain = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testSource = "cccccccccccccccccccccccccccccccccccccccc"
const testDev = "dddddddddddddddddddddddddddddddddddddddd"

type fixture struct {
	repo         repository
	main, dev    string
	workflows    map[string]workflow
	histories    map[string][]run
	live         map[int64]run
	attempts     map[int64]run
	jobLists     map[int64][]job
	files        map[string][]byte
	reads        map[string]int
	override     func(string, int) (any, bool)
	denyAncestry map[string]bool
}

func newFixture(t *testing.T, mode string, p producer) *fixture {
	t.Helper()
	f := &fixture{
		repo: repository{ID: 123, FullName: "example/hikyo", DefaultBranch: "main"},
		main: testMain, dev: testDev,
		workflows: map[string]workflow{}, histories: map[string][]run{}, live: map[int64]run{},
		attempts: map[int64]run{}, jobLists: map[int64][]job{}, files: map[string][]byte{},
		reads: map[string]int{}, denyAncestry: map[string]bool{},
	}
	for index, definition := range append(append([]producer{}, mainProducers...), devProducer) {
		f.workflows[definition.File] = workflow{ID: int64(index + 21), Path: ".github/workflows/" + definition.File, Name: definition.Name, State: "active"}
		f.histories[definition.File] = []run{}
	}
	w := f.workflows[p.File]
	branch := p.Branch
	if p.Queue {
		branch = "gh-readonly-queue/main/pr-7-" + testMain
	}
	r := run{ID: 100, Attempt: 1, WorkflowID: w.ID, Name: p.Name, Path: w.Path, StartedAt: "2026-11-01T10:00:00Z",
		Event: p.Events[0], HeadSHA: testSource, HeadBranch: branch,
		Status: "completed", Conclusion: "success", Repository: f.repo, HeadRepository: f.repo}
	for _, path := range []string{".github/workflows/ci.yml", ".github/workflows/floor-bench.yml"} {
		if p.Legacy && path == ".github/workflows/ci.yml" {
			continue
		}
		r.References = append(r.References, workflowReference{Path: f.repo.FullName + "/" + path + "@" + testSource, Ref: "refs/heads/" + branch, SHA: testSource})
	}
	f.addRun(r, p)
	paths := append(append([]string{}, policyPaths...), w.Path)
	for _, path := range paths {
		data := []byte("approved immutable definition: " + path + "\n")
		if path == "scripts/ci/ci-job-registry.json" {
			var err error
			data, err = os.ReadFile(filepath.Join("..", "..", "..", path))
			if err != nil {
				t.Fatal(err)
			}
		}
		f.files[testMain+":"+path] = data
		f.files[testSource+":"+path] = data
	}
	return f
}

func (f *fixture) addRun(r run, p producer) {
	f.histories[p.File] = append(f.histories[p.File], r)
	f.live[r.ID], f.attempts[r.ID] = r, r
	selected := selectedRun{r, p, f.workflows[p.File]}
	names := make([]string, 0, len(expectedJobs(selected)))
	for name := range expectedJobs(selected) {
		names = append(names, name)
	}
	sort.Strings(names)
	jobs := make([]job, 0, len(names))
	for index, name := range names {
		expected := expectedJobs(selected)[name]
		steps := append([]string{}, expected.Steps...)
		if expected.Guard != "" {
			steps = append([]string{expected.Guard}, steps...)
		}
		current := job{ID: int64(index) + r.ID*1000, RunID: r.ID, Attempt: r.Attempt,
			HeadSHA: r.HeadSHA, Name: name, Status: "completed", Conclusion: "success", RunnerName: "GitHub Actions fixture",
			StartedAt: "2026-11-01T00:00:00Z", CompletedAt: "2026-11-01T00:01:00Z"}
		for number, name := range steps {
			current.Steps = append(current.Steps, step{Name: name, Number: number + 1, Status: "completed", Conclusion: "success", StartedAt: current.StartedAt, CompletedAt: current.CompletedAt})
		}
		jobs = append(jobs, current)
	}
	f.jobLists[r.ID] = jobs
}

func (f *fixture) mutateRun(mut func(*run)) {
	r := f.histories["ci-dev.yml"][0]
	mut(&r)
	f.histories["ci-dev.yml"][0] = r
	f.live[r.ID], f.attempts[r.ID] = r, r
}

func (f *fixture) mutateMain(mut func(*run)) {
	r := f.histories["ci-main.yml"][0]
	mut(&r)
	f.histories["ci-main.yml"][0] = r
	f.live[r.ID], f.attempts[r.ID] = r, r
}

func (f *fixture) read(_ context.Context, endpoint string, target any) error {
	f.reads[endpoint]++
	if f.override != nil {
		if data, ok := f.override(endpoint, f.reads[endpoint]); ok {
			return copyResponse(data, target)
		}
	}
	if endpoint == "repos/"+f.repo.FullName {
		return copyResponse(f.repo, target)
	}
	prefix := "repos/" + f.repo.FullName + "/"
	if !strings.HasPrefix(endpoint, prefix) {
		return fmt.Errorf("unexpected fixture repository: %s", endpoint)
	}
	parsed, err := url.Parse(strings.TrimPrefix(endpoint, prefix))
	if err != nil {
		return err
	}
	path, query := parsed.Path, parsed.Query()
	switch {
	case path == "git/ref/heads/main" || path == "git/ref/heads/dev":
		branch := strings.TrimPrefix(path, "git/ref/heads/")
		sha := f.main
		if branch == "dev" {
			sha = f.dev
		}
		return copyResponse(map[string]any{"ref": "refs/heads/" + branch, "object": map[string]any{"type": "commit", "sha": sha}}, target)
	case strings.HasPrefix(path, "compare/"):
		parts := strings.Split(strings.TrimPrefix(path, "compare/"), "...")
		if len(parts) != 2 {
			return errors.New("invalid fixture comparison")
		}
		mergeBase := parts[0]
		status := "ahead"
		if parts[0] == parts[1] {
			status = "identical"
		}
		if f.denyAncestry[parts[0]+"..."+parts[1]] {
			mergeBase, status = strings.Repeat("e", 40), "diverged"
		}
		return copyResponse(map[string]any{"status": status, "base_commit": map[string]any{"sha": parts[0]}, "merge_base_commit": map[string]any{"sha": mergeBase}}, target)
	case strings.HasPrefix(path, "actions/workflows/"):
		file := strings.TrimPrefix(path, "actions/workflows/")
		if !strings.HasSuffix(file, "/runs") {
			w, ok := f.workflows[file]
			if !ok {
				return errors.New("unknown workflow")
			}
			return copyResponse(w, target)
		}
		file = strings.TrimSuffix(file, "/runs")
		runs := []run{}
		for _, current := range f.histories[file] {
			if query.Get("head_sha") == "" || query.Get("head_sha") == current.HeadSHA {
				runs = append(runs, current)
			}
		}
		sort.Slice(runs, func(i, j int) bool { return runs[i].ID > runs[j].ID })
		page, err := strconv.Atoi(query.Get("page"))
		if err != nil || page <= 0 || query.Get("per_page") != "100" {
			return errors.New("invalid run pagination query")
		}
		start, end := min((page-1)*100, len(runs)), min(page*100, len(runs))
		return copyResponse(map[string]any{"total_count": len(runs), "workflow_runs": runs[start:end]}, target)
	case strings.HasPrefix(path, "actions/runs/"):
		parts := strings.Split(strings.TrimPrefix(path, "actions/runs/"), "/")
		id, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return err
		}
		if len(parts) == 1 {
			return copyResponse(f.live[id], target)
		}
		if len(parts) < 3 || parts[1] != "attempts" || parts[2] != strconv.FormatInt(f.attempts[id].Attempt, 10) {
			return errors.New("wrong attempt requested")
		}
		if len(parts) == 3 {
			return copyResponse(f.attempts[id], target)
		}
		if len(parts) != 4 || parts[3] != "jobs" || query.Get("per_page") != "100" {
			return errors.New("invalid job query")
		}
		page, err := strconv.Atoi(query.Get("page"))
		if err != nil || page <= 0 {
			return errors.New("invalid job pagination")
		}
		jobs := f.jobLists[id]
		start, end := min((page-1)*100, len(jobs)), min(page*100, len(jobs))
		return copyResponse(map[string]any{"total_count": len(jobs), "jobs": jobs[start:end]}, target)
	case strings.HasPrefix(path, "contents/"):
		name := strings.TrimPrefix(path, "contents/")
		data, ok := f.files[query.Get("ref")+":"+name]
		if !ok {
			return fmt.Errorf("unknown immutable fixture source %s", endpoint)
		}
		digest := sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(data))), data...))
		return copyResponse(map[string]any{"type": "file", "path": name, "sha": hex.EncodeToString(digest[:]), "encoding": "base64", "content": base64.StdEncoding.EncodeToString(data)}, target)
	default:
		return fmt.Errorf("unexpected fixture read %s", endpoint)
	}
}

func copyResponse(value any, target any) error {
	data, err := json.Marshal(value, jsonwire.Stable)
	if err != nil {
		return err
	}
	return decode(data, target)
}

func resolveFixture(f *fixture, opt options) (proof, error) {
	r := resolver{api: f, requested: f.repo.FullName}
	opt.Repository = f.repo.FullName
	return r.resolve(context.Background(), opt)
}

func TestFullSourceProofForApprovedProducers(t *testing.T) {
	for _, p := range append(append([]producer{}, mainProducers...), devProducer) {
		t.Run(p.Name, func(t *testing.T) {
			mode := "main"
			if p.File == "ci-dev.yml" {
				mode = "dev"
			}
			f := newFixture(t, mode, p)
			got, err := resolveFixture(f, options{Mode: mode, Commit: testSource, Authority: testMain})
			if err != nil {
				t.Fatal(err)
			}
			if got.Schema != proofSchema || got.Commit != testSource || got.AuthorityCommit != testMain || got.RunID != 100 || got.RunAttempt != 1 || len(got.ConfigurationSHA256) != 64 || got.WorkflowPath != ".github/workflows/"+p.File {
				t.Fatalf("wrong verified proof: %+v", got)
			}
			if got.Event != p.Events[0] || got.Ref != "refs/heads/"+f.live[100].HeadBranch {
				t.Fatal("receipt lost event/ref identity")
			}
			if f.reads["repos/example/hikyo/actions/runs/100"] != 2 {
				t.Fatal("source metadata was not independently reread")
			}
		})
	}
}

func TestFullSourceProofRejectsAdversarialEvidence(t *testing.T) {
	cases := map[string]func(*fixture){
		"wrong repository ID":   func(f *fixture) { f.mutateRun(func(r *run) { r.Repository.ID++ }) },
		"fork source":           func(f *fixture) { f.mutateRun(func(r *run) { r.HeadRepository.Fork = true }) },
		"wrong head repository": func(f *fixture) { f.mutateRun(func(r *run) { r.HeadRepository.ID++ }) },
		"PR producer":           func(f *fixture) { f.mutateRun(func(r *run) { r.Event = "pull_request" }) },
		"manual dev producer":   func(f *fixture) { f.mutateRun(func(r *run) { r.Event = "workflow_dispatch" }) },
		"wrong branch":          func(f *fixture) { f.mutateRun(func(r *run) { r.HeadBranch = "main" }) },
		"wrong workflow path":   func(f *fixture) { f.mutateRun(func(r *run) { r.Path = ".github/workflows/fake.yml" }) },
		"wrong workflow ID":     func(f *fixture) { f.mutateRun(func(r *run) { r.WorkflowID++ }) },
		"wrong workflow name":   func(f *fixture) { f.mutateRun(func(r *run) { r.Name = "fake" }) },
		"pending run":           func(f *fixture) { f.mutateRun(func(r *run) { r.Status, r.Conclusion = "in_progress", "" }) },
		"failed run":            func(f *fixture) { f.mutateRun(func(r *run) { r.Conclusion = "failure" }) },
		"cancelled run":         func(f *fixture) { f.mutateRun(func(r *run) { r.Conclusion = "cancelled" }) },
		"skipped run":           func(f *fixture) { f.mutateRun(func(r *run) { r.Conclusion = "skipped" }) },
		"zero run attempt":      func(f *fixture) { f.mutateRun(func(r *run) { r.Attempt = 0 }) },
		"duplicate run":         func(f *fixture) { f.histories["ci-dev.yml"] = append(f.histories["ci-dev.yml"], f.live[100]) },
		"disabled workflow": func(f *fixture) {
			w := f.workflows["ci-dev.yml"]
			w.State = "disabled_manually"
			f.workflows["ci-dev.yml"] = w
		},
		"foreign reusable source": func(f *fixture) {
			f.mutateRun(func(r *run) { r.References[0].Path = "foreign/repo/.github/workflows/ci.yml@" + testSource })
		},
		"wrong reusable digest":    func(f *fixture) { f.mutateRun(func(r *run) { r.References[0].SHA = testMain }) },
		"wrong reusable ref":       func(f *fixture) { f.mutateRun(func(r *run) { r.References[0].Ref = "refs/heads/main" }) },
		"missing reusable graph":   func(f *fixture) { f.mutateRun(func(r *run) { r.References = r.References[1:] }) },
		"duplicate reusable graph": func(f *fixture) { f.mutateRun(func(r *run) { r.References = append(r.References, r.References[0]) }) },
		"unapproved dev workflow":  func(f *fixture) { f.files[testSource+":.github/workflows/ci-dev.yml"] = []byte("forged graph") },
		"unapproved graph":         func(f *fixture) { f.files[testSource+":.github/workflows/ci.yml"] = []byte("forged graph") },
		"unapproved planner":       func(f *fixture) { f.files[testSource+":scripts/ci/analysis-shards-go/main.go"] = []byte("omit tests") },
		"unapproved registry":      func(f *fixture) { f.files[testSource+":scripts/ci/ci-job-registry.json"] = []byte(`{"version":2}`) },
		"missing execution job":    func(f *fixture) { f.jobLists[100] = f.jobLists[100][1:] },
		"duplicate execution name": func(f *fixture) { f.jobLists[100][1].Name = f.jobLists[100][0].Name },
		"duplicate execution ID":   func(f *fixture) { f.jobLists[100][1].ID = f.jobLists[100][0].ID },
		"wrong job attempt":        func(f *fixture) { f.jobLists[100][0].Attempt++ },
		"wrong job run":            func(f *fixture) { f.jobLists[100][0].RunID++ },
		"wrong job source":         func(f *fixture) { f.jobLists[100][0].HeadSHA = testMain },
		"skipped execution":        func(f *fixture) { f.jobLists[100][0].Conclusion = "skipped" },
		"synthetic execution":      func(f *fixture) { f.jobLists[100][0].RunnerName = "" },
		"missing positive step":    func(f *fixture) { f.jobLists[100][0].Steps = nil },
		"skipped positive step":    func(f *fixture) { f.jobLists[100][0].Steps[0].Conclusion = "skipped" },
		"duplicate step number":    func(f *fixture) { f.jobLists[100][0].Steps[1].Number = f.jobLists[100][0].Steps[0].Number },
		"failed execution step":    func(f *fixture) { f.jobLists[100][0].Steps[0].Conclusion = "failure" },
		"missing job interval":     func(f *fixture) { f.jobLists[100][0].StartedAt = "" },
		"source not on dev":        func(f *fixture) { f.denyAncestry[testSource+"..."+testDev] = true },
		"authority not on main":    func(f *fixture) { f.denyAncestry[testMain+"..."+testMain] = true },
		"newer failed attempt": func(f *fixture) {
			r := f.live[100]
			r.Attempt++
			r.Conclusion = "failure"
			f.live[100], f.attempts[100] = r, r
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, "dev", devProducer)
			mutate(f)
			if got, err := resolveFixture(f, options{Mode: "dev", Commit: testSource}); err == nil {
				t.Fatalf("accepted invalid evidence: %+v", got)
			}
		})
	}
}

func TestLatestDevKeepsGreenAncestorAndNeverRetargetsExactProof(t *testing.T) {
	f := newFixture(t, "dev", devProducer)
	newer := f.live[100]
	newer.ID, newer.HeadSHA, newer.Conclusion = 101, testDev, "failure"
	f.addRun(newer, devProducer)
	got, err := resolveFixture(f, options{Mode: "dev", Latest: true})
	if err != nil || got.Commit != testSource {
		t.Fatalf("last green ancestor lost behind a failed newer commit: %+v %v", got, err)
	}
	// A new source becoming green must not retarget an exact pinned source.
	newer.Conclusion = "success"
	f.histories["ci-dev.yml"][1] = newer
	f.live[101], f.attempts[101] = newer, newer
	for i := range f.jobLists[101] {
		f.jobLists[101][i].HeadSHA = testDev
	}
	got, err = resolveFixture(f, options{Mode: "dev", Commit: testSource})
	if err != nil || got.Commit != testSource || got.RunID != 100 {
		t.Fatalf("exact pin was retargeted: %+v %v", got, err)
	}
	// A newer failed execution for the same source invalidates its older pass.
	f.addRun(run{ID: 102, Attempt: 1, WorkflowID: f.workflows["ci-dev.yml"].ID, Name: "ci-dev", Path: ".github/workflows/ci-dev.yml", Event: "push", HeadSHA: testSource, HeadBranch: "dev", Status: "completed", Conclusion: "failure", Repository: f.repo, HeadRepository: f.repo}, devProducer)
	if _, err := resolveFixture(f, options{Mode: "dev", Commit: testSource}); err == nil {
		t.Fatal("older green source hid a newer same-source failure")
	}
}

func TestMainDoesNotHideNewerManualFailureBehindQueueProof(t *testing.T) {
	f := newFixture(t, "main", mainProducers[0])
	newer := f.live[100]
	newer.ID, newer.Name, newer.Path, newer.HeadBranch, newer.Event, newer.Conclusion = 101, "ci-main", ".github/workflows/ci-main.yml", "main", "workflow_dispatch", "failure"
	newer.WorkflowID = f.workflows["ci-main.yml"].ID
	f.addRun(newer, mainProducers[1])
	if _, err := resolveFixture(f, options{Mode: "main", Commit: testSource}); err == nil {
		t.Fatal("older queue proof hid a newer manual failure")
	}
}

// Reruns keep their run ID, so an older run's later failed attempt must still
// supersede a newer run's earlier success.
func TestMainOrdersExecutionsByLatestAttemptNotRunID(t *testing.T) {
	setup := func(t *testing.T, conclusion, rerunStart string) *fixture {
		f := newFixture(t, "main", mainProducers[0])
		manual := f.live[100]
		manual.ID, manual.Name, manual.Path, manual.HeadBranch, manual.Event = 200, "ci-main", ".github/workflows/ci-main.yml", "main", "workflow_dispatch"
		manual.WorkflowID, manual.StartedAt = f.workflows["ci-main.yml"].ID, "2026-11-01T11:00:00Z"
		manual.References = append([]workflowReference{}, manual.References...)
		for index := range manual.References {
			manual.References[index].Ref = "refs/heads/main"
		}
		f.addRun(manual, mainProducers[1])
		f.files[testMain+":"+manual.Path] = []byte("approved immutable definition\n")
		f.files[testSource+":"+manual.Path] = f.files[testMain+":"+manual.Path]
		rerun := f.live[100]
		rerun.Attempt, rerun.Conclusion, rerun.StartedAt = 2, conclusion, rerunStart
		f.histories["ci-merge-group.yml"][0] = rerun
		f.live[100], f.attempts[100] = rerun, rerun
		return f
	}
	for name, tc := range map[string]struct {
		conclusion, start string
		want              int64
	}{
		"later failed rerun refuses":     {"failure", "2026-11-01T12:00:00Z", 0},
		"later cancelled rerun refuses":  {"cancelled", "2026-11-01T12:00:00Z", 0},
		"same-second failure is refused": {"failure", "2026-11-01T11:00:00Z", 0},
		"earlier failure stays older":    {"failure", "2026-11-01T10:30:00Z", 200},
		"later success selects rerun":    {"success", "2026-11-01T12:00:00Z", 100},
		"same-second success takes ID":   {"success", "2026-11-01T11:00:00Z", 200},
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t, tc.conclusion, tc.start)
			for index := range f.jobLists[100] {
				f.jobLists[100][index].Attempt = 2
			}
			got, err := resolveFixture(f, options{Mode: "main", Commit: testSource})
			if tc.want == 0 {
				if err == nil {
					t.Fatalf("selected run %d despite a newer unsuccessful execution", got.RunID)
				}
				return
			}
			if err != nil || got.RunID != tc.want {
				t.Fatalf("selected %d/%v, want %d", got.RunID, err, tc.want)
			}
		})
	}
	for _, startedAt := range []string{"", "yesterday"} {
		f := newFixture(t, "dev", devProducer)
		f.mutateRun(func(r *run) { r.StartedAt = startedAt })
		if _, err := resolveFixture(f, options{Mode: "dev", Commit: testSource}); err == nil {
			t.Fatalf("accepted attempt start time %q", startedAt)
		}
	}
}

func TestScheduledMainRunIsQualifyingEvidence(t *testing.T) {
	for conclusion, wantProof := range map[string]bool{"success": true, "failure": false} {
		t.Run(conclusion, func(t *testing.T) {
			f := newFixture(t, "main", mainProducers[1])
			f.mutateMain(func(r *run) { r.Event, r.Conclusion = "schedule", conclusion })
			got, err := resolveFixture(f, options{Mode: "main", Commit: testSource})
			if (err == nil) != wantProof {
				t.Fatalf("scheduled %s: %+v %v", conclusion, got, err)
			}
		})
	}
}

func TestLatestDevDoesNotEnumerateItsEntireLifetimeHistory(t *testing.T) {
	f := newFixture(t, "dev", devProducer)
	for id := int64(101); id <= 2201; id++ {
		old := f.live[100]
		old.ID, old.HeadSHA, old.Conclusion = id, fmt.Sprintf("%040x", id), "failure"
		f.histories["ci-dev.yml"] = append(f.histories["ci-dev.yml"], old)
	}
	newest := f.live[100]
	newest.ID = 2202
	f.addRun(newest, devProducer)
	got, err := resolveFixture(f, options{Mode: "dev", Latest: true})
	if err != nil || got.RunID != 2202 || got.Commit != testSource {
		t.Fatalf("large lifetime history hid a recent valid source: %+v %v", got, err)
	}
	for path := range f.reads {
		if strings.Contains(path, "/runs?per_page=100&branch=dev&event=push&page=2") {
			t.Fatal("recent source discovery enumerated unrelated old history")
		}
	}
}

func TestFinalReadbackRefusesChangedProofAndAuthority(t *testing.T) {
	for _, scenario := range []string{"run", "attempt", "jobs", "main", "dev-ancestry", "workflow", "repository"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t, "dev", devProducer)
			f.override = func(endpoint string, count int) (any, bool) {
				switch {
				case scenario == "run" && endpoint == "repos/example/hikyo/actions/runs/100" && count == 2:
					r := f.live[100]
					r.Conclusion = "failure"
					return r, true
				case scenario == "attempt" && endpoint == "repos/example/hikyo/actions/runs/100/attempts/1" && count == 2:
					r := f.live[100]
					r.Attempt++
					return r, true
				case scenario == "jobs" && strings.Contains(endpoint, "/jobs?") && count == 2:
					return map[string]any{"total_count": 54, "jobs": f.jobLists[100][1:]}, true
				case scenario == "main" && strings.HasSuffix(endpoint, "git/ref/heads/main") && count == 2:
					return map[string]any{"ref": "refs/heads/main", "object": map[string]any{"sha": testDev, "type": "commit"}}, true
				case scenario == "dev-ancestry" && strings.Contains(endpoint, "compare/"+testSource+"..."+testDev) && count == 2:
					return map[string]any{"status": "diverged", "base_commit": map[string]any{"sha": testSource}, "merge_base_commit": map[string]any{"sha": testMain}}, true
				case scenario == "workflow" && strings.HasSuffix(endpoint, "actions/workflows/ci-dev.yml") && count == 2:
					w := f.workflows["ci-dev.yml"]
					w.State = "disabled_manually"
					return w, true
				case scenario == "repository" && endpoint == "repos/example/hikyo" && count == 2:
					r := f.repo
					r.DefaultBranch = "dev"
					return r, true
				}
				return nil, false
			}
			if _, err := resolveFixture(f, options{Mode: "dev", Commit: testSource}); err == nil {
				t.Fatal("accepted changed final proof")
			}
		})
	}
}

func TestCompleteJobPagination(t *testing.T) {
	for _, scenario := range []string{"complete", "missing-page", "duplicate-page", "changed-total"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t, "dev", devProducer)
			original := f.jobLists[100][0]
			f.jobLists[100] = nil
			for i := 0; i < 201; i++ {
				current := original
				current.ID = int64(i + 1)
				f.jobLists[100] = append(f.jobLists[100], current)
			}
			f.override = func(endpoint string, _ int) (any, bool) {
				if !strings.Contains(endpoint, "/jobs?per_page=100&page=2") {
					return nil, false
				}
				switch scenario {
				case "missing-page":
					return map[string]any{"total_count": 201, "jobs": []job{}}, true
				case "duplicate-page":
					return map[string]any{"total_count": 201, "jobs": f.jobLists[100][:100]}, true
				case "changed-total":
					return map[string]any{"total_count": 101, "jobs": f.jobLists[100][100:200]}, true
				}
				return nil, false
			}
			r := resolver{api: f, repo: f.repo}
			jobs, err := r.jobs(context.Background(), f.live[100])
			if (err == nil) != (scenario == "complete") || (err == nil && len(jobs) != 201) {
				t.Fatalf("wrong pagination disposition: %d %v", len(jobs), err)
			}
		})
	}
}

func TestSyntheticChecksCannotSupplyJobCoverage(t *testing.T) {
	for _, name := range []string{"ci-queue-required", "ci-dev-required"} {
		f := newFixture(t, "main", mainProducers[0])
		j := job{ID: 999999, RunID: 100, Attempt: 1, HeadSHA: testSource, Name: name, Status: "completed", Conclusion: "success"}
		f.jobLists[100] = append(f.jobLists[100], j)
		if _, err := resolveFixture(f, options{Mode: "main", Commit: testSource}); err != nil {
			t.Fatal(err)
		}
		f.jobLists[100] = f.jobLists[100][1:]
		if _, err := resolveFixture(f, options{Mode: "main", Commit: testSource}); err == nil {
			t.Fatal("synthetic record filled a missing native execution")
		}
	}
}

func TestClosedRegistryAndExecutionInventory(t *testing.T) {
	f := newFixture(t, "dev", devProducer)
	if len(f.jobLists[100]) != 54 {
		t.Fatalf("full schema must conserve 54 native executions: %d", len(f.jobLists[100]))
	}
	for _, name := range []string{"validation / web (desktop, 4)", "validation / web (mobile, 4)", "validation / race shard (11)", "validation / isolation shard (2)", "validation / fuzz shard (2)", "validation / isolation shard (5)", "validation / fuzz shard (5)", "validation / floor-bench / measure", "validation / scan-xplat (windows-latest)", "validation / source-artifact-security"} {
		found := false
		for _, current := range f.jobLists[100] {
			found = found || current.Name == name
		}
		if !found {
			t.Fatalf("lost mandatory execution %s", name)
		}
	}
	data := f.files[testMain+":scripts/ci/ci-job-registry.json"]
	if err := validateRegistry(data); err != nil {
		t.Fatal(err)
	}
	var registry map[string]any
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}
	registry["version"] = 3
	changed, _ := json.Marshal(registry, jsonwire.Stable)
	if err := validateRegistry(changed); err == nil {
		t.Fatal("unknown registry schema accepted")
	}
}

func TestStrictProviderJSON(t *testing.T) {
	for _, raw := range []string{`{"id":1,"id":2}`, `{"nested":{"a":1,"a":2}}`, `{} {}`, `{"a":1}junk`, `{"a":[{"x":1,"x":2}]}`} {
		var value any
		if err := decode([]byte(raw), &value); err == nil {
			t.Fatalf("accepted ambiguous JSON %s", raw)
		}
	}
	var value any
	if err := decode([]byte(`{"id":1,"nested":{"a":[1,2]}}`), &value); err != nil {
		t.Fatal(err)
	}
}

func TestCommandRejectsInvalidSelectorsBeforeProviderAccess(t *testing.T) {
	// A provider call would fail too, so make any such call visible.
	dir := t.TempDir()
	called := filepath.Join(dir, "called")
	stub := "#!/bin/sh\n: >\"" + called + "\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(stub), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_BIN", filepath.Join(dir, "gh"))
	t.Cleanup(func() {
		if _, err := os.Stat(called); err == nil {
			t.Fatal("invalid selectors reached the provider")
		}
	})
	for _, args := range [][]string{
		{}, {"--repository", "../bad", "--mode", "dev", "--latest"},
		{"--repository", "./bad", "--mode", "dev", "--latest"},
		{"--repository", "bad/..", "--mode", "dev", "--latest"},
		{"--repository", "bad/.", "--mode", "dev", "--latest"},
		{"--repository", "example/hikyo", "--mode", "main", "--latest"},
		{"--repository", "example/hikyo", "--mode", "dev", "--latest", "--commit", testSource},
		{"--repository", "example/hikyo", "--mode", "dev", "--commit", "dev"},
		{"--repository", "example/hikyo", "--mode", "dev", "--latest", "--authority-commit", "main"},
	} {
		if err := execute(context.Background(), args, os.Stdout); err == nil {
			t.Fatalf("accepted invalid selectors %v", args)
		}
	}
}

func TestReceiptContainsOnlyStableExactIdentity(t *testing.T) {
	f := newFixture(t, "dev", devProducer)
	a, err := resolveFixture(f, options{Mode: "dev", Commit: testSource, Authority: testMain})
	if err != nil {
		t.Fatal(err)
	}
	b, err := resolveFixture(f, options{Mode: "dev", Commit: testSource, Authority: testMain})
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("proof identity is nondeterministic: %+v %+v %v", a, b, err)
	}
}

// The closed shard inventory must follow the workflow planner counts; a count
// change that misses inventory.go would otherwise fail only post-merge proof.
func TestShardInventoryMatchesWorkflowPlanner(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	count := func(name string) int {
		m := regexp.MustCompile(`(?m)^\s*`+name+`=([0-9]+)$`).FindAllSubmatch(data, -1)
		if len(m) != 1 {
			t.Fatalf("ci.yml must assign %s exactly once, found %d", name, len(m))
		}
		n, err := strconv.Atoi(string(m[0][1]))
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	want := map[string]int{"isolation shard (": count("shard_count"), "fuzz shard (": count("shard_count"), "race shard (": count("race_count")}
	got := map[string]int{}
	for name := range expectedJobs(selectedRun{}) {
		for family := range want {
			if strings.HasPrefix(name, "validation / "+family) {
				got[family]++
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("source-proof shard inventory %v differs from ci.yml planner counts %v", got, want)
	}
}
