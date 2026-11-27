package ci_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// bash before 4.1 (macOS /bin/bash 3.2) ignores errexit for a failed bare [[ ]] or
// (( )) statement, so a guard written as `[[ $x =~ re ]]` under `set -e` lets the
// script carry on: it fails open. Every such statement therefore carries an explicit
// failure branch (`|| exit 1`, `|| return 1`), which every bash version honours.

// minBashMajor and minBashMinor name the oldest bash the Go harness runs the guards with.
const (
	minBashMajor = 4
	minBashMinor = 1
)

// checkBash reports why the bash at path cannot run the workflow-shell guards
// faithfully. No CI job runs this package on macOS (ci.yml's scripts/ci steps run on
// ubuntu-latest), so the prerequisite only affects local contributors.
func checkBash(ctx context.Context, path string) error {
	out, err := exec.CommandContext(ctx, path, "-c", `printf '%s.%s' "${BASH_VERSINFO[0]}" "${BASH_VERSINFO[1]}"`).Output()
	if err != nil {
		return fmt.Errorf("cannot run %s: %w", path, err)
	}
	if !bashAtLeast(string(out), minBashMajor, minBashMinor) {
		return fmt.Errorf("%s is bash %s: the scripts/ci tests need bash >= %d.%d (older bash ignores errexit for a failed [[ ]] or (( )), so the guards under test fail open there); put a newer bash first on PATH, for example Homebrew bash on macOS",
			path, out, minBashMajor, minBashMinor)
	}
	return nil
}

func bashAtLeast(version string, major, minor int) bool {
	majorText, minorText, ok := strings.Cut(version, ".")
	if !ok {
		return false
	}
	gotMajor, err := strconv.Atoi(majorText)
	if err != nil {
		return false
	}
	gotMinor, err := strconv.Atoi(minorText)
	if err != nil {
		return false
	}
	return gotMajor > major || (gotMajor == major && gotMinor >= minor)
}

func TestMain(m *testing.M) {
	if err := checkBash(context.Background(), "bash"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func TestCheckBashRefusesOldBash(t *testing.T) {
	fake := func(version string) string {
		path := filepath.Join(t.TempDir(), "bash")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf "+version+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, old := range []string{"3.2", "2.5", "4.0"} {
		if err := checkBash(t.Context(), fake(old)); err == nil || !strings.Contains(err.Error(), "bash >= 4.1") {
			t.Fatalf("bash %s: got %v, want refusal naming bash >= 4.1", old, err)
		}
	}
	for _, ok := range []string{"4.1", "4.4", "5.0", "5.3"} {
		if err := checkBash(t.Context(), fake(ok)); err != nil {
			t.Fatalf("bash %s: %v", ok, err)
		}
	}
	if err := checkBash(t.Context(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing bash must be refused")
	}
}

var (
	heredocStart = regexp.MustCompile(`(?:^|[^<])<<-?\s*['"]?([A-Za-z_]\w*)['"]?`)
	loopOrThen   = regexp.MustCompile(`^;\s*(then|do)\b`)
	bashShebang  = regexp.MustCompile(`^#!\s*(?:/usr/bin/env\s+bash|/(?:usr/)?bin/bash)(?:\s|$)`)
)

// testEnd returns the index just past the [[ ]] or (( )) construct that starts at 0.
func testEnd(stmt string) int {
	var quote byte
	depth := 0
	for i := 0; i < len(stmt); i++ {
		c := stmt[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '\\':
			i++
		case strings.HasPrefix(stmt, "[["):
			if c == ']' && i+1 < len(stmt) && stmt[i+1] == ']' && i > 0 && (stmt[i-1] == ' ' || stmt[i-1] == '\t') {
				return i + 2
			}
		case c == '(':
			depth++
		case c == ')':
			if depth--; depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// bareTests returns the 1-based lines of [[ ]] and (( )) commands that rely on errexit
// alone: a test in any command position (line start, or after ; & && || | ( ) { then do
// else ! and a case-arm )) that is not followed by && or ||, and not an if/while/until
// condition. A test that ENDS an and-or list (`a && [[ x ]]`) is flagged too. Heredoc
// bodies are skipped, except one that is itself a bash script.
func bareTests(script string) []int {
	var found []int
	n := len(script)
	atStart, condOpen, sub := true, false, 0
	var pending []string // heredoc terminators waiting for the next newline
	line := func(idx int) int { return 1 + strings.Count(script[:idx], "\n") }
	isSpace := func(c byte) bool { return c == ' ' || c == '\t' }
	for i := 0; i < n; {
		c := script[i]
		switch {
		case c == '\\':
			if i+1 < n && script[i+1] != '\n' {
				atStart = false
			}
			i += 2
		case c == '\n':
			i++
			atStart = true
			for _, tag := range pending {
				start := i
				for i < n {
					end := strings.IndexByte(script[i:], '\n')
					if end < 0 {
						end = n - i
					}
					text := script[i : i+end]
					i += end
					if i < n {
						i++
					}
					if strings.TrimSpace(text) == tag {
						break
					}
				}
				if start < n && bashShebang.MatchString(script[start:min(n, start+80)]) {
					for _, l := range bareTests(script[start:i]) {
						found = append(found, line(start)-1+l)
					}
				}
			}
			pending = nil
		case isSpace(c):
			i++
		case c == '#' && (i == 0 || strings.IndexByte(" \t\n;&|(", script[i-1]) >= 0):
			for i < n && script[i] != '\n' {
				i++
			}
		case c == '\'':
			end := strings.IndexByte(script[i+1:], '\'')
			i = map[bool]int{true: n, false: i + 1 + end + 1}[end < 0]
			atStart = false
		case c == '"':
			i++
			for i < n && script[i] != '"' {
				if script[i] == '\\' {
					i++
				}
				i++
			}
			i++
			atStart = false
		case c == '`':
			end := strings.IndexByte(script[i+1:], '`')
			i = map[bool]int{true: n, false: i + 1 + end + 1}[end < 0]
			atStart = false
		case c == '$' && i+1 < n && (script[i+1] == '(' || script[i+1] == '{'):
			i = skipGroup(script, i+1)
			atStart = false
		case c == ';':
			i++
			if i < n && script[i] == ';' {
				i++
			}
			atStart = true
		case c == '&' && (i > 0 && (script[i-1] == '>' || script[i-1] == '<') || i+1 < n && script[i+1] == '>'):
			i++ // a redirection such as 2>&1 or &>file
		case c == '&' || c == '|':
			i++
			if i < n && (script[i] == c || script[i] == '&') {
				i++
			}
			atStart = true
		case c == '(' && (!atStart || !strings.HasPrefix(script[i:], "((")):
			switch {
			case i > 0 && (script[i-1] == '<' || script[i-1] == '>'):
				i = skipGroup(script, i)
				atStart = false
			case atStart:
				sub++
				i++
			default:
				j := i + 1
				for j < n && isSpace(script[j]) {
					j++
				}
				if j < n && script[j] == ')' { // f() { ...; }
					i, atStart = j+1, true
				} else {
					i = skipGroup(script, i)
				}
			}
		case c == ')':
			if sub > 0 {
				sub--
				atStart = false
			} else {
				atStart = true // a case arm ends here
			}
			i++
		case c == '<' && strings.HasPrefix(script[i:], "<<") && !strings.HasPrefix(script[i:], "<<<"):
			if match := heredocStart.FindStringSubmatch(script[i:min(n, i+80)]); match != nil {
				pending = append(pending, match[1])
				i += strings.Index(script[i:], match[1]) + len(match[1])
				if i < n && (script[i] == '\'' || script[i] == '"') {
					i++ // the closing quote of <<'TAG'
				}
			} else {
				i += 2
			}
		default:
			if atStart && (strings.HasPrefix(script[i:], "((") ||
				strings.HasPrefix(script[i:], "[[") && i+2 < n && strings.IndexByte(" \t\n", script[i+2]) >= 0) {
				end := testEnd(script[i:])
				if end < 0 {
					found = append(found, line(i))
					i = n
					break
				}
				j := i + end
				for j < n && (isSpace(script[j]) || strings.HasPrefix(script[j:], "\\\n")) {
					j += map[bool]int{true: 2, false: 1}[script[j] == '\\']
				}
				bare := false
				switch {
				case j >= n || strings.IndexByte("\n})#", script[j]) >= 0:
					bare = true
				case script[j] == '&':
					bare = !strings.HasPrefix(script[j:], "&&")
				case script[j] == ';':
					bare = !loopOrThen.MatchString(script[j:min(n, j+20)])
				}
				if bare && !condOpen {
					found = append(found, line(i))
				}
				i, atStart = j, false
				break
			}
			start := i
			for i < n && strings.IndexByte(" \t\n;&|()<>'\"`$\\#", script[i]) < 0 {
				i++
			}
			if i == start {
				i++
				atStart = false
				break
			}
			if atStart {
				switch script[start:i] {
				case "then", "do":
					condOpen = false
				case "if", "elif", "while", "until":
					condOpen = true
				case "else", "{", "!":
				default:
					atStart = false
				}
			}
		}
	}
	return found
}

// skipGroup returns the index just past the (...) or {...} group opening at i.
func skipGroup(s string, i int) int {
	open := s[i]
	closer := byte(')')
	if open == '{' {
		closer = '}'
	}
	depth := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '\'':
			if end := strings.IndexByte(s[i+1:], '\''); end >= 0 {
				i += end + 1
			}
		case open:
			depth++
		case closer:
			if depth--; depth == 0 {
				return i + 1
			}
		}
	}
	return len(s)
}

func TestBareTestScannerClassifiesStatements(t *testing.T) {
	flagged := map[string]string{
		"top level test":            "set -e\n[[ \"$x\" =~ ^[0-9]+$ ]]\n",
		"top level arith":           "(( n >= 21 ))\n",
		"negated test":              "! [[ -e f ]]\n",
		"inside a loop":             "for a in b; do\n  [[ -n $a ]]\ndone\n",
		"inside then body":          "if true; then\n  [[ -n $a ]]\nfi\n",
		"inside else body":          "if true; then :; else\n  [[ -n $a ]]\nfi\n",
		"last in a function":        "f() {\n  ((a == 1 || b == 2))\n}\n",
		"test with comment":         "[[ -n $a ]] # why\n",
		"semicolon":                 "[[ -n $a ]];\n",
		"quoted bracket":            "[[ \"$a ]] b\" == x ]]\n",
		"nested bash heredoc":       "cat >f <<'EOF'\n#!/usr/bin/env bash\n[[ -n $a ]]\nEOF\n",
		"continued test":            "[[ -n $a &&\n  -n $b ]]\n",
		"after semicolon":           "x=1; [[ -n $x ]]\n",
		"brace group":               "{ [[ -n $a ]]; }\n",
		"subshell":                  "( [[ -n $a ]] )\n",
		"inline then":               "if c; then [[ -n $a ]]; fi\n",
		"inline else":               "if c; then :; else [[ -n $a ]]; fi\n",
		"inline then and else":      "if [[ -z $f ]]; then [[ $r == 0 ]]; else [[ $r != 0 ]]; fi\n",
		"inline do":                 "for a in b; do [[ -n $a ]]; done\n",
		"inline do arith":           "while c; do ((n > 0)); done\n",
		"case arm":                  "case $x in a) [[ -n $x ]] ;; esac\n",
		"case arm last":             "case $x in\n  a) [[ -n $x ]]\n  ;;\nesac\n",
		"function one-liner":        "f() { [[ -n $1 ]]; }\n",
		"command substitution then": "if c; then x=1; [[ -n $x ]]; fi\n",
		"and tail":                  "a &&\n  [[ -n $b ]]\n",
		"and tail inline":           "true && [[ a == b ]]\n",
		"or tail":                   "false || [[ a == b ]]\n",
		"test and test":             "[[ a == a ]] && [[ a == b ]]\n",
		"arith and tail":            "true && (( 0 ))\n",
		"tab after bracket":         "[[\t-n $a ]]\n",
		"comment ending in or":      "# explain ||\n[[ -n $a ]]\n",
		"comment ending in and":     "# explain &&\n[[ -n $a ]]\n",
		"comment ending in pipe":    "# explain |\n((a))\n",
	}
	for name, script := range flagged {
		if len(bareTests(script)) == 0 {
			t.Errorf("%s: not flagged:\n%s", name, script)
		}
	}
	allowed := map[string]string{
		"or exit":            "[[ -n $a ]] || exit 1\n",
		"or return":          "((a == 1)) || return 1\n",
		"or brace":           "[[ $a =~ x ]] || { echo no >&2; exit 1; }\n",
		"and chain":          "[[ $a == b ]] && x=1\n",
		"or inside the test": "[[ $a == b || $c == d ]] || exit 1\n",
		"or in a regex":      "[[ $a =~ ^(x\\|y)$ ]] || exit 1\n",
		"if":                 "if [[ -n $a ]]; then :; fi\n",
		"elif":               "if true; then :; elif ((a)); then :; fi\n",
		"while":              "while ((n > 0)); do n=$((n - 1)); done\n",
		"until":              "until [[ -n $a ]]; do :; done\n",
		"bare if keyword":    "if\n  [[ -n $a ]]\nthen :; fi\n",
		"and tail then":      "if a && [[ -n $b ]]; then :; fi\n",
		"or tail then":       "while a || ((n)); do :; done\n",
		"tail then or":       "a && [[ -n $b ]] || exit 1\n",
		"inline then or":     "if c; then [[ -n $a ]] || exit 1; else [[ -n $b ]] || return 1; fi\n",
		"case arm or":        "case $x in a) [[ -n $x ]] || exit 1 ;; esac\n",
		"redirect ampersand": "cmd 2>&1 || exit 1\n[[ -n $a ]] || exit 1\n",
		"array and subst":    "a=(1 2); b=$([[ -n $a ]] && echo y); c=<(cat) ; [[ -n $c ]] || exit 1\n",
		"function header":    "f() {\n  [[ -n $1 ]] || return 1\n}\n", "wrapped or": "[[ -n $a ]] \\\n  || exit 1\n",
		"multi-line test":   "[[ -n $a &&\n  -n $b ]] ||\n  fail x\n",
		"test in condition": "if ! x ||\n  [[ -n $a ]]; then :; fi\n",
		"plain heredoc":     "cat <<EOF\n[[ -n $a ]]\nEOF\n",
		"sh heredoc":        "cat >f <<'EOF'\n#!/bin/sh\n[[ -n $a ]]\nEOF\n",
		"here string":       "read -r a <<<\"$b\"\n[[ -n $a ]] || exit 1\n",
		"arith expansion":   "x=$((a + 1))\n",
		"command":           "test -n \"$a\"\n",
		"comment":           "# [[ -n $a ]]\n",
		"subshell":          "(cd x && ls)\n",
	}
	for name, script := range allowed {
		if lines := bareTests(script); len(lines) != 0 {
			t.Errorf("%s: flagged lines %v:\n%s", name, lines, script)
		}
	}
	if got := bareTests("a\n\n[[ -n $a ]]\n"); !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("line numbers: got %v, want [3]", got)
	}
}

type failClosedStep struct {
	Name  string `yaml:"name"`
	Shell string `yaml:"shell"`
	Run   string `yaml:"run"`
}

type failClosedDefaults struct {
	Run struct {
		Shell string `yaml:"shell"`
	} `yaml:"run"`
}

type failClosedWorkflow struct {
	Defaults failClosedDefaults `yaml:"defaults"`
	Jobs     map[string]struct {
		RunsOn   string             `yaml:"runs-on"`
		Defaults failClosedDefaults `yaml:"defaults"`
		Steps    []failClosedStep   `yaml:"steps"`
	} `yaml:"jobs"`
}

// isBashShell reports whether a step runs under bash: an unset shell is bash on the
// Linux and macOS runners.
func isBashShell(shell, runsOn string) bool {
	if shell == "" {
		// GitHub's default on a Windows runner is pwsh. A runs-on we cannot resolve
		// (an expression, a matrix or a list) is treated as bash, which fails safe.
		return !strings.HasPrefix(strings.ToLower(runsOn), "windows")
	}
	return shell == "bash" || strings.HasPrefix(shell, "bash ")
}

// Every statement-level [[ ]] or (( )) in a bash workflow step or bash script needs an
// explicit failure branch: bash before 4.1 ignores errexit for it and fails open.
func TestNoBareBashTestRelyingOnErrexit(t *testing.T) {
	root := repositoryRoot(t)
	workflows, err := filepath.Glob(filepath.Join(root, ".github/workflows/*.yml"))
	if err != nil || len(workflows) == 0 {
		t.Fatalf("workflows: %v", err)
	}
	for _, path := range workflows {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var workflow failClosedWorkflow
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		reported := map[string]bool{} // a YAML anchor repeats one step in many jobs
		for name, job := range workflow.Jobs {
			for _, step := range job.Steps {
				shell := firstNonEmpty(step.Shell, job.Defaults.Run.Shell, workflow.Defaults.Run.Shell)
				if step.Run == "" || !isBashShell(shell, job.RunsOn) {
					continue
				}
				for _, line := range bareTests(step.Run) {
					if key := fmt.Sprint(step.Name, step.Run, line); reported[key] {
						continue
					} else {
						reported[key] = true
					}
					t.Errorf("%s job %s step %q, line %d of its script: bare test relies on errexit; append `|| exit 1`", filepath.Base(path), name, step.Name, line)
				}
			}
		}
	}
	scripts := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules", "graphify-out":
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.HasPrefix(raw, []byte("#!")) {
			return err
		}
		firstLine, _, _ := strings.Cut(string(raw), "\n")
		if !bashShebang.MatchString(firstLine) {
			return nil
		}
		scripts++
		for _, line := range bareTests(string(raw)) {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s:%d: bare test relies on errexit; append `|| exit 1` (or `|| return 1` in a function)", rel, line)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scripts < 20 {
		t.Fatalf("found only %d bash scripts; the scan is not seeing the repository", scripts)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// oldBash returns /bin/bash when it is older than 4.1 (macOS ships 3.2), else "".
func oldBash(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "/bin/bash", "-c", `printf '%s.%s' "${BASH_VERSINFO[0]}" "${BASH_VERSINFO[1]}"`).Output()
	if err != nil || bashAtLeast(string(out), minBashMajor, minBashMinor) {
		return ""
	}
	return "/bin/bash"
}

// stepScript returns the run script of a workflow job's step.
func stepScript(t *testing.T, workflowFile, job string, step int) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", workflowFile))
	if err != nil {
		t.Fatal(err)
	}
	var workflow failClosedWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow.Jobs[job].Steps[step].Run
}

// through keeps the script up to and including the first line that contains marker.
func through(t *testing.T, script, marker string) string {
	t.Helper()
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		if strings.Contains(line, marker) {
			return strings.Join(lines[:i+1], "\n")
		}
	}
	t.Fatalf("marker %q not found in script", marker)
	return ""
}

// The guard statements that failed open on bash 3.2 must refuse there. Each case runs
// the real step or script, cut right after the guard under test, with a good
// environment (must exit 0, so the guard is the only thing that can stop it) and with
// a failing one (must exit non-zero). Skips only where no bash older than 4.1 exists;
// TestNoBareBashTestRelyingOnErrexit holds the line everywhere.
func TestGuardsFailClosedOnOldBash(t *testing.T) {
	bash := oldBash(t)
	if bash == "" {
		t.Skip("/bin/bash is not older than 4.1 (macOS ships 3.2): no old bash to run the guards under")
	}
	root := repositoryRoot(t)
	sha := strings.Repeat("a", 40)
	publisher, err := os.ReadFile(filepath.Join(root, "scripts/ci/build-queue-publisher.sh"))
	if err != nil {
		t.Fatal(err)
	}
	validate := stepScript(t, "ci.yml", "changes", 0)
	validateEnv := map[string]string{
		"GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10",
		"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/tags/v1.2.3", "GITHUB_SHA": sha, "SOURCE_REVISION": sha,
		"DEVELOPMENT": "false", "DEFAULT_BRANCH": "main", "TAG_DELETED": "false", "MERGE_GROUP_SHA": "",
	}
	floor := stepScript(t, "floor-bench.yml", "measure", 0)
	floorEnv := map[string]string{
		"GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10",
		"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/tags/v1.2.3", "GITHUB_SHA": sha, "SOURCE_REVISION": "",
		"DEVELOPMENT": "false", "DEFAULT_BRANCH": "main", "TAG_DELETED": "false", "MERGE_GROUP_SHA": "",
	}
	release := stepScript(t, "security-release.yml", "resolve", 0)
	releaseEnv := map[string]string{
		"GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "DEFAULT_BRANCH": "main",
		"GITHUB_EVENT_NAME": "workflow_run", "NIGHTLY_PATH": ".github/workflows/nightly.yml", "NIGHTLY_BRANCH": "main",
		"NIGHTLY_REPOSITORY": "Hikyo-Org/Hikyo", "NIGHTLY_EVENT": "schedule", "NIGHTLY_RUN_NUMBER": "42", "NIGHTLY_SHA": sha,
	}
	worker := stepScript(t, "ci-queue-worker.yml", "publish", 0)
	workerEnv := map[string]string{"HEAD": sha, "GITHUB_SHA": sha}
	cases := []struct {
		name, script, marker, suffix string
		args                         []string
		goMod                        string
		env                          map[string]string
		bad                          map[string]map[string]string
		badArgs                      []string
		badGoMod                     string
	}{
		{name: "validate the immutable caller revision", script: validate, marker: `!= *-nightly.*`, suffix: "\nfi\n;;\n*) ;;\nesac", env: validateEnv, bad: map[string]map[string]string{
			"non-numeric repository id": {"EVENT_REPOSITORY_ID": "abc"},
			"dev ref is not a tag":      {"GITHUB_REF": "refs/heads/dev"},
			"nightly tag":               {"GITHUB_REF": "refs/tags/v1.2.3-nightly.20261014.1.gaaaaaaaa"},
		}},
		{name: "floor-bench source guard", script: floor, marker: `"$EVENT_REPOSITORY_ID" =~`, env: floorEnv, bad: map[string]map[string]string{
			"push without a tag":        {"GITHUB_REF": "refs/tags/"},
			"push at a non-hex sha":     {"GITHUB_SHA": "zzz"},
			"non-numeric repository id": {"EVENT_REPOSITORY_ID": "0"},
		}},
		{name: "queue worker head binding", script: worker, marker: `[[ "$JOB_HEAD"`, env: workerEnv, bad: map[string]map[string]string{
			"head is not a sha":     {"HEAD": "main"},
			"job head is not a sha": {"GITHUB_SHA": "123"},
		}},
		{name: "security release nightly resolver", script: release, marker: `[[ "$NIGHTLY_SHA"`, suffix: "\n;;\nesac", env: releaseEnv, bad: map[string]map[string]string{
			"unexpected nightly event": {"NIGHTLY_EVENT": "push"},
			"zero run number":          {"NIGHTLY_RUN_NUMBER": "0"},
			"short nightly sha":        {"NIGHTLY_SHA": "abc"},
		}},
		{name: "publisher builder arguments", script: string(publisher), marker: `[[ $# == 0 ]]`, env: map[string]string{}, badArgs: []string{"unexpected"}},
		{name: "publisher builder relative tool cache", script: string(publisher), marker: `[[ "$RUNNER_TOOL_CACHE" == /* ]]`,
			env: map[string]string{"RUNNER_TEMP": "/tmp", "RUNNER_TOOL_CACHE": "/opt/cache"}, bad: map[string]map[string]string{"relative tool cache": {"RUNNER_TOOL_CACHE": "cache"}}},
		{name: "publisher builder go pin", script: string(publisher), marker: `(( 10#${BASH_REMATCH[1]} >= 21 ))`,
			env: map[string]string{"RUNNER_TEMP": "/tmp", "RUNNER_TOOL_CACHE": "/opt/cache"}, goMod: "go 1.27.2\n", badGoMod: "go 1.20.0\n"},
	}
	run := func(t *testing.T, script string, args []string, goMod string, env map[string]string) error {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), bash, append([]string{"-e", "-o", "pipefail", "-c", script, "guard"}, args...)...)
		cmd.Dir = dir
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
		for key, value := range env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			err = fmt.Errorf("%w: %s", err, out)
		}
		return err
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The trailing `:` keeps a bare guard from setting the exit status as the last command.
			script := through(t, tc.script, tc.marker) + tc.suffix + "\n:"
			goMod := tc.goMod
			if goMod == "" {
				goMod = "go 1.27.2\n"
			}
			if err := run(t, script, nil, goMod, tc.env); err != nil {
				t.Fatalf("control run with a good environment must pass: %v", err)
			}
			assertRefused := func(err error, what string) {
				t.Helper()
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() == 0 {
					t.Errorf("%s was accepted under %s (err=%v): the guard fails open", what, bash, err)
				}
			}
			for what, change := range tc.bad {
				env := map[string]string{}
				for key, value := range tc.env {
					env[key] = value
				}
				for key, value := range change {
					env[key] = value
				}
				assertRefused(run(t, script, nil, goMod, env), what)
			}
			if tc.badArgs != nil {
				assertRefused(run(t, script, tc.badArgs, goMod, tc.env), "an unexpected argument")
			}
			if tc.badGoMod != "" {
				assertRefused(run(t, script, tc.args, tc.badGoMod, tc.env), "a go.mod below go 1.21")
			}
		})
	}
}

func TestIsBashShellTreatsWindowsDefaultAsPowerShell(t *testing.T) {
	for _, tc := range []struct {
		shell, runsOn string
		want          bool
	}{
		{"", "ubuntu-latest", true},
		{"", "macos-15", true},
		{"", "windows-latest", false},
		{"", "${{ matrix.os }}", true}, // unresolvable: scanned as bash, which fails safe
		{"bash", "windows-latest", true},
		{"bash -e {0}", "windows-latest", true},
		{"pwsh", "ubuntu-latest", false},
		{"sh", "", false},
	} {
		if got := isBashShell(tc.shell, tc.runsOn); got != tc.want {
			t.Errorf("isBashShell(%q, %q) = %v, want %v", tc.shell, tc.runsOn, got, tc.want)
		}
	}
}
