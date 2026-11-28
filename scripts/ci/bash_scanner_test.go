package ci_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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
	bashShebang = regexp.MustCompile(`^#!\s*(?:/usr/bin/env\s+(?:-S\s+)?bash|/(?:usr/)?bin/bash)(?:\s|$)`)
	// An sh script is scanned too: on macOS sh is bash 3.2, the shell the scan guards against.
	scriptShebang = regexp.MustCompile(`^#!\s*(?:/usr/bin/env\s+(?:-S\s+)?(?:ba)?sh|/(?:usr/)?bin/(?:ba)?sh)(?:\s|$)`)
	// A shebang naming bash or sh that the patterns above miss (env -S -i bash, a quoted,
	// versioned or upper-case name, rbash, nix-shell); the scan fails on it rather than skip
	// the file. zsh, dash and the like do not match.
	shellMention = regexp.MustCompile(`(?:^|[^a-zA-Z]|-[a-zA-Z]*S)[rR]?(?:[bB][aA])?[sS][hH](?:[^a-zA-Z]|$)|nix-shell`) // -iSbash: env flags
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
	inertStmts := map[*syntax.Stmt]bool{}
	stmt = func(s *syntax.Stmt, inert bool) {
		// An async list runs in its own subshell, where errexit still applies to its body.
		inert = inert || s.Negated
		inertStmts[s] = inertStmts[s] || inert
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
	// Substitution bodies are scanned as active, which is conservative. bash 5 ignores errexit in
	// a process substitution inside a condition but not in a redirect one. Whether a command
	// substitution inherits errexit (set -e inside it, inherit_errexit, POSIX mode from sh or a
	// shell flag, SHELLOPTS, BASH_ENV, a sourced file) cannot be decided from the text, and where
	// it does not, bash 3.2 and 5 agree.
	syntax.Walk(file, func(node syntax.Node) bool {
		switch sub := node.(type) {
		case *syntax.ProcSubst:
			stmts(sub.Stmts, false)
		case *syntax.CmdSubst:
			stmts(sub.Stmts, false)
		}
		return true
	})
	// Code the script hands to bash is scanned too: a heredoc or here-string that is a bash
	// script or is fed to bash or sh, an eval's words (unless the eval is in a condition, where
	// its code is too), a trap's action, an env -S string and a bash or sh -c string, through
	// builtin, command, exec, env and nohup. One that does not parse is an error, except the
	// single deliberately broken fixture fast-feedback_test.sh writes. So is bash or sh reading
	// its script from stdin (or sourcing /dev/stdin) without its own heredoc, here-string or <
	// file on fd 0: a pipe, an inherited stdin, a group or function fed a heredoc, exec 0<<,
	// < /dev/stdin or < <(..). When an expanded word may be an option (bash "$opt" <<EOF), a
	// heredoc or here-string on fd 0 is scanned for bare tests, but may be data that does not
	// parse. An expansion in the code reads as a placeholder word. shortcut: code built at run time
	// (eval "$cmd", bash <(..)), a shell named by a variable ("$BASH" -c), a wrapper script or
	// command (timeout, sudo, xargs, nice, find -exec) and \x..-style $'...' escapes are not
	// seen; spell handed code literally and call bash directly.
	nested := func(body string, line int, what string, strict bool) {
		lines, herr := bareTests(body)
		if herr != nil && strict && body != brokenSourceFixture {
			err = fmt.Errorf("%s at line %d: %w", what, line, herr)
		}
		for _, n := range lines {
			found = append(found, line-1+n)
		}
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		s, ok := node.(*syntax.Stmt)
		if !ok || err != nil {
			return err == nil
		}
		call, _ := s.Cmd.(*syntax.CallExpr)
		name, code, word, stdin := handedCode(call)
		fedStdin := false
		for _, redirect := range s.Redirs {
			fedTo := ""
			if stdin != noStdin && onStdin(redirect) {
				fedTo = name
				fedStdin = feedsScript(redirect) // the last redirect of fd 0 wins
			}
			if redirect.Op == syntax.WordHdoc && fedTo != "" {
				nested(codeText(redirect.Word), int(redirect.Word.Pos().Line()), name+" here-string", stdin == readsStdin)
				continue
			}
			body, isBash := bashHeredoc(script, redirect, fedTo)
			first, _, _ := strings.Cut(body, "\n")
			if isBash {
				nested(body, int(redirect.Hdoc.Pos().Line()), "bash heredoc", stdin == readsStdin || scriptShebang.MatchString(body))
			} else if strings.HasPrefix(first, "#!") && shebangKind(first) == "unread" {
				err = fmt.Errorf("heredoc at line %d: shebang %q names bash or sh in a form the scan does not read", redirect.Hdoc.Pos().Line(), first)
			}
		}
		if stdin == readsStdin && !fedStdin {
			err = fmt.Errorf("line %d: %s reads a script from stdin that cannot be scanned; feed it a heredoc directly", s.Pos().Line(), name)
		}
		if word != nil && (name != "eval" || !inertStmts[s]) {
			nested(code, int(word.Pos().Line()), name+" string", true)
		}
		return err == nil
	})
	// A substitution inside an unquoted bash heredoc is seen by both walks; report its line once.
	slices.Sort(found)
	return slices.Compact(found), err
}

// bashHeredoc returns the body of redirect's heredoc as bash runs it, when the scan reads it:
// a bash or sh script, or one fed to bash or sh (fedTo). An unquoted delimiter's body has its
// \$, \` and \\ escapes removed, as the shell writing it does.
func bashHeredoc(script string, redirect *syntax.Redirect, fedTo string) (string, bool) {
	if redirect.Hdoc == nil {
		return "", false
	}
	body := script[redirect.Hdoc.Pos().Offset():redirect.Hdoc.End().Offset()]
	body = body[:strings.LastIndex(body, "\n")+1] // the slice ends with the closing delimiter
	if redirect.Op == syntax.DashHdoc {
		body = leadingTabs.ReplaceAllString(body, "") // <<- strips leading tabs from every line
	}
	if word := redirect.Word; len(word.Parts) == 1 && isPlainLit(word.Parts[0]) {
		body = heredocEscape.ReplaceAllString(body, "$1")
	}
	return body, fedTo != "" || scriptShebang.MatchString(body)
}

var heredocEscape = regexp.MustCompile("\\\\([$`\\\\])")

// handedCode reads the command call runs, past builtin, command, exec, env and nohup and their
// options: its name, the code it hands to bash (an eval's words, a trap's action, an env -S
// string, a bash or sh -c string, as codeText reads them) with the word that holds it, and
// whether it reads its script from stdin: bash or sh with -s or no script operand, a script
// operand of /dev/stdin, or a source of /dev/stdin read it; an expanded word in place of the
// operand may. Through env -S, the shell the string runs is returned, with no code.
func handedCode(call *syntax.CallExpr) (name, code string, word *syntax.Word, stdin stdinUse) {
	if call == nil {
		return "", "", nil, noStdin
	}
	words := call.Args
prefixes:
	for len(words) > 1 {
		prefix, _ := plainText(words[0])
		switch prefix[strings.LastIndex(prefix, "/")+1:] {
		case "builtin", "command", "exec", "nohup":
			words = words[1:]
			for len(words) > 1 {
				switch option, _ := plainText(words[0]); {
				case option == "--" || execFlags.MatchString(option):
					words = words[1:]
				case execNamed.MatchString(option) && len(words) > 2:
					words = words[2:] // exec -a NAME
				default:
					continue prefixes
				}
			}
		case "env":
			words = words[1:]
			for len(words) > 0 { // env -S may hold the whole command line
				option := codeText(words[0])
				long, value, hasValue := strings.Cut(option, "=")
				if strings.HasPrefix(long, "--") && len(long) > 2 {
					long = envLongOption(long) // GNU env takes a unique prefix
					if !hasValue {
						option = long
					}
				}
				switch {
				case option == "-" || option == "--" || option == "EXPANSION" || envFlags.MatchString(option) || envLongFlags[long]:
					words = words[1:] // an expanded word is taken for options
				case !strings.HasPrefix(option, "-") && strings.Contains(option, "="):
					words = words[1:] // NAME=VALUE
				case envSplit.MatchString(option) || long == "--split-string":
					// env -S splits its string into a command line, followed by the remaining words.
					split, rest := value, words[1:]
					if at := strings.Index(option, "S"); long != "--split-string" {
						split, hasValue = option[at+1:], at+1 < len(option)
					}
					if !hasValue && len(rest) > 0 {
						split, rest = codeText(rest[0]), rest[1:]
					}
					for _, arg := range rest {
						quoted, _ := syntax.Quote(codeText(arg), syntax.LangBash)
						split += " " + quoted
					}
					// env performs no redirection: a < in the string is a word it passes on, which a
					// shell reading stdin may take as its script file.
					if f, perr := syntax.NewParser().Parse(strings.NewReader(split), ""); perr == nil && len(f.Stmts) == 1 {
						if inner, ok := f.Stmts[0].Cmd.(*syntax.CallExpr); ok {
							if innerName, _, _, innerStdin := handedCode(inner); innerStdin != noStdin {
								if len(f.Stmts[0].Redirs) > 0 && innerStdin == readsStdin {
									innerStdin = mayReadStdin
								}
								return innerName, "", nil, innerStdin // the heredoc env is fed goes to this shell
							}
						}
					}
					return "env", split, words[0], noStdin
				case envValued.MatchString(option) || envLongValued[long] && hasValue:
					words = words[1:] // -uNAME, -CDIR, -aNAME, --unset=NAME, --chdir=DIR, --argv0=NAME
				case (envValueNext.MatchString(option) || envLongValued[option]) && len(words) > 2:
					words = words[2:]
				default:
					continue prefixes
				}
			}
		default:
			break prefixes
		}
	}
	if len(words) == 0 {
		return "", "", nil, noStdin // assignments only
	}
	name, _ = plainText(words[0])
	name = name[strings.LastIndex(name, "/")+1:]
	args := words[1:]
	if len(args) > 0 {
		if first, _ := plainText(args[0]); first == "--" && name != "bash" && name != "sh" {
			args = args[1:]
		}
	}
	switch name {
	case "eval":
		// eval joins its words with spaces. A word that is only an expansion may hold whole
		// commands, so the words around it are read as separate commands.
		var pieces, run []string
		for _, arg := range args {
			if text := codeText(arg); text != "EXPANSION" {
				run = append(run, text)
				continue
			}
			if len(run) > 0 {
				pieces = append(pieces, strings.Join(run, " "))
			}
			pieces, run = append(pieces, "EXPANSION"), nil
		}
		if len(run) > 0 {
			pieces = append(pieces, strings.Join(run, " "))
		}
		if len(args) > 0 {
			return name, strings.Join(pieces, "; "), args[0], noStdin
		}
	case "trap":
		if len(args) > 0 { // a lone signal name or -p scans as a harmless command
			return name, codeText(args[0]), args[0], noStdin
		}
	case "source", ".":
		if len(args) > 0 {
			if target, _ := plainText(args[0]); stdinPaths[target] {
				return name, "", nil, readsStdin
			}
		}
	case "bash", "sh":
		// Options come first; -c takes the first word after them as its code, -o and -O take an
		// option name, --rcfile and --init-file a file, and -s (or no word left) reads stdin.
		dashC, dashS, expanded, operand := false, false, false, len(args)
	options:
		for i := 0; i < len(args); i++ {
			text, ok := plainText(args[i])
			switch {
			case !ok && slices.ContainsFunc(args[i+1:], func(later *syntax.Word) bool {
				text, _ := plainText(later)
				return shellFlags.MatchString(text) && strings.Contains(text, "c")
			}):
				// An expanded word may be an option: a later -c still takes its code.
			case !ok:
				expanded, operand = true, i
				break options
			case text == "--version" || text == "--help":
				return name, "", nil, noStdin
			case text == "--" || text == "-":
				operand = i + 1
				break options
			case text == "--rcfile" || text == "--init-file":
				i++
			case strings.HasPrefix(text, "--"):
			case shellFlags.MatchString(text):
				dashC = dashC || strings.Contains(text, "c")
				dashS = dashS || strings.Contains(text, "s")
				i += strings.Count(text, "o") + strings.Count(text, "O")
			default:
				operand = i
				break options
			}
		}
		target := ""
		if operand < len(args) {
			target, _ = plainText(args[operand])
		}
		switch {
		case dashC && operand < len(args):
			return name, codeText(args[operand]), args[operand], noStdin
		case dashC:
			return name, "", nil, noStdin
		case dashS || operand >= len(args) || stdinPaths[target]:
			return name, "", nil, readsStdin
		case expanded:
			return name, "", nil, mayReadStdin // the expanded word may be an option or a script file
		}
		// Otherwise a script file, and stdin is its data.
	}
	return name, "", nil, noStdin
}

// stdinUse is whether a command reads its script from stdin.
type stdinUse int

const (
	noStdin stdinUse = iota
	mayReadStdin
	readsStdin
)

// envLongOption is the GNU env long option name abbreviates uniquely, or name itself. No
// option name is a prefix of another, so an exact name matches only itself.
func envLongOption(name string) string {
	var match []string
	for _, known := range envLongNames {
		if strings.HasPrefix(known, name) {
			match = append(match, known)
		}
	}
	if len(match) == 1 {
		return match[0]
	}
	return name
}

var envLongNames = []string{"--ignore-environment", "--null", "--debug", "--default-signal", "--ignore-signal", "--block-signal", "--list-signal-handling", "--unset", "--chdir", "--argv0", "--env0-from", "--split-string"}

// onStdin reports whether redirect applies to fd 0.
func onStdin(redirect *syntax.Redirect) bool {
	if redirect.N != nil {
		return redirect.N.Value == "0"
	}
	switch redirect.Op {
	case syntax.RdrIn, syntax.RdrInOut, syntax.DplIn, syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
		return true
	}
	return false
}

// feedsScript reports whether redirect gives fd 0 a script the scan can see: a heredoc, a
// here-string or a file, not stdin itself or a process substitution.
func feedsScript(redirect *syntax.Redirect) bool {
	switch redirect.Op {
	case syntax.Hdoc, syntax.DashHdoc, syntax.WordHdoc:
		return true
	case syntax.RdrIn:
		target, _ := plainText(redirect.Word)
		_, substituted := redirect.Word.Parts[0].(*syntax.ProcSubst)
		return !stdinPaths[target] && !substituted
	}
	return false
}

var (
	execFlags     = regexp.MustCompile(`^-[cpl]+$`)
	execNamed     = regexp.MustCompile(`^-[cl]*a$`)
	envFlags      = regexp.MustCompile(`^-[iv0]+$`)
	envValued     = regexp.MustCompile(`^-[iv0]*[uCPa].+$`)
	envValueNext  = regexp.MustCompile(`^-[iv0]*[uCPa]$`)
	envLongValued = map[string]bool{"--unset": true, "--chdir": true, "--argv0": true, "--env0-from": true}
	envLongFlags  = map[string]bool{"--ignore-environment": true, "--null": true, "--debug": true, "--default-signal": true, "--ignore-signal": true, "--block-signal": true, "--list-signal-handling": true}
	envSplit      = regexp.MustCompile(`^-[iv0]*S`)
	shellFlags    = regexp.MustCompile(`^[-+][a-zA-Z]+$`)
	stdinPaths    = map[string]bool{"/dev/stdin": true, "/dev/fd/0": true, "/proc/self/fd/0": true}
)

// codeText is word's value as code: quotes and backslashes removed, $'...' escapes decoded,
// each expansion read as the word EXPANSION.
func codeText(word *syntax.Word) string {
	var value strings.Builder
	var parts func([]syntax.WordPart, *regexp.Regexp)
	parts = func(list []syntax.WordPart, escape *regexp.Regexp) {
		for _, part := range list {
			switch part := part.(type) {
			case *syntax.Lit:
				value.WriteString(escape.ReplaceAllString(part.Value, "$1"))
			case *syntax.SglQuoted:
				if part.Dollar {
					value.WriteString(ansiEscapes.Replace(part.Value))
				} else {
					value.WriteString(part.Value)
				}
			case *syntax.DblQuoted:
				parts(part.Parts, quotedEscape)
			default:
				value.WriteString("EXPANSION")
			}
		}
	}
	parts(word.Parts, backslashEscape)
	return value.String()
}

// Unquoted, a backslash escapes any character; inside double quotes only $ ` " and \. The
// parser has already removed escaped newlines.
var (
	backslashEscape = regexp.MustCompile(`\\(.)`)
	quotedEscape    = regexp.MustCompile("\\\\([$`\"\\\\])")
)

// ansiEscapes decodes the $'...' escapes that change how code parses; shortcut: \x, \0 and
// \u escapes are left as text.
var ansiEscapes = strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\\`, `\`, `\'`, "'", `\"`, `"`)

// plainText returns word's value when it has no expansion or escape: plain, '...' or "..." text.
func plainText(word *syntax.Word) (string, bool) {
	var value strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			if strings.Contains(part.Value, `\`) {
				return "", false
			}
			value.WriteString(part.Value)
		case *syntax.SglQuoted:
			if part.Dollar {
				return "", false
			}
			value.WriteString(part.Value)
		case *syntax.DblQuoted:
			for _, inner := range part.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok || strings.Contains(lit.Value, `\`) {
					return "", false
				}
				value.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}
	return value.String(), true
}

func isPlainLit(part syntax.WordPart) bool {
	lit, ok := part.(*syntax.Lit)
	return ok && !strings.Contains(lit.Value, `\`)
}

// brokenSourceFixture is the one bash heredoc that must not parse: fast-feedback_test.sh
// writes it so ShellCheck reports a sourced-file parse failure.
const brokenSourceFixture = "#!/usr/bin/env bash\nfixture_message() { printf 'source fixture reached\\n'; }\nfixture_invalid() {\n"

// bareTestCases pin what bareTests flags and allows. Each flagged form fails open on
// bash 3.2 and exits on bash 5; each allowed form behaves the same on both. The scanner
// is conservative in one direction: pipeline members, substitution bodies, handed code (a
// trap action, a bash -c string) and the final statement of a background subshell are flagged
// although bash 3.2 and 5 may agree there.
var bareTestCases = struct{ flagged, allowed map[string]string }{
	flagged: map[string]string{
		"exec -a name":                       "exec -a n bash -c '[[ a == b ]]'\n",
		"exec -l":                            "exec -l bash -c '[[ a == b ]]'\n",
		"bash <<- heredoc":                   "bash <<-'EOF'\n\t[[ a == b ]]\n\tEOF\n",
		"expanded word then heredoc":         "bash \"$opt\" <<'EOF'\n[[ a == b ]]\nEOF\n",
		"expanded word then -s":              "bash \"$opt\" -s <<'EOF'\n[[ a == b ]]\nEOF\n",
		"expanded word, -c, option":          "bash \"$x\" -c -e '[[ a == b ]]'\n",
		"env -S reading stdin":               "env -S 'bash -s' <<'EOF'\n[[ a == b ]]\nEOF\n",
		"env -iu X":                          "env -iu X bash -c '[[ a == b ]]'\n",
		"env -vC /":                          "env -vC / bash -c '[[ a == b ]]'\n",
		"env --default-signal":               "env --default-signal bash -c '[[ a == b ]]'\n",
		"env expanded option":                "env $flags bash -c '[[ a == b ]]'\n",
		"env --ignore-environment":           "env --ignore-environment bash -c '[[ a == b ]]'\n",
		"env --null":                         "env --null bash -c '[[ a == b ]]'\n",
		"env --debug":                        "env --debug bash -c '[[ a == b ]]'\n",
		"env --chdir=":                       "env --chdir=/ bash -c '[[ a == b ]]'\n",
		"env --unset X":                      "env --unset X bash -c '[[ a == b ]]'\n",
		"env --chdir /":                      "env --chdir / bash -c '[[ a == b ]]'\n",
		"env -P":                             "env -P /bin bash -c '[[ a == b ]]'\n",
		"env --split-string then words":      "env --split-string 'bash -c' '[[ a == b ]]'\n",
		"env --argv0 n":                      "env --argv0 n bash -c '[[ a == b ]]'\n",
		"env --env0-from f":                  "env --env0-from f bash -c '[[ a == b ]]'\n",
		"env --arg= prefix":                  "env --arg=n bash -c '[[ a == b ]]'\n",
		"env --ch prefix":                    "env --ch / bash -c '[[ a == b ]]'\n",
		"env --spl prefix":                   "env --spl 'bash -c' '[[ a == b ]]'\n",
		"env --ignore-env prefix":            "env --ignore-env bash -c '[[ a == b ]]'\n",
		"env -S < as argument":               "env -S 'bash -s <f' <<'EOF'\n[[ a == b ]]\nEOF\nenv -S 'bash <f' <<'EOF'\n[[ a == b ]]\nEOF\n",
		"env -aNAME":                         "env -an bash -c '[[ a == b ]]'\n",
		"env -a":                             "env -a n bash -c '[[ a == b ]]'\n",
		"env --argv0=":                       "env --argv0=n bash -c '[[ a == b ]]'\n",
		"env --env0-from=":                   "env --env0-from=f bash -c '[[ a == b ]]'\n",
		"env --block-signal=":                "env --block-signal=INT bash -c '[[ a == b ]]'\n",
		"env --ignore-signal":                "env --ignore-signal bash -c '[[ a == b ]]'\n",
		"env --list-signal-handling":         "env --list-signal-handling bash -c '[[ a == b ]]'\n",
		"heredoc, then stdout":               "bash <<'EOF' >out\n[[ a == b ]]\nEOF\n",
		"exec -la name":                      "exec -la n bash -c '[[ a == b ]]'\n",
		"env -iv":                            "env -iv bash -c '[[ a == b ]]'\n",
		"env -CDIR":                          "env -C/ bash -c '[[ a == b ]]'\n",
		"env -S then words":                  "env -S 'bash -c' '[[ a == b ]]'\n",
		"coproc body":                        "coproc { [[ a == b ]]; }\n",
		"exec -cl":                           "exec -cl bash -c '[[ a == b ]]'\n",
		"nohup":                              "nohup bash -c '[[ a == b ]]'\n",
		"env -u":                             "env -u X bash -c '[[ a == b ]]'\n",
		"env -uX":                            "env -uX bash -c '[[ a == b ]]'\n",
		"env --unset=":                       "env --unset=X bash -c '[[ a == b ]]'\n",
		"env -C":                             "env -C / bash -c '[[ a == b ]]'\n",
		"env - and --":                       "env - -- bash -c '[[ a == b ]]'\n",
		"env -S":                             "env -S \"bash -c '[[ a == b ]]'\"\n",
		"env -S attached":                    "env -iSbash -c '[[ a == b ]]'\n",
		"env --split-string=":                "env '--split-string=bash -c \"[[ a == b ]]\"'\n",
		"bash - reads stdin":                 "bash - <<'EOF'\n[[ a == b ]]\nEOF\n",
		"heredoc on fd 0":                    "bash 0<<'EOF'\n[[ a == b ]]\nEOF\n",
		"bash /dev/stdin":                    "bash /dev/stdin <<'EOF'\n[[ a == b ]]\nEOF\n",
		"sh /dev/fd/0":                       "sh /dev/fd/0 <<'EOF'\n[[ a == b ]]\nEOF\n",
		"source /dev/stdin with args":        "source /dev/stdin x <<'EOF'\n[[ a == b ]]\nEOF\n",
		"bash --init-file":                   "bash --init-file f <<'EOF'\n[[ a == b ]]\nEOF\n",
		"ansi tab decoded":                   "eval $'x=1;\\t[[ a == b ]]'\n",
		"eval double-quoted backslash":       "eval \"echo \\' ; [[ a == b ]] ; echo \\'\"\n",
		"expanded word before -c":            "bash \"$opt\" -c '[[ a == b ]]'\n",
		"top level test":                     "set -e\n[[ \"$x\" =~ ^[0-9]+$ ]]\n",
		"top level arith":                    "(( n >= 21 ))\n",
		"inside a loop":                      "for a in b; do\n  [[ -n $a ]]\ndone\n",
		"inside then body":                   "if true; then\n  [[ -n $a ]]\nfi\n",
		"inside else body":                   "if true; then :; else\n  [[ -n $a ]]\nfi\n",
		"last in a function":                 "f() {\n  ((a == 1 || b == 2))\n}\n",
		"function called with or":            "f() { [[ a == b ]]; }\nf || die\n", // conservative: bodies are active
		"test with comment":                  "[[ -n $a ]] # why\n",
		"semicolon":                          "[[ -n $a ]];\n",
		"quoted bracket":                     "[[ \"$a ]] b\" == x ]]\n",
		"nested bash heredoc":                "cat >f <<'EOF'\n#!/usr/bin/env bash\n[[ -n $a ]]\nEOF\n",
		"continued test":                     "[[ -n $a &&\n  -n $b ]]\n",
		"after semicolon":                    "x=1; [[ -n $x ]]\n",
		"brace group":                        "{ [[ -n $a ]]; }\n",
		"subshell":                           "( [[ -n $a ]] )\n",
		"inline then":                        "if c; then [[ -n $a ]]; fi\n",
		"inline else":                        "if c; then :; else [[ -n $a ]]; fi\n",
		"inline then and else":               "if [[ -z $f ]]; then [[ $r == 0 ]]; else [[ $r != 0 ]]; fi\n",
		"inline do":                          "for a in b; do [[ -n $a ]]; done\n",
		"inline do arith":                    "while c; do ((n > 0)); done\n",
		"case arm":                           "case $x in a) [[ -n $x ]] ;; esac\n",
		"case arm last":                      "case $x in\n  a) [[ -n $x ]]\n  ;;\nesac\n",
		"case arm leading paren":             "case $x in (a) [[ a == b ]] ;; esac\n",
		"function one-liner":                 "f() { [[ -n $1 ]]; }\n",
		"and tail":                           "a &&\n  [[ -n $b ]]\n",
		"and tail inline":                    "true && [[ a == b ]]\n",
		"or tail":                            "false || [[ a == b ]]\n",
		"test and test":                      "[[ a == a ]] && [[ a == b ]]\n",
		"arith and tail":                     "true && (( 0 ))\n",
		"tab after bracket":                  "[[\t-n $a ]]\n",
		"comment ending in or":               "# explain ||\n[[ -n $a ]]\n",
		"comment ending in and":              "# explain &&\n[[ -n $a ]]\n",
		"comment ending in pipe":             "# explain |\n((a))\n",
		"redirect after test":                "[[ a == b ]] >/dev/null\n",
		"stderr redirect after":              "[[ a == b ]] 2>/dev/null\n",
		"time prefix":                        "time [[ a == b ]]\n",
		"after quoted paren":                 "x=$(echo \")\")\n[[ a == b ]]\n",
		"after quoted brace":                 "x=${y:-\"}\"}\n[[ a == b ]]\n",
		"after ansi-c quote":                 "x=$'it\\'s'\n[[ a == b ]]\n",
		"after grep with quotes":             "e=\"$(grep -E 'a\\(\"1\"|\"::1\"' \"$t\" || true)\"\n[[ a == b ]]\n",
		"group closed by plain end":          "{ [[ -n $a ]]; other; }\n",
		"background group":                   "{ [[ a == b ]]; echo x; } &\n",
		"background subshell":                "( [[ a == b ]]; echo x ) &\n",
		"process substitution":               "cat <( [[ a == b ]]; echo x )\n",
		"plain substitution":                 "value=$([[ a == b ]]; echo x)\n", // conservative unless errexit reaches $( )
		"set -e in a substitution":           "value=$(set -e; [[ a == b ]]; echo accepted)\n",
		"sh script inherits errexit":         "#!/bin/sh\nset -e\nx=$([[ a == b ]]; echo leaked)\n",
		"quoted sh heredoc":                  "cat >f <<'EOF'\n#!/bin/sh\n[[ -n $a ]]\nEOF\n",
		"partly quoted sh heredoc":           "cat >f <<E\"O\"F\n#!/bin/sh\n[[ -n $a ]]\nEOF\n",
		"literal eval":                       "eval '[[ a == b ]]'\n",
		"literal bash -c":                    "bash -c '[[ a == b ]]'\n",
		"literal sh -ec":                     "/bin/sh -ec \"[[ a == b ]]\"\n",
		"literal trap":                       "trap '[[ a == b ]]' EXIT\n",
		"trap after --":                      "trap -- '[[ a == b ]]' EXIT\n",
		"unquoted sh heredoc fed to sh":      "sh <<EOF\n[[ -n \\$a ]]\nEOF\n",
		"unquoted sh heredoc":                "cat >f <<EOF\n#!/bin/sh\n[[ -n \\$a ]]\nEOF\n",
		"unquoted bash heredoc with escapes": "bash <<EOF\nx=\\$(printf hi)\n[[ a == b ]]\nEOF\n",
		"bash -c after other options":        "bash -c -e '[[ a == b ]]'\n",
		"bash -c --":                         "bash -c -- '[[ a == b ]]'\n",
		"bash -co errexit":                   "bash -co errexit '[[ a == b ]]'\n",
		"bash +o posix -c":                   "bash +o posix -c '[[ a == b ]]'\n",
		"bash -s with arguments":             "bash -s -- foo <<'EOF'\n[[ a == b ]]\nEOF\n",
		"bash -s with an expansion":          "bash -s \"$x\" <<'EOF'\n[[ a == b ]]\nEOF\n",
		"bash --rcfile":                      "bash --rcfile f <<'EOF'\n[[ a == b ]]\nEOF\n",
		"bash long option before stdin":      "bash --norc <<'EOF'\n[[ a == b ]]\nEOF\n",
		"here-string to bash":                "bash <<<'[[ a == b ]]'\n",
		"exec bash -c":                       "exec bash -c '[[ a == b ]]'\n",
		"env bash -c":                        "env -i PATH=/bin bash -c '[[ a == b ]]'\n",
		"command -p bash -c":                 "command -p bash -c '[[ a == b ]]'\n",
		"eval --":                            "eval -- '[[ a == b ]]'\n",
		"eval of an ansi-c string":           "eval $'x=1\\n[[ a == b ]]'\n",
		"bash -c with an expansion":          "bash -c \"set -e; [[ $a == b ]]\"\n",
		"eval with an expansion":             "eval \"$x\" '[[ a == b ]]'\n",
		"heredoc fed to bash":                "bash <<'EOF'\n[[ a == b ]]\nEOF\n",
		"heredoc fed to sh -s":               "sh -s <<'EOF'\n[[ a == b ]]\nEOF\n",
		"heredoc fed to command bash":        "command bash -o pipefail <<EOF\n[[ a == b ]]\nEOF\n",
		"tab-indented bash heredoc":          "cat >f <<-'EOF'\n\t#!/usr/bin/env bash\n\t[[ -n $a ]]\n\tEOF\n",
	},
	allowed: map[string]string{
		"trap with or":                  "trap '[[ a == b ]] || exit 1' EXIT\n",
		"trap -p":                       "trap -p EXIT\n",
		"script file after --":          "bash -- -s.sh <<'EOF'\n[[ data\nEOF\n",
		"eval in a condition":           "if eval '[[ a == b ]]'; then :; fi\n",
		"eval or exit":                  "eval '[[ a == b ]]' || exit 1\n",
		"heredoc on another descriptor": "bash ./x.sh 3<<'EOF'\n[[ data\nEOF\n",
		"bash --version":                "bash --version\nsh --help\n",
		"expanded script, flag, data":   "bash \"$script\" -e <<'EOF'\nnot ( shell\nEOF\n",
		"expanded script, here-string":  "bash \"$script\" <<<'not ( shell'\n",
		"env -S < script data":          "env -S 'bash <f' <<'EOF'\nnot ( shell\nEOF\n",
		"env --i ambiguous":             "env --i bash -c '[[ a == b ]]'\n", // env refuses an ambiguous prefix
		"expanded script, no stdin":     "bash \"$script\"\necho x | bash \"$script\"\n",
		"env -S expanded script data":   "env -S 'bash \"$x\"' <<'EOF'\nnot ( shell\nEOF\n",
		"expanded script fed data":      "bash \"$script\" <<'EOF'\nnot ( shell\nEOF\n",
		"double-quoted continuation":    "eval \"echo \\\n[[ a == b ]]\"\n",
		"ansi quote decoded":            "eval $'echo \\\"; [[ a == b ]] #\\\"'\n",
		"ansi apostrophe decoded":       "eval $'echo \\'; [[ a == b ]] #\\''\n",
		"ansi backslash decoded":        "eval $'echo \\\\; [[ a == b ]]'\n",
		"eval built at run time":        "eval \"$cmd\"\n", // shortcut: not seen
		"stdin data for bash -c":        "bash -c 'cat' <<'EOF'\n[[ data\nEOF\n",
		"stdin data for a bash script":  "bash ./x.sh <<'EOF'\n[[ data\nEOF\n",
		"heredoc text naming sh":        "cat <<EOF\nUsage: run.sh\nSSH key setup\nPUSH the button\nbash is required\nEOF\n",
		"assignment only":               "x=1\n",
		"or exit":                       "[[ -n $a ]] || exit 1\n",
		"or return":                     "((a == 1)) || return 1\n",
		"or brace":                      "[[ $a =~ x ]] || { echo no >&2; exit 1; }\n",
		"and chain":                     "[[ $a == b ]] && x=1\n",
		"or inside the test":            "[[ $a == b || $c == d ]] || exit 1\n",
		"or in a regex":                 "[[ $a =~ ^(x\\|y)$ ]] || exit 1\n",
		"if":                            "if [[ -n $a ]]; then :; fi\n",
		"elif":                          "if true; then :; elif ((a)); then :; fi\n",
		"while":                         "while ((n > 0)); do n=$((n - 1)); done\n",
		"until":                         "until [[ -n $a ]]; do :; done\n",
		"bare if keyword":               "if\n  [[ -n $a ]]\nthen :; fi\n",
		"and tail then":                 "if a && [[ -n $b ]]; then :; fi\n",
		"or tail then":                  "while a || ((n)); do :; done\n",
		"read loop condition":           "while read -r l && [[ -n $l ]]; do :; done\n",
		"tail then or":                  "a && [[ -n $b ]] || exit 1\n",
		"inline then or":                "if c; then [[ -n $a ]] || exit 1; else [[ -n $b ]] || return 1; fi\n",
		"case arm or":                   "case $x in a) [[ -n $x ]] || exit 1 ;; esac\n",
		"redirect ampersand":            "cmd 2>&1 || exit 1\n[[ -n $a ]] || exit 1\n",
		"array and subst":               "a=(1 2); b=$([[ -n $a ]] && echo y); cat <(echo) >/dev/null; [[ -n $a ]] || exit 1\n",
		"function header":               "f() {\n  [[ -n $1 ]] || return 1\n}\n",
		"wrapped or":                    "[[ -n $a ]] \\\n  || exit 1\n",
		"multi-line test":               "[[ -n $a &&\n  -n $b ]] ||\n  fail x\n",
		"test in condition":             "if ! x ||\n  [[ -n $a ]]; then :; fi\n",
		"plain heredoc":                 "cat <<EOF\n[[ -n $a ]]\nEOF\n",
		"here string":                   "read -r a <<<\"$b\"\n[[ -n $a ]] || exit 1\n",
		"arith expansion":               "x=$((a + 1))\n",
		"command":                       "test -n \"$a\"\n",
		"comment":                       "# [[ -n $a ]]\n",
		"subshell list":                 "(cd x && ls)\n",
		"negated test":                  "! [[ -e f ]]\n", // errexit ignores a ! pipeline on every bash
		"negated group":                 "! { [[ -n $a ]]; }\n",
		"group then or":                 "{ [[ -n $a ]]; other; } || die x\n",
		"subshell then or":              "( [[ a == b ]] ) || exit 1\n",
		"group tail then or":            "{ true && [[ a == b ]]; } || exit 1\n",
		"group redirect or":             "{ read -r a && [[ -z $a ]]; } <\"$f\" ||\n  die x\n",
		"group then and":                "{ [[ -n $a ]]; } && x\n",
		"group in condition":            "if { [[ -n $a ]]; }; then :; fi\n",
		"subshell in condition":         "if ( [[ -n $a ]] ); then :; fi\n",
		"background":                    "[[ -n $a ]] &\n",
		"quoted bracket text":           "echo '[[ -n $a ]]' \"[[ x ]]\"\n",
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
	if got, err := scan("a\neval 'x=1\n[[ a == b ]]'\n"); err != nil || !reflect.DeepEqual(got, []int{3}) {
		t.Errorf("a multi-line eval string: got %v (%v), want [3]", got, err)
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
	for _, script := range []string{
		"echo x | bash\n", "printf x | sh -s -- a\n", "echo x | bash >/dev/null\n", "echo x | bash 2>&1\n", "echo x |& bash\n",
		"source /dev/stdin\n", ". /dev/fd/0\n", "source -- /proc/self/fd/0\n", "bash 3<<'EOF'\nx\nEOF\n", "bash <&3\n",
		"{ bash; } <<'EOF'\n[[ a == b ]]\nEOF\n", "( bash -s ) <<'EOF'\nx\nEOF\n", "exec 0<<'EOF'\nx\nEOF\nbash\n", "run() { bash -s; }\n", "coproc bash\n",
		"source /dev/stdin </dev/stdin\n", "bash <<'EOF' 0<&3\nx\nEOF\n", "bash <<'EOF' <&3\nx\nEOF\n",
		"bash </dev/stdin\n", "bash < <(printf x)\n", "bash <<'EOF' <>f\nx\nEOF\n",
		"sh -c 'bash -s' <<'EOF'\nx\nEOF\n", // fails closed: a heredoc reaching handed code's shell is not followed
	} {
		if _, err := bareTests(script); err == nil || !strings.Contains(err.Error(), "cannot be scanned") {
			t.Errorf("%q: got %v, want a script read from stdin rejected", script, err)
		}
	}
	for _, script := range []string{"echo x | bash ./y.sh\n", "echo x | bash -c 'cat'\n", "source ./lib.sh\n", ". /dev/fd/0 <f\n", "bash /dev/stdin <f\n"} {
		if _, err := bareTests(script); err != nil {
			t.Errorf("%q: %v, want accepted", script, err)
		}
	}
	if _, err := bareTests("bash -s <<'EOF'\nif\nEOF\n"); err == nil {
		t.Error("an unparsable heredoc fed to bash -s must be an error")
	}
	if _, err := bareTests("cat >f <<'EOF'\n#!/bin/bash5\n[[ a == b ]]\nEOF\n"); err == nil || !strings.Contains(err.Error(), "does not read") {
		t.Errorf("a heredoc whose shebang the scan does not read must be an error: %v", err)
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

// TestRepositoryScanSelectors pins what the repository scan reads: which shebangs it scans,
// and which workflow steps run bash (the step's shell over the job's default over the
// workflow's), each distinct step scanned once.
func TestRepositoryScanSelectors(t *testing.T) {
	for line, want := range map[string]string{
		"#!/usr/bin/env bash": "bash", "#!/bin/bash -e": "bash", "#!/usr/bin/env -S bash -e": "bash",
		"#!/bin/sh": "sh", "#!/bin/sh -e": "sh", "#!/usr/bin/env sh": "sh", "#!/usr/bin/env -S sh": "sh",
		"#!/usr/bin/env -S -i bash": "unread", "#!/usr/bin/env -S VAR=1 sh": "unread", "#!/usr/bin/env -Ssh": "unread",
		"#!/usr/bin/env --split-string=sh": "unread", "#!/usr/bin/env -S 'bash' -e": "unread", "#!/usr/bin/env -S \"sh\"": "unread",
		"#!/bin/bash5": "unread", "#!/usr/bin/env bash-5.2": "unread", "#!/bin/rbash": "unread", "#!/bin/BASH": "unread",
		"#!/usr/bin/env nix-shell": "unread", "#!/usr/bin/env -iSbash": "unread", "#!/usr/bin/env -vSsh": "unread", "#!/usr/bin/env -S deno run --allow-env=SHELL": "",
		"#!/bin/zsh": "", "#!/bin/dash": "", "#!/usr/bin/env -S uv run --script": "", "#!/usr/bin/env python3": "",
	} {
		if got := shebangKind(line); got != want {
			t.Errorf("shebangKind(%q) = %q, want %q", line, got, want)
		}
	}
	fixture := "defaults:\n  run:\n    shell: pwsh\njobs:\n" +
		"  a:\n    runs-on: ubuntu-latest\n    defaults:\n      run:\n        shell: pwsh\n    steps:\n" +
		"      - name: step-over-job\n        shell: bash\n        run: '[[ -n $a ]]'\n" +
		"      - name: job-default\n        run: '[[ -n $a ]]'\n" +
		"  b:\n    runs-on: ubuntu-latest\n    defaults:\n      run:\n        shell: bash\n    steps:\n" +
		"      - name: job-over-workflow\n        run: '[[ -n $a ]]'\n" +
		"      - name: step-pwsh\n        shell: pwsh\n        run: '[[ -n $a ]]'\n" +
		"  c:\n    runs-on: ubuntu-latest\n    steps:\n      - name: workflow-default\n        run: '[[ -n $a ]]'\n" +
		"  d:\n    runs-on: ubuntu-latest\n    steps:\n" +
		"      - shell: bash\n        run: echo ok\n      - shell: bash\n        run: '[[ -n $b ]]'\n"
	problems, err := workflowProblems("fixture.yml", []byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	for _, problem := range problems {
		if match := regexp.MustCompile(`job (\S+) step "([^"]*)"`).FindStringSubmatch(problem); match != nil {
			steps = append(steps, match[1]+"/"+match[2])
		}
	}
	slices.Sort(steps)
	if want := []string{"a/step-over-job", "b/job-over-workflow", "d/"}; !reflect.DeepEqual(steps, want) {
		t.Errorf("fixture workflow: problems in %v, want %v:\n%s", steps, want, strings.Join(problems, "\n"))
	}
}

// shebangKind is how the repository walk reads a first line: "bash" or "sh" to scan, "unread"
// for one naming bash or sh in a form the scan does not read, and "" for another interpreter.
func shebangKind(firstLine string) string {
	switch {
	case bashShebang.MatchString(firstLine):
		return "bash"
	case scriptShebang.MatchString(firstLine):
		return "sh"
	case shellMention.MatchString(firstLine):
		return "unread"
	}
	return ""
}

// workflowProblems returns the scan problems of each bash step in one workflow file, a step
// repeated by a YAML anchor scanned once.
func workflowProblems(name string, raw []byte) ([]string, error) {
	var workflow failClosedWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return nil, err
	}
	var problems []string
	scanned := map[string]bool{}
	for jobName, job := range workflow.Jobs {
		for _, step := range job.Steps {
			shell := firstNonEmpty(step.Shell, job.Defaults.Run.Shell, workflow.Defaults.Run.Shell)
			key := step.Name + "\x00" + step.Run
			if step.Run == "" || !isBashShell(shell, string(job.RunsOn)) || scanned[key] {
				continue
			}
			scanned[key] = true
			script := workflowExpression.ReplaceAllString(step.Run, "GITHUB_EXPRESSION")
			problems = append(problems, scanProblems(fmt.Sprintf("%s job %s step %q (line of its script)", name, jobName, step.Name), script, "append `|| exit 1`")...)
		}
	}
	return problems, nil
}

// repositoryScripts returns the bash and sh scripts under root that git does not ignore, by
// path relative to root with the kind shebangKind gives them, and the paths whose shebang
// names bash or sh in a form the scan does not read. Untracked files are included, so a new
// script is checked before it is committed.
func repositoryScripts(root string) (map[string]string, []string, error) {
	// precomposeunicode off: on macOS git would otherwise report a decomposed name composed.
	out, err := exec.Command("git", "-C", root, "-c", "core.precomposeunicode=false", "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			err = fmt.Errorf("%w: %s", err, bytes.TrimSpace(exit.Stderr))
		}
		return nil, nil, fmt.Errorf("listing ignored files: %w", err)
	}
	ignored := map[string]bool{}
	for _, name := range strings.Split(string(out), "\x00") {
		ignored[filepath.FromSlash(strings.TrimSuffix(name, "/"))] = true // --directory lists an ignored tree as dir/
	}
	scripts := map[string]string{}
	var unread []string
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || ignored[rel] {
				return fs.SkipDir
			}
			if path != root && holdsRepository(path) {
				return fs.SkipDir // another repository or worktree, not this one's files
			}
			return nil
		}
		if !d.Type().IsRegular() || ignored[rel] {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil || !bytes.HasPrefix(raw, []byte("#!")) {
			return err
		}
		firstLine, _, _ := strings.Cut(string(raw), "\n")
		switch kind := shebangKind(firstLine); kind {
		case "unread":
			unread = append(unread, rel)
		case "bash", "sh":
			scripts[rel] = kind
		}
		return nil
	})
	return scripts, unread, err
}

// holdsRepository reports whether dir is a repository or worktree root: a .git directory
// with a HEAD, or a .git file pointing at one.
func holdsRepository(dir string) bool {
	info, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	if info.IsDir() {
		_, err := os.Stat(filepath.Join(dir, ".git", "HEAD"))
		return err == nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".git"))
	return err == nil && bytes.HasPrefix(raw, []byte("gitdir: "))
}

func TestRepositoryScriptsSkipsIgnoredFiles(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("LC_ALL", "C") // git's own message is matched below
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		t.Setenv(name, "") // a hook's git environment would point git at the outer repository
		os.Unsetenv(name)
	}
	root := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	files := map[string]string{
		".gitignore": "ignored/\n*.log\n", "a.sh": "#!/usr/bin/env bash\n", "b.sh": "#!/bin/sh\n",
		"ignored/c.sh": "#!/usr/bin/env bash\n", "with space.log": "#!/usr/bin/env bash\n",
		"new\nline.log": "#!/usr/bin/env bash\n", "cafe\u0301.log": "#!/usr/bin/env bash\n", // decomposed é
		"u.sh": "#!/bin/rbash\n", ".claude/worktrees/w1/.git": "gitdir: elsewhere\n", "stray/.git": "", "stray/s.sh": "#!/usr/bin/env bash\n", "headless/.git/config": "", "headless/s.sh": "#!/usr/bin/env bash\n", "nested/.git/HEAD": "ref: refs/heads/main\n", "nested/n.sh": "#!/usr/bin/env bash\n", ".claude/worktrees/w1/old.sh": "#!/usr/bin/env bash\n", "py": "#!/usr/bin/env python3\n",
	}
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	scripts, unread, err := repositoryScripts(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]string{"a.sh": "bash", "b.sh": "sh", filepath.Join("stray", "s.sh"): "bash", filepath.Join("headless", "s.sh"): "bash"}; !reflect.DeepEqual(scripts, want) {
		t.Errorf("scripts = %v, want %v", scripts, want)
	}
	if !reflect.DeepEqual(unread, []string{"u.sh"}) {
		t.Errorf("unread = %v, want [u.sh]", unread)
	}
	outside := t.TempDir()
	if _, _, err := repositoryScripts(outside); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("outside a repository: got %v, want git's own error", err)
	}
}

// Every statement-level [[ ]] or (( )) in a bash workflow step or bash script needs an
// explicit failure branch: bash before 4.1 ignores errexit for it and fails open.
func TestNoBareBashTestRelyingOnErrexit(t *testing.T) {
	root := repositoryRoot(t)
	var workflows []string
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		matches, err := filepath.Glob(filepath.Join(root, ".github/workflows", pattern))
		if err != nil {
			t.Fatal(err)
		}
		workflows = append(workflows, matches...)
	}
	if len(workflows) == 0 {
		t.Fatal("no workflows found")
	}
	for _, path := range workflows {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		problems, err := workflowProblems(filepath.Base(path), raw)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, problem := range problems {
			t.Error(problem)
		}
	}
	scripts, unread, err := repositoryScripts(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range unread {
		t.Errorf("%s: its shebang names bash or sh in a form the scan does not read; use #!/usr/bin/env bash or #!/bin/sh", rel)
	}
	sh := 0
	for rel, kind := range scripts {
		if kind == "sh" {
			sh++
		}
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		scanScript(t, rel, string(raw), "append `|| exit 1` (or `|| return 1` in a function)")
	}
	// About 5% under the repository's counts, so losing a few scripts is noticed; lower it when
	// scripts are deleted on purpose.
	if len(scripts) < 240 || sh < 180 {
		t.Fatalf("found only %d bash scripts, %d of them sh; the scan is not seeing the repository", len(scripts), sh)
	}
}
