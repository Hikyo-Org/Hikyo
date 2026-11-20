package lint

import (
	"fmt"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
)

// SummaryCategory is the reserved diagnostic category for per-package facts an
// analyzer hands to the driver instead of reporting a finding. The driver
// separates these from findings and aggregates them per context (lint-analysis
// driver ADR, decision 3). A summary message is "<kind> <subject>".
const SummaryCategory = "summary"

// Summary kinds.
const (
	// summaryTarget records that the package an analyzer selects by path was
	// analyzed. Its absence from a whole-module run is a finding: a renamed or
	// unloaded target must not turn a check into a silent no-op.
	summaryTarget = "target"
	// summaryPin records one compile-time redaction pin found in crypto.
	summaryPin = "pin"
)

// Summary is one decoded summary diagnostic.
type Summary struct{ Kind, Subject string }

// ParseSummary decodes a summary diagnostic message.
func ParseSummary(message string) (Summary, error) {
	kind, subject, ok := strings.Cut(message, " ")
	if !ok || kind == "" || subject == "" {
		return Summary{}, fmt.Errorf("malformed summary diagnostic %q", message)
	}
	return Summary{Kind: kind, Subject: subject}, nil
}

func reportSummary(pass *analysis.Pass, kind, subject string) {
	pass.Report(analysis.Diagnostic{Pos: packagePos(pass), Category: SummaryCategory, Message: kind + " " + subject})
}

func reportTarget(pass *analysis.Pass) { reportSummary(pass, summaryTarget, pass.Pkg.Path()) }

// packagePos anchors package-level diagnostics on the first file's package clause.
func packagePos(pass *analysis.Pass) token.Pos {
	if len(pass.Files) == 0 {
		return token.NoPos
	}
	return pass.Files[0].Package
}

// Check pairs an analyzer with the package paths it selects by path; the
// driver requires a target summary for each.
type Check struct {
	Analyzer *analysis.Analyzer
	Targets  []string
}

// MissingTargets renders a finding per required target no analyzer pass reported.
func MissingTargets(analyzer string, required []string, seen map[string]bool) []string {
	var out []string
	for _, path := range required {
		if !seen[path] {
			out = append(out, fmt.Sprintf("%s: target package %s was not analyzed", analyzer, path))
		}
	}
	return out
}

// FormatFinding is the one rendering both harnesses use, so their findings
// compare by string.
func FormatFinding(posn, analyzer, message string) string {
	return fmt.Sprintf("%s: %s: %s", posn, analyzer, message)
}

// Result is what one analyzer reported over a set of packages.
type Result struct {
	Findings  []string
	Summaries []Summary
}

// RunOver runs one analyzer over the type-checked packages the old go/packages
// loader produced and renders its diagnostics, so the legacy Check* entry
// points share the analysis.Pass core with the vet tool. Like the old loader
// walk it analyzes each package path once and skips packages without type
// information (imports that are not load roots).
func RunOver(c Check, pkgs []*packages.Package) Result {
	var res Result
	seenTargets := map[string]bool{}
	for _, p := range flatten(pkgs) {
		if p.TypesInfo == nil || p.Types == nil {
			continue
		}
		pkg := p.Types
		if pkg.Path() != p.PkgPath {
			// Tests relocate a loaded package by rewriting PkgPath.
			pkg = types.NewPackage(p.PkgPath, p.Name)
		}
		pass := &analysis.Pass{
			Analyzer:  c.Analyzer,
			Fset:      p.Fset,
			Files:     p.Syntax,
			Pkg:       pkg,
			TypesInfo: p.TypesInfo,
			Report: func(d analysis.Diagnostic) {
				if d.Category != SummaryCategory {
					res.Findings = append(res.Findings, FormatFinding(p.Fset.Position(d.Pos).String(), c.Analyzer.Name, d.Message))
					return
				}
				summary, err := ParseSummary(d.Message)
				if err != nil {
					res.Findings = append(res.Findings, FormatFinding("-", c.Analyzer.Name, err.Error()))
					return
				}
				if summary.Kind == summaryTarget {
					seenTargets[summary.Subject] = true
				}
				res.Summaries = append(res.Summaries, summary)
			},
		}
		if _, err := c.Analyzer.Run(pass); err != nil {
			res.Findings = append(res.Findings, FormatFinding(p.PkgPath, c.Analyzer.Name, "analyzer failed: "+err.Error()))
		}
	}
	res.Findings = append(res.Findings, MissingTargets(c.Analyzer.Name, c.Targets, seenTargets)...)
	slices.Sort(res.Findings)
	return res
}

// findPackage returns the package with the given path among pkg and its
// transitive imports, or nil.
func findPackage(pkg *types.Package, path string) *types.Package {
	seen := map[*types.Package]bool{}
	var walk func(p *types.Package) *types.Package
	walk = func(p *types.Package) *types.Package {
		if p.Path() == path {
			return p
		}
		if seen[p] {
			return nil
		}
		seen[p] = true
		for _, imp := range p.Imports() {
			if found := walk(imp); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(pkg)
}

// inModule reports whether path is the module or one of its packages.
func inModule(path string) bool {
	return path == Module || strings.HasPrefix(path, Module+"/")
}

// Checks is the registered per-package analyzer set with the targets each
// requires on a whole-module run.
func Checks() []Check {
	return []Check{
		{Analyzer: ProofSig, Targets: []string{Module + "/internal/store"}},
		{Analyzer: ForgeGuard},
		{Analyzer: ServiceSeams, Targets: []string{Module + "/internal/service"}},
		{Analyzer: Fence, Targets: []string{Module + "/internal/service"}},
		{Analyzer: Handles},
		{Analyzer: RedactSurfaces, Targets: []string{sensitiveOwner}},
		{Analyzer: RedactFormat},
		{Analyzer: TxResult},
		{Analyzer: TestObservers, Targets: []string{Module + "/internal/store/authn", Module + "/internal/service"}},
		{Analyzer: UpgradeBuild},
		{Analyzer: ActorSplit, Targets: []string{Module + "/internal/server"}},
		{Analyzer: RedactPins, Targets: []string{sensitiveOwner}},
	}
}

// Analyzers lists the analyzers the vet tool registers.
func Analyzers() []*analysis.Analyzer {
	checks := Checks()
	out := make([]*analysis.Analyzer, len(checks))
	for i, c := range checks {
		out[i] = c.Analyzer
	}
	return out
}

// fileName is the filename of the file containing pos.
func fileName(pass *analysis.Pass, pos token.Pos) string {
	return pass.Fset.Position(pos).Filename
}
