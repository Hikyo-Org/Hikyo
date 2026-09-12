package parameters

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestEscapedReferencesRemainLiteral(t *testing.T) {
	for _, tc := range []struct {
		value, want string
		refs        []string
	}{
		{"$${UNKNOWN}", "${UNKNOWN}", nil},
		{"$${", "${", nil},
		{"$${not a name}", "${not a name}", nil},
		{"$${NAME}/${NAME}", "${NAME}/123", []string{"NAME"}},
		{"${NAME}", "123", []string{"NAME"}},
		{"$$${NAME}", "$${NAME}", nil},
		{"$$$${NAME}", "$$${NAME}", nil},
		{"$$", "$$", nil},
		{"$$plain$$", "$$plain$$", nil},
		{"$$/${NAME}/$$", "$$/123/$$", []string{"NAME"}},
	} {
		t.Run(tc.value, func(t *testing.T) {
			if err := CheckReferences(tc.value, map[string]string{"NAME": "[0-9]+"}); err != nil {
				t.Fatal(err)
			}
			refs, err := References(tc.value)
			if err != nil || !slices.Equal(refs, tc.refs) {
				t.Fatalf("references = %v, %v; want %v", refs, err, tc.refs)
			}
			got, err := Resolve(tc.value, map[string]string{"NAME": "123"}, 256)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	if _, err := Resolve("$${NAME}", nil, 6); err == nil {
		t.Fatal("escape bypassed output limit")
	}
	if err := CheckReferences("${unfinished", nil); err != nil {
		t.Fatal("unparameterized literal rejected", err)
	}
	if err := CheckReferences("${unfinished", map[string]string{"NAME": ".*"}); err == nil {
		t.Fatal("active malformed template accepted")
	}
}

func TestPatternCacheRemainsBoundedAndConcurrent(t *testing.T) {
	for n := 0; n < maxCachedPatterns+20; n++ {
		pattern := fmt.Sprintf("x{%d}", n)
		if err := Validate(map[string]string{"VALUE": pattern}, map[string]string{"VALUE": strings.Repeat("x", n)}); err != nil {
			t.Fatal(err)
		}
	}
	patternCache.Lock()
	size := len(patternCache.entries)
	patternCache.Unlock()
	if size > maxCachedPatterns {
		t.Fatalf("unbounded cache: %d", size)
	}
	for n := 0; n < 8; n++ {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Parallel()
			for i := 0; i < 20; i++ {
				if err := Validate(map[string]string{"VALUE": "[0-9]+"}, map[string]string{"VALUE": "123"}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
