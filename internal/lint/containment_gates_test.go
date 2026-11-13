package lint

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestContainmentGatesRepo(t *testing.T) {
	root := repoRoot(t)
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			queries, err := ParseQueries(filepath.Join(root, "internal/store/queries", engine))
			if err != nil {
				t.Fatal(err)
			}
			for _, finding := range CheckContainmentGates(queries, ContainmentGateExemptions) {
				t.Error(finding)
			}
		})
	}
}

func TestContainmentGatesCatchesMissingCondition(t *testing.T) {
	queries, err := ParseQueries(filepath.Join("testdata", "badcontainment"))
	if err != nil {
		t.Fatal(err)
	}
	findings := CheckContainmentGates(queries, map[string]string{
		"Exempted":         "fixture",
		"ExemptedButGated": "fixture",
		"Vanished":         "fixture",
	})
	want := []string{
		"query PrivacyOnly tests",
		"query WrongQualifier tests",
		"query BareUngated tests",
		"query CommentedLine tests",
		"query CommentedBlock tests",
		"query CommentedNested tests",
		"query ReversedPrivacy tests",
		"query InListPrivacy tests",
		"query NotEqualRestricted tests",
		"query BangEqualRestricted tests",
		"query LoudPrivacy tests",
		"query QuotedAliasPredicate tests",
		"query StringAliasPredicate tests",
		"query QuotedDashesUngated tests",
		"query OrInsideGroup tests",
		"query OrAtTopLevel tests",
		"query OrBothInGroup tests",
		"query NegatedGroup tests",
		"query NegatedCondition tests",
		"query CaseOnly tests",
		"query SelectListOnly tests",
		"query ContainmentInOtherSubquery tests",
		"query BareContainmentInSubquery tests",
		"query UnionHalfGated tests",
		"query QuotedQualifiedPrivacy tests",
		"query QuotedBarePrivacy tests",
		"query BacktickQualifiedPrivacy tests",
		"query BacktickBarePrivacy tests",
		"query QuotedEverywherePrivacy tests",
		"query LeftJoinOnGate tests",
		"query LeftOuterJoinOnGate tests",
		"query FullJoinOnGate tests",
		"query RightJoinPrincipalOnRight tests",
		"query ExemptedButGated is exempted but already carries",
		"exempted query Vanished has no privacy_state",
	}
	assertFindings(t, findings, want)
	if len(findings) != len(want) {
		t.Errorf("want exactly %d findings, got %d:\n%v", len(want), len(findings), findings)
	}
	for _, clean := range []string{"query GatedBoth ", "query BareGated ", "query NoPrivacyAtAll ", "query Exempted ", "query LiteralInComment ", "query CommentedPrivacyOnly ", "query ReversedGated ", "query InListGated ", "query NotEqualGated ", "query LoudGated ", "query QuotedDashesGated ", "query OrElsewhereGated ", "query OrInJoinGated ", "query BothInSubquery ", "query UnionBothGated ", "query QuotedIdentifierPredicate ", "query QuotedEverywhereGated ", "query InnerJoinOnGate ", "query LeftJoinWhereGate ", "query BacktickGated ", "query QuotedBareGated "} {
		for _, f := range findings {
			if strings.Contains(f, clean) {
				t.Errorf("clean query flagged: %s", f)
			}
		}
	}
}
