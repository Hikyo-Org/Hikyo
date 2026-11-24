package lint

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
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
	// summaryGoFile records one Go file an analyzed package contains, from
	// jsonv1: the driver compares them with the repository's files.
	summaryGoFile = "gofile"
	// summaryException records that the package allowed to import
	// encoding/json does, so its exception is not stale.
	summaryException = "exception"
)

// Summary is one decoded summary diagnostic.
type Summary struct{ Kind, Subject string }

// parseSummary decodes a summary diagnostic message.
func parseSummary(message string) (Summary, error) {
	kind, subject, ok := strings.Cut(message, " ")
	if !ok || kind == "" || subject == "" {
		return Summary{}, fmt.Errorf("malformed summary diagnostic %q", message)
	}
	return Summary{Kind: kind, Subject: subject}, nil
}

// validate rejects a summary kind the driver does not know, and a pin from any
// analyzer but redactpins: an unknown or misplaced summary must fail, not be
// dropped.
func (s Summary) validate(analyzer string) error {
	switch s.Kind {
	case summaryTarget:
		return nil
	case summaryPin:
		if analyzer != RedactPins.Name {
			return fmt.Errorf("analyzer %s reported a %s summary; only %s may", analyzer, summaryPin, RedactPins.Name)
		}
		return nil
	case summaryRawOwner, summaryRawCaller, summaryRawHelper, summaryRawUnreached, summaryRawDump:
		if analyzer != RawSQL.Name {
			return fmt.Errorf("analyzer %s reported a %s summary; only %s may", analyzer, s.Kind, RawSQL.Name)
		}
		return nil
	case summaryGoFile, summaryException:
		if analyzer != JSONv1.Name {
			return fmt.Errorf("analyzer %s reported a %s summary; only %s may", analyzer, s.Kind, JSONv1.Name)
		}
		return nil
	}
	return fmt.Errorf("analyzer %s reported unknown summary kind %q", analyzer, s.Kind)
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

// targetFindings renders a finding per required target no analyzer pass reported.
func targetFindings(analyzer string, required []string, seen map[string]bool) []string {
	var out []string
	for _, path := range required {
		if !seen[path] {
			out = append(out, fmt.Sprintf("%s: target package %s was not analyzed", analyzer, path))
		}
	}
	return out
}

// formatFinding renders a finding as position, analyzer and message.
func formatFinding(posn, analyzer, message string) string {
	return fmt.Sprintf("%s: %s: %s", posn, analyzer, message)
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
		{Analyzer: RecoveryLookup, Targets: ruleTargets(RecoveryLookupRules)},
		{Analyzer: ContainmentSetters, Targets: ruleTargets(ContainmentSetterRules)},
		{Analyzer: ActorSplit, Targets: []string{Module + "/internal/server"}},
		{Analyzer: RedactPins, Targets: []string{sensitiveOwner}},
		{Analyzer: GrantLock, Targets: []string{Module + "/internal/store/authn"}},
		{Analyzer: DenialWriter, Targets: []string{denialSurface}},
		{Analyzer: AuditAppendOnly, Targets: []string{Module + "/internal/store"}},
		{Analyzer: ContainmentGates, Targets: []string{Module + "/internal/store"}},
		{Analyzer: ReencryptCoverage, Targets: []string{Module + "/internal/store"}},
		{Analyzer: SQLPredicates, Targets: []string{Module + "/internal/store"}},
		{Analyzer: JSONv1},
		{Analyzer: StableCopies, Targets: []string{Module + "/internal/jsonwire"}},
	}
}

// Analyzers lists the analyzers the vet tool registers.
//
// RawSQL is registered here and not in Checks: it has no target package, and
// it exports facts and aggregates its own summaries (rawSQLSummaryFindings).
func Analyzers() []*analysis.Analyzer {
	checks := Checks()
	out := make([]*analysis.Analyzer, len(checks), len(checks)+1)
	for i, c := range checks {
		out[i] = c.Analyzer
	}
	return append(out, RawSQL)
}

// fileName is the filename of the file containing pos, ignoring `//line`
// directives so a production file cannot present itself as a test file.
func fileName(pass *analysis.Pass, pos token.Pos) string {
	return pass.Fset.PositionFor(pos, false).Filename
}

func isTestFile(name string) bool { return strings.HasSuffix(name, "_test.go") }
