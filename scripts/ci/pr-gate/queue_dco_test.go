package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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
	tree := object("tree", "")
	base := object("commit", "tree "+tree+"\nauthor Fixture Base <base@example.test> 1700000000 +0000\ncommitter Fixture Base <base@example.test> 1700000000 +0000\n\nFixture base\n")
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
			head := object("commit", "tree "+tree+"\nparent "+base+"\nauthor "+tc.identity+" 1700000001 +0000\ncommitter "+tc.identity+" 1700000001 +0000\n\n"+tc.message)
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
