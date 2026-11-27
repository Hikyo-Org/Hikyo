package ci_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"mvdan.cc/sh/v3/syntax"
)

var (
	bashShebang = regexp.MustCompile(`^#!\s*(?:/usr/bin/env\s+bash|/(?:usr/)?bin/bash)(?:\s|$)`)
	// Workflow expressions are replaced before parsing; they are not shell syntax.
	workflowExpression = regexp.MustCompile(`\$\{\{[^}]*\}\}`)
)

// bareTests parses script as bash and returns the 1-based lines of [[ ]] and (( ))
// commands that rely on errexit alone: bash 4.1 and later exit when one fails, older
// bash ignores the failure. A test is safe where errexit is off on every bash version:
// an if/while/until condition, a `!` pipeline, a non-final member of an &&/|| list, and
// anything inside a group, subshell or branch that is itself in such a position (for
// example `{ ...; } || die`). The final member of an &&/|| list is not safe. Function
// bodies are treated as active, which is conservative: a function that is only ever
// called as `f || die` is flagged too. A parse error is returned, never skipped.
func bareTests(script string) ([]int, error) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(script), "")
	if err != nil {
		return nil, err
	}
	var found []int
	var stmts func([]*syntax.Stmt, bool)
	var stmt func(*syntax.Stmt, bool)
	stmts = func(list []*syntax.Stmt, inert bool) {
		for _, s := range list {
			stmt(s, inert)
		}
	}
	stmt = func(s *syntax.Stmt, inert bool) {
		inert = inert || s.Negated || s.Background
		switch c := s.Cmd.(type) {
		case *syntax.TestClause, *syntax.ArithmCmd:
			if !inert {
				found = append(found, int(s.Pos().Line()))
			}
		case *syntax.BinaryCmd:
			if c.Op == syntax.AndStmt || c.Op == syntax.OrStmt {
				stmt(c.X, true)
			} else {
				stmt(c.X, inert)
			}
			stmt(c.Y, inert)
		case *syntax.IfClause:
			for clause := c; clause != nil; clause = clause.Else { // an else is a clause without a condition
				stmts(clause.Cond, true)
				stmts(clause.Then, inert)
			}
		case *syntax.WhileClause:
			stmts(c.Cond, true)
			stmts(c.Do, inert)
		case *syntax.ForClause:
			stmts(c.Do, inert)
		case *syntax.CaseClause:
			for _, item := range c.Items {
				stmts(item.Stmts, inert)
			}
		case *syntax.Block:
			stmts(c.Stmts, inert)
		case *syntax.Subshell:
			stmts(c.Stmts, inert)
		case *syntax.FuncDecl:
			stmt(c.Body, false)
		case *syntax.TimeClause:
			if c.Stmt != nil {
				stmt(c.Stmt, inert)
			}
		case *syntax.CoprocClause:
			stmt(c.Stmt, inert)
		}
	}
	stmts(file.Stmts, false)
	// A heredoc that is itself a bash script is scanned too. One that does not parse is
	// a deliberately broken fixture (fast-feedback_test.sh writes one): its content is
	// data for another tool, so it is not an error of this script.
	syntax.Walk(file, func(node syntax.Node) bool {
		redirect, ok := node.(*syntax.Redirect)
		if !ok || redirect.Hdoc == nil {
			return true
		}
		body := script[redirect.Hdoc.Pos().Offset():redirect.Hdoc.End().Offset()]
		if !bashShebang.MatchString(body) {
			return true
		}
		lines, _ := bareTests(body)
		for _, line := range lines {
			found = append(found, int(redirect.Hdoc.Pos().Line())-1+line)
		}
		return true
	})
	return found, nil
}

// bareTestCases pin what bareTests flags and allows. Each flagged form fails open on
// bash 3.2 and exits on bash 5; each allowed form behaves the same on both.
var bareTestCases = struct{ flagged, allowed map[string]string }{
	flagged: map[string]string{
		"top level test":            "set -e\n[[ \"$x\" =~ ^[0-9]+$ ]]\n",
		"top level arith":           "(( n >= 21 ))\n",
		"inside a loop":             "for a in b; do\n  [[ -n $a ]]\ndone\n",
		"inside then body":          "if true; then\n  [[ -n $a ]]\nfi\n",
		"inside else body":          "if true; then :; else\n  [[ -n $a ]]\nfi\n",
		"last in a function":        "f() {\n  ((a == 1 || b == 2))\n}\n",
		"function called with or":   "f() { [[ a == b ]]; }\nf || die\n", // conservative: bodies are active
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
		"case arm leading paren":    "case $x in (a) [[ a == b ]] ;; esac\n",
		"function one-liner":        "f() { [[ -n $1 ]]; }\n",
		"and tail":                  "a &&\n  [[ -n $b ]]\n",
		"and tail inline":           "true && [[ a == b ]]\n",
		"or tail":                   "false || [[ a == b ]]\n",
		"test and test":             "[[ a == a ]] && [[ a == b ]]\n",
		"arith and tail":            "true && (( 0 ))\n",
		"tab after bracket":         "[[\t-n $a ]]\n",
		"comment ending in or":      "# explain ||\n[[ -n $a ]]\n",
		"comment ending in and":     "# explain &&\n[[ -n $a ]]\n",
		"comment ending in pipe":    "# explain |\n((a))\n",
		"redirect after test":       "[[ a == b ]] >/dev/null\n",
		"stderr redirect after":     "[[ a == b ]] 2>/dev/null\n",
		"time prefix":               "time [[ a == b ]]\n",
		"after quoted paren":        "x=$(echo \")\")\n[[ a == b ]]\n",
		"after quoted brace":        "x=${y:-\"}\"}\n[[ a == b ]]\n",
		"after ansi-c quote":        "x=$'it\\'s'\n[[ a == b ]]\n",
		"after grep with quotes":    "e=\"$(grep -E 'a\\(\"1\"|\"::1\"' \"$t\" || true)\"\n[[ a == b ]]\n",
		"group closed by plain end": "{ [[ -n $a ]]; other; }\n",
	},
	allowed: map[string]string{
		"or exit":               "[[ -n $a ]] || exit 1\n",
		"or return":             "((a == 1)) || return 1\n",
		"or brace":              "[[ $a =~ x ]] || { echo no >&2; exit 1; }\n",
		"and chain":             "[[ $a == b ]] && x=1\n",
		"or inside the test":    "[[ $a == b || $c == d ]] || exit 1\n",
		"or in a regex":         "[[ $a =~ ^(x\\|y)$ ]] || exit 1\n",
		"if":                    "if [[ -n $a ]]; then :; fi\n",
		"elif":                  "if true; then :; elif ((a)); then :; fi\n",
		"while":                 "while ((n > 0)); do n=$((n - 1)); done\n",
		"until":                 "until [[ -n $a ]]; do :; done\n",
		"bare if keyword":       "if\n  [[ -n $a ]]\nthen :; fi\n",
		"and tail then":         "if a && [[ -n $b ]]; then :; fi\n",
		"or tail then":          "while a || ((n)); do :; done\n",
		"read loop condition":   "while read -r l && [[ -n $l ]]; do :; done\n",
		"tail then or":          "a && [[ -n $b ]] || exit 1\n",
		"inline then or":        "if c; then [[ -n $a ]] || exit 1; else [[ -n $b ]] || return 1; fi\n",
		"case arm or":           "case $x in a) [[ -n $x ]] || exit 1 ;; esac\n",
		"redirect ampersand":    "cmd 2>&1 || exit 1\n[[ -n $a ]] || exit 1\n",
		"array and subst":       "a=(1 2); b=$([[ -n $a ]] && echo y); cat <(echo) >/dev/null; [[ -n $a ]] || exit 1\n",
		"function header":       "f() {\n  [[ -n $1 ]] || return 1\n}\n",
		"wrapped or":            "[[ -n $a ]] \\\n  || exit 1\n",
		"multi-line test":       "[[ -n $a &&\n  -n $b ]] ||\n  fail x\n",
		"test in condition":     "if ! x ||\n  [[ -n $a ]]; then :; fi\n",
		"plain heredoc":         "cat <<EOF\n[[ -n $a ]]\nEOF\n",
		"sh heredoc":            "cat >f <<'EOF'\n#!/bin/sh\n[[ -n $a ]]\nEOF\n",
		"here string":           "read -r a <<<\"$b\"\n[[ -n $a ]] || exit 1\n",
		"arith expansion":       "x=$((a + 1))\n",
		"command":               "test -n \"$a\"\n",
		"comment":               "# [[ -n $a ]]\n",
		"subshell list":         "(cd x && ls)\n",
		"negated test":          "! [[ -e f ]]\n", // errexit ignores a ! pipeline on every bash
		"negated group":         "! { [[ -n $a ]]; }\n",
		"group then or":         "{ [[ -n $a ]]; other; } || die x\n",
		"subshell then or":      "( [[ a == b ]] ) || exit 1\n",
		"group tail then or":    "{ true && [[ a == b ]]; } || exit 1\n",
		"group redirect or":     "{ read -r a && [[ -z $a ]]; } <\"$f\" ||\n  die x\n",
		"group then and":        "{ [[ -n $a ]]; } && x\n",
		"group in condition":    "if { [[ -n $a ]]; }; then :; fi\n",
		"subshell in condition": "if ( [[ -n $a ]] ); then :; fi\n",
		"background":            "[[ -n $a ]] &\n",
		"quoted bracket text":   "echo '[[ -n $a ]]' \"[[ x ]]\"\n",
	},
}

func checkBareTestTable(t *testing.T, scan func(string) ([]int, error)) {
	t.Helper()
	for name, script := range bareTestCases.flagged {
		lines, err := scan(script)
		if err != nil {
			t.Errorf("%s: %v", name, err)
		} else if len(lines) == 0 {
			t.Errorf("%s: not flagged:\n%s", name, script)
		}
	}
	for name, script := range bareTestCases.allowed {
		lines, err := scan(script)
		if err != nil {
			t.Errorf("%s: %v", name, err)
		} else if len(lines) != 0 {
			t.Errorf("%s: flagged lines %v:\n%s", name, lines, script)
		}
	}
	if got, err := scan("a\n\n[[ -n $a ]]\n"); err != nil || !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("line numbers: got %v (%v), want [3]", got, err)
	}
}

func TestBareTestScannerClassifiesStatements(t *testing.T) {
	checkBareTestTable(t, bareTests)
	if _, err := bareTests("if [[ x\n"); err == nil {
		t.Error("a parse error must be returned, not skipped")
	}
}

// scanScript reports every bare test in script and proves the scan has no blind spot:
// a sentinel test appended at the end, and one on the first blank line between
// top-level commands, must both be flagged.
func scanScript(t *testing.T, name, script, hint string) {
	t.Helper()
	lines, err := bareTests(script)
	if err != nil {
		t.Errorf("%s: cannot parse as bash: %v", name, err)
		return
	}
	for _, line := range lines {
		t.Errorf("%s:%d: bare test relies on errexit; %s", name, line, hint)
	}
	const sentinel = "[[ 1 == 2 ]]"
	appended := script + "\n" + sentinel + "\n"
	if got, err := bareTests(appended); err != nil || !containsLine(got, strings.Count(script, "\n")+2) {
		t.Errorf("%s: a test appended at the end is not flagged (%v, %v): the scan has a blind spot", name, got, err)
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(script), "")
	if err != nil {
		return
	}
	text := strings.Split(script, "\n")
	for index, line := range text {
		if strings.TrimSpace(line) != "" {
			continue
		}
		covered := false
		for _, top := range file.Stmts {
			covered = covered || (index+1 >= int(top.Pos().Line()) && index+1 <= int(top.End().Line()))
		}
		if covered {
			continue
		}
		mutated := append(append([]string{}, text[:index]...), append([]string{sentinel}, text[index+1:]...)...)
		if got, err := bareTests(strings.Join(mutated, "\n")); err != nil || !containsLine(got, index+1) {
			t.Errorf("%s: a test inserted at top-level line %d is not flagged (%v, %v): the scan has a blind spot", name, index+1, got, err)
		}
		return
	}
}

func containsLine(lines []int, want int) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
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
		scanned := map[string]bool{} // a YAML anchor repeats one step in many jobs
		for name, job := range workflow.Jobs {
			for _, step := range job.Steps {
				shell := firstNonEmpty(step.Shell, job.Defaults.Run.Shell, workflow.Defaults.Run.Shell)
				if step.Run == "" || !isBashShell(shell, job.RunsOn) || scanned[step.Name+step.Run] {
					continue
				}
				scanned[step.Name+step.Run] = true
				scanScript(t, fmt.Sprintf("%s job %s step %q (line of its script)", filepath.Base(path), name, step.Name),
					workflowExpression.ReplaceAllString(step.Run, "GITHUB_EXPRESSION"), "append `|| exit 1`")
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
		rel, _ := filepath.Rel(root, path)
		scanScript(t, rel, string(raw), "append `|| exit 1` (or `|| return 1` in a function)")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scripts < 20 {
		t.Fatalf("found only %d bash scripts; the scan is not seeing the repository", scripts)
	}
}
