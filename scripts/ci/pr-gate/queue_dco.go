package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type queueDCOVerifier func(context.Context, pull) (int, error)

func queueDCOEnvironment() []string {
	// Preserve only host tool lookup and temporary-directory selection. No PR
	// input sets these, but ambient Git selectors/configuration and credentials
	// must not redirect immutable metadata reads or alter canonical DCO policy.
	env := []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=0", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	for _, key := range []string{"HOME", "TMPDIR"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}

// Read immutable Git metadata in a bare temporary repository. Neither PR
// files, hooks nor Git configuration are checked out or executed. The actual
// validation script remains the canonical trusted default-branch DCO owner.
func verifyQueueDCO(ctx context.Context, repoName string, p pull) (int, error) {
	if !repoPattern.MatchString(repoName) || !shaPattern.MatchString(p.Base.SHA) || !shaPattern.MatchString(p.Head.SHA) {
		return 0, errors.New("invalid immutable queue DCO range")
	}
	dir, err := os.MkdirTemp("", "hikyo-queue-dco-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(dir)
	repo := filepath.Join(dir, "metadata.git")
	init := gateCommand(ctx, "git", "-c", "core.hooksPath=/dev/null", "init", "--bare", "--quiet", "--template=", repo)
	init.Env = queueDCOEnvironment()
	if err := init.Run(); err != nil {
		return 0, errors.New("cannot initialize queue DCO metadata repository")
	}
	cmd := gateCommand(ctx, "git", "-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never", "-c", "http.lowSpeedLimit=1", "-c", "http.lowSpeedTime=30", "-C", repo, "fetch", "--quiet", "--no-tags", "--filter=blob:none", "https://github.com/"+repoName+".git", p.Base.SHA, p.Head.SHA)
	cmd.Env = queueDCOEnvironment()
	if err := cmd.Run(); err != nil {
		return 0, errors.New("cannot fetch immutable queue DCO Git metadata")
	}
	return checkQueueDCO(ctx, p, repo)
}
func checkQueueDCO(ctx context.Context, p pull, repo string) (int, error) {
	if p.User.ID <= 0 || p.User.Login == "" {
		return 0, errors.New("missing authenticated PR actor for DCO policy")
	}
	cmd := gateCommand(ctx, "sh", "scripts/ci/check-dco.sh", p.Base.SHA, p.Head.SHA, repo)
	cmd.Env = queueDCOEnvironment()
	if p.User.Login == "dependabot[bot]" {
		cmd.Env = append(cmd.Env, "DCO_EXEMPT_AUTHOR=dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>")
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return 1, nil
		}
		return 0, fmt.Errorf("trusted canonical queue DCO verification failed: %w", err)
	}
	return 0, nil
}
