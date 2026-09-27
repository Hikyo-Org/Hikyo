package cli

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Hikyo-Org/hikyo/internal/repscan"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
)

// `hikyo scan` (#153): local repository and CI secret scanning with the same
// vendored ruleset the server's scan-on-value-entry runs. It is a PURE LOCAL
// verb, like `definitions scaffold`: it never constructs a client, reads no
// session, and sends neither source nor findings anywhere.
//
// Exit codes are the verb's own documented contract, on the `definitions
// check` precedent: 0 clean; 1 unsuppressed findings (a verdict, not an error,
// so no `hikyo:` diagnostic); 2 invalid usage or configuration; 4 the scan
// was refused or failed (a budget exceeded, a malformed repository, an
// unreadable root, any other scanner failure); 6 git is not installed. A scan
// that could not complete never exits 0.

// scanFormatSARIF extends the output grammar for this verb only: SARIF is the
// interchange code-scanning consumers read.
const scanFormatSARIF = "sarif"

func runScan(ctx context.Context, ios IO, args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(ios.Stderr)
	var staged, unstaged, history bool
	var rangeSpec, configFile, suppressionsFile, format string
	var timeout time.Duration
	var excludePaths, excludeRules stringList
	fs.BoolVar(&staged, "staged", false, "scan the index: exactly what the next commit records")
	fs.BoolVar(&unstaged, "unstaged", false, "scan unstaged working-tree changes and untracked files")
	fs.BoolVar(&history, "history", false, "scan every blob every reachable commit introduced")
	fs.StringVar(&rangeSpec, "range", "", "scan the commits of a revision range A..B")
	fs.StringVar(&configFile, "config", "", "scan configuration (default: "+repscan.DefaultConfigName+" at the repository top level)")
	fs.StringVar(&suppressionsFile, "suppressions", "", "an additional file of reviewed fingerprint suppressions")
	fs.Var(&excludePaths, "exclude-path", "exclude paths matching this glob (repeatable)")
	fs.Var(&excludeRules, "exclude-rule", "exclude this rule id (repeatable)")
	fs.DurationVar(&timeout, "timeout", repscan.DefaultTimeout, "the scan's wall-clock budget")
	fs.StringVar(&format, "o", "table", "output format: table, json or sarif")
	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}

	mode := repscan.ModePaths
	selected := 0
	for _, m := range []struct {
		on   bool
		mode repscan.Mode
	}{{staged, repscan.ModeStaged}, {unstaged, repscan.ModeUnstaged}, {history, repscan.ModeHistory}, {rangeSpec != "", repscan.ModeRange}} {
		if m.on {
			mode = m.mode
			selected++
		}
	}
	if selected > 1 {
		return failf(ExitUsage, "hikyo scan: --staged, --unstaged, --history and --range are mutually exclusive")
	}
	if mode != repscan.ModePaths && len(positional) > 0 {
		return failf(ExitUsage, "hikyo scan: --%s takes no paths, got: %s", mode, strings.Join(positional, " "))
	}
	switch format {
	case string(FormatTable), string(FormatJSON), scanFormatSARIF:
	default:
		return failf(ExitUsage, "unknown output format %q: use table, json or sarif", format)
	}
	if timeout <= 0 {
		return failf(ExitUsage, "hikyo scan: --timeout must be positive")
	}

	rules, err := scanning.Load()
	if err != nil {
		return failf(ExitRefused, "hikyo scan: the embedded ruleset failed to load: %v", err)
	}
	limits := repscan.DefaultLimits()
	limits.Timeout = timeout
	report, err := repscan.Run(ctx, rules, repscan.Options{
		Mode:             mode,
		Workdir:          ios.Workdir,
		Paths:            positional,
		Range:            rangeSpec,
		ConfigFile:       configFile,
		SuppressionsFile: suppressionsFile,
		ExcludePaths:     excludePaths,
		ExcludeRules:     excludeRules,
		Limits:           limits,
		Now:              ios.Now,
	})
	if err != nil {
		code := ExitRefused
		switch repscan.ErrorKind(err) {
		case repscan.KindConfig:
			code = ExitUsage
		case repscan.KindUnavailable:
			code = ExitUnavailable
		}
		return &Error{Code: code, Err: err}
	}

	switch format {
	case string(FormatJSON):
		if err := Render(ios.Stdout, FormatJSON, Table{JSON: report}); err != nil {
			return failf(ExitRefused, "hikyo scan: writing the report: %v", err)
		}
	case scanFormatSARIF:
		if err := repscan.WriteSARIF(ios.Stdout, report, ios.Version); err != nil {
			return failf(ExitRefused, "hikyo scan: writing the report: %v", err)
		}
	default:
		if open := report.Unsuppressed(); len(open) > 0 {
			if err := Render(ios.Stdout, FormatTable, scanTable(open)); err != nil {
				return failf(ExitRefused, "hikyo scan: writing the report: %v", err)
			}
		}
	}
	fmt.Fprintf(ios.Stderr, "scan (%s, ruleset %s): %s\n", report.Mode, report.Ruleset.Snapshot, report.Summary)
	if report.Summary.Findings > 0 {
		return &silentExit{Code: 1}
	}
	return nil
}

// scanTable is the redacted human listing: where, which rule, which commit,
// and the fingerprint a reviewed suppression would name. Never match text.
func scanTable(findings []repscan.Finding) Table {
	rows := make([][]string, 0, len(findings))
	for _, f := range findings {
		commit := f.Commit
		if len(commit) > 12 {
			commit = commit[:12]
		}
		if commit == "" {
			commit = "-"
		}
		rows = append(rows, []string{displaySafe(f.Path) + ":" + strconv.Itoa(f.Line), f.RuleID, commit, f.Fingerprint})
	}
	return Table{Columns: []string{"LOCATION", "RULE", "COMMIT", "FINGERPRINT"}, Rows: rows}
}

// displaySafe quotes a path that carries anything a terminal would not print
// literally (control, format or separator characters, tabs), so an untrusted
// file name cannot forge table rows or rewrite the terminal.
func displaySafe(p string) string {
	for _, r := range p {
		if !unicode.IsPrint(r) || r == '\t' {
			return strconv.Quote(p)
		}
	}
	return p
}
