package ci_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const supplyChainInputList = "scripts/ci/supply-chain-fixture-inputs.txt"

// Listed on top of the derived inputs: the list itself, the workflow that wires
// the job, and the replay runner. A change to any of them can break the PR job.
var supplyChainListExtras = []string{supplyChainInputList, ".github/workflows/ci-feedback.yml", "scripts/ci/run-job-steps/"}

func supplyChainListEntries(raw string) []string {
	var entries []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			entries = append(entries, line)
		}
	}
	return entries
}

// supplyChainListMisses returns the derived inputs (Go packages excluded: they
// are scoped to the test domain) that no list entry selects.
func supplyChainListMisses(entries []string, inputs map[string]string) []string {
	var misses []string
	for p, origin := range inputs {
		if supplyChainGoPackages[p] {
			continue
		}
		covered := false
		for _, entry := range entries {
			if strings.HasSuffix(entry, "/") {
				covered = covered || strings.HasPrefix(p+"/", entry)
			} else {
				covered = covered || p == entry
			}
		}
		if !covered {
			misses = append(misses, fmt.Sprintf("%s (from %s) has no entry in %s", p, origin, supplyChainInputList))
		}
	}
	sort.Strings(misses)
	return misses
}

func readSupplyChainList(t *testing.T, root string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, supplyChainInputList))
	if err != nil {
		t.Fatal(err)
	}
	return supplyChainListEntries(string(raw))
}

func supplyChainDerived(t *testing.T) (string, supplyChainRepo, map[string]string, map[string]string) {
	t.Helper()
	root := repositoryRoot(t)
	ci, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	repo := supplyChainRepository(t, root)
	runs := supplyChainRuns(t, ci)
	inputs, err := supplyChainInputs(repo, runs)
	if err != nil {
		t.Fatal(err)
	}
	return root, repo, runs, inputs
}

func TestSupplyChainInputListCoversEveryInput(t *testing.T) {
	root, repo, _, inputs := supplyChainDerived(t)
	entries := readSupplyChainList(t, root)
	for _, miss := range supplyChainListMisses(entries, inputs) {
		t.Error(miss)
	}
	for _, extra := range supplyChainListExtras {
		found := false
		for _, entry := range entries {
			found = found || entry == extra
		}
		if !found {
			t.Errorf("%s lost its declared entry %s", supplyChainInputList, extra)
		}
	}
	// An entry for a path that no longer exists silently selects nothing.
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry] {
			t.Errorf("duplicate entry %s", entry)
		}
		seen[entry] = true
		if _, ok := repo.files[entry]; !ok && !repo.isDir(strings.TrimSuffix(entry, "/")) {
			t.Errorf("entry %s names no tracked file or directory", entry)
		}
	}
}

// Prove each guard can fail on its own: break the classifier, then the list,
// each in a copy, and require only that guard to name what escaped. A fixture
// added in an uncovered area must trip both.
func TestSupplyChainGuardsFailIndependently(t *testing.T) {
	root, repo, runs, inputs := supplyChainDerived(t)
	entries := readSupplyChainList(t, root)
	registry := filepath.Join(root, "scripts/ci/ci-job-registry.json")
	realClassifier := filepath.Join(root, "scripts/ci/classify-changed-paths.sh")
	classifier, err := os.ReadFile(realClassifier)
	if err != nil {
		t.Fatal(err)
	}
	narrow := func(t *testing.T, pattern string) string {
		t.Helper()
		const anchor = "\t\tcase \"$path\" in\n\t\t# Only the nightly-desktop"
		if !strings.Contains(string(classifier), anchor) {
			t.Fatal("classifier lost the case the mutation anchors to")
		}
		mutated := strings.Replace(string(classifier), anchor,
			"\t\tcase \"$path\" in\n\t\t"+pattern+")\n\t\t\tselect_class docs\n\t\t\t;;\n\t\t# Only the nightly-desktop", 1)
		file := filepath.Join(t.TempDir(), "classify.sh")
		if err := os.WriteFile(file, []byte(mutated), 0o600); err != nil {
			t.Fatal(err)
		}
		return file
	}
	without := func(drop string) []string {
		var kept []string
		for _, entry := range entries {
			if entry != drop {
				kept = append(kept, entry)
			}
		}
		if len(kept) == len(entries) {
			t.Fatalf("list has no entry %s to remove", drop)
		}
		return kept
	}
	mentions := func(lines []string, want string) bool { return strings.Contains(strings.Join(lines, "\n"), want) }

	t.Run("classifier narrowed away, list intact", func(t *testing.T) {
		bad := supplyChainUncovered(t, narrow(t, ".githooks/*"), registry, repo, inputs)
		if !mentions(bad, ".githooks/pre-push") {
			t.Fatalf("narrowing .githooks/* escaped the classifier guard: %v", bad)
		}
		if misses := supplyChainListMisses(entries, inputs); len(misses) != 0 {
			t.Fatalf("the list guard tripped on a classifier change: %v", misses)
		}
	})
	t.Run("path removed from the list, classifier intact", func(t *testing.T) {
		misses := supplyChainListMisses(without(".githooks/pre-push"), inputs)
		if !mentions(misses, ".githooks/pre-push") {
			t.Fatalf("removing the hook from the list escaped the list guard: %v", misses)
		}
		if bad := supplyChainUncovered(t, realClassifier, registry, repo, inputs); len(bad) != 0 {
			t.Fatalf("the classifier guard tripped on a list change: %v", bad)
		}
	})
	t.Run("directory entry removed from the list", func(t *testing.T) {
		if misses := supplyChainListMisses(without("scripts/deploy/"), inputs); !mentions(misses, "scripts/deploy") {
			t.Fatalf("removing a directory entry escaped the list guard: %v", misses)
		}
	})
	t.Run("new fixture in an uncovered area trips both", func(t *testing.T) {
		files := map[string]string{"web/new-fixture_test.sh": "#!/bin/sh\n"}
		for k, v := range repo.files {
			files[k] = v
		}
		extended := newSupplyChainRepo(files)
		withNew := map[string]string{"New fixture": "./web/new-fixture_test.sh"}
		for k, v := range runs {
			withNew[k] = v
		}
		extendedInputs, err := supplyChainInputs(extended, withNew)
		if err != nil {
			t.Fatal(err)
		}
		if bad := supplyChainUncovered(t, realClassifier, registry, extended, extendedInputs); !mentions(bad, "web/new-fixture_test.sh") {
			t.Fatalf("a new fixture escaped the classifier guard: %v", bad)
		}
		if misses := supplyChainListMisses(entries, extendedInputs); !mentions(misses, "web/new-fixture_test.sh") {
			t.Fatalf("a new fixture escaped the list guard: %v", misses)
		}
	})
	t.Run("new fixture naming no path", func(t *testing.T) {
		withNew := map[string]string{"New fixture": "make check"}
		for k, v := range runs {
			withNew[k] = v
		}
		if _, err := supplyChainInputs(repo, withNew); err == nil {
			t.Fatal("a fixture that names no input was accepted")
		}
	})
}

// The selector runs in the PR's merge commit, whose first parent is the base.
func TestSupplyChainSelectionScript(t *testing.T) {
	script, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts/ci/select-supply-chain-fixtures.sh"))
	if err != nil {
		t.Fatal(err)
	}
	job := func(step string) string {
		return "jobs:\n  other:\n    steps:\n      - run: other\n  supply-chain-checks:\n    steps:\n      - run: " + step + "\n  later:\n    steps:\n      - run: later\n"
	}
	const workflow = ".github/workflows/ci.yml"
	base := map[string]string{
		supplyChainInputList: "# comment\nhooks/pre-push\nfixtures/\n",
		workflow:             job("./fixtures/a.sh"),
		"hooks/pre-push":     "1", "fixtures/a.sh": "1", "unrelated.txt": "1",
	}
	run := func(t *testing.T, files, change map[string]string) ([]byte, error) {
		t.Helper()
		dir := t.TempDir()
		repoGit := func(args ...string) {
			full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
			if out, err := exec.Command("git", full...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
		write := func(files map[string]string) {
			for name, content := range files {
				full := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		}
		repoGit("init", "-q")
		write(files)
		repoGit("add", "-A")
		repoGit("commit", "-q", "-m", "base")
		write(change)
		repoGit("add", "-A")
		repoGit("commit", "-q", "--allow-empty", "-m", "change")
		cmd := exec.Command("sh", "-s")
		cmd.Dir = dir
		cmd.Stdin = strings.NewReader(string(script))
		return cmd.CombinedOutput()
	}
	for name, tc := range map[string]struct {
		change map[string]string
		want   string
	}{
		"listed file":                 {map[string]string{"hooks/pre-push": "2"}, "run=true"},
		"file under listed directory": {map[string]string{"fixtures/deep/new.sh": "2"}, "run=true"},
		"unlisted file":               {map[string]string{"unrelated.txt": "2"}, "run=false"},
		"another job changed":         {map[string]string{workflow: strings.Replace(job("./fixtures/a.sh"), "run: other", "run: changed", 1)}, "run=false"},
		"job step changed":            {map[string]string{workflow: job("./fixtures/b.sh")}, "run=true"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := run(t, base, tc.change)
			if err != nil {
				t.Fatalf("selector failed: %v\n%s", err, out)
			}
			if got := strings.TrimSpace(string(out)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("empty list fails closed", func(t *testing.T) {
		files := map[string]string{supplyChainInputList: "\n", workflow: job("x")}
		if out, err := run(t, files, nil); err == nil {
			t.Fatalf("an empty input list selected %q instead of failing", out)
		}
	})
}
