// Run the `run:` steps of one workflow job in order, on a checkout, outside the
// workflow that defines them. ci-feedback replays ci.yml's supply-chain-checks
// job this way, so a fixture added to that job runs on pull requests without a
// second copy of the step list.
//
// Only what the replayed job needs is emulated: step `env`, the default and
// explicit bash shells, and $GITHUB_PATH appends carried into later steps.
// A step's timeout-minutes is enforced. Anything else that would change what a
// step does under Actions (expressions, conditions, fail-open flags, working
// directories) is refused, never ignored.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// validateRevisionStep binds the caller's immutable revision to the Actions
// event. That binding does not exist for a pull request replay.
const validateRevisionStep = "Validate the immutable caller revision"

// cachePreparationSteps only prepare the shared Go cache restores, which a PR
// replay does not perform, so they are skipped by name rather than run for
// nothing.
var cachePreparationSteps = map[string]bool{
	"Select runner cache ABI":                  true,
	"Reserve disk for shared Go cache restore": true,
}

// stepTimeoutUnit scales timeout-minutes; tests shrink it.
var stepTimeoutUnit = time.Minute

type step struct {
	Name            string            `yaml:"name"`
	Uses            string            `yaml:"uses"`
	Run             string            `yaml:"run"`
	Shell           string            `yaml:"shell"`
	Env             map[string]string `yaml:"env"`
	If              string            `yaml:"if"`
	WorkingDir      string            `yaml:"working-directory"`
	ContinueOnError any               `yaml:"continue-on-error"`
	Timeout         int               `yaml:"timeout-minutes"`
}

// runnable returns the steps to execute: every `run` step except the revision
// binding. `uses` steps are the caller workflow's responsibility.
func runnable(steps []step) ([]step, error) {
	var out []step
	for _, s := range steps {
		if s.Run == "" || s.Name == validateRevisionStep || cachePreparationSteps[s.Name] {
			continue
		}
		if s.If != "" || s.WorkingDir != "" || s.ContinueOnError != nil {
			return nil, fmt.Errorf("step %q uses a condition, working directory or continue-on-error that a replay cannot honor", s.Name)
		}
		if s.Shell != "" && s.Shell != "bash" {
			return nil, fmt.Errorf("step %q uses unsupported shell %q", s.Name, s.Shell)
		}
		raw := s.Run
		for k, v := range s.Env {
			raw += k + v
		}
		if strings.Contains(raw, "${{") {
			return nil, fmt.Errorf("step %q uses an Actions expression that a replay cannot evaluate", s.Name)
		}
		out = append(out, s)
	}
	return out, nil
}

func load(path, job string) ([]step, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []step `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	j, ok := workflow.Jobs[job]
	if !ok {
		return nil, fmt.Errorf("%s has no job %q", path, job)
	}
	return runnable(j.Steps)
}

func execute(steps []step) error {
	dir, err := os.MkdirTemp("", "run-job-steps-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	pathFile := filepath.Join(dir, "github_path")
	path := os.Getenv("PATH")
	for i, s := range steps {
		if err := os.WriteFile(pathFile, nil, 0o600); err != nil {
			return err
		}
		script := filepath.Join(dir, fmt.Sprintf("step-%d.sh", i))
		if err := os.WriteFile(script, []byte(s.Run), 0o600); err != nil {
			return err
		}
		fmt.Printf("::group::%s\n", s.Name)
		ctx, cancel := context.Background(), context.CancelFunc(func() {})
		if s.Timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, time.Duration(s.Timeout)*stepTimeoutUnit)
		}
		// Constant arguments only: the step script is sourced from the environment,
		// as Actions runs `bash -e {0}` (the default shell) or the pipefail form.
		const source = `. "$RUN_JOB_STEP_SCRIPT"`
		cmd := exec.CommandContext(ctx, "bash", "-e", "-c", source, "step")
		if s.Shell == "bash" {
			cmd = exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", source, "step")
		}
		cmd.WaitDelay = time.Second
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		cmd.Env = append(os.Environ(), "PATH="+path, "GITHUB_PATH="+pathFile, "RUN_JOB_STEP_SCRIPT="+script)
		for k, v := range s.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		err := cmd.Run()
		timedOut := ctx.Err() != nil
		cancel()
		fmt.Println("::endgroup::")
		if timedOut {
			return fmt.Errorf("step %q exceeded its timeout-minutes: %d", s.Name, s.Timeout)
		}
		if err != nil {
			return fmt.Errorf("step %q: %w", s.Name, err)
		}
		added, err := os.ReadFile(pathFile)
		if err != nil {
			return err
		}
		// Actions prepends in append order, so the last line wins.
		for _, line := range strings.Split(strings.TrimSpace(string(added)), "\n") {
			if line != "" {
				path = line + string(os.PathListSeparator) + path
			}
		}
	}
	return nil
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: run-job-steps <workflow.yml> <job>")
		os.Exit(2)
	}
	steps, err := load(os.Args[1], os.Args[2])
	if err == nil && len(steps) == 0 {
		err = fmt.Errorf("%s job %q has no runnable steps", os.Args[1], os.Args[2])
	}
	if err == nil {
		err = execute(steps)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "run-job-steps: %v\n", err)
		os.Exit(1)
	}
}
