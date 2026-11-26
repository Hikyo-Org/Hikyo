package api

import "testing"

// The web client computes the same digest (web/src/api/incidents.test.ts pins this vector).
func TestIncidentItemsDigestIsOrderIndependentAndPinned(t *testing.T) {
	t.Parallel()
	const want = "3dbcd4b098200b219ec0411f546330e8a2430e5baed3762ecb0a6280a4f5d8a4"
	if got := IncidentItemsDigest([]string{"itm_b", "itm_a"}); got != want {
		t.Fatalf("digest = %s, want %s", got, want)
	}
	if IncidentItemsDigest([]string{"itm_a"}) == want {
		t.Fatal("a different set produced the same digest")
	}
}
