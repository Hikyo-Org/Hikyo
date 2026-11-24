package lint

import (
	"go/ast"
	"go/types"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// legacySubexpressionTypes is the redactfmt collector before it was narrowed
// (#1468): the type of the argument and of every sub-expression. It is the
// reference the narrowed rule is compared against.
func legacySubexpressionTypes(info *types.Info, arg ast.Expr) []types.Type {
	var out []types.Type
	ast.Inspect(arg, func(n ast.Node) bool {
		if expr, ok := n.(ast.Expr); ok {
			if tv, ok := info.Types[expr]; ok && tv.Type != nil {
				out = append(out, tv.Type)
			}
		}
		return true
	})
	return out
}

// reviewedFalsePositives are the only diagnostics the old redactfmt rule raised
// and the narrowed rule does not. Each formats a field selected from a holder
// struct that is not itself pinned, and the selected field or method result
// is plain data (an int, a string; no interface, func or pinned type). The key
// is the fixture line without its
// comment, prefixed by the enclosing function so a line cannot be listed for
// a different function. A pinned value reachable through the selected field, a method, a
// call argument, a literal or an audit-content receiver must never be listed.
var reviewedFalsePositives = map[string]bool{
	`NarrowingNonSensitive: _ = fmt.Sprintf("%d", h.seq)`:          true, // int field
	`NarrowingNonSensitive: _ = fmt.Sprintf("%s", h.inner.name)`:   true, // string field two levels deep
	`NarrowingNonSensitive: _ = fmt.Sprintf("%d", (*hp).seq)`:      true, // int field through a dereference
	`NarrowingNonSensitive: _ = fmt.Sprintf("%d", hp.seq)`:         true, // int field through a pointer
	`NarrowingNonSensitive: _ = fmt.Sprintf("%d", mkHolder().seq)`: true, // int field of a call result
	`NarrowingNonSensitive: slog.Info("n", "n", h.seq)`:            true, // int field, logging call
	`NarrowingNonSensitive: _ = fmt.Sprintf("%d", h.count())`:      true, // method with a plain int result
	`NarrowingNonSensitive: _ = fmt.Sprintf("%v", h.when())`:       true, // time.Time result; zone name and instant are not inspected (named limit)
}

// TestRedactFormatNarrowingDifferential runs the pre-narrowing and the
// narrowed redactfmt over the fixture corpus. Every diagnostic only the old
// rule raises must be on reviewedFalsePositives, and the narrowed rule must
// raise nothing the old one did not.
func TestRedactFormatNarrowingDifferential(t *testing.T) {
	pkgs, err := loadDefault(Module + "/internal/lint/testdata/badredact")
	if err != nil {
		t.Fatal(err)
	}
	oldRule := runAnalyzer(Check{Analyzer: newRedactFormat(legacySubexpressionTypes)}, pkgs)
	newRule := runAnalyzer(Check{Analyzer: RedactFormat}, pkgs)
	if len(newRule) == 0 || len(oldRule) <= len(newRule) {
		t.Fatalf("old rule raised %d diagnostics, narrowed rule %d: the corpus no longer shows the narrowing", len(oldRule), len(newRule))
	}
	dropped := map[string]bool{}
	for _, f := range oldRule {
		if !slices.Contains(newRule, f) {
			dropped[fixtureLine(t, f)] = true
		}
	}
	for _, f := range newRule {
		if !slices.Contains(oldRule, f) {
			t.Errorf("the narrowed rule raises a diagnostic the old rule did not: %s", f)
		}
	}
	for line := range dropped {
		if !reviewedFalsePositives[line] {
			t.Errorf("the narrowed rule drops an unreviewed diagnostic: %s", line)
		}
	}
	for line := range reviewedFalsePositives {
		if !dropped[line] {
			t.Errorf("reviewed false positive %s is still reported or no longer in the corpus", line)
		}
	}
}

var funcName = regexp.MustCompile(`^func (?:\([^)]*\) )?(\w+)`)

// fixtureLine is the source line a finding points at, without its trailing
// comment, prefixed by its enclosing function ("Func: line"). Findings read "file:line:col: analyzer: message".
func fixtureLine(t *testing.T, finding string) string {
	t.Helper()
	parts := strings.SplitN(finding, ":", 4)
	if len(parts) < 4 {
		t.Fatalf("finding %q has no position", finding)
	}
	line, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("finding %q: %v", finding, err)
	}
	src, err := os.ReadFile(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Split(string(src), "\n")
	text, _, _ := strings.Cut(all[line-1], " //")
	for i := line - 1; i >= 0; i-- {
		if m := funcName.FindStringSubmatch(all[i]); m != nil {
			return m[1] + ": " + strings.TrimSpace(text)
		}
	}
	t.Fatalf("finding %q is outside a function", finding)
	return ""
}
