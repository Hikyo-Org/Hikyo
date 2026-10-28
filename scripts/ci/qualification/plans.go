package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type raceLeg struct {
	Job     string   `json:"job"`
	Targets []string `json:"targets"`
}

func bindPlans(i *inventory) error {
	var full struct {
		Race      [][]string `json:"race_plan"`
		Isolation [][]string `json:"isolation_plan"`
	}
	if err := json.Unmarshal(i.Plans["ci-main"], &full); err != nil {
		return err
	}
	var legs []raceLeg
	if err := json.Unmarshal(i.Plans["reliability-race"], &legs); err != nil {
		return err
	}
	bind := func(refs []testReference) error {
		for k := range refs {
			r := &refs[k]
			r.ExecutionJobs, r.IsolationRaceJob = nil, ""
			if r.BuildConstraint != "" {
				owner := map[string]string{"k8se2e": "k8s-e2e", "floorbench": "floor-bench / measure", "flooracceptance": "release-acceptance / floor-backup-restore", "opsfloor": "ops-floor (arm64 and amd64)"}[r.BuildConstraint]
				if owner == "" {
					return fmt.Errorf("unmapped build constraint %s in %s", r.BuildConstraint, r.Path)
				}
				r.ExecutionJobs = []string{owner}
				r.DefaultRaceExclusion = "Build-tagged source is outside default Go planner input; dedicated tagged execution is required. No race execution is claimed."
				continue
			}
			if r.Package == "./internal/isolation" {
				for shard, targets := range full.Isolation {
					for _, name := range targets {
						if name == r.Name {
							r.ExecutionJobs = append(r.ExecutionJobs, fmt.Sprintf("isolation shard (%d)", shard))
						}
					}
				}
				for _, l := range legs {
					for _, name := range l.Targets {
						if name == r.Name {
							r.IsolationRaceJob = l.Job
						}
					}
				}
				if len(r.ExecutionJobs) != 1 || r.IsolationRaceJob == "" {
					return fmt.Errorf("criterion test lacks complete planner ownership: %s", r.Name)
				}
			} else {
				r.ExecutionJobs = []string{"test_core"}
				for shard, packages := range full.Race {
					for _, assignment := range packages {
						path, filter, _ := strings.Cut(assignment, "\t")
						if path != r.Package && !strings.HasSuffix(path, strings.TrimPrefix(r.Package, ".")) {
							continue
						}
						selected := true
						if filter != "" {
							pattern, err := regexp.Compile(filter)
							if err != nil {
								return fmt.Errorf("invalid race filter: %w", err)
							}
							selected = pattern.MatchString(r.Name)
						}
						if selected {
							r.ExecutionJobs = append(r.ExecutionJobs, fmt.Sprintf("race shard (%d)", shard))
						}
					}
				}
			}
		}
		return nil
	}
	for n := range i.Criteria {
		if err := bind(i.Criteria[n].Tests); err != nil {
			return err
		}
	}
	for n := range i.Specification.Requirements {
		if err := bind(i.Specification.Requirements[n].Tests); err != nil {
			return err
		}
	}
	return nil
}

// Use the owning planner, including its fuzz/example inventory. The reliability
// workflow moves analyzer targets into one additional job; they are not drops.
func planShards(root string, workflows map[string]map[string]job) (plans map[string]json.RawMessage, returnedErr error) {
	ci := workflows[".github/workflows/ci.yml"]
	reliability := workflows[".github/workflows/reliability.yml"]
	steps := func(j job) string {
		var result strings.Builder
		for _, s := range j.Steps {
			result.WriteString(s.Run)
			result.WriteByte('\n')
		}
		return result.String()
	}
	ciSource, raceSource := steps(ci["analysis_shards"]), steps(reliability["plan"])
	number := func(pattern, source string) (int, error) {
		m := regexp.MustCompile(pattern).FindStringSubmatch(source)
		if len(m) != 2 {
			return 0, fmt.Errorf("workflow planner contract changed: %s", pattern)
		}
		return strconv.Atoi(m[1])
	}
	raceCount, err := number(`(?m)^race_count=([0-9]+)$`, ciSource)
	if err != nil {
		return nil, err
	}
	count, err := number(`(?m)^shard_count=([0-9]+)$`, ciSource)
	if err != nil {
		return nil, err
	}
	last, err := number(`seq 0 ([0-9]+)`, raceSource)
	if err != nil {
		return nil, err
	}
	m := regexp.MustCompile(`analyzers='([^']+)'`).FindStringSubmatch(raceSource)
	if len(m) != 2 {
		return nil, fmt.Errorf("reliability analyzer inventory changed")
	}
	var analyzers []string
	if err := json.Unmarshal([]byte(m[1]), &analyzers); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp("", "hikyo-qualification-planner-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := os.RemoveAll(work); err != nil {
			returnedErr = errors.Join(returnedErr, fmt.Errorf("remove owned planner directory: %w", err))
		}
	}()
	planner := filepath.Join(work, "planner")
	build := exec.Command("go", "build", "-p", "2", "-o", planner, "./scripts/ci/analysis-shards-go")
	build.Dir = root
	if raw, err := build.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("build planner: %w: %s", err, raw)
	}
	invoke := func(args ...string) ([]byte, error) {
		c := exec.Command(planner, args...)
		c.Dir = root
		raw, err := c.Output()
		if err != nil {
			var failure *exec.ExitError
			if errors.As(err, &failure) {
				return nil, fmt.Errorf("planner %s: %w: %s", strings.Join(args, " "), err, failure.Stderr)
			}
			return nil, fmt.Errorf("planner %s: %w", strings.Join(args, " "), err)
		}
		return raw, nil
	}
	full, err := invoke("all", "--root", ".", "--race-shards", strconv.Itoa(raceCount), "--shards", strconv.Itoa(count))
	if err != nil {
		return nil, err
	}
	if !json.Valid(full) {
		return nil, fmt.Errorf("invalid complete shard plan")
	}
	legs := []raceLeg{{Job: "race-analyzers", Targets: analyzers}}
	seen := map[string]bool{}
	for _, name := range analyzers {
		if seen[name] {
			return nil, fmt.Errorf("duplicate analyzer %s", name)
		}
		seen[name] = true
	}
	for n := 0; n <= last; n++ {
		raw, err := invoke("isolation-race", "--root", ".", "--shard", strconv.Itoa(n), "--shards", strconv.Itoa(last+1))
		if err != nil {
			return nil, err
		}
		l := raceLeg{Job: fmt.Sprintf("race-isolation (%d)", n), Targets: []string{}}
		for _, name := range strings.Fields(string(raw)) {
			isAnalyzer := false
			for _, analyzer := range analyzers {
				if name == analyzer {
					isAnalyzer = true
				}
			}
			if isAnalyzer {
				continue
			}
			if seen[name] {
				return nil, fmt.Errorf("duplicate isolation race target %s", name)
			}
			seen[name] = true
			l.Targets = append(l.Targets, name)
		}
		if len(l.Targets) != 0 {
			legs = append(legs, l)
		}
	}
	raw, err := invoke("isolation-race", "--root", ".", "--shard", "0", "--shards", "1")
	if err != nil {
		return nil, err
	}
	expected := strings.Fields(string(raw))
	if len(expected) != len(seen) {
		return nil, fmt.Errorf("incomplete isolation race plan: expected %d, got %d", len(expected), len(seen))
	}
	for _, name := range expected {
		if !seen[name] {
			return nil, fmt.Errorf("missing isolation race target %s", name)
		}
	}
	encoded, err := json.Marshal(legs)
	if err != nil {
		return nil, err
	}
	return map[string]json.RawMessage{"ci-main": full, "reliability-race": encoded}, nil
}
