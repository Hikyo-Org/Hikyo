package lint

import (
	"slices"
	"strings"
	"testing"
)

func TestSQLSplitSharesLiteralBoundaries(t *testing.T) {
	for _, literal := range []string{`'a OR b''c'`, `E'a\' OR b'`, `$tag$a OR (b)$tag$`, `"a OR b"`} {
		source := "name=" + literal + " AND id=?"
		got, ok := splitSQLTop(source, " AND ")
		if !ok || !slices.Equal(got, []string{"name=" + literal, "id=?"}) {
			t.Fatalf("split %q: %v %t", source, got, ok)
		}
		if strings.Contains(maskSQLContractLiteralsAndComments(source), " OR ") {
			t.Fatalf("literal escaped mask: %s", source)
		}
		if reason := unsupportedPredicateLexeme("postgres", source); reason != "" {
			t.Fatalf("literal refused: %s: %s", source, reason)
		}
	}
	for _, source := range []string{`name='unclosed`, `name=$tag$unclosed`, `a=? /* comment */ AND b=?`, `a=? -- comment`} {
		if _, ok := splitSQLTop(source, " AND "); ok {
			t.Fatalf("unsupported split accepted: %s", source)
		}
	}
}
