package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
)

// The unattended stable-release App holds contents: write, so a stolen App key
// could push any branch and open a pull request from it. Everything the gate
// would otherwise admit is therefore refused for that App unless it is exactly
// what stable-reconcile.sh sync-trust writes: a release/sync-trust-VERSION
// branch of this repository that changes only the four public trust files.
// Anything else needs a person. The App is bound by its numeric bot user id from
// the PR_GATE_STABLE_RELEASE_APP_BOT_ID configuration (the repository variable
// STABLE_RELEASE_APP_BOT_ID), never by a name pattern. Unset, the feature is
// inert; set but malformed, the gate fails instead of guessing.
const stableReleaseAppBotEnv = "PR_GATE_STABLE_RELEASE_APP_BOT_ID"

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
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%s must be the numeric bot user id", stableReleaseAppBotEnv)
	}
	return id, nil
}

// stableReleaseAppRefusal returns why p must not be admitted, or "" when p is
// not authored by the stable-release App or is exactly a trust-sync pull request.
func (c *controller) stableReleaseAppRefusal(ctx context.Context, p pull) (string, error) {
	bot, err := stableReleaseAppBotID()
	if err != nil {
		return "", err
	}
	if bot == 0 || p.User.ID != bot {
		return "", nil
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
