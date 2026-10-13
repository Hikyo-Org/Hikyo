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

type admissionDCOFixture struct {
	p         pull
	log, fail string
}

func newAdmissionDCOFixture(t *testing.T, bot bool) admissionDCOFixture {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if out, err := exec.Command(git, "init", "--bare", "--quiet", repo).CombinedOutput(); err != nil {
		t.Fatalf("init: %v %s", err, out)
	}
	object := func(kind, body string) string {
		cmd := exec.Command(git, "-C", repo, "hash-object", "-w", "-t", kind, "--stdin")
		cmd.Stdin = strings.NewReader(body)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	tree := object("tree", "")
	base := object("commit", "tree "+tree+"\nauthor Fixture <fixture@example.test> 1700000000 +0000\ncommitter Fixture <fixture@example.test> 1700000000 +0000\n\nBase\n")
	identity, message, actor := "Fixture <fixture@example.test>", "Edit\n\nSigned-off-by: Fixture <fixture@example.test>\n", "contributor"
	if bot {
		identity = "dependabot[bot] <49699333+dependabot[bot]@users.noreply.github.com>"
		message = "Dependency edit\n"
		actor = "dependabot[bot]"
	}
	head := object("commit", "tree "+tree+"\nparent "+base+"\nauthor "+identity+" 1700000001 +0000\ncommitter "+identity+" 1700000001 +0000\n\n"+message)
	tools := t.TempDir()
	log := filepath.Join(tools, "calls")
	fail := filepath.Join(tools, "fail-fetch")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	shim := fmt.Sprintf(`#!/bin/sh
set -eu
test "$GIT_CONFIG_GLOBAL" = /dev/null
test "$GIT_CONFIG_SYSTEM" = /dev/null
test "$GIT_CONFIG_NOSYSTEM" = 1
test "$GIT_CONFIG_COUNT" = 0
test "${GH_TOKEN+x}" != x
fetch=false; target=; previous=
for argument in "$@"; do
 if [ "$previous" = -C ]; then target=$argument; fi
 if [ "$argument" = fetch ]; then fetch=true; fi
 if [ "$argument" = rev-list ]; then printf 'check\n' >> %s; fi
 previous=$argument
done
if $fetch; then
 printf 'fetch %%s\n' "$target" >> %s
 test ! -e %s
 cp -R %s/. "$target/"
 exit 0
fi
exec %s "$@"
`, quote(log), quote(log), quote(fail), quote(repo), quote(git))
	if err := os.WriteFile(filepath.Join(tools, "git"), []byte(shim), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GH_TOKEN", "must-not-reach-metadata")
	r := repository{ID: 10, FullName: "Hikyo-Org/Hikyo", DefaultBranch: "main"}
	p := pull{Number: 7, State: "open", Base: reference{SHA: base, Ref: "main", Repo: r}, Head: reference{SHA: head, Ref: "feature", Repo: r}}
	p.User.ID, p.User.Login = 12, actor
	return admissionDCOFixture{p, log, fail}
}
func (f admissionDCOFixture) counts(t *testing.T) (int, int, []string) {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		t.Fatal(err)
	}
	fetches, checks := 0, 0
	var dirs []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasPrefix(line, "fetch ") {
			fetches++
			dirs = append(dirs, strings.TrimPrefix(line, "fetch "))
		}
		if line == "check" {
			checks++
		}
	}
	return fetches, checks, dirs
}
func TestAdmissionDCOReusesMetadataButRerunsCanonicalChecker(t *testing.T) {
	fixture := newAdmissionDCOFixture(t, false)
	f := newQueueFixture(t)
	f.pr = fixture.p
	f.c.expectedHead = f.pr.Head.SHA
	f.refresh()
	f.feedbackRun.HeadSHA = f.pr.Head.SHA
	f.feedbackRun.Pulls = []pull{f.pr}
	f.feedbackRefresh()
	f.groupRefresh()
	f.q.dco = nil
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	fetches, checks, dirs := fixture.counts(t)
	if fetches != 1 || checks != 2 {
		t.Fatalf("actual admission fetched=%d checked=%d, want1/2", fetches, checks)
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("metadata survived admission: %s %v", dir, err)
		}
	}
	if got := f.api.checks[len(f.api.checks)-1].Conclusion; got != "success" {
		t.Fatalf("canonical admission: %s", got)
	}
	rows := len(f.api.checks)
	if err := f.admit(); err != nil {
		t.Fatal(err)
	}
	fetches, checks, dirs = fixture.counts(t)
	if fetches != 2 || checks != 4 || len(f.api.checks) != rows {
		t.Fatalf("separate admission borrowed state or grew verdicts: fetches=%d checks=%d rows=%d/%d", fetches, checks, len(f.api.checks), rows)
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("second admission retained metadata: %v", err)
		}
	}
}

func TestAdmissionDCOMetadataIdentityAndLifetime(t *testing.T) {
	for _, change := range []string{"pr number", "pr state", "base sha", "head sha", "base repo id", "base repo name", "head repo id", "head repo name", "actor id", "actor login", "head ref", "base ref", "cancelled", "closed", "missing metadata"} {
		t.Run(change, func(t *testing.T) {
			fixture := newAdmissionDCOFixture(t, true)
			verify, close := admissionDCOVerifier("Hikyo-Org/Hikyo")
			defer close()
			if code, err := verify(context.Background(), fixture.p); err != nil || code != 0 {
				t.Fatalf("first canonical check: %d %v", code, err)
			}
			p := fixture.p
			ctx := context.Background()
			switch change {
			case "pr number":
				p.Number++
			case "pr state":
				p.State = "closed"
			case "base sha":
				p.Base.SHA = strings.Repeat("b", 40)
			case "head sha":
				p.Head.SHA = strings.Repeat("a", 40)
			case "base repo id":
				p.Base.Repo.ID++
			case "base repo name":
				p.Base.Repo.FullName = "Other/Repo"
			case "head repo id":
				p.Head.Repo.ID++
			case "head repo name":
				p.Head.Repo.FullName = "Other/Fork"
			case "actor id":
				p.User.ID++
			case "actor login":
				p.User.Login = "contributor"
			case "head ref":
				p.Head.Ref = "other"
			case "base ref":
				p.Base.Ref = "release"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "closed":
				close()
			case "missing metadata":
				_, _, dirs := fixture.counts(t)
				if err := os.RemoveAll(dirs[0]); err != nil {
					t.Fatal(err)
				}
			}
			if code, err := verify(ctx, p); err == nil {
				t.Fatalf("%s accepted stale metadata/exemption: %d", change, code)
			}
			fetches, checks, dirs := fixture.counts(t)
			if fetches != 1 || checks != 1 {
				t.Fatalf("mismatch caused more transport/canonical execution: %d/%d", fetches, checks)
			}
			close()
			for _, dir := range dirs {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatalf("metadata not removed on refusal: %v", err)
				}
			}
		})
	}
}
func TestAdmissionDCOFailedFetchCannotPopulateSession(t *testing.T) {
	f := newAdmissionDCOFixture(t, false)
	verify, close := admissionDCOVerifier("Hikyo-Org/Hikyo")
	defer close()
	if err := os.WriteFile(f.fail, []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := verify(context.Background(), f.p); err == nil {
		t.Fatal("failed fetch accepted")
	}
	fetches, checks, dirs := f.counts(t)
	if fetches != 1 || checks != 0 {
		t.Fatalf("failed fetch validated: %d/%d", fetches, checks)
	}
	if _, err := os.Stat(dirs[0]); !os.IsNotExist(err) {
		t.Fatalf("partial metadata survived: %v", err)
	}
	if err := os.Remove(f.fail); err != nil {
		t.Fatal(err)
	}
	if code, err := verify(context.Background(), f.p); err != nil || code != 0 {
		t.Fatalf("fresh fetch after refusal: %d %v", code, err)
	}
	fetches, checks, _ = f.counts(t)
	if fetches != 2 || checks != 1 {
		t.Fatalf("partial fetch was reused: %d/%d", fetches, checks)
	}
}
func TestAdmissionDCOFreshActorReadCannotBorrowBotExemption(t *testing.T) {
	fixture := newAdmissionDCOFixture(t, true)
	f := newQueueFixture(t)
	f.pr = fixture.p
	f.c.expectedHead = f.pr.Head.SHA
	f.refresh()
	f.feedbackRun.HeadSHA = f.pr.Head.SHA
	f.feedbackRun.Pulls = []pull{f.pr}
	f.feedbackRefresh()
	f.groupRefresh()
	f.q.dco = nil
	f.api.readHook = func(path string, n int) {
		if path == f.c.endpoint("pulls/7") && n == 2 {
			p := f.pr
			p.User.Login = "contributor"
			f.api.responses[path] = p
		}
	}
	if err := f.admit(); err == nil {
		t.Fatal("fresh actor movement accepted")
	}
	f.assertNoSuccess(t)
	fetches, checks, _ := fixture.counts(t)
	if fetches != 1 || checks != 1 {
		t.Fatalf("fresh actor did not refuse metadata reuse: %d/%d", fetches, checks)
	}
}

func TestAdmissionDCOCanonicalRefusalIsNotReplacedByCachedVerdict(t *testing.T) {
	f := newAdmissionDCOFixture(t, true)
	f.p.User.Login = "contributor"
	verify, close := admissionDCOVerifier("Hikyo-Org/Hikyo")
	defer close()
	for i := 0; i < 2; i++ {
		if code, err := verify(context.Background(), f.p); err != nil || code != 1 {
			t.Fatalf("unsigned canonical refusal: %d %v", code, err)
		}
	}
	fetches, checks, _ := f.counts(t)
	if fetches != 1 || checks != 2 {
		t.Fatalf("refusal was cached as a verdict: %d/%d", fetches, checks)
	}
}
