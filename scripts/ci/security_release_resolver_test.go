package ci_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMainValidationWorkflowPolicySecurityReleaseDisablesPackageManagerCaches(t *testing.T) {
	w := readConsolidatedWorkflow(t, "security-release")
	goSteps, nodeSteps := 0, 0
	for _, step := range w.Jobs["validate"].Steps {
		switch {
		case strings.HasPrefix(step.Uses, "actions/setup-go@"):
			goSteps++
			if step.With["cache"] != "false" {
				t.Fatal("release validation must disable Go caching")
			}
		case strings.HasPrefix(step.Uses, "actions/setup-node@"):
			nodeSteps++
			if step.With["package-manager-cache"] != "false" || step.With["cache"] != "" {
				t.Fatal("release validation must disable automatic and explicit package-manager caching")
			}
		case strings.HasPrefix(step.Uses, "actions/cache"):
			t.Fatal("release validation may not restore or save workflow caches")
		}
	}
	if goSteps != 1 || nodeSteps != 1 {
		t.Fatal("release validation must configure both toolchains exactly once")
	}
}

type releaseResolverAPICall struct {
	args []string
	body string
}

func TestMainValidationWorkflowPolicySecurityReleaseAuthenticatesImmutableCommits(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatal("release resolver behavioral coverage requires jq:", err)
	}
	w := readConsolidatedWorkflow(t, "security-release")
	steps := w.Jobs["resolve"].Steps
	if len(steps) != 1 || steps[0].Run == "" || steps[0].Uses != "" {
		t.Fatal("resolver must run its API-only shell without a source checkout")
	}
	const repository = "Hikyo-Org/Hikyo"
	source, harness, annotated := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	const stable = "v1.2.3"
	const nightly = "v1.2.3-nightly.20261014.42.gaaaaaaaa"
	api := func(endpoint string, body any) releaseResolverAPICall {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return releaseResolverAPICall{args: []string{"api", "repos/" + repository + "/" + endpoint}, body: string(raw)}
	}
	object := func(kind, sha string) any {
		return map[string]any{"object": map[string]string{"type": kind, "sha": sha}}
	}
	published := func(tag string) releaseResolverAPICall {
		// This mutable field deliberately disagrees with the immutable tag.
		return api("releases/tags/"+tag, map[string]any{"tag_name": tag, "draft": false, "published_at": "2026-10-14T00:00:00Z", "target_commitish": "untrusted-branch"})
	}
	main := releaseResolverAPICall{args: []string{"api", "repos/" + repository + "/git/ref/heads/main", "--jq", ".object.sha"}, body: harness}
	refs := func(tags ...string) releaseResolverAPICall {
		// --paginate --slurp returns one array per page; put each tag on its
		// own page so the test also executes the resolver's pagination shape.
		pages := [][]map[string]string{{}}
		for _, tag := range tags {
			pages = append(pages, []map[string]string{{"ref": "refs/tags/" + tag}})
		}
		call := api("git/matching-refs/tags/v", pages)
		call.args = []string{"api", "--paginate", "--slurp", call.args[1]}
		return call
	}
	stableCalls := []releaseResolverAPICall{published(stable), api("git/ref/tags/"+stable, object("commit", source)), main}
	nightlyCalls := []releaseResolverAPICall{refs("v1.2.3-nightly.20261014.41.gdddddddd", nightly), published(nightly), api("git/ref/tags/"+nightly, object("commit", source)), main}
	available := func(tag string) string {
		return fmt.Sprintf("available=true\ntag=%s\nsource=%s\nharness=%s\n", tag, source, harness)
	}
	for _, tt := range []struct {
		name   string
		event  string
		env    map[string]string
		calls  []releaseResolverAPICall
		output string
		reject bool
	}{
		{name: "lightweight stable tag", event: "release", calls: stableCalls, output: available(stable)},
		{name: "annotated stable tag", event: "release", calls: []releaseResolverAPICall{published(stable), api("git/ref/tags/"+stable, object("tag", annotated)), api("git/tags/"+annotated, object("commit", source)), main}, output: available(stable)},
		{name: "nested annotated tag", event: "release", calls: []releaseResolverAPICall{published(stable), api("git/ref/tags/"+stable, object("tag", annotated)), api("git/tags/"+annotated, object("tag", harness)), api("git/tags/"+harness, object("commit", source)), main}, output: available(stable)},
		{name: "scheduled nightly exact SHA across API pages", event: "workflow_run", calls: nightlyCalls, output: available(nightly)},
		{name: "dispatched annotated nightly exact SHA", event: "workflow_run", env: map[string]string{"NIGHTLY_EVENT": "repository_dispatch"}, calls: []releaseResolverAPICall{refs(nightly), published(nightly), api("git/ref/tags/"+nightly, object("tag", annotated)), api("git/tags/"+annotated, object("commit", source)), main}, output: available(nightly)},
		{name: "nightly resolved SHA mismatch", event: "workflow_run", calls: []releaseResolverAPICall{refs(nightly), published(nightly), api("git/ref/tags/"+nightly, object("commit", harness))}, reject: true},
		{name: "annotated nightly resolved SHA mismatch", event: "workflow_run", calls: []releaseResolverAPICall{refs(nightly), published(nightly), api("git/ref/tags/"+nightly, object("tag", annotated)), api("git/tags/"+annotated, object("commit", harness))}, reject: true},
		{name: "nightly publishing run has no tag", event: "workflow_run", calls: []releaseResolverAPICall{refs("v1.2.3-nightly.20261014.41.gdddddddd")}, output: "available=false\n"},
		{name: "ambiguous nightly run tags", event: "workflow_run", calls: []releaseResolverAPICall{refs(nightly, "v1.2.4-nightly.20261014.42.gaaaaaaaa")}, reject: true},
		{name: "nightly release event is suppressed", event: "release", env: map[string]string{"RELEASE_TAG": nightly}, output: "available=false\n"},
		{name: "draft event", event: "release", env: map[string]string{"RELEASE_DRAFT": "true"}, reject: true},
		{name: "published release tag mismatch", event: "release", calls: []releaseResolverAPICall{api("releases/tags/"+stable, map[string]any{"tag_name": "v9.9.9", "draft": false, "published_at": "2026-10-14T00:00:00Z"})}, reject: true},
		{name: "unpublished release", event: "release", calls: []releaseResolverAPICall{api("releases/tags/"+stable, map[string]any{"tag_name": stable, "draft": false, "published_at": nil})}, reject: true},
		{name: "unsupported tag object", event: "release", calls: []releaseResolverAPICall{published(stable), api("git/ref/tags/"+stable, object("tree", source))}, reject: true},
		{name: "malformed resolved SHA", event: "release", calls: []releaseResolverAPICall{published(stable), api("git/ref/tags/"+stable, object("commit", "main"))}, reject: true},
		{name: "annotated cycle exceeds peel limit", event: "release", calls: []releaseResolverAPICall{published(stable), api("git/ref/tags/"+stable, object("tag", annotated)), api("git/tags/"+annotated, object("tag", annotated)), api("git/tags/"+annotated, object("tag", annotated)), api("git/tags/"+annotated, object("tag", annotated)), api("git/tags/"+annotated, object("tag", annotated))}, reject: true},
		{name: "malformed trusted main SHA", event: "release", calls: []releaseResolverAPICall{stableCalls[0], stableCalls[1], {args: main.args, body: "main"}}, reject: true},
		{name: "foreign event repository", event: "release", env: map[string]string{"EVENT_REPOSITORY": "attacker/Hikyo"}, reject: true},
		{name: "foreign nightly repository", event: "workflow_run", env: map[string]string{"NIGHTLY_REPOSITORY": "attacker/Hikyo"}, reject: true},
		{name: "untrusted default branch", event: "release", env: map[string]string{"DEFAULT_BRANCH": "feature"}, reject: true},
		{name: "untrusted nightly branch", event: "workflow_run", env: map[string]string{"NIGHTLY_BRANCH": "feature"}, reject: true},
		{name: "untrusted nightly workflow", event: "workflow_run", env: map[string]string{"NIGHTLY_PATH": ".github/workflows/untrusted.yml"}, reject: true},
		{name: "pull request nightly event", event: "workflow_run", env: map[string]string{"NIGHTLY_EVENT": "pull_request"}, reject: true},
		{name: "malformed nightly run number", event: "workflow_run", env: map[string]string{"NIGHTLY_RUN_NUMBER": "42.*"}, reject: true},
		{name: "malformed nightly SHA", event: "workflow_run", env: map[string]string{"NIGHTLY_SHA": "main"}, reject: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			// The fake transport accepts only this scenario's ordered, read-only
			// API requests. Unexpected commands, writes, and extra calls fail.
			transport := `#!/bin/bash
set -euo pipefail
trap 'status=$?; if [ "$status" -ne 0 ]; then touch "$RESOLVER_FIXTURES/transport-failed"; fi' EXIT
count=$(cat "$RESOLVER_FIXTURES/count")
count=$((count + 1))
printf '%s\n' "$count" >"$RESOLVER_FIXTURES/count"
actual=$(printf '%s\n' "$@")
expected=$(cat "$RESOLVER_FIXTURES/$count.args")
if [ "$actual" != "$expected" ]; then
  printf 'unexpected gh request: %s\n' "$actual" >&2
  exit 1
fi
cat "$RESOLVER_FIXTURES/$count.body"
`
			write := func(name, content string, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			write("gh", transport, 0700)
			write("count", "0\n", 0600)
			for i, call := range tt.calls {
				write(fmt.Sprintf("%d.args", i+1), strings.Join(call.args, "\n")+"\n", 0600)
				write(fmt.Sprintf("%d.body", i+1), call.body+"\n", 0600)
			}
			outputFile := filepath.Join(dir, "output")
			write("output", "", 0600)
			env := map[string]string{
				"PATH": dir + string(os.PathListSeparator) + os.Getenv("PATH"), "RESOLVER_FIXTURES": dir,
				"GITHUB_OUTPUT": outputFile, "GITHUB_EVENT_NAME": tt.event,
				"GITHUB_REPOSITORY": repository, "EVENT_REPOSITORY": repository, "DEFAULT_BRANCH": "main",
				"RELEASE_TAG": stable, "RELEASE_DRAFT": "false",
				"NIGHTLY_PATH": ".github/workflows/nightly.yml", "NIGHTLY_BRANCH": "main",
				"NIGHTLY_REPOSITORY": repository, "NIGHTLY_SHA": source, "NIGHTLY_RUN_NUMBER": "42", "NIGHTLY_EVENT": "schedule",
			}
			for key, value := range tt.env {
				env[key] = value
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", steps[0].Run)
			cmd.Dir = dir
			// Do not inherit developer credentials or contact the live API.
			for key, value := range env {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
			logs, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatal("resolver timed out:", ctx.Err())
			}
			if _, transportErr := os.Stat(filepath.Join(dir, "transport-failed")); !os.IsNotExist(transportErr) {
				t.Fatalf("resolver issued an unexpected API request: %v\n%s", transportErr, logs)
			}
			if (err != nil) != tt.reject {
				t.Fatalf("reject=%t, resolver error=%v\n%s", tt.reject, err, logs)
			}
			result, err := os.ReadFile(outputFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(result) != tt.output {
				t.Fatalf("unexpected resolver outputs: got %q, want %q", result, tt.output)
			}
			count, err := os.ReadFile(filepath.Join(dir, "count"))
			if err != nil {
				t.Fatal(err)
			}
			if string(count) != fmt.Sprintf("%d\n", len(tt.calls)) {
				t.Fatalf("API calls: got %s, want %d\n%s", count, len(tt.calls), logs)
			}
		})
	}
}
