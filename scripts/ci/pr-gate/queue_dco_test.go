package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestQueueDCOIsolatesEveryGitSubprocessFromAmbientConfiguration(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	metadata := t.TempDir()
	if out, err := exec.Command(git, "init", "--bare", "--quiet", metadata).CombinedOutput(); err != nil {
		t.Fatalf("init fixture: %v %s", err, out)
	}
	object := func(kind, body string) string {
		t.Helper()
		cmd := exec.Command(git, "-C", metadata, "hash-object", "-w", "-t", kind, "--stdin")
		cmd.Stdin = strings.NewReader(body)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	tree := object("tree", "")
	base := object("commit", "tree "+tree+"\nauthor Fixture <fixture@example.test> 1700000000 +0000\ncommitter Fixture <fixture@example.test> 1700000000 +0000\n\nFixture base\n")
	head := object("commit", "tree "+tree+"\nparent "+base+"\nauthor Fixture <fixture@example.test> 1700000001 +0000\ncommitter Fixture <fixture@example.test> 1700000001 +0000\n\nFixture edit\n\nSigned-off-by: Fixture <fixture@example.test>\n")
	tools := t.TempDir()
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	// Fetch is an offline transport seam only. Init and all canonical Git
	// reads execute the real Git binary; every subprocess must share the safe
	// environment. No external request or untrusted code executes in this test.
	wrapper := fmt.Sprintf(`#!/bin/sh
set -eu
for key in $(env | sed -n 's/^\(GIT_[^=]*\)=.*/\1/p'); do
  case "$key" in GIT_CONFIG_GLOBAL|GIT_CONFIG_SYSTEM|GIT_CONFIG_NOSYSTEM|GIT_CONFIG_COUNT|GIT_TERMINAL_PROMPT) ;; *) exit 91 ;; esac
done
test "${GIT_CONFIG_GLOBAL:-}" = /dev/null
test "${GIT_CONFIG_SYSTEM:-}" = /dev/null
test "${GIT_CONFIG_NOSYSTEM:-}" = 1
test "${GIT_CONFIG_COUNT:-}" = 0
test "${GIT_TERMINAL_PROMPT:-}" = 0
test "${GH_TOKEN+x}" != x
test "${DCO_EXEMPT_AUTHOR+x}" != x
fetch=false
target=
previous=
for argument in "$@"; do
  if [ "$previous" = -C ]; then target=$argument; fi
  if [ "$argument" = fetch ]; then fetch=true; fi
  previous=$argument
done
if $fetch; then
  test -n "$target"
  cp -R %s/. "$target/"
  exit 0
fi
exec %s "$@"
`, quote(metadata), quote(git))
	if err := os.WriteFile(filepath.Join(tools, "git"), []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	for key, value := range map[string]string{"GIT_DIR": "/missing.git", "GIT_WORK_TREE": "/missing", "GIT_INDEX_FILE": "/missing.index", "GIT_OBJECT_DIRECTORY": "/missing.objects", "GIT_ALTERNATE_OBJECT_DIRECTORIES": "/missing.alternates", "GIT_CONFIG_GLOBAL": "/missing.config", "GIT_CONFIG_SYSTEM": "/missing.system", "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "url.http://127.0.0.1:1/.insteadOf", "GIT_CONFIG_VALUE_0": "https://github.com/", "GIT_TEMPLATE_DIR": "/missing.templates", "GIT_SSH_COMMAND": "false", "DCO_EXEMPT_AUTHOR": "Fixture <fixture@example.test>", "GH_TOKEN": "fixture-secret-must-not-be-inherited"} {
		t.Setenv(key, value)
	}
	p := pull{Base: reference{SHA: base}, Head: reference{SHA: head}}
	p.User.ID, p.User.Login = 12, "contributor"
	code, err := verifyQueueDCO(context.Background(), "Hikyo-Org/Hikyo", p)
	if err != nil || code != 0 {
		t.Fatalf("ambient configuration influenced immutable DCO reads: %d %v", code, err)
	}
}

func TestQueueUsesCanonicalDCOSignoffAndAuthenticatedException(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "--quiet", repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	object := func(kind, body string) string {
		t.Helper()
		cmd := exec.Command("git", "-C", repo, "hash-object", "-w", "-t", kind, "--stdin")
		cmd.Stdin = strings.NewReader(body)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	tree := func(name, blob string) string {
		t.Helper()
		cmd := exec.Command("git", "-C", repo, "mktree")
		cmd.Stdin = strings.NewReader("100644 blob " + blob + "\t" + name + "\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	deleted := object("blob", "one\ntwo\nthree\nfour\nfive\n")
	baseTree := tree("deleted.txt", deleted)
	headTree := tree("added.txt", object("blob", "one\ntwo\nthree\nfour\nhead\n"))
	base := object("commit", "tree "+baseTree+"\nauthor Fixture Base <base@example.test> 1700000000 +0000\ncommitter Fixture Base <base@example.test> 1700000000 +0000\n\nFixture base\n")
	// Blob-filtered admission metadata contains commits and trees, but the
	// deleted file need not exist locally. DCO must not run rename detection
	// or read file contents while checking commit authors and trailers.
	if err := os.Remove(filepath.Join(repo, "objects", deleted[:2], deleted[2:])); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, identity, actor, message string
		want                           int
	}{
		{"signed", "Fixture Contributor <fixture@example.test>", "contributor", "Scoped edit\n\nSigned-off-by: Fixture Contributor <fixture@example.test>\n", 0},
		{"missing", "Fixture Contributor <fixture@example.test>", "contributor", "Scoped edit\n", 1},
		{"mismatched", "Fixture Contributor <fixture@example.test>", "contributor", "Scoped edit\n\nSigned-off-by: Someone Else <else@example.test>\n", 1},
		{"bot header spoof", "dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>", "contributor", "Dependency edit\n", 1},
		{"authenticated dependabot", "dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>", "dependabot[bot]", "Dependency edit\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			head := object("commit", "tree "+headTree+"\nparent "+base+"\nauthor "+tc.identity+" 1700000001 +0000\ncommitter "+tc.identity+" 1700000001 +0000\n\n"+tc.message)
			p := pull{Base: reference{SHA: base}, Head: reference{SHA: head}}
			p.User.ID = 12
			p.User.Login = tc.actor
			// An inherited exemption may never exempt a human/spoofed author.
			t.Setenv("DCO_EXEMPT_AUTHOR", tc.identity)
			code, err := checkQueueDCO(context.Background(), p, repo)
			if err != nil || code != tc.want {
				t.Fatalf("canonical DCO outcome %d/%v, want%d", code, err, tc.want)
			}
		})
	}
}
func TestQueueDCOSignoffRefusalBlocksAdmissionAndGroup(t *testing.T) {
	for _, where := range []string{"admission", "group", "final admission", "final group"} {
		t.Run(where, func(t *testing.T) {
			f := newQueueFixture(t)
			calls := 0
			f.q.dco = func(context.Context, pull) (int, error) {
				calls++
				if strings.HasPrefix(where, "final") && calls == 1 {
					return 0, nil
				}
				return 1, nil
			}
			if strings.Contains(where, "admission") {
				_ = f.admit()
			} else {
				_ = f.group()
			}
			f.assertNoSuccess(t)
			if calls == 0 {
				t.Fatal("canonical DCO was not invoked")
			}
		})
	}
}
