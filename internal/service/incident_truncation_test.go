package service

import (
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// Past the source cap the unreached members share one scope-level row, so the
// ledger stays bounded and the cut is still visible. A member already reached,
// and a repeated candidate, get no row.
func TestTruncateUnreachedRotationsBoundsTheRowsAtTheSourceCap(t *testing.T) {
	scope := domain.Scope{Org: "org_a"}
	reached := map[domain.PrincipalID]bool{"usr_reached": true}
	rest := []domain.PrincipalID{"usr_reached", "usr_dup", "usr_dup"}
	for i := 0; i < maxSourceItems+5; i++ {
		rest = append(rest, domain.PrincipalID(fmt.Sprintf("usr_%05d", i)))
	}
	c := newCollector(0)
	c.truncateUnreachedRotations(rest, reached, "org_a", scope)

	var members, scopeRows int
	for _, cand := range c.cands {
		if cand.owner != domain.OwnerRotationHalt || cand.reason != domain.ReasonEnumerationTruncated {
			t.Fatalf("unexpected candidate %+v", cand)
		}
		switch cand.id {
		case "org_a":
			scopeRows++
		case "usr_reached":
			t.Errorf("a reached member got a truncation row")
		default:
			members++
		}
	}
	if members != maxSourceItems || scopeRows != 1 {
		t.Fatalf("rows: %d member rows and %d scope rows, want %d and 1", members, scopeRows, maxSourceItems)
	}
}
