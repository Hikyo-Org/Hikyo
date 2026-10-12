package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type queueDCOVerifier func(context.Context, pull) (int, error)

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
	if err := exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "init", "--bare", "--quiet", repo).Run(); err != nil {
		return 0, errors.New("cannot initialize queue DCO metadata repository")
	}
	cmd := exec.CommandContext(ctx, "git", "-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never", "-c", "http.lowSpeedLimit=1", "-c", "http.lowSpeedTime=30", "-C", repo, "fetch", "--quiet", "--no-tags", "--filter=blob:none", "https://github.com/"+repoName+".git", p.Base.SHA, p.Head.SHA)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := cmd.Run(); err != nil {
		return 0, errors.New("cannot fetch immutable queue DCO Git metadata")
	}
	return checkQueueDCO(ctx, p, repo)
}
func checkQueueDCO(ctx context.Context, p pull, repo string) (int, error) {
	if p.User.ID <= 0 || p.User.Login == "" {
		return 0, errors.New("missing authenticated PR actor for DCO policy")
	}
	cmd := exec.CommandContext(ctx, "sh", "scripts/ci/check-dco.sh", p.Base.SHA, p.Head.SHA, repo)
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "DCO_EXEMPT_AUTHOR=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
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
