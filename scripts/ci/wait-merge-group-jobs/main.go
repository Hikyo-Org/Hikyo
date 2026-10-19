// Wait for the two native proof jobs to become visible before the workflow
// completion callback reaches the trusted publisher. This process only reads
// Actions metadata and never publishes a check or grants merge authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type source struct {
	repo, head   string
	run, attempt int64
}

type job struct {
	ID         int64  `json:"id"`
	Run        int64  `json:"run_id"`
	Attempt    int64  `json:"run_attempt"`
	Head       string `json:"head_sha"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type page struct {
	Total *int  `json:"total_count"`
	Jobs  []job `json:"jobs"`
}

type apiReadError struct {
	status int
	cause  error
}

func (e *apiReadError) Error() string {
	if e.status != 0 {
		return fmt.Sprintf("GitHub job metadata read failed (HTTP %d)", e.status)
	}
	return "GitHub job metadata read failed"
}

func (e *apiReadError) Unwrap() error { return e.cause }

func commandReadError(err error) error {
	failure := &apiReadError{cause: err}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// Keep bounded status diagnostics, never command stderr or response data.
		if match := regexp.MustCompile(`\bHTTP ([0-9]{3})\b`).FindSubmatch(exit.Stderr); len(match) == 2 {
			failure.status, _ = strconv.Atoi(string(match[1]))
		}
	}
	return failure
}

func environment(get func(string) string) (source, error) {
	s := source{repo: get("GITHUB_REPOSITORY"), head: get("GITHUB_SHA")}
	var err error
	s.run, err = strconv.ParseInt(get("GITHUB_RUN_ID"), 10, 64)
	if err != nil || s.run <= 0 {
		return s, errors.New("positive GitHub run identity required")
	}
	s.attempt, err = strconv.ParseInt(get("GITHUB_RUN_ATTEMPT"), 10, 64)
	if err != nil || s.attempt <= 0 {
		return s, errors.New("positive GitHub run attempt required")
	}
	if get("GITHUB_EVENT_NAME") != "merge_group" || !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(s.repo) || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(s.head) {
		return s, errors.New("exact merge-group repository and head required")
	}
	return s, nil
}

func requiredJobs(s source, jobs []job) ([]string, error) {
	counts := map[string]int{}
	states := map[string]bool{}
	seen := map[int64]bool{}
	for _, j := range jobs {
		if j.ID <= 0 || seen[j.ID] || j.Run != s.run || j.Attempt != s.attempt || j.Head != s.head || j.Name == "" {
			return nil, errors.New("job metadata has a foreign, duplicated or invalid source identity")
		}
		seen[j.ID] = true
		if j.Name != "validation / ci-required" && j.Name != "merge-group-proof" {
			continue
		}
		counts[j.Name]++
		if counts[j.Name] != 1 {
			return nil, errors.New("required job name is duplicated")
		}
		switch j.Status {
		case "completed":
			if j.Conclusion != "success" {
				return nil, fmt.Errorf("required job did not succeed: %s", j.Name)
			}
			states[j.Name] = true
		case "queued", "in_progress", "waiting", "pending", "requested":
			if j.Conclusion != "" {
				return nil, errors.New("nonterminal required job has a terminal conclusion")
			}
		default:
			return nil, errors.New("required job has an unsupported status")
		}
	}
	var pending []string
	for _, name := range []string{"validation / ci-required", "merge-group-proof"} {
		if !states[name] {
			pending = append(pending, name)
		}
	}
	return pending, nil
}

func readJobs(ctx context.Context, fetch func(context.Context, int) (page, error)) ([]job, error) {
	var all []job
	total := -1
	for number := 1; number <= 10; number++ {
		p, err := fetch(ctx, number)
		if err != nil {
			return nil, err
		}
		if p.Total == nil || *p.Total < 0 || *p.Total > 1000 || p.Jobs == nil {
			return nil, errors.New("invalid job pagination")
		}
		if total == -1 {
			total = *p.Total
		}
		if *p.Total != total || len(p.Jobs) != min(100, total-len(all)) {
			return nil, errors.New("incomplete or changing job pagination")
		}
		all = append(all, p.Jobs...)
		if len(all) == total {
			return all, nil
		}
	}
	return nil, errors.New("job pagination limit exceeded")
}

func wait(ctx context.Context, s source, fetch func(context.Context, int) (page, error), pause func(context.Context) error, out io.Writer) error {
	readFailures := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		jobs, err := readJobs(ctx, fetch)
		if err != nil {
			readFailures++
			var failure *apiReadError
			if errors.As(err, &failure) && failure.status >= 500 && failure.status < 600 && readFailures < 3 {
				if _, writeErr := fmt.Fprintln(out, failure, "; retrying the read within the existing deadline"); writeErr != nil {
					return writeErr
				}
				if err := pause(ctx); err != nil {
					return err
				}
				continue
			}
			return err
		}
		readFailures = 0
		pending, err := requiredJobs(s, jobs)
		if err != nil {
			return err
		}
		if len(pending) == 0 {
			_, err := fmt.Fprintln(out, "Both required proof jobs are complete and successful for this exact run, attempt and head.")
			return err
		}
		if _, err := fmt.Fprintln(out, "Waiting for GitHub job metadata:", strings.Join(pending, ", ")); err != nil {
			return err
		}
		if err := pause(ctx); err != nil {
			return err
		}
	}
}

func execute() error {
	if len(os.Args) != 1 {
		return errors.New("unexpected arguments")
	}
	s, err := environment(os.Getenv)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fetch := func(ctx context.Context, number int) (page, error) {
		path := fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100&page=%d", s.repo, s.run, s.attempt, number)
		raw, err := exec.CommandContext(ctx, "gh", "api", path).Output()
		if err != nil {
			if ctx.Err() != nil {
				return page{}, ctx.Err()
			}
			return page{}, commandReadError(err)
		}
		var p page
		err = json.Unmarshal(raw, &p)
		return p, err
	}
	pause := func(ctx context.Context) error {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
	return wait(ctx, s, fetch, pause, os.Stdout)
}

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, "merge-group job metadata:", err)
		os.Exit(1)
	}
}
