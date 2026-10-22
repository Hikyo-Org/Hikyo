package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Execute the canonical shell verifier, stubbing only its external gh transport.
// The Go controller retains both independent verifier subprocess invocations.
func canonicalPolicyFixture(t *testing.T, f *queueFixture, authority, movement string) string {
	t.Helper()
	script, err := os.ReadFile("../check-fork-validation.sh")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, path := range []string{"scripts/ci", "bin"} {
		if err := os.MkdirAll(filepath.Join(dir, path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if authority == "BDFL" {
		f.pr.User.ID, f.pr.User.Login = 991668, "Dunky13"
		f.refresh()
		f.feedbackRun.Pulls = []pull{f.pr}
		f.feedbackRefresh()
	}
	pr, err := json.Marshal(struct {
		pull
		ChangedFiles int `json:"changed_files"`
	}{pull: f.pr, ChangedFiles: 1})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"scripts/ci/check-fork-validation.sh": script,
		"pr.json":                             pr,
		"permission.json":                     []byte(`{"user":{"id":991668},"permission":"write"}`),
		"reviews.json":                        []byte(`[[{"id":20,"user":{"id":11,"login":"maintainer"},"state":"APPROVED","commit_id":"` + f.pr.Head.SHA + `","submitted_at":"2026-10-01T00:00:00Z"}]]`),
		"bin/gh": []byte(`#!/bin/sh
set -eu
[ "$1" = api ]
url=$2
printf '%s\n' "$url" >> "$POLICY_FIXTURES/reads"
case "$url" in
*/pulls/7/files*) printf '[{"filename":".github/workflows/ci-feedback.yml"}]\n' ;;
*/pulls/7/reviews*) cat "$POLICY_FIXTURES/reviews.json" ;;
*/collaborators/*/permission)
  printf 'permission\n' >> "$POLICY_FIXTURES/authority"
  if [ "$POLICY_MOVEMENT" = head ] && [ "$(wc -l < "$POLICY_FIXTURES/authority")" -eq 2 ]; then
    jq '.head.sha = "cccccccccccccccccccccccccccccccccccccccc"' "$POLICY_FIXTURES/pr.json" > "$POLICY_FIXTURES/moved.json"
    mv "$POLICY_FIXTURES/moved.json" "$POLICY_FIXTURES/pr.json"
  fi
  cat "$POLICY_FIXTURES/permission.json"
  ;;
*/pulls/7) cat "$POLICY_FIXTURES/pr.json" ;;
*) printf 'unexpected fixture endpoint: %s\n' "$url" >&2; exit 1 ;;
esac
`),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", filepath.Join(dir, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("POLICY_FIXTURES", dir)
	t.Setenv("POLICY_MOVEMENT", movement)
	t.Setenv("GH_REPO", f.c.repoName)
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	t.Chdir(dir)
	return dir
}

func TestAdmissionComposesBothCanonicalPolicyVerifiers(t *testing.T) {
	for _, route := range []string{"head worker", "direct callback"} {
		for _, authority := range []string{"BDFL", "independent reviewer"} {
			for _, movement := range []string{"none", "permission", "approval", "head"} {
				if authority == "BDFL" && movement == "approval" {
					continue
				}
				t.Run(route+"/"+authority+"/"+movement, func(t *testing.T) {
					f := newQueueFixture(t)
					dir := canonicalPolicyFixture(t, f, authority, movement)
					old := snapshotPriorFailure(f)
					calls := 0
					f.c.verify = func(ctx context.Context, pr int, head string) (policyDecision, error) {
						calls++
						decision, err := verifyPolicy(ctx, pr, head)
						if calls == 1 && err == nil && decision.Code == 0 {
							var name, data string
							switch movement {
							case "permission":
								name, data = "permission.json", `{"user":{"id":991668},"permission":"none"}`
							case "approval":
								name, data = "reviews.json", `[[]]`
							}
							if name != "" {
								if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
									t.Fatal(err)
								}
							}
						}
						return decision, err
					}
					var err error
					if route == "head worker" {
						err = snapshotAdmission(f)
					} else {
						err = directSnapshotAdmission(f)
					}
					if err != nil {
						t.Fatal(err)
					}
					if calls != 2 {
						t.Fatalf("canonical verifier invocations=%d, want 2", calls)
					}
					if f.api.checks[0] != old {
						t.Fatal("reconciliation modified the previous terminal check")
					}
					if movement != "none" {
						f.assertNoSuccess(t)
						return
					}
					if f.api.checks[len(f.api.checks)-1].Conclusion != "success" {
						t.Fatal("canonical workflow authority did not compose with admission")
					}
					reads, err := os.ReadFile(filepath.Join(dir, "reads"))
					if err != nil {
						t.Fatal(err)
					}
					want := 10
					if authority == "independent reviewer" {
						want = 12
					}
					if got := strings.Count(string(reads), "\n"); got != want {
						t.Fatalf("canonical shell API reads=%d, want %d", got, want)
					}
					controllerReads := 0
					for _, count := range f.api.reads {
						controllerReads += count
					}
					if route == "head worker" && controllerReads != 22 {
						t.Fatalf("head-worker controller reads=%d, want 22", controllerReads)
					}
					t.Logf("%d controller + %d canonical shell API reads (%s)", controllerReads, want, authority)
				})
			}
		}
	}
}
