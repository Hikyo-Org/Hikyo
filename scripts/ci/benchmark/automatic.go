package benchmark

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

type changedFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
}

func performancePath(name string) bool {
	if name == "" || strings.HasSuffix(name, ".md") {
		return false
	}
	if name == "go.mod" || name == "go.sum" || name == "sqlc.yaml" || name == ".nvmrc" || strings.HasPrefix(name, "api/") {
		return true
	}
	if strings.HasPrefix(name, "internal/") || strings.HasPrefix(name, "cmd/") {
		// Embedded assets and benchmark fixtures can change measured behavior
		// without a Go source edit, so include the runtime tree conservatively.
		return strings.HasPrefix(name, "internal/") || path.Ext(name) == ".go" || path.Ext(name) == ".sql"
	}
	if strings.HasPrefix(name, "web/") || strings.HasPrefix(name, "clients/ts/") {
		// Storybook fixtures and unit tests do not affect the measured app.
		if strings.Contains(name, ".stories.") || strings.Contains(name, ".test.") {
			return false
		}
		return true
	}
	return strings.HasPrefix(name, "scripts/bench/") || strings.HasPrefix(name, "scripts/ci/benchmark/") ||
		name == "scripts/ci/install-corepack.sh" ||
		name == ".github/workflows/codspeed.yml" || name == ".github/workflows/pr-benchmark.yml" ||
		name == ".github/workflows/matrix-performance.yml" || name == ".github/workflows/benchmark-control.yml"
}

func performanceChanges(repository string, pr pull) (bool, error) {
	// GitHub's files endpoint is capped at 3,000. Never silently classify a
	// truncated or incomplete response as documentation-only.
	if pr.ChangedFiles <= 0 || pr.ChangedFiles > 3000 {
		return false, fmt.Errorf("cannot classify %d changed files safely; request the benchmark manually", pr.ChangedFiles)
	}
	var pages [][]changedFile
	if err := api(fmt.Sprintf("repos/%s/pulls/%d/files?per_page=100", repository, pr.Number), &pages); err != nil {
		return false, err
	}
	count, relevant := 0, false
	for _, page := range pages {
		for _, file := range page {
			if file.Filename == "" {
				return false, fmt.Errorf("changed file has no name")
			}
			count++
			relevant = relevant || performancePath(file.Filename) || performancePath(file.PreviousFilename)
		}
	}
	if count != pr.ChangedFiles {
		return false, fmt.Errorf("incomplete changed files: received %d of %d", count, pr.ChangedFiles)
	}
	return relevant, nil
}

func skipAutomatic(repository string, pr pull, c comment, reason string) error {
	latest, err := currentPull(repository, pr.Number)
	if err != nil || latest.State != "open" || latest.Draft || latest.Head.SHA != pr.Head.SHA {
		return err
	}
	current, err := botComment(repository, pr.Number)
	if err != nil {
		return err
	}
	if current.ID != c.ID || current.Body != c.Body {
		return nil // A newer head or a maintainer request owns the comment now.
	}
	return updateComment(repository, current, commentBody(pr, request{head: pr.Head.SHA}, "Automatic benchmark **not run**: "+reason+". Manual requests remain subject to the budget."))
}

func automaticRequest(repository string, e controlEvent) (pull, comment, bool, error) {
	r := e.WorkflowRun
	if r.Event != "pull_request" || r.Status != "completed" || r.Conclusion != "success" || r.Attempt != 1 {
		return pull{}, comment{}, false, nil
	}
	parts := regexp.MustCompile(`^pr-benchmark #([1-9][0-9]*)$`).FindStringSubmatch(r.Title)
	if r.ID <= 0 || parts == nil {
		return pull{}, comment{}, false, fmt.Errorf("invalid automatic benchmark run identity")
	}
	number, err := strconv.Atoi(parts[1])
	if err != nil {
		return pull{}, comment{}, false, err
	}
	// Recheck the provider-owned run before using any event metadata to spend.
	var live benchmarkRun
	if err := readAPI(fmt.Sprintf("repos/%s/actions/runs/%d", repository, r.ID), &live); err != nil {
		return pull{}, comment{}, false, err
	}
	if live.Attempt > 1 {
		return pull{}, comment{}, false, nil
	}
	if live != r {
		return pull{}, comment{}, false, fmt.Errorf("automatic benchmark run identity changed")
	}
	pr, err := currentPull(repository, number)
	if err != nil || pr.State != "open" || pr.Draft || pr.Head.SHA != r.Head {
		return pr, comment{}, false, err
	}
	if err := prepare(repository, controlEvent{Number: number}); err != nil {
		return pr, comment{}, false, err
	}
	c, err := botComment(repository, number)
	if err != nil {
		return pr, c, false, err
	}
	identity, err := parseRequest(c.Body)
	if err != nil {
		return pr, c, false, err
	}
	if identity.head != pr.Head.SHA || identity.run != 0 {
		return pr, c, false, nil // Another request already claimed this head.
	}
	relevant, err := performanceChanges(repository, pr)
	if err != nil {
		return pr, c, false, err
	}
	latest, err := currentPull(repository, number)
	if err != nil || latest.Head.SHA != pr.Head.SHA || latest.State != "open" || latest.Draft {
		return pr, c, false, err
	}
	if !relevant {
		return pr, c, false, skipAutomatic(repository, pr, c, "no performance-relevant changes")
	}
	return pr, c, true, nil
}
