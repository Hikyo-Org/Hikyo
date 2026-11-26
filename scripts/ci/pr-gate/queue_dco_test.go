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
	for _, ref := range []string{queueTrustedMainRef, queueTrustedDevRef} {
		if out, err := exec.CommandContext(t.Context(), git, "-C", metadata, "update-ref", ref, base).CombinedOutput(); err != nil {
			t.Fatalf("update-ref: %v %s", err, out)
		}
	}
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
refspec=false
devspec=false
target=
previous=
for argument in "$@"; do
  if [ "$argument" = %s ]; then refspec=true; fi
  if [ "$argument" = %s ]; then devspec=true; fi
  if [ "$previous" = -C ]; then target=$argument; fi
  if [ "$argument" = fetch ]; then fetch=true; fi
  previous=$argument
done
if $fetch; then
  test -n "$target"
  $refspec
  $devspec
  cp -R %s/. "$target/"
  exit 0
fi
exec %s "$@"
`, "'"+queueTrustedMainRefspec+"'", "'"+queueTrustedDevRefspec+"'", quote(metadata), quote(git))
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
	const botIdentity = "dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>"
	canonicalRepo := repository{ID: 1, FullName: "example/hikyo"}
	forkRepo := repository{ID: 2, FullName: "contributor/hikyo"}
	for _, tc := range []struct {
		name, identity, actor, message string
		want                           int
		ref                            string
		repo                           repository
		onRef                          string
		backmerge                      bool
	}{
		{"signed", "Fixture Contributor <fixture@example.test>", "contributor", "Scoped edit\n\nSigned-off-by: Fixture Contributor <fixture@example.test>\n", 0, "", repository{}, "", false},
		{"missing", "Fixture Contributor <fixture@example.test>", "contributor", "Scoped edit\n", 1, "", repository{}, "", false},
		{"mismatched", "Fixture Contributor <fixture@example.test>", "contributor", "Scoped edit\n\nSigned-off-by: Someone Else <else@example.test>\n", 1, "", repository{}, "", false},
		{"bot header spoof", "dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>", "contributor", "Dependency edit\n", 1, "", repository{}, "", false},
		{"authenticated dependabot", "dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>", "dependabot[bot]", "Dependency edit\n", 0, "", repository{}, "", false},
		// Branch names no longer matter: a Dependabot commit is exempt only
		// when the canonical main or dev already holds it.
		{"promotion forging dependabot is refused", botIdentity, "maintainer", "Dependency edit\n", 1, "release/1.1.0", canonicalRepo, "", false},
		{"main-named head forging dependabot is refused", botIdentity, "maintainer", "Dependency edit\n", 1, "main", canonicalRepo, "", false},
		{"feature branch spoof", botIdentity, "maintainer", "Dependency edit\n", 1, "feat/x", canonicalRepo, "", false},
		{"fork spoof", botIdentity, "contributor", "Dependency edit\n", 1, "release/1.1.0", forkRepo, "", false},
		{"dependabot commit already on main", botIdentity, "maintainer", "Dependency edit\n", 0, "feat/x", canonicalRepo, "main", false},
		{"backmerge carries main's dependabot commit", botIdentity, "maintainer", "Dependency edit\n", 0, "backmerge/main-into-dev", canonicalRepo, "main", true},
		// Dependabot's commits enter dev through PRs it opened, so a
		// dev to main promotion carries them and must pass.
		{"promotion carries dev's dependabot commit", botIdentity, "maintainer", "Dependency edit\n", 0, "release/nightly-20261126", canonicalRepo, "dev", false},
		{"promotion merge carries dev's dependabot commit", botIdentity, "maintainer", "Dependency edit\n", 0, "release/nightly-20261126", canonicalRepo, "dev", true},
		{"forged dependabot author in a human PR into dev is refused", botIdentity, "contributor", "Dependency edit\n", 1, "fix/x", canonicalRepo, "", false},
		{"dev ancestry never exempts humans", "Fixture Contributor <fixture@example.test>", "maintainer", "Scoped edit\n", 1, "release/nightly-20261126", canonicalRepo, "dev", false},
		{"main ancestry never exempts humans", "Fixture Contributor <fixture@example.test>", "maintainer", "Scoped edit\n", 1, "release/1.1.0", canonicalRepo, "main", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			head := object("commit", "tree "+headTree+"\nparent "+base+"\nauthor "+tc.identity+" 1700000001 +0000\ncommitter "+tc.identity+" 1700000001 +0000\n\n"+tc.message)
			bot := head
			mainTip, devTip := base, base
			switch tc.onRef {
			case "main":
				mainTip = bot
			case "dev":
				devTip = bot
			}
			if tc.backmerge {
				const merger = "Fixture Maintainer <maintainer@example.test>"
				head = object("commit", "tree "+headTree+"\nparent "+base+"\nparent "+bot+"\nauthor "+merger+" 1700000002 +0000\ncommitter "+merger+" 1700000002 +0000\n\nBackmerge\n\nSigned-off-by: "+merger+"\n")
			}
			for ref, tip := range map[string]string{queueTrustedMainRef: mainTip, queueTrustedDevRef: devTip} {
				if out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "update-ref", ref, tip).CombinedOutput(); err != nil {
					t.Fatalf("update-ref: %v %s", err, out)
				}
			}
			p := pull{Base: reference{SHA: base}, Head: reference{SHA: head, Ref: tc.ref, Repo: tc.repo}}
			p.User.ID = 12
			p.User.Login = tc.actor
			// An inherited exemption may never exempt a human/spoofed author.
			t.Setenv("DCO_EXEMPT_AUTHOR", tc.identity)
			code, err := checkQueueDCO(context.Background(), "example/hikyo", p, repo)
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

// A fully signed range that check-dco.sh would admit with exit 0 if the gate
// ignored a missing trusted tip: only the one named ref exists, so the gate
// itself must refuse with its own error rather than lean on the script.
func queueDCOFailClosedFixture(t *testing.T, present string) (string, pull) {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	repo := t.TempDir()
	if out, err := exec.CommandContext(t.Context(), "git", "init", "--bare", "--quiet", repo).CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	object := func(kind, body string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", "-C", repo, "hash-object", "-w", "-t", kind, "--stdin")
		cmd.Stdin = strings.NewReader(body)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	const author = "Fixture Contributor <fixture@example.test>"
	tree := object("tree", "")
	base := object("commit", "tree "+tree+"\nauthor "+author+" 1700000000 +0000\ncommitter "+author+" 1700000000 +0000\n\nBase\n")
	head := object("commit", "tree "+tree+"\nparent "+base+"\nauthor "+author+" 1700000001 +0000\ncommitter "+author+" 1700000001 +0000\n\nEdit\n\nSigned-off-by: "+author+"\n")
	if out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "update-ref", present, base).CombinedOutput(); err != nil {
		t.Fatalf("update-ref: %v %s", err, out)
	}
	p := pull{Base: reference{SHA: base}, Head: reference{SHA: head}}
	p.User.ID, p.User.Login = 12, "contributor"
	return repo, p
}

func requireQueueDCOUnavailable(t *testing.T, present string) {
	repo, p := queueDCOFailClosedFixture(t, present)
	code, err := checkQueueDCO(context.Background(), "example/hikyo", p, repo)
	if err == nil || !strings.Contains(err.Error(), "trusted canonical main or dev is unavailable") || code != 0 {
		t.Fatalf("missing trusted tip did not fail closed in the gate: %d %v", code, err)
	}
}

func TestQueueDCOFailsClosedWithoutTrustedMain(t *testing.T) {
	requireQueueDCOUnavailable(t, queueTrustedDevRef)
}

func TestQueueDCOFailsClosedWithoutTrustedDev(t *testing.T) {
	requireQueueDCOUnavailable(t, queueTrustedMainRef)
}
