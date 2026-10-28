package benchmark

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type changedFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
}

func performancePath(name string) bool {
	return pathWorkloads(name) != 0
}

func performanceChanges(repository string, pr pull) (bool, error) {
	selected, err := changedWorkloads(repository, pr)
	return selected != 0, err
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
	if current.ID != c.ID || current.Body != c.Body || checked(current.Body, checkbox) {
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
	if err != nil || pr.State != "open" || pr.Draft || pr.Head.SHA != r.Head || !strings.EqualFold(pr.Head.Repo.FullName, repository) {
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
	if identity.head != pr.Head.SHA || identity.run != 0 || checked(c.Body, checkbox) {
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
