package ci_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"mvdan.cc/sh/v3/syntax"
)

var (
	leadingTabs = regexp.MustCompile(`(?m)^\t+`)
	bashShebang = regexp.MustCompile(`^#!\s*(?:/usr/bin/env\s+bash|/(?:usr/)?bin/bash)(?:\s|$)`)
	// Workflow expressions are replaced before parsing; they are not shell syntax.
	workflowExpression = regexp.MustCompile(`\$\{\{(?:[^}']|'(?:[^']|'')*')*\}\}`) // a quoted '{0}' does not end it
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
		// An async list runs in its own subshell, where errexit still applies to its body.
		inert = inert || s.Negated
		switch c := s.Cmd.(type) {
		case *syntax.TestClause, *syntax.ArithmCmd:
			if !inert && !s.Background {
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
	// A process substitution body is scanned as active. That is conservative: bash 5 ignores
	// errexit in an argument substitution inside a condition, but not in a redirect one.
	syntax.Walk(file, func(node syntax.Node) bool {
		if sub, ok := node.(*syntax.ProcSubst); ok {
			stmts(sub.Stmts, false)
		}
		return true
	})
	// A command substitution does not inherit errexit, so its body is ordinary code unless
	// errexit is back on there: through set -e inside it, or shopt -s inherit_errexit in the
	// script. Then it is scanned as active, which is conservative for statements before set -e.
	inherit := callsWith(file, "shopt", func(args []string) bool { return contains(args, "-s") && contains(args, "inherit_errexit") })
	syntax.Walk(file, func(node syntax.Node) bool {
		if sub, ok := node.(*syntax.CmdSubst); ok && (inherit || callsWith(sub, "set", setsErrexit)) {
			stmts(sub.Stmts, false)
		}
		return true
	})
	// A heredoc that is itself a bash script is scanned too. One that does not parse is an
	// error, except the single deliberately broken fixture fast-feedback_test.sh writes.
	syntax.Walk(file, func(node syntax.Node) bool {
		redirect, ok := node.(*syntax.Redirect)
		if !ok || redirect.Hdoc == nil {
			return true
		}
		body := script[redirect.Hdoc.Pos().Offset():redirect.Hdoc.End().Offset()]
		body = body[:strings.LastIndex(body, "\n")+1] // the slice ends with the closing delimiter
		if redirect.Op == syntax.DashHdoc {
			body = leadingTabs.ReplaceAllString(body, "") // <<- strips leading tabs from every line
		}
		if !bashShebang.MatchString(body) {
			return true
		}
		lines, herr := bareTests(body)
		if herr != nil && body != brokenSourceFixture {
			err = fmt.Errorf("bash heredoc at line %d: %w", redirect.Hdoc.Pos().Line(), herr)
			return false
		}
		for _, line := range lines {
			found = append(found, int(redirect.Hdoc.Pos().Line())-1+line)
		}
		return true
	})
	// A substitution inside an unquoted bash heredoc is seen by both walks; report its line once.
	slices.Sort(found)
	return slices.Compact(found), err
}

// callsWith reports whether node contains a call to name whose literal arguments satisfy match.
func callsWith(node syntax.Node, name string, match func([]string) bool) bool {
	found := false
	syntax.Walk(node, func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return !found
		}
		words := commandWords(call)
		if len(words) == 0 || literal(words[0]) != name {
			return !found
		}
		args := make([]string, 0, len(words)-1)
		for _, word := range words[1:] {
			args = append(args, literal(word))
		}
		found = found || match(args)
		return !found
	})
	return found
}

// commandWords returns call's words from the command it runs: builtin [--] name and
// command [-p] [--] name run the same builtin as name.
func commandWords(call *syntax.CallExpr) []*syntax.Word {
	words := call.Args
	for len(words) > 1 && (literal(words[0]) == "builtin" || literal(words[0]) == "command") {
		words = words[1:]
		for len(words) > 1 && (literal(words[0]) == "-p" || literal(words[0]) == "--") {
			words = words[1:]
		}
	}
	return words
}

// literal returns the value bash gives word when it has no expansion, with quotes and
// backslashes removed (s"et", \set and 'set' are all set), or "" when it may expand.
func literal(word *syntax.Word) string {
	var value strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			value.WriteString(backslashEscape.ReplaceAllString(part.Value, "$1"))
		case *syntax.SglQuoted:
			if part.Dollar {
				return ""
			}
			value.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok || strings.Contains(lit.Value, `\`) {
					return ""
				}
				value.WriteString(lit.Value)
			}
		default:
			return ""
		}
	}
	return value.String()
}

var setOrShopt = regexp.MustCompile(`\b(?:set|shopt)\b`)

var backslashEscape = regexp.MustCompile(`\\(.)`)

// setsErrexit reports whether set arguments turn errexit on: an e in a -group (-e, -oe,
// -euo pipefail) or a -group whose o takes errexit as its name (-o errexit, -uo errexit,
// -oo pipefail errexit). Each o takes the next word unless that word is an option itself
// (-o -e lists the options, then sets -e). Options end at -- , - or the first word that
// is not an option. A word that may expand ("" from literal) may be -e or errexit, so it
// counts as one. Conservative: odd groups such as
// -+e count as -e although bash leaves errexit off.
func setsErrexit(args []string) bool {
	unknown := func(word string) bool { return word == "" }
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case unknown(arg):
			return true
		case arg == "--" || arg == "-" || (arg[0] != '-' && arg[0] != '+'):
			return false
		case arg[0] == '-' && strings.Contains(arg[1:], "e"):
			return true
		}
		for range strings.Count(arg[1:], "o") {
			if i+1 >= len(args) || (!unknown(args[i+1]) && (args[i+1][0] == '-' || args[i+1][0] == '+')) {
				break
			}
			if i++; unknown(args[i]) || (arg[0] == '-' && args[i] == "errexit") {
				return true
			}
		}
	}
	return false
}

// unscannedErrexitForms are the ways to turn errexit on that setsErrexit and the shopt match in
// bareTests do not read, so a script using one could hide a bare test. None is used in the
// repository; scanScript rejects them instead of parsing them. literal() already reads plainly
// quoted or escaped set and shopt words, so the quote and backslash parts of the first two
// patterns are needed only for spellings it leaves unknown ($'...', a backslash inside "...").
// eval is also found by the parser (unscannedErrexitUses), which reads a quoted or escaped
// eval. The match is textual, so it also rejects such text in a comment or string, and it
// cannot see a form built at run time (eval "$cmd", a set or shopt name from a variable) or a
// ${ } or alias spelling. shortcut: rejected, not parsed; teach bareTests the form if a script
// ever needs it.
var unscannedErrexitForms = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{"shopt with a quoted, escaped, expanded, grouped or -o argument", regexp.MustCompile(`\bshopt\b[^\n;|&]*(?:["'\\$]|\s[-+][a-z]{2,}|\s-o\b)`)},
	{"quoted or escaped set or shopt command name", regexp.MustCompile(`(?:\\(?:set|shopt)|"(?:set|shopt)"|'(?:set|shopt)')\s+[-+]`)},
	{"eval of set or shopt", regexp.MustCompile(`(?:^|[\s;|&(\x60])(eval)\b(?:[^;|&\n"']|\\\n|"[^"]*"|'[^']*')*?(?:\b|"[^"]*\b|'[^']*\b)(?:set|shopt)\b`)},
}

// unscannedErrexitUses returns "line: form" for each unscannedErrexitForms match in script,
// and for each eval, however quoted, whose literal arguments name set or shopt.
func unscannedErrexitUses(script string) []string {
	var found []string
	if file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(script), ""); err == nil {
		syntax.Walk(file, func(node syntax.Node) bool {
			if call, ok := node.(*syntax.CallExpr); ok {
				if words := commandWords(call); len(words) > 1 && literal(words[0]) == "eval" {
					for _, word := range words[1:] {
						if setOrShopt.MatchString(literal(word)) {
							found = append(found, fmt.Sprintf("%d: eval of set or shopt", call.Pos().Line()))
							break
						}
					}
				}
			}
			return true
		})
	}
	for _, form := range unscannedErrexitForms {
		if loc := form.pattern.FindStringSubmatchIndex(script); loc != nil {
			start := loc[0]
			if len(loc) > 2 {
				start = loc[2] // a pattern that matches a separator first captures the command
			}
			found = append(found, fmt.Sprintf("%d: %s", strings.Count(script[:start], "\n")+1, form.name))
		}
	}
	slices.Sort(found)
	return slices.Compact(found) // the parser and the eval pattern can both report a line
}

// brokenSourceFixture is the one bash heredoc that must not parse: fast-feedback_test.sh
// writes it so ShellCheck reports a sourced-file parse failure.
const brokenSourceFixture = "#!/usr/bin/env bash\nfixture_message() { printf 'source fixture reached\\n'; }\nfixture_invalid() {\n"

// bareTestCases pin what bareTests flags and allows. Each flagged form fails open on
// bash 3.2 and exits on bash 5; each allowed form behaves the same on both. The scanner
// is conservative in one direction: pipeline members, process-substitution bodies, and the final
// statement of a background or substitution subshell are flagged although bash 3.2 and 5 agree there.
var bareTestCases = struct{ flagged, allowed map[string]string }{
	flagged: map[string]string{
		"top level test":                      "set -e\n[[ \"$x\" =~ ^[0-9]+$ ]]\n",
		"top level arith":                     "(( n >= 21 ))\n",
		"inside a loop":                       "for a in b; do\n  [[ -n $a ]]\ndone\n",
		"inside then body":                    "if true; then\n  [[ -n $a ]]\nfi\n",
		"inside else body":                    "if true; then :; else\n  [[ -n $a ]]\nfi\n",
		"last in a function":                  "f() {\n  ((a == 1 || b == 2))\n}\n",
		"function called with or":             "f() { [[ a == b ]]; }\nf || die\n", // conservative: bodies are active
		"test with comment":                   "[[ -n $a ]] # why\n",
		"semicolon":                           "[[ -n $a ]];\n",
		"quoted bracket":                      "[[ \"$a ]] b\" == x ]]\n",
		"nested bash heredoc":                 "cat >f <<'EOF'\n#!/usr/bin/env bash\n[[ -n $a ]]\nEOF\n",
		"continued test":                      "[[ -n $a &&\n  -n $b ]]\n",
		"after semicolon":                     "x=1; [[ -n $x ]]\n",
		"brace group":                         "{ [[ -n $a ]]; }\n",
		"subshell":                            "( [[ -n $a ]] )\n",
		"inline then":                         "if c; then [[ -n $a ]]; fi\n",
		"inline else":                         "if c; then :; else [[ -n $a ]]; fi\n",
		"inline then and else":                "if [[ -z $f ]]; then [[ $r == 0 ]]; else [[ $r != 0 ]]; fi\n",
		"inline do":                           "for a in b; do [[ -n $a ]]; done\n",
		"inline do arith":                     "while c; do ((n > 0)); done\n",
		"case arm":                            "case $x in a) [[ -n $x ]] ;; esac\n",
		"case arm last":                       "case $x in\n  a) [[ -n $x ]]\n  ;;\nesac\n",
		"case arm leading paren":              "case $x in (a) [[ a == b ]] ;; esac\n",
		"function one-liner":                  "f() { [[ -n $1 ]]; }\n",
		"and tail":                            "a &&\n  [[ -n $b ]]\n",
		"and tail inline":                     "true && [[ a == b ]]\n",
		"or tail":                             "false || [[ a == b ]]\n",
		"test and test":                       "[[ a == a ]] && [[ a == b ]]\n",
		"arith and tail":                      "true && (( 0 ))\n",
		"tab after bracket":                   "[[\t-n $a ]]\n",
		"comment ending in or":                "# explain ||\n[[ -n $a ]]\n",
		"comment ending in and":               "# explain &&\n[[ -n $a ]]\n",
		"comment ending in pipe":              "# explain |\n((a))\n",
		"redirect after test":                 "[[ a == b ]] >/dev/null\n",
		"stderr redirect after":               "[[ a == b ]] 2>/dev/null\n",
		"time prefix":                         "time [[ a == b ]]\n",
		"after quoted paren":                  "x=$(echo \")\")\n[[ a == b ]]\n",
		"after quoted brace":                  "x=${y:-\"}\"}\n[[ a == b ]]\n",
		"after ansi-c quote":                  "x=$'it\\'s'\n[[ a == b ]]\n",
		"after grep with quotes":              "e=\"$(grep -E 'a\\(\"1\"|\"::1\"' \"$t\" || true)\"\n[[ a == b ]]\n",
		"group closed by plain end":           "{ [[ -n $a ]]; other; }\n",
		"background group":                    "{ [[ a == b ]]; echo x; } &\n",
		"background subshell":                 "( [[ a == b ]]; echo x ) &\n",
		"process substitution":                "cat <( [[ a == b ]]; echo x )\n",
		"set -e in a substitution":            "value=$(set -e; [[ a == b ]]; echo accepted)\n",
		"set -eo in a substitution":           "value=$(set -eo pipefail; [[ a == b ]]; echo accepted)\n",
		"set -oe in a substitution":           "value=$(set -oe pipefail; [[ a == b ]]; echo accepted)\n",
		"set -uo errexit in a substitution":   "value=$(set -uo errexit; [[ a == b ]]; echo accepted)\n",
		"quoted set -e in a substitution":     "value=$(set \"-e\"; [[ a == b ]]; echo accepted)\n",
		"quoted errexit in a substitution":    "value=$(set -o \"errexit\"; [[ a == b ]]; echo accepted)\n",
		"expanded option in a substitution":   "value=$(set -uo \"$x\"; [[ a == b ]]; echo accepted)\n",
		"set -o -e in a substitution":         "value=$(set -o -e; [[ a == b ]]; echo accepted)\n",
		"second o names errexit":              "value=$(set -oo pipefail errexit; [[ a == b ]]; echo accepted)\n",
		"ansi-c quoted set option":            "value=$(set $'\\x2de'; [[ a == b ]]; echo accepted)\n",
		"expanded set option in quotes":       "value=$(set \"-$x\"; [[ a == b ]]; echo accepted)\n",
		"expanded set -o name in quotes":      "value=$(set -o \"err$x\"; [[ a == b ]]; echo accepted)\n",
		"partly quoted set in a substitution": "value=$(s\"et\" -e; [[ a == b ]]; echo accepted)\n",
		"escaped set name in a substitution":  "value=$(se\\t -e; [[ a == b ]]; echo accepted)\n",
		"quoted inherit_errexit":              "shopt -s \"inherit_errexit\"\nvalue=$([[ a == b ]]; echo accepted)\n",
		"escaped set -e in a substitution":    "value=$(set \\-e; [[ a == b ]]; echo accepted)\n",
		"escaped errexit in a substitution":   "value=$(set -o err\\exit; [[ a == b ]]; echo accepted)\n",
		"builtin set -e in a substitution":    "value=$(builtin set -e; [[ a == b ]]; echo accepted)\n",
		"set +o expanded in a substitution":   "value=$(set +o \"$x\"; [[ a == b ]]; echo accepted)\n", // x=-e: bash lists the options, then sets -e
		"command -- set -e in a substitution": "value=$(command -- set -e; [[ a == b ]]; echo accepted)\n",
		"command -p set -e in a substitution": "value=$(command -p set -e; [[ a == b ]]; echo accepted)\n",
		"inherited errexit":                   "shopt -s inherit_errexit\nvalue=$([[ a == b ]]; echo accepted)\n",
		"tab-indented bash heredoc":           "cat >f <<-'EOF'\n\t#!/usr/bin/env bash\n\t[[ -n $a ]]\n\tEOF\n",
	},
	allowed: map[string]string{
		"or exit":                      "[[ -n $a ]] || exit 1\n",
		"or return":                    "((a == 1)) || return 1\n",
		"or brace":                     "[[ $a =~ x ]] || { echo no >&2; exit 1; }\n",
		"and chain":                    "[[ $a == b ]] && x=1\n",
		"or inside the test":           "[[ $a == b || $c == d ]] || exit 1\n",
		"or in a regex":                "[[ $a =~ ^(x\\|y)$ ]] || exit 1\n",
		"if":                           "if [[ -n $a ]]; then :; fi\n",
		"elif":                         "if true; then :; elif ((a)); then :; fi\n",
		"while":                        "while ((n > 0)); do n=$((n - 1)); done\n",
		"until":                        "until [[ -n $a ]]; do :; done\n",
		"bare if keyword":              "if\n  [[ -n $a ]]\nthen :; fi\n",
		"and tail then":                "if a && [[ -n $b ]]; then :; fi\n",
		"or tail then":                 "while a || ((n)); do :; done\n",
		"read loop condition":          "while read -r l && [[ -n $l ]]; do :; done\n",
		"tail then or":                 "a && [[ -n $b ]] || exit 1\n",
		"inline then or":               "if c; then [[ -n $a ]] || exit 1; else [[ -n $b ]] || return 1; fi\n",
		"case arm or":                  "case $x in a) [[ -n $x ]] || exit 1 ;; esac\n",
		"redirect ampersand":           "cmd 2>&1 || exit 1\n[[ -n $a ]] || exit 1\n",
		"array and subst":              "a=(1 2); b=$([[ -n $a ]] && echo y); cat <(echo) >/dev/null; [[ -n $a ]] || exit 1\n",
		"function header":              "f() {\n  [[ -n $1 ]] || return 1\n}\n",
		"wrapped or":                   "[[ -n $a ]] \\\n  || exit 1\n",
		"multi-line test":              "[[ -n $a &&\n  -n $b ]] ||\n  fail x\n",
		"test in condition":            "if ! x ||\n  [[ -n $a ]]; then :; fi\n",
		"plain heredoc":                "cat <<EOF\n[[ -n $a ]]\nEOF\n",
		"plain substitution":           "value=$([[ a == b ]]; echo x)\n", // errexit is off in $( ) on every bash
		"set +e substitution":          "value=$(set +e; [[ a == b ]]; echo x)\n",
		"set -- -e substitution":       "value=$(set -- -e; [[ a == b ]]; echo x)\n", // -e is a positional parameter
		"set -o pipefail substitution": "value=$(set -o pipefail; [[ a == b ]]; echo x)\n",
		"command -v set substitution":  "value=$(command -v set -e; [[ a == b ]]; echo x)\n", // only prints how set resolves
		"set - -e substitution":        "value=$(set - -e; [[ a == b ]]; echo x)\n",
		"set x -e substitution":        "value=$(set x -e; [[ a == b ]]; echo x)\n", // options end at the first positional word
		"set +o errexit substitution":  "value=$(set +o errexit; [[ a == b ]]; echo x)\n",
		"set -o emacs substitution":    "value=$(set -o emacs; [[ a == b ]]; echo x)\n",
		"sh heredoc":                   "cat >f <<'EOF'\n#!/bin/sh\n[[ -n $a ]]\nEOF\n",
		"here string":                  "read -r a <<<\"$b\"\n[[ -n $a ]] || exit 1\n",
		"arith expansion":              "x=$((a + 1))\n",
		"command":                      "test -n \"$a\"\n",
		"comment":                      "# [[ -n $a ]]\n",
		"subshell list":                "(cd x && ls)\n",
		"negated test":                 "! [[ -e f ]]\n", // errexit ignores a ! pipeline on every bash
		"negated group":                "! { [[ -n $a ]]; }\n",
		"group then or":                "{ [[ -n $a ]]; other; } || die x\n",
		"subshell then or":             "( [[ a == b ]] ) || exit 1\n",
		"group tail then or":           "{ true && [[ a == b ]]; } || exit 1\n",
		"group redirect or":            "{ read -r a && [[ -z $a ]]; } <\"$f\" ||\n  die x\n",
		"group then and":               "{ [[ -n $a ]]; } && x\n",
		"group in condition":           "if { [[ -n $a ]]; }; then :; fi\n",
		"subshell in condition":        "if ( [[ -n $a ]] ); then :; fi\n",
		"background":                   "[[ -n $a ]] &\n",
		"quoted bracket text":          "echo '[[ -n $a ]]' \"[[ x ]]\"\n",
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
	if got, err := scan("if [[ -z $f ]]\nthen [[ $r == 0 ]]\nelse [[ $r != 0 ]]\nfi\n"); err != nil || !reflect.DeepEqual(got, []int{2, 3}) {
		t.Errorf("both branches, not the condition: got %v (%v), want [2 3]", got, err)
	}
	if got, err := scan("cat >f <<EOF\n#!/usr/bin/env bash\nv=$(set -e; [[ a == b ]]; echo x)\nEOF\n"); err != nil || !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("a substitution in an unquoted bash heredoc: got %v (%v), want [3] once", got, err)
	}
	if got, err := scan("a\n\n[[ -n $a ]]\n"); err != nil || !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("line numbers: got %v (%v), want [3]", got, err)
	}
}

func TestWorkflowExpressionSpansQuotedBraces(t *testing.T) {
	for _, run := range []string{
		`echo "${{ github.ref_name }}"`,
		`echo "${{ format('refs/heads/{0}', github.ref_name) }}"`,
		`echo "${{ format('it''s {0}', 'x') }}"`,
	} {
		if got := workflowExpression.ReplaceAllString(run, "GITHUB_EXPRESSION"); got != `echo "GITHUB_EXPRESSION"` {
			t.Errorf("%s: replaced to %s", run, got)
		}
	}
}

func TestBareTestScannerClassifiesStatements(t *testing.T) {
	checkBareTestTable(t, bareTests)
	if _, err := bareTests("if [[ x\n"); err == nil {
		t.Error("a parse error must be returned, not skipped")
	}
	if _, err := bareTests("cat >f <<'EOF'\n#!/usr/bin/env bash\n[[ a == b ]]\ng() {\nEOF\n"); err == nil {
		t.Error("an unparsable bash heredoc must be returned as an error, not skipped")
	}
	if _, err := bareTests("cat >f <<'EOF'\n" + brokenSourceFixture + "EOF\n"); err != nil {
		t.Errorf("the deliberately broken fast-feedback fixture must stay accepted: %v", err)
	}
}

// scanScript reports every bare test in script and proves the scan has no blind spot:
// a sentinel test appended at the end, and one on the first blank line between
// top-level commands, must both be flagged.
func scanScript(t *testing.T, name, script, hint string) {
	t.Helper()
	for _, problem := range scanProblems(name, script, hint) {
		t.Error(problem)
	}
}

// scanProblems returns every problem scanScript reports for script.
func scanProblems(name, script, hint string) []string {
	var problems []string
	errorf := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	for _, found := range unscannedErrexitUses(script) {
		errorf("%s:%s; bareTests cannot see whether it turns errexit on, so spell it as a plain set or shopt -s inherit_errexit call (the check matches text: reword a comment or string that trips it)", name, found)
	}
	lines, err := bareTests(script)
	if err != nil {
		errorf("%s: cannot parse as bash: %v", name, err)
		return problems
	}
	for _, line := range lines {
		errorf("%s:%d: bare test relies on errexit; %s", name, line, hint)
	}
	const sentinel = "[[ 1 == 2 ]]"
	appended := script + "\n" + sentinel + "\n"
	if got, err := bareTests(appended); err != nil || !containsLine(got, strings.Count(script, "\n")+2) {
		errorf("%s: a test appended at the end is not flagged (%v, %v): the scan has a blind spot", name, got, err)
	}
	file, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(script), "")
	if err != nil {
		return problems
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
			errorf("%s: a test inserted at top-level line %d is not flagged (%v, %v): the scan has a blind spot", name, index+1, got, err)
		}
		return problems
	}
	return problems
}

func containsLine(lines []int, want int) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

// TestUnscannedErrexitFormsAreRejected pins what unscannedErrexitUses rejects and allows.
func TestUnscannedErrexitFormsAreRejected(t *testing.T) {
	matches := unscannedErrexitUses
	for _, script := range []string{
		"shopt -s \"inherit_errexit\"\n", "shopt '-s' inherit_errexit\n", "shopt -sq inherit_errexit\n", "shopt -so errexit\n",
		"shopt -s -o errexit\n", "\"set\" -e\n", "'set' -e\n", "\\set -e\n", "\\shopt -s inherit_errexit\n",
		"eval set -e\n", "eval 'set -e'\n", "eval \"shopt -s inherit_errexit\"\n",
		"shopt -s inherit_err\\exit\n", "shopt -s $opt\n", "\"eval\" set -e\n", "\\eval set -e\n", "'eval' set -e\n", "e\"val\" set -e\n", "x=$(\"eval\" set -e)\n", "eval \\\n  set -e\n", "eval \"\nset -e\"\n", "builtin eval set -e\n",
	} {
		if len(matches(script)) == 0 {
			t.Errorf("not rejected: %q", script)
		}
	}
	for _, script := range []string{
		"shopt -s nullglob\n", "shopt -s extglob inherit_errexit\n", "set -euo pipefail\n", "echo \"set -e is on\"\n",
		"printf 'set -e\\n'\n", "eval \"$(ssh-agent -s)\"\n", "reset -e\n", "x=set; echo $x\n",
		"eval \"$(dircolors)\"; set -e\n", "shopt -s -q inherit_errexit\n", "node --eval 'x.set(1)'\n",
	} {
		if got := matches(script); len(got) != 0 {
			t.Errorf("rejected %q as %v", script, got)
		}
	}
	if got := scanProblems("fixture.sh", "#!/usr/bin/env bash\neval set -e\n", "hint"); len(got) == 0 || !strings.HasPrefix(got[0], "fixture.sh:2: eval of set or shopt") {
		t.Errorf("scanScript must report an unscanned form: got %q", got)
	}
	if got := matches("a\nb\neval set -e\n"); !reflect.DeepEqual(got, []string{"3: eval of set or shopt"}) {
		t.Errorf("line numbers: got %v, want [3: eval of set or shopt]", got)
	}
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
				if step.Run == "" || !isBashShell(shell, string(job.RunsOn)) || scanned[step.Name+step.Run] {
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
