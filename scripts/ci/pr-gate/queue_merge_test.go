package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type queueMergeFixture struct {
	p                pull
	source, git, log string
	fault, shared    string
}

func newQueueMergeFixture(t *testing.T, conflict bool) queueMergeFixture {
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
	source := t.TempDir()
	run := func(input string, args ...string) string {
		t.Helper()
		cmd := exec.Command(git, args...)
		cmd.Env = queueDCOEnvironment()
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture Git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("", "init", "--bare", "--quiet", "--template=", source)
	run("", "-C", source, "config", "uploadpack.allowFilter", "true")
	blob := func(body string) string { return run(body, "-C", source, "hash-object", "-w", "--stdin") }
	commit := func(body, parent, subject string) string {
		tree := run("100644 blob "+blob(body)+"\tshared.txt\n", "-C", source, "mktree")
		parents := ""
		if parent != "" {
			parents = "parent " + parent + "\n"
		}
		return run("tree "+tree+"\n"+parents+"author Fixture <fixture@example.test> 1700000000 +0000\ncommitter Fixture <fixture@example.test> 1700000000 +0000\n\n"+subject+"\n\nSigned-off-by: Fixture <fixture@example.test>\n", "-C", source, "hash-object", "-w", "-t", "commit", "--stdin")
	}
	original := "one\ntwo\nthree\nfour\nfive\n"
	shared := blob(original)
	ancestor := commit(original, "", "Ancestor")
	base := commit("base\ntwo\nthree\nfour\nfive\n", ancestor, "Base")
	headBody := "one\ntwo\nthree\nfour\nhead\n"
	if conflict {
		headBody = "head\ntwo\nthree\nfour\nfive\n"
	}
	head := commit(headBody, ancestor, "Head")
	run("", "-C", source, "update-ref", "refs/heads/main", base)
	run("", "-C", source, "update-ref", "refs/heads/feature", head)
	tools := t.TempDir()
	log, fault := filepath.Join(tools, "calls"), filepath.Join(tools, "fault")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	// The only substitution is transport: production's explicit HTTPS origin is
	// checked below, then real Git uses an offline fixture with partial-clone
	// support. Fetch, missing-blob hydration, merging and DCO all remain real Git.
	shim := strings.NewReplacer("@LOG@", quote(log), "@GIT@", quote(git), "@ORIGIN@", quote("file://"+source), "@FAULT@", quote(fault), "@HEAD@", quote(head), "@SHARED@", quote(shared)).Replace(`#!/bin/sh
set -eu
test "$GIT_CONFIG_GLOBAL" = /dev/null
test "$GIT_CONFIG_SYSTEM" = /dev/null
test "$GIT_CONFIG_COUNT" = 0
test "${GH_TOKEN+x}" != x
test "${GITHUB_TOKEN+x}" != x
operation=; target=; previous=; penultimate=; origin=false; promisor=false; filter=false; fetch_filter=false; write_tree=false
for argument in "$@"; do
 if [ "$previous" = -C ]; then target=$argument; fi
 case "$argument" in
  fetch|merge-tree) operation=$argument ;;
  rev-list) printf 'dco\n' >> @LOG@ ;;
  remote.origin.url=https://github.com/Hikyo-Org/Hikyo.git) origin=true ;;
  remote.origin.promisor=true) promisor=true ;;
  remote.origin.partialclonefilter=blob:none) filter=true ;;
  --filter=blob:none) fetch_filter=true ;;
  --write-tree) write_tree=true ;;
 esac
 penultimate=$previous
 previous=$argument
done
if [ -n "$operation" ]; then
 printf '%s %s\n' "$operation" "$target" >> @LOG@
 $origin && $promisor && $filter
fi
if [ "$operation" = fetch ]; then
 $fetch_filter
 @GIT@ -c protocol.file.allow=always -c remote.origin.url=@ORIGIN@ -c remote.origin.promisor=true -c remote.origin.partialclonefilter=blob:none -C "$target" fetch --quiet --no-tags --filter=blob:none origin "$penultimate" "$previous"
 if @GIT@ -c remote.origin.promisor=false -C "$target" cat-file -e @SHARED@ 2>/dev/null; then exit 92; fi
 printf 'filtered\n' >> @LOG@
 exit 0
fi
if [ "$operation" = merge-tree ]; then
 $write_tree
 if [ -e @FAULT@ ]; then
  case "$(cat @FAULT@)" in
   conflict) exit 1 ;;
   malformed) printf 'not-an-object\n'; exit 0 ;;
   extra) printf '%s\nunexpected output\n' @HEAD@; exit 0 ;;
   commit) printf '%s\n' @HEAD@; exit 0 ;;
   no-newline) printf '%s' @HEAD@; exit 0 ;;
   missing) printf '0000000000000000000000000000000000000000\n'; exit 0 ;;
   overflow) head -c 65536 /dev/zero; exit 0 ;;
   blocked) exec sleep 30 ;;
  esac
 fi
 exec @GIT@ -c core.hooksPath=/dev/null -c protocol.file.allow=always -c remote.origin.url=@ORIGIN@ -c remote.origin.promisor=true -c remote.origin.partialclonefilter=blob:none -C "$target" merge-tree --write-tree "$penultimate" "$previous"
fi
exec @GIT@ "$@"
`)

	if err := os.WriteFile(filepath.Join(tools, "git"), []byte(shim), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := repository{ID: 10, FullName: "Hikyo-Org/Hikyo", DefaultBranch: "main"}
	p := pull{Number: 7, State: "open", Base: reference{SHA: base, Ref: "main", Repo: r}, Head: reference{SHA: head, Ref: "feature", Repo: r}}
	p.User.ID, p.User.Login = 12, "contributor"
	return queueMergeFixture{p: p, source: source, git: git, log: log, fault: fault, shared: shared}
}

func TestQueueDCORejectsSignedButConflictingImmutableHeads(t *testing.T) {
	f := newQueueMergeFixture(t, true)
	code, err := verifyQueueDCO(context.Background(), "Hikyo-Org/Hikyo", f.p)
	if err == nil {
		t.Fatalf("signed conflicting PR passed queue proof: %d", code)
	}
}

func (f queueMergeFixture) calls(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.log)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestQueueDCOCleanMergeHydratesFilteredObjectsWithoutCheckout(t *testing.T) {
	f := newQueueMergeFixture(t, false)
	t.Setenv("GH_TOKEN", "must-not-reach-git")
	t.Setenv("GITHUB_TOKEN", "must-not-reach-git")
	verify, close := admissionDCOVerifier("Hikyo-Org/Hikyo")
	defer close()
	if code, err := verify(context.Background(), f.p); err != nil || code != 0 {
		t.Fatalf("clean signed PR failed immutable proof: %d/%v", code, err)
	}
	log := f.calls(t)
	if strings.Count(log, "filtered\n") != 1 || strings.Count(log, "merge-tree ") != 1 {
		t.Fatalf("filtered fetch or real merge was omitted: %s", log)
	}
	metadata := strings.Split(strings.TrimPrefix(log, "fetch "), "\n")[0]
	cmd := exec.Command(f.git, "-c", "remote.origin.promisor=false", "-C", metadata, "cat-file", "-t", f.shared)
	cmd.Env = queueDCOEnvironment()
	if out, err := cmd.Output(); err != nil || string(out) != "blob\n" {
		t.Fatalf("merge did not hydrate the missing ancestor blob: %q/%v", out, err)
	}
	for _, name := range []string{"index", "shared.txt"} {
		if _, err := os.Stat(filepath.Join(metadata, name)); !os.IsNotExist(err) {
			t.Fatalf("bare conflict proof created a checkout or index: %s/%v", name, err)
		}
	}
}

func TestAdmissionDCOReusesObjectsAndRepeatsConflictProof(t *testing.T) {
	f := newQueueMergeFixture(t, false)
	verify, close := admissionDCOVerifier("Hikyo-Org/Hikyo")
	defer close()
	if code, err := verify(context.Background(), f.p); err != nil || code != 0 {
		t.Fatalf("initial immutable proof: %d/%v", code, err)
	}
	if err := os.WriteFile(f.fault, []byte("conflict"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := verify(context.Background(), f.p); err == nil {
		t.Fatal("final verification reused a cached conflict verdict")
	}
	log := f.calls(t)
	if strings.Count(log, "fetch ") != 1 || strings.Count(log, "dco\n") != 2 || strings.Count(log, "merge-tree ") != 2 {
		t.Fatalf("final proof refetched immutable objects or skipped DCO/conflict verification: %s", log)
	}
}

func TestQueueConflictProofRejectsMalformedAndUnauthenticatedTrees(t *testing.T) {
	for _, fault := range []string{"malformed", "extra", "commit", "no-newline", "missing", "overflow"} {
		t.Run(fault, func(t *testing.T) {
			f := newQueueMergeFixture(t, false)
			if err := os.WriteFile(f.fault, []byte(fault), 0600); err != nil {
				t.Fatal(err)
			}
			if code, err := verifyQueueDCO(context.Background(), "Hikyo-Org/Hikyo", f.p); err == nil {
				t.Fatalf("unproven merge tree accepted: %d", code)
			}
		})
	}
}

func TestQueueConflictProofRejectsMissingHydrationObjects(t *testing.T) {
	f := newQueueMergeFixture(t, false)
	dir, repo, err := fetchQueueDCO(context.Background(), "Hikyo-Org/Hikyo", f.p)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupQueueDCOMetadata(dir)
	if err := os.Remove(filepath.Join(f.source, "objects", f.shared[:2], f.shared[2:])); err != nil {
		t.Fatal(err)
	}
	if err := checkQueueMerge(context.Background(), "Hikyo-Org/Hikyo", f.p, repo); err == nil {
		t.Fatal("missing promised blob authorized conflict-free merge")
	}
}

func TestQueueConflictProofCancellationFailsClosed(t *testing.T) {
	f := newQueueMergeFixture(t, false)
	dir, repo, err := fetchQueueDCO(context.Background(), "Hikyo-Org/Hikyo", f.p)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupQueueDCOMetadata(dir)
	if err := os.WriteFile(f.fault, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := checkQueueMerge(ctx, "Hikyo-Org/Hikyo", f.p, repo); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled conflict proof returned %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancelled conflict subprocess did not release its budget")
	}
}
