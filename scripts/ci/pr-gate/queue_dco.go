package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
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
	dir, repo, err := fetchQueueDCO(ctx, repoName, p)
	if err != nil {
		return 0, err
	}
	defer cleanupQueueDCOMetadata(dir)
	code, err := checkQueueDCO(ctx, p, repo)
	if err != nil || code != 0 {
		return code, err
	}
	return 0, checkQueueMerge(ctx, repoName, p, repo)
}

// Both the initial filtered fetch and merge-time blob hydration use the same
// fixed repository origin. No PR configuration or ambient credential helper can
// select an alternate transport for a missing object.
func queueDCOMetadataArgs(repoName, repo string) []string {
	return []string{"-c", "core.hooksPath=/dev/null", "-c", "credential.helper=", "-c", "protocol.file.allow=never", "-c", "protocol.ext.allow=never", "-c", "http.lowSpeedLimit=1", "-c", "http.lowSpeedTime=30", "-c", "remote.origin.url=https://github.com/" + repoName + ".git", "-c", "remote.origin.promisor=true", "-c", "remote.origin.partialclonefilter=blob:none", "-C", repo}
}

func fetchQueueDCO(ctx context.Context, repoName string, p pull) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	if !repoPattern.MatchString(repoName) || !shaPattern.MatchString(p.Base.SHA) || !shaPattern.MatchString(p.Head.SHA) {
		return "", "", errors.New("invalid immutable queue DCO range")
	}
	dir, err := os.MkdirTemp("", "hikyo-queue-dco-")
	if err != nil {
		return "", "", err
	}
	complete := false
	defer func() {
		if !complete {
			cleanupQueueDCOMetadata(dir)
		}
	}()
	repo := filepath.Join(dir, "metadata.git")
	init := gateCommand(ctx, "git", "-c", "core.hooksPath=/dev/null", "init", "--bare", "--quiet", "--template=", repo)
	init.Env = queueDCOEnvironment()
	if err := init.Run(); err != nil {
		return "", "", errors.New("cannot initialize queue DCO metadata repository")
	}
	args := append(queueDCOMetadataArgs(repoName, repo), "fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--filter=blob:none", "origin", p.Base.SHA, p.Head.SHA)
	cmd := gateCommand(ctx, "git", args...)
	cmd.Env = queueDCOEnvironment()
	if err := cmd.Run(); err != nil {
		return "", "", errors.New("cannot fetch immutable queue DCO Git metadata")
	}
	complete = true
	return dir, repo, nil
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

// The session belongs to one admission invocation, never to a cached verdict.
// Only complete immutable metadata is reused; DCO and conflict proof rerun.
func admissionDCOVerifier(repoName string) (queueDCOVerifier, func()) {
	var captured *pull
	var dir, repo string
	closed := false
	verify := func(ctx context.Context, p pull) (int, error) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if closed {
			return 0, errors.New("queue DCO metadata session is closed")
		}
		if p.Base.Repo.ID <= 0 || p.Head.Repo.ID <= 0 || !strings.EqualFold(p.Base.Repo.FullName, repoName) || !repoPattern.MatchString(p.Head.Repo.FullName) || p.User.ID <= 0 || p.User.Login == "" {
			return 0, errors.New("invalid authenticated queue DCO metadata identity")
		}
		if captured != nil && (p.Number != captured.Number || p.State != captured.State || p.Head != captured.Head || p.Base != captured.Base || p.User != captured.User) {
			return 0, errors.New("queue DCO metadata identity changed within admission")
		}
		reused := captured != nil
		fetchStart := time.Now()
		if !reused {
			var err error
			dir, repo, err = fetchQueueDCO(ctx, repoName, p)
			if err != nil {
				return 0, err
			}
			snapshot := p
			captured = &snapshot
		}
		fetchSeconds := time.Since(fetchStart).Seconds()
		checkStart := time.Now()
		code, err := checkQueueDCO(ctx, p, repo)
		checkSeconds := time.Since(checkStart).Seconds()
		mergeSeconds := 0.0
		if err == nil && code == 0 {
			mergeStart := time.Now()
			err = checkQueueMerge(ctx, repoName, p, repo)
			mergeSeconds = time.Since(mergeStart).Seconds()
		}
		fmt.Fprintf(os.Stdout, "Queue DCO timing: reused_metadata=%t metadata_seconds=%.3f canonical_check_seconds=%.3f conflict_check_seconds=%.3f\n", reused, fetchSeconds, checkSeconds, mergeSeconds)
		return code, err
	}
	close := func() {
		closed = true
		captured = nil
		if dir != "" {
			cleanupQueueDCOMetadata(dir)
		}
		dir, repo = "", ""
	}
	return verify, close
}

// Cleanup is operational diagnostics, not a substitute validation verdict.
// Quote and bound trusted filesystem errors without logging metadata contents.
func cleanupQueueDCOMetadata(dir string) {
	if err := os.RemoveAll(dir); err != nil {
		message := err.Error()
		if len(message) > 160 {
			message = message[:160] + "..."
		}
		fmt.Fprintf(os.Stderr, "Queue DCO metadata cleanup failed: %q\n", message)
	}
}
