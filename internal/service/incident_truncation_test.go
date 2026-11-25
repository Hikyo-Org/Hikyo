package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Past the source cap the unreached members share one scope-level row, so the
// ledger stays bounded and the cut is still visible. A member already reached,
// and a repeated candidate, get no row.
func TestTruncateUnreachedRotationsBoundsTheRowsAtTheSourceCap(t *testing.T) {
	scope := domain.Scope{Org: "org_a"}
	reached := map[domain.PrincipalID]bool{"usr_reached": true}
	rest := []domain.PrincipalID{"usr_reached", "usr_machine", "usr_dup", "usr_dup"}
	for i := 0; i < maxSourceItems+5; i++ {
		rest = append(rest, domain.PrincipalID(fmt.Sprintf("usr_%05d", i)))
	}
	c := newCollector(0)
	// A principal that is no account (usr_machine) never gets a row and takes no slot.
	isAccount := func(p domain.PrincipalID) (bool, error) { return p != "usr_machine", nil }
	if err := c.truncateUnreachedRotations(rest, reached, "org_a", scope, isAccount); err != nil {
		t.Fatal(err)
	}

	var members, scopeRows int
	seen := map[string]bool{}
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
			if seen[cand.id] {
				t.Errorf("member %s got more than one truncation row", cand.id)
			}
			seen[cand.id] = true
		}
	}
	// The rows name exactly the first maxSourceItems unreached members, each once
	// (usr_dup is a repeated candidate and takes one slot).
	want := map[string]bool{"usr_dup": true}
	for i := 0; len(want) < maxSourceItems; i++ {
		want[fmt.Sprintf("usr_%05d", i)] = true
	}
	for id := range want {
		if !seen[id] {
			t.Errorf("expected member %s has no truncation row", id)
		}
	}
	for id := range seen {
		if !want[id] {
			t.Errorf("unexpected member %s has a truncation row", id)
		}
	}
	if members != maxSourceItems || scopeRows != 1 {
		t.Fatalf("rows: %d member rows and %d scope rows, want %d and 1", members, scopeRows, maxSourceItems)
	}
}

// The rotation cap is the source's, not each member's: members that together hold
// more than the cap list at most the cap, and the cut is one scope row.
func TestRotationListerCapsTheWholeSourceAcrossMembers(t *testing.T) {
	const perMember = maxSourceItems/2 + 300
	read := func(_ context.Context, _ authz.Proof, target domain.PrincipalID, after string, limit int) ([]store.RotationAuthorityRef, error) {
		var out []store.RotationAuthorityRef
		for i := 0; i < perMember && len(out) < limit; i++ {
			id := fmt.Sprintf("rot_%s_%05d", target, i)
			if id <= after {
				continue
			}
			out = append(out, store.RotationAuthorityRef{ID: id, OrgID: "org_a"})
		}
		return out, nil
	}
	l := rotationLister{read: read, scope: domain.Scope{Org: "org_a"}}
	c := newCollector(1 << 20)
	a := incidentAccess{scopeID: "org_a"}
	for _, p := range []domain.PrincipalID{"usr_one", "usr_two", "usr_three"} {
		if err := l.page(t.Context(), a, p, c); err != nil {
			t.Fatal(err)
		}
	}
	var rotations, cuts int
	for _, cand := range c.cands {
		switch {
		case cand.kind == domain.ObjectKindRotation:
			rotations++
		case cand.reason == domain.ReasonEnumerationTruncated && cand.id == "org_a":
			cuts++
		}
	}
	if rotations != maxSourceItems || cuts != 1 {
		t.Fatalf("listed %d rotations and %d scope cuts, want %d and 1", rotations, cuts, maxSourceItems)
	}
}
