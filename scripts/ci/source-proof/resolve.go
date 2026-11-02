package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"
)

const maxPages = 20

type repository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Fork          bool   `json:"fork"`
}

type workflow struct {
	ID    int64  `json:"id"`
	Path  string `json:"path"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type workflowReference struct {
	Path string `json:"path"`
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
}

type run struct {
	ID             int64               `json:"id"`
	Attempt        int64               `json:"run_attempt"`
	WorkflowID     int64               `json:"workflow_id"`
	Name           string              `json:"name"`
	Path           string              `json:"path"`
	Event          string              `json:"event"`
	HeadSHA        string              `json:"head_sha"`
	HeadBranch     string              `json:"head_branch"`
	Status         string              `json:"status"`
	Conclusion     string              `json:"conclusion"`
	StartedAt      string              `json:"run_started_at"`
	Repository     repository          `json:"repository"`
	HeadRepository repository          `json:"head_repository"`
	References     []workflowReference `json:"referenced_workflows"`
}

type producer struct {
	File, Name, Branch string
	Events             []string
	Queue, Legacy      bool
}

var mainProducers = []producer{
	{File: "ci-merge-group.yml", Name: "trusted-merge-ci", Events: []string{"merge_group"}, Queue: true},
	{File: "ci-main.yml", Name: "ci-main", Branch: "main", Events: []string{"workflow_dispatch"}},
	{File: "ci.yml", Name: "ci", Branch: "main", Events: []string{"push", "workflow_dispatch"}, Legacy: true},
}

var devProducer = producer{File: "ci-dev.yml", Name: "ci-dev", Branch: "dev", Events: []string{"push"}}

type selectedRun struct {
	run      run
	producer producer
	workflow workflow
}

type resolver struct {
	api       reader
	requested string
	repo      repository
	mainHead  string
	authority string
	files     map[string][]byte
}

func (r *resolver) endpoint(path string) string { return "repos/" + r.repo.FullName + "/" + path }

func (r *resolver) resolve(ctx context.Context, opt options) (proof, error) {
	var empty proof
	if err := r.api.read(ctx, "repos/"+r.requested, &r.repo); err != nil {
		return empty, err
	}
	if r.repo.ID <= 0 || !repoPattern.MatchString(r.repo.FullName) || !strings.EqualFold(r.repo.FullName, r.requested) || r.repo.DefaultBranch != "main" || r.repo.Fork {
		return empty, errors.New("source proof requires the canonical non-fork repository with main as default")
	}
	mainHead, err := r.branch(ctx, "main")
	if err != nil {
		return empty, err
	}
	r.mainHead, r.authority = mainHead, opt.Authority
	if r.authority == "" {
		r.authority = mainHead
	}
	if err := r.ancestor(ctx, r.authority, mainHead); err != nil {
		return empty, fmt.Errorf("approved authority is not on main: %w", err)
	}
	r.files = map[string][]byte{}
	var selected selectedRun
	if opt.Mode == "dev" {
		selected, err = r.selectDev(ctx, opt)
	} else {
		if err = r.ancestor(ctx, opt.Commit, mainHead); err == nil {
			selected, err = r.selectMain(ctx, opt.Commit)
		}
	}
	if err != nil {
		return empty, err
	}
	configuration, err := r.verify(ctx, selected)
	if err != nil {
		return empty, err
	}
	// Resolve the same exact commit again, not the moving dev tip or a new
	// "latest green" selection. Newer failed runs/attempts must invalidate it.
	var final selectedRun
	if opt.Mode == "dev" {
		final, err = r.selectDev(ctx, options{Mode: "dev", Commit: selected.run.HeadSHA})
	} else {
		final, err = r.selectMain(ctx, selected.run.HeadSHA)
	}
	if err != nil || !reflect.DeepEqual(selected, final) {
		return empty, errors.New("selected full-validation run or attempt changed during verification")
	}
	finalJobs, err := r.jobs(ctx, selected.run)
	if err != nil {
		return empty, err
	}
	if err := validateJobs(selected, finalJobs); err != nil {
		return empty, err
	}
	finalMain, err := r.branch(ctx, "main")
	if err != nil || finalMain != r.mainHead {
		return empty, errors.New("main authority moved during source verification; retry with a fresh snapshot")
	}
	var finalRepo repository
	if err := r.api.read(ctx, "repos/"+r.repo.FullName, &finalRepo); err != nil {
		return empty, err
	}
	if finalRepo != r.repo {
		return empty, errors.New("repository authority changed during source verification")
	}
	return proof{
		Schema: proofSchema, Repository: r.repo.FullName, RepositoryID: r.repo.ID,
		Commit: selected.run.HeadSHA, Ref: "refs/heads/" + selected.run.HeadBranch,
		WorkflowPath: selected.run.Path, Event: selected.run.Event,
		RunID: selected.run.ID, RunAttempt: selected.run.Attempt, WorkflowID: selected.run.WorkflowID,
		AuthorityCommit: r.authority, ConfigurationSHA256: configuration,
	}, nil
}

func (r *resolver) branch(ctx context.Context, name string) (string, error) {
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	if err := r.api.read(ctx, r.endpoint("git/ref/heads/"+url.PathEscape(name)), &ref); err != nil {
		return "", err
	}
	if ref.Ref != "refs/heads/"+name || ref.Object.Type != "commit" || !shaPattern.MatchString(ref.Object.SHA) {
		return "", errors.New("invalid immutable branch identity")
	}
	return ref.Object.SHA, nil
}

func (r *resolver) ancestor(ctx context.Context, base, head string) error {
	if !shaPattern.MatchString(base) || !shaPattern.MatchString(head) {
		return errors.New("invalid ancestry source")
	}
	var compare struct {
		Status string `json:"status"`
		Base   struct {
			SHA string `json:"sha"`
		} `json:"base_commit"`
		MergeBase struct {
			SHA string `json:"sha"`
		} `json:"merge_base_commit"`
	}
	if err := r.api.read(ctx, r.endpoint("compare/"+base+"..."+head), &compare); err != nil {
		return err
	}
	if compare.Base.SHA != base || compare.MergeBase.SHA != base || (compare.Status != "ahead" && compare.Status != "identical") {
		return errors.New("source is not an ancestor of the protected branch")
	}
	return nil
}

func (r *resolver) loadWorkflow(ctx context.Context, p producer) (workflow, error) {
	var w workflow
	if err := r.api.read(ctx, r.endpoint("actions/workflows/"+p.File), &w); err != nil {
		return w, err
	}
	if w.ID <= 0 || w.Path != ".github/workflows/"+p.File || w.Name != p.Name || w.State == "" {
		return w, errors.New("unapproved producer workflow")
	}
	return w, nil
}

func (r *resolver) runs(ctx context.Context, p producer, w workflow, commit string) ([]run, error) {
	query := "actions/workflows/" + p.File + "/runs?per_page=100"
	if commit != "" {
		query += "&head_sha=" + commit
	}
	if p.Branch != "" {
		query += "&branch=" + p.Branch
	}
	if len(p.Events) == 1 {
		query += "&event=" + p.Events[0]
	}
	var all []run
	seen := map[int64]bool{}
	total := -1
	for page := 1; page <= maxPages; page++ {
		var response struct {
			Total int   `json:"total_count"`
			Runs  []run `json:"workflow_runs"`
		}
		if err := r.api.read(ctx, r.endpoint(fmt.Sprintf("%s&page=%d", query, page)), &response); err != nil {
			return nil, err
		}
		if response.Runs == nil || response.Total < 0 || response.Total > maxPages*100 || len(response.Runs) > 100 || (total >= 0 && response.Total != total) {
			return nil, errors.New("incomplete or changing workflow-run pagination")
		}
		total = response.Total
		for _, current := range response.Runs {
			if seen[current.ID] || r.validateRun(current, p, w, commit) != nil {
				return nil, errors.New("duplicated or invalid producer run")
			}
			seen[current.ID] = true
			all = append(all, current)
		}
		if len(all) > total || (len(response.Runs) < 100 && len(all) != total) {
			return nil, errors.New("workflow-run list does not conserve total_count")
		}
		if len(all) == total {
			return all, nil
		}
	}
	return nil, errors.New("workflow-run history exceeds bounded complete discovery")
}

func (r *resolver) validateRun(current run, p producer, w workflow, commit string) error {
	if current.ID <= 0 || current.Attempt <= 0 || current.WorkflowID != w.ID || current.Name != p.Name || current.Path != w.Path ||
		!shaPattern.MatchString(current.HeadSHA) || (commit != "" && current.HeadSHA != commit) || !validStart(current.StartedAt) ||
		!equalRepository(current.Repository, r.repo) || !equalRepository(current.HeadRepository, r.repo) || current.HeadRepository.Fork {
		return errors.New("invalid producer identity")
	}
	event := false
	for _, expected := range p.Events {
		event = event || current.Event == expected
	}
	if !event {
		return errors.New("producer event is not approved")
	}
	if p.Queue {
		if !regexpQueue.MatchString(current.HeadBranch) {
			return errors.New("unsupported main merge-group topology")
		}
	} else if current.HeadBranch != p.Branch {
		return errors.New("producer branch is not approved")
	}
	return nil
}

func latest(runs []selectedRun) (selectedRun, error) {
	if len(runs) == 0 {
		return selectedRun{}, errors.New("no qualifying full-validation source run")
	}
	// A rerun keeps its run ID, so only the latest attempt's start orders
	// executions. RFC 3339 UTC seconds from the API compare as parsed times.
	started := func(r selectedRun) time.Time {
		t, _ := time.Parse(time.RFC3339, r.run.StartedAt)
		return t
	}
	sort.Slice(runs, func(i, j int) bool {
		if a, b := started(runs[i]), started(runs[j]); !a.Equal(b) {
			return a.After(b)
		}
		return runs[i].run.ID > runs[j].run.ID
	})
	if len(runs) > 1 && runs[0].run.ID == runs[1].run.ID {
		return selectedRun{}, errors.New("ambiguous source run")
	}
	// One-second timestamps cannot order a same-second tie; never let it hide an
	// unsuccessful execution behind a success.
	for _, tied := range runs[1:] {
		if started(tied).Equal(started(runs[0])) && (tied.run.Status != "completed" || tied.run.Conclusion != "success") {
			return selectedRun{}, errors.New("ambiguous source run order")
		}
	}
	return runs[0], nil
}

func validStart(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

func (r *resolver) selectMain(ctx context.Context, commit string) (selectedRun, error) {
	var candidates []selectedRun
	for _, p := range mainProducers {
		w, err := r.loadWorkflow(ctx, p)
		if err != nil {
			return selectedRun{}, err
		}
		runs, err := r.runs(ctx, p, w, commit)
		if err != nil {
			return selectedRun{}, err
		}
		for _, current := range runs {
			candidates = append(candidates, selectedRun{current, p, w})
		}
	}
	chosen, err := latest(candidates)
	if err != nil {
		return chosen, err
	}
	return r.liveRun(ctx, chosen)
}

func (r *resolver) selectDev(ctx context.Context, opt options) (selectedRun, error) {
	devHead, err := r.branch(ctx, "dev")
	if err != nil {
		return selectedRun{}, err
	}
	w, err := r.loadWorkflow(ctx, devProducer)
	if err != nil {
		return selectedRun{}, err
	}
	if opt.Commit == "" {
		return r.latestDev(ctx, w)
	}
	runs, err := r.runs(ctx, devProducer, w, opt.Commit)
	if err != nil {
		return selectedRun{}, err
	}
	chosen, err := latest(selectedRuns(runs, devProducer, w))
	if err != nil {
		return chosen, err
	}
	if err := r.ancestor(ctx, chosen.run.HeadSHA, devHead); err != nil {
		return chosen, err
	}
	return r.liveRun(ctx, chosen)
}

// Search an authenticated descending prefix, then perform complete exact-SHA
// discovery. The repository's lifetime dev history need not fit a fixed page
// limit, while a missing page or newer same-SHA failure still cannot be hidden.
func (r *resolver) latestDev(ctx context.Context, w workflow) (selectedRun, error) {
	seenCommit := map[string]bool{}
	previousID := int64(0)
	total, visited := -1, 0
	for page := 1; page <= maxPages; page++ {
		var response struct {
			Total int   `json:"total_count"`
			Runs  []run `json:"workflow_runs"`
		}
		endpoint := fmt.Sprintf("actions/workflows/ci-dev.yml/runs?per_page=100&branch=dev&event=push&page=%d", page)
		if err := r.api.read(ctx, r.endpoint(endpoint), &response); err != nil {
			return selectedRun{}, err
		}
		if response.Runs == nil || response.Total < 0 || len(response.Runs) > 100 || (total >= 0 && response.Total != total) || visited+len(response.Runs) > response.Total || (len(response.Runs) < 100 && visited+len(response.Runs) != response.Total) {
			return selectedRun{}, errors.New("incomplete or changing dev source discovery")
		}
		total = response.Total
		for _, candidate := range response.Runs {
			if r.validateRun(candidate, devProducer, w, "") != nil || (previousID != 0 && candidate.ID >= previousID) {
				return selectedRun{}, errors.New("invalid or unordered dev source history")
			}
			previousID = candidate.ID
			if seenCommit[candidate.HeadSHA] {
				continue
			}
			seenCommit[candidate.HeadSHA] = true
			if candidate.Status != "completed" || candidate.Conclusion != "success" {
				continue
			}
			return r.selectDev(ctx, options{Mode: "dev", Commit: candidate.HeadSHA})
		}
		visited += len(response.Runs)
		if visited == total {
			break
		}
	}
	return selectedRun{}, errors.New("no completed successful dev source in bounded recent discovery")
}

func selectedRuns(runs []run, p producer, w workflow) []selectedRun {
	result := make([]selectedRun, 0, len(runs))
	for _, current := range runs {
		result = append(result, selectedRun{current, p, w})
	}
	return result
}

func (r *resolver) liveRun(ctx context.Context, selected selectedRun) (selectedRun, error) {
	if selected.workflow.State != "active" {
		return selected, errors.New("selected full-validation producer is disabled")
	}
	var live, attempt run
	if err := r.api.read(ctx, r.endpoint(fmt.Sprintf("actions/runs/%d", selected.run.ID)), &live); err != nil {
		return selected, err
	}
	if r.validateRun(live, selected.producer, selected.workflow, selected.run.HeadSHA) != nil || !reflect.DeepEqual(live, selected.run) {
		return selected, errors.New("run identity or latest attempt changed after discovery")
	}
	if err := r.api.read(ctx, r.endpoint(fmt.Sprintf("actions/runs/%d/attempts/%d", live.ID, live.Attempt)), &attempt); err != nil {
		return selected, err
	}
	if !reflect.DeepEqual(attempt, live) || live.Status != "completed" || live.Conclusion != "success" {
		return selected, errors.New("latest exact-source full-validation attempt is not successful")
	}
	return selected, nil
}

func (r *resolver) file(ctx context.Context, path, commit string) ([]byte, error) {
	key := commit + ":" + path
	if data, ok := r.files[key]; ok {
		return data, nil
	}
	var content struct {
		Type     string `json:"type"`
		Path     string `json:"path"`
		SHA      string `json:"sha"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := r.api.read(ctx, r.endpoint("contents/"+path+"?ref="+commit), &content); err != nil {
		return nil, err
	}
	if content.Type != "file" || content.Path != path || content.Encoding != "base64" || !shaPattern.MatchString(content.SHA) {
		return nil, errors.New("invalid immutable source file metadata")
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil || len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("invalid immutable source file content")
	}
	encoded := append([]byte(fmt.Sprintf("blob %d\x00", len(data))), data...)
	digest := sha1.Sum(encoded)
	if hex.EncodeToString(digest[:]) != content.SHA {
		return nil, errors.New("immutable source blob hash mismatch")
	}
	r.files[key] = data
	return data, nil
}

var policyPaths = []string{
	".github/workflows/ci.yml", ".github/workflows/floor-bench.yml",
	".github/workflows/nightly.yml",
	"scripts/ci/ci-job-registry.json", "scripts/ci/check-required-jobs.sh",
	"scripts/ci/classify-changed-paths.sh", "scripts/ci/plan-merge-group.sh",
	"scripts/ci/analysis-shards-go/main.go", "scripts/ci/test-race-packages.sh",
	"scripts/ci/test-core-packages.sh",
	"scripts/release/nightly_inputs.py", "scripts/release/nightly/main.go",
	"scripts/release/sign-apple-artifacts.sh", "scripts/release/apple-artifact-digest.sh",
	"scripts/release/apple_notary_state.py", "scripts/release/with-apple-ticket-network.sh",
	"scripts/release/verify-apple-install.sh", "scripts/release/prepare-image-root.sh",
	"scripts/release/create-nightly-manifest.sh", "scripts/release/assemble-nightly.sh",
	"scripts/release/nightly-image.sh", "scripts/release/smoke-release-startup.sh",
}

func (r *resolver) verify(ctx context.Context, selected selectedRun) (string, error) {
	if err := validateReferences(r.repo.FullName, selected); err != nil {
		return "", err
	}
	paths := append(append([]string{}, policyPaths...), selected.workflow.Path)
	sort.Strings(paths)
	configuration := sha256.New()
	previous := ""
	for _, path := range paths {
		if path == previous {
			continue
		}
		previous = path
		approved, err := r.file(ctx, path, r.authority)
		if err != nil {
			return "", err
		}
		actual, err := r.file(ctx, path, selected.run.HeadSHA)
		if err != nil {
			return "", err
		}
		if !bytes.Equal(approved, actual) {
			return "", fmt.Errorf("source validation definition differs from approved main: %s", path)
		}
		if path == "scripts/ci/ci-job-registry.json" {
			if err := validateRegistry(actual); err != nil {
				return "", err
			}
		}
		digest := sha256.Sum256(actual)
		fmt.Fprintf(configuration, "%s\x00%x\n", path, digest)
	}
	jobs, err := r.jobs(ctx, selected.run)
	if err != nil {
		return "", err
	}
	if err := validateJobs(selected, jobs); err != nil {
		return "", err
	}
	return hex.EncodeToString(configuration.Sum(nil)), nil
}

func validateReferences(repo string, selected selectedRun) error {
	wanted := map[string]bool{".github/workflows/floor-bench.yml": false}
	if !selected.producer.Legacy {
		wanted[".github/workflows/ci.yml"] = false
	}
	for _, reference := range selected.run.References {
		parts := strings.Split(reference.Path, "@")
		if len(parts) != 2 || !strings.HasPrefix(parts[0], repo+"/") || parts[1] != selected.run.HeadSHA || reference.SHA != selected.run.HeadSHA || reference.Ref != "refs/heads/"+selected.run.HeadBranch {
			return errors.New("reusable validation workflow source mismatch")
		}
		path := strings.TrimPrefix(parts[0], repo+"/")
		seen, ok := wanted[path]
		if !ok || seen {
			return errors.New("unexpected or duplicate reusable validation workflow")
		}
		wanted[path] = true
	}
	for _, seen := range wanted {
		if !seen {
			return errors.New("missing immutable reusable validation reference")
		}
	}
	return nil
}
