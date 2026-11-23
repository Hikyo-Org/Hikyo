package lint

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func rawSQLFixtureRoot() string {
	return filepath.Join(moduleRoot(), "internal/lint/testdata/badrawsql")
}

// flagValue is the value of the flag with the given "-name=" prefix.
func flagValue(flags []string, prefix string) string {
	for _, flag := range flags {
		if value, ok := strings.CutPrefix(flag, prefix); ok {
			return value
		}
	}
	return ""
}

// rawSQLAggregate is the driver's whole-repository half of the RawSQL check for
// one run, judged against exactly the inputs the passes saw (the digest the run
// passed).
func rawSQLAggregate(out *vetOutcome, root string) ([]string, error) {
	specs, _, err := inputDeclaration(RawSQL.Name, root)
	if err != nil {
		return nil, err
	}
	in, err := OpenInputs(root, flagValue(out.Flags, "-rawsql.inputs="), specs)
	if err != nil {
		return nil, err
	}
	native := flagValue(out.Flags, "-rawsql.native=")
	if native == "" {
		native = runtime.GOOS
	}
	return rawSQLSummaryFindings(in, root, out.Context, native, out.Summaries[RawSQL.Name])
}

// The aggregate refuses a summary it cannot parse, a context it does not know
// and an inventory it cannot read, instead of judging nothing.
func TestRawSQLSummaryFindingsRefusals(t *testing.T) {
	root := rawSQLFixtureRoot()
	specs, _, err := inputDeclaration(RawSQL.Name, root)
	if err != nil {
		t.Fatal(err)
	}
	in, err := SnapshotInputs(root, specs)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, context, want string
		summaries           []Summary
	}{
		{"malformed caller summary", "default", "malformed rawcaller summary", []Summary{{Kind: summaryRawCaller, Subject: "only-one-field"}}},
		{"malformed unreached summary", "default", "malformed rawunreached summary", []Summary{{Kind: summaryRawUnreached, Subject: "a b c"}}},
		{"multi-field owner summary", "default", "malformed rawowner summary", []Summary{{Kind: summaryRawOwner, Subject: "a b"}}},
		{"multi-field helper summary", "default", "malformed rawhelper summary", []Summary{{Kind: summaryRawHelper, Subject: "a b"}}},
		{"unknown kind", "default", `unknown rawsql summary kind "rawbogus"`, []Summary{{Kind: "rawbogus", Subject: "a"}}},
		{"unknown context", "plan9", `unknown build context "plan9"`, nil},
		{"no context", "", "did not pass -rawsql.context", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := rawSQLSummaryFindings(in, root, tc.context, runtime.GOOS, tc.summaries); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one containing %q", err, tc.want)
			}
		})
	}
	// An inventory that is not under the root is refused, not read as empty.
	other := t.TempDir()
	otherIn, err := SnapshotInputs(other, specs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawSQLSummaryFindings(otherIn, other, "default", runtime.GOOS, nil); err == nil {
		t.Fatal("an absent inventory was accepted")
	}
}

// The pin decode refuses what json v2 refuses: unknown and duplicate members,
// malformed input and trailing content.
func TestParseReviewedPinsStrict(t *testing.T) {
	for name, source := range map[string]string{
		"unknown member":   `{"protocols":{},"extra":1}`,
		"duplicate member": `{"protocols":{},"protocols":{}}`,
		"trailing content": `{"protocols":{}} {}`,
		"malformed":        `{"protocols":`,
	} {
		if _, err := parseReviewedPins([]byte(source)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := parseReviewedPins([]byte(`{"protocols":{}}`)); err != nil {
		t.Errorf("an empty inventory was refused: %v", err)
	}
}

// An owner or helper key from a file name with a space stays one field.
func TestRawFieldsRoundTripKeysWithSpaces(t *testing.T) {
	const owner, helper = "/write protocol.go:Write", "/a%b c.go:help"
	protocols := map[string]RawSQLProtocol{owner: {Callers: map[string]string{"/x y.go:Call": "h"}, Dependencies: map[string]string{helper: "h"}}}
	summaries := []Summary{
		{Kind: summaryRawOwner, Subject: rawFields(owner)},
		{Kind: summaryRawHelper, Subject: rawFields(helper)},
		{Kind: summaryRawCaller, Subject: rawFields(owner, "/x y.go:Call")},
	}
	findings, err := rawSQLProtocolSummaryFindings(protocols, summaries)
	if err != nil || len(findings) != 0 {
		t.Fatalf("findings %v, error %v; want none", findings, err)
	}
	fields, err := splitRawFields(rawFields("dep", owner, helper, "hash"))
	if err != nil || !slices.Equal(fields, []string{"dep", owner, helper, "hash"}) {
		t.Fatalf("dump fields %q, error %v", fields, err)
	}
}
