package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
)

// The unattended stable-release App holds contents: write, so a stolen App key
// could push any branch and open a pull request from it, or add a GitHub-signed
// commit to somebody else's pull request. Everything the gate would otherwise
// admit is therefore refused for that App unless it is exactly what
// stable-reconcile.sh sync-trust writes: a release/sync-trust-VERSION branch of
// this repository that changes only the four public trust files. A pull request
// of anyone else is refused when any of its commits is authored or committed by
// the App. Anything else needs a person. The App is bound by its numeric bot
// user id from PR_GATE_STABLE_RELEASE_APP_BOT_ID (the repository variable
// STABLE_RELEASE_APP_BOT_ID), never by a name pattern.
//
// Unset, the feature is inert, which is right only before the App exists. Once
// the App is configured (PR_GATE_STABLE_RELEASE_APP_CLIENT_ID, the repository
// variable that switches the reconciler on) an empty or malformed bot id fails
// the gate instead of disabling the rule: a half-configured state must not
// admit what the App could write.
const (
	stableReleaseAppBotEnv    = "PR_GATE_STABLE_RELEASE_APP_BOT_ID"
	stableReleaseAppClientEnv = "PR_GATE_STABLE_RELEASE_APP_CLIENT_ID"
)

// GitHub lists at most 250 commits of a pull request; a list that long may be cut.
const pullCommitLimit = 250

var syncTrustBranch = regexp.MustCompile(`^release/sync-trust-[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z][0-9A-Za-z.-]*)?$`)

var syncTrustFiles = map[string]bool{
	"release/trust/metadata.json":          true,
	"release/trust/metadata.sigstore.json": true,
	"release/trust/catalog.json":           true,
	"release/trust/catalog.sigstore.json":  true,
}

func stableReleaseAppBotID() (int64, error) {
	raw := os.Getenv(stableReleaseAppBotEnv)
	if raw == "" {
		if os.Getenv(stableReleaseAppClientEnv) != "" {
			return 0, fmt.Errorf("%s is set but %s is not: the stable-release App is configured without its bot user id", stableReleaseAppClientEnv, stableReleaseAppBotEnv)
		}
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s must be the numeric bot user id", stableReleaseAppBotEnv)
	}
	return id, nil
}

type pullCommit struct {
	Author *struct {
		ID int64 `json:"id"`
	} `json:"author"`
	Committer *struct {
		ID int64 `json:"id"`
	} `json:"committer"`
	Commit struct {
		Author struct {
			Email string `json:"email"`
		} `json:"author"`
		Committer struct {
			Email string `json:"email"`
		} `json:"committer"`
	} `json:"commit"`
}

// stableReleaseAppCommitRefusal refuses a pull request that carries a commit of the App.
func (c *controller) stableReleaseAppCommitRefusal(ctx context.Context, p pull, bot int64) (string, error) {
	noreply := regexp.MustCompile(fmt.Sprintf(`^%d\+[^@]+@users\.noreply\.github\.com$`, bot))
	total := 0
	for page := 1; ; page++ {
		var list []pullCommit
		if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("pulls/%d/commits?per_page=100&page=%d", p.Number, page)), &list); err != nil {
			return "", err
		}
		for _, commit := range list {
			if (commit.Author != nil && commit.Author.ID == bot) || (commit.Committer != nil && commit.Committer.ID == bot) ||
				noreply.MatchString(commit.Commit.Author.Email) || noreply.MatchString(commit.Commit.Committer.Email) {
				return "a commit of the stable-release App sits in a pull request that is not its trust-sync pull request", nil
			}
		}
		total += len(list)
		if len(list) < 100 || page >= pullCommitLimit/100+1 {
			break
		}
	}
	if total >= pullCommitLimit {
		return "the pull request lists as many commits as GitHub shows, so the App's commits cannot be ruled out", nil
	}
	return "", nil
}

// stableReleaseAppRefusal returns why p must not be admitted, or "" when p is
// not authored by the stable-release App or is exactly a trust-sync pull request.
func (c *controller) stableReleaseAppRefusal(ctx context.Context, p pull) (string, error) {
	bot, err := stableReleaseAppBotID()
	if err != nil {
		return "", err
	}
	if bot == 0 {
		return "", nil
	}
	if p.User.ID != bot {
		return c.stableReleaseAppCommitRefusal(ctx, p, bot)
	}
	if p.Head.Repo.ID != c.repo.ID || p.Base.Ref != c.repo.DefaultBranch || !syncTrustBranch.MatchString(p.Head.Ref) {
		return "the stable-release App may only open a release/sync-trust-VERSION pull request from this repository", nil
	}
	var files []pullFile
	for page := 1; ; page++ {
		if page > pullFilePages {
			return "the trust-sync pull request lists too many files", nil
		}
		var list []pullFile
		if err := c.api.read(ctx, c.endpoint(fmt.Sprintf("pulls/%d/files?per_page=100&page=%d", p.Number, page)), &list); err != nil {
			return "", err
		}
		files = append(files, list...)
		if len(list) < 100 {
			break
		}
	}
	if len(files) == 0 {
		return "the trust-sync pull request changes no trust file", nil
	}
	for _, f := range files {
		if !syncTrustFiles[f.Name] || (f.Status != "added" && f.Status != "modified") {
			return "the stable-release App may change only the four release/trust files that sync-trust writes", nil
		}
	}
	return "", nil
}
