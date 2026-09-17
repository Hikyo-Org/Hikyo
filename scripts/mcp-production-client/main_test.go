package main

import (
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
)

// TestCheckCatalogRequiresTheExactClosedSet pins that the production probe
// accepts only the complete read catalog or the complete read-plus-write
// catalog: an incomplete, duplicated, or unknown-name catalog of the right
// size must fail, so the probe can never report five read calls it did not make.
func TestCheckCatalogRequiresTheExactClosedSet(t *testing.T) {
	reads, all := mcpserver.ProductionToolNames(), mcpserver.AllToolNames()
	writes := mcpserver.WriteToolNames()
	if len(reads) != 5 || len(writes) != 2 {
		t.Fatalf("catalog sizes = %d reads, %d writes", len(reads), len(writes))
	}
	shuffledAll := slices.Clone(all)
	slices.Reverse(shuffledAll)
	for name, tc := range map[string]struct {
		names []string
		ok    bool
	}{
		"complete read catalog":                         {reads, true},
		"complete read-plus-write catalog":              {all, true},
		"read-plus-write in another order":              {shuffledAll, true},
		"five entries mixing reads and writes":          {append(slices.Clone(reads[:3]), writes...), false},
		"five entries with a duplicate read":            {append(slices.Clone(reads[:4]), reads[0]), false},
		"five entries with an unknown name":             {append(slices.Clone(reads[:4]), "hikyo_publish"), false},
		"seven entries with a duplicate instead of one": {append(slices.Clone(all[:6]), all[0]), false},
		"write tools only":                              {writes, false},
		"empty":                                         {nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkCatalog(tc.names); (err == nil) != tc.ok {
				t.Fatalf("checkCatalog(%v) = %v, want ok=%v", tc.names, err, tc.ok)
			}
		})
	}
}
