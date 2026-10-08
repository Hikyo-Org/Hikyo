// Package benchmark admits daily/manual walltime jobs before allocating
// a Macro runner. GitHub run history is usage data, never executable input.
package benchmark

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	macroLabel     = "codspeed-macro-arm64-graviton-ubuntu-22-04"
	reservation    = 16  // 15-minute timeout plus a minute for setup/cleanup rounding.
	automaticLimit = 528 // Includes 32 prior daily jobs plus the new reservation.
	manualLimit    = 72
	creditLimit    = 156 // $4.992 at $0.032/min, only on an explicit manual request.
)

type run struct {
	ID        int64     `json:"id"`
	Attempt   int       `json:"run_attempt"`
	Event     string    `json:"event"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

type job struct {
	Name        string     `json:"name"`
	ID          int64      `json:"id"`
	Labels      []string   `json:"labels"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	RunnerName  string     `json:"runner_name"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type usage struct{ automatic, manual int }

func jobMinutes(j job, cutoff time.Time) (int, error) {
	macro := false
	for _, label := range j.Labels {
		if strings.HasPrefix(label, "codspeed-macro") {
			if label != macroLabel {
				return 0, fmt.Errorf("job %d uses unaccounted Macro label %q", j.ID, label)
			}
			macro = true
		}
	}
	if !macro {
		if j.Name == "Track main performance" || j.Name == "Requested PR walltime" {
			return 0, fmt.Errorf("walltime job %d has no approved Macro label", j.ID)
		}
		return 0, nil
	}
	if j.ID <= 0 {
		return 0, fmt.Errorf("Macro job has no ID")
	}
	if j.Status != "completed" {
		return 0, fmt.Errorf("Macro job %d is still %q; wait for it to finish or cancel it", j.ID, j.Status)
	}
	// GitHub puts queue timestamps on cancelled jobs that never had a runner.
	if j.RunnerName == "" && (j.Conclusion == "cancelled" || j.Conclusion == "skipped") {
		return 0, nil
	}
	if j.RunnerName == "" || j.StartedAt == nil || j.CompletedAt == nil || j.CompletedAt.Before(*j.StartedAt) {
		return 0, fmt.Errorf("Macro job %d has incomplete usage evidence", j.ID)
	}
	if j.CompletedAt.Before(cutoff) {
		return 0, nil
	}
	// Count the entire assigned job, including failed attempts. Round up and add
	// a minute rather than treating measured benchmark time as runner usage.
	return int(math.Ceil(j.CompletedAt.Sub(*j.StartedAt).Minutes())) + 1, nil
}

func admit(u usage, event string, credit bool) (bool, string) {
	if event != "schedule" && event != "workflow_dispatch" {
		return false, "unsupported walltime request context"
	}
	extra := 0
	if credit {
		extra = creditLimit
	}
	// Earlier credit-funded requests must not consume capacity reserved for
	// daily main. Their already-authorized overage remains in the total ledger.
	if event == "schedule" && u.manual > manualLimit {
		extra = min(creditLimit, u.manual-manualLimit)
	}
	if u.automatic+u.manual+reservation > automaticLimit+manualLimit+extra {
		return false, "the rolling allowance cannot cover another complete job"
	}
	if event == "schedule" && u.automatic+reservation > automaticLimit {
		return false, "the daily allowance is exhausted"
	}
	if event == "workflow_dispatch" && u.manual+reservation > manualLimit+extra {
		return false, "the manual allowance is exhausted; daily capacity remains reserved"
	}
	return true, "a complete walltime job fits the allowance"
}

func api(path string, target any) error {
	command := exec.Command("gh", "api", "--paginate", "--slurp", path)
	command.Stderr = os.Stderr
	raw, err := command.Output()
	if err != nil {
		return fmt.Errorf("read GitHub usage: %w", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode GitHub usage: %w", err)
	}
	return nil
}

func collect(repository string, now time.Time, currentMacroRun int64) (usage, error) {
	// A 32-day lookback covers every monthly billing interval without relying
	// on an unverified billing reset date, and includes boundary-crossing jobs.
	cutoff := now.Add(-32 * 24 * time.Hour)
	var pages []struct {
		Runs []run `json:"workflow_runs"`
	}
	// Do not filter by creation date: a rerun of an old run is newly billable.
	for _, workflow := range []string{"codspeed.yml", "pr-benchmark.yml"} {
		var workflowPages []struct {
			Runs []run `json:"workflow_runs"`
		}
		if err := api("repos/"+repository+"/actions/workflows/"+workflow+"/runs?per_page=100", &workflowPages); err != nil {
			return usage{}, err
		}
		pages = append(pages, workflowPages...)
	}
	if len(pages) == 0 {
		return usage{}, fmt.Errorf("GitHub returned no run-history pages")
	}
	u := usage{}
	seen := map[int64]bool{}
	for _, page := range pages {
		if page.Runs == nil {
			return usage{}, fmt.Errorf("GitHub returned incomplete run history")
		}
		for _, r := range page.Runs {
			if r.ID <= 0 || r.Attempt < 1 || r.UpdatedAt.IsZero() || r.Status == "" || r.Event == "" {
				return usage{}, fmt.Errorf("run has incomplete identity or timestamp")
			}
			if r.Status == "completed" && r.UpdatedAt.Before(cutoff) {
				continue
			}
			// PR attempt one is discovery/smoke only. Paid PR measurements require
			// an admitted rerun, so ordinary PR pushes need no per-run jobs call.
			// Keep all history pages: a newly rerun old run can still be billable.
			if r.Event == "pull_request" && r.Attempt == 1 {
				continue
			}
			var jobPages []struct {
				Jobs []job `json:"jobs"`
			}
			// filter=all includes prior attempts, including prior attempts of this
			// run when only the failed jobs or the whole workflow are rerun.
			if err := api(fmt.Sprintf("repos/%s/actions/runs/%d/jobs?filter=all&per_page=100", repository, r.ID), &jobPages); err != nil {
				return usage{}, err
			}
			if len(jobPages) == 0 {
				return usage{}, fmt.Errorf("run %d has no job-history pages", r.ID)
			}
			for _, page := range jobPages {
				if page.Jobs == nil {
					return usage{}, fmt.Errorf("run %d has incomplete job history", r.ID)
				}
				for _, j := range page.Jobs {
					if j.ID <= 0 {
						return usage{}, fmt.Errorf("run %d contains a job without an ID", r.ID)
					}
					if seen[j.ID] {
						continue
					}
					seen[j.ID] = true
					// The main admission check reserves its own pending or active
					// Macro attempt. GitHub may expose a dependent queued job before
					// the hosted gate finishes. Earlier completed attempts still count.
					if r.ID == currentMacroRun && j.Name == "Track main performance" && j.Status != "completed" {
						continue
					}
					minutes, err := jobMinutes(j, cutoff)
					if err != nil {
						return usage{}, err
					}
					if r.Event == "schedule" || r.Event == "push" {
						u.automatic += minutes
					} else {
						u.manual += minutes
					}
				}
			}
		}
	}
	return u, nil
}

func CheckAllowance() error {
	repository := os.Getenv("GITHUB_REPOSITORY")
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repository) {
		return fmt.Errorf("invalid GITHUB_REPOSITORY")
	}
	currentID, err := strconv.ParseInt(os.Getenv("GITHUB_RUN_ID"), 10, 64)
	if err != nil || currentID <= 0 {
		return fmt.Errorf("invalid GITHUB_RUN_ID")
	}
	event := os.Getenv("GITHUB_EVENT_NAME")
	if os.Getenv("GITHUB_REF") != "refs/heads/main" || (event != "schedule" && event != "workflow_dispatch") {
		return fmt.Errorf("walltime admission requires daily or manual main context")
	}
	credit, err := strconv.ParseBool(os.Getenv("CODSPEED_USE_CREDIT"))
	if err != nil {
		return fmt.Errorf("invalid CODSPEED_USE_CREDIT")
	}
	if credit && (event != "workflow_dispatch" || os.Getenv("CODSPEED_CREDIT_CONFIRMED") != "true") {
		return fmt.Errorf("credit requires a manual request and CODSPEED_CREDIT_CONFIRMED=true after account credit verification")
	}
	u, err := collect(repository, time.Now().UTC(), currentID)
	if err != nil {
		return err
	}
	allowed, reason := admit(u, event, credit)
	message := fmt.Sprintf("CodSpeed: daily %d/%d min, requested %d/%d min over the last 32 days; reserving %d min: %s.\n", u.automatic, automaticLimit, u.manual, manualLimit, reservation, reason)
	fmt.Print(message)
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		if err := appendFile(path, message); err != nil {
			return err
		}
	}
	if path := os.Getenv("GITHUB_OUTPUT"); path != "" {
		if err := appendFile(path, fmt.Sprintf("allowed=%t\n", allowed)); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("GITHUB_OUTPUT is required")
	}
	if !allowed && event == "workflow_dispatch" {
		return fmt.Errorf("manual walltime run declined: %s", reason)
	}
	return nil
}

func appendFile(path, value string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(value)
	return err
}
