package crypto

import (
	"context"
	"testing"
)

// TestProjectDEKHAFreshnessRevalidatesAfterCrossNodeRotation models a
// rotate-dek performed on another node: the shared store gains a new active DEK
// version while this node's cache still holds the old one. Without HA freshness
// the stale cache is served (the single-node contract); with it on, the fetch
// revalidates against the store and rebuilds, so this node seals under and can
// open records at the new active version rather than fencing every write.
func TestProjectDEKHAFreshnessRevalidatesAfterCrossNodeRotation(t *testing.T) {
	ctx := context.Background()
	ks := newMemStore()
	kr, err := LoadKeyring(ctx, ks, newRoot(t))
	if err != nil {
		t.Fatal(err)
	}

	s1, err := kr.ForProject(ctx, "org_1", "prj_1")
	if err != nil {
		t.Fatal(err)
	}
	if s1.ActiveVersion() != 1 {
		t.Fatalf("first active version = %d, want 1", s1.ActiveVersion())
	}

	// Mint the next version and commit it to the SHARED store directly, as
	// another node's rotate-dek would, without evicting this node's cache.
	row, _, err := kr.PrepareProjectDEKRotation(ctx, "org_1", "prj_1")
	if err != nil {
		t.Fatal(err)
	}
	scope := t3key(PurposeProject, "org_1", "prj_1")
	ks.mu.Lock()
	ks.tier3[scope] = append(ks.tier3[scope], row)
	ks.mu.Unlock()

	// Single-node contract: the cache is authoritative, so the stale set is
	// still served.
	s2, err := kr.ForProject(ctx, "org_1", "prj_1")
	if err != nil {
		t.Fatal(err)
	}
	if s2.ActiveVersion() != 1 {
		t.Fatalf("without HA freshness active version = %d, want the cached 1", s2.ActiveVersion())
	}

	// Under HA the fetch revalidates and rebuilds to the new active version.
	kr.SetHAFreshness(true)
	s3, err := kr.ForProject(ctx, "org_1", "prj_1")
	if err != nil {
		t.Fatal(err)
	}
	if s3.ActiveVersion() != row.Version {
		t.Fatalf("with HA freshness active version = %d, want the rotated %d", s3.ActiveVersion(), row.Version)
	}
}
