package bridgerevocation_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

type scripted struct {
	list, signature []byte
	err             error
	calls           int
}

func (s *scripted) Fetch(context.Context) ([]byte, []byte, error) {
	s.calls++
	return s.list, s.signature, s.err
}

// testNow is the checker clock in the tests; lists are issued an hour before it
// unless a test says otherwise.
var testNow = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

func signedList(t *testing.T, f *testfixture.Fixture, sequence int64, revoked ...releaseidentity.Digest) ([]byte, []byte) {
	t.Helper()
	return signedListAt(t, f, testNow.Add(-time.Hour), sequence, revoked...)
}

func signedListAt(t *testing.T, f *testfixture.Fixture, issued time.Time, sequence int64, revoked ...releaseidentity.Digest) ([]byte, []byte) {
	t.Helper()
	raw := testfixture.JSON(t, releasetrust.RevocationList{Schema: releasetrust.RevocationSchema, Sequence: sequence, IssuedAt: issued.UTC().Format(time.RFC3339), TrustRootSHA256: releaseidentity.Hash(f.Pinned.Root), Revoked: revoked})
	return raw, testfixture.Sign(t, f.RecoverySigner, raw)
}

func TestCheckerFailsClosedOnEveryInvalidAnswerAndFallsBackOnlyWhenUnreachable(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	bridge := releaseidentity.Hash([]byte("bridge"))
	directory := t.TempDir()
	fetcher := &scripted{}
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}

	// Unreachable with nothing known: fall back to expiry, record it.
	fetcher.err = bridgerevocation.ErrUnreachable
	result, err := checker.Check(t.Context(), snapshot)
	if err != nil || result.Outcome != bridgerevocation.Unreachable || len(result.Revoked) != 0 {
		t.Fatalf("unreachable falls back to expiry: %+v %v", result, err)
	}
	outcome, _, at, err := bridgerevocation.LastCheck(directory)
	if err != nil || outcome != bridgerevocation.Unreachable || at != "2026-12-01T00:00:00Z" {
		t.Fatalf("the fallback is recorded: %v %v %v", outcome, at, err)
	}

	// A valid list at sequence 5 revokes the bridge and is remembered.
	fetcher.err = nil
	fetcher.list, fetcher.signature = signedList(t, f, 5, bridge)
	result, err = checker.Check(t.Context(), snapshot)
	if err != nil || result.Outcome != bridgerevocation.Checked || result.Sequence != 5 || !bridgerevocation.Contains(result.Revoked, []releaseidentity.Digest{bridge}) {
		t.Fatalf("valid list: %+v %v", result, err)
	}

	// Offline afterwards: the remembered list still withdraws the bridge.
	fetcher.err = bridgerevocation.ErrUnreachable
	result, err = checker.Check(t.Context(), snapshot)
	if err != nil || result.Outcome != bridgerevocation.Unreachable || !bridgerevocation.Contains(result.Revoked, []releaseidentity.Digest{bridge}) {
		t.Fatalf("an offline install keeps honoring the last verified list: %+v %v", result, err)
	}

	fetcher.err = nil
	remembered, err := os.ReadFile(filepath.Join(directory, "bridge-revocations-store.json"))
	if err != nil {
		t.Fatal(err)
	}
	other := testfixture.New(t)
	for name, tc := range map[string]struct {
		list, signature []byte
		fetchErr        error
		want            string
	}{
		"older sequence (rollback)":  {list: listBytes(t, f, 4), want: "rollback"},
		"same sequence, other bytes": {list: listBytes(t, f, 5), want: "equivocation"},
		"bad signature":              {list: listBytes(t, f, 6), signature: testfixture.Sign(t, other.RecoverySigner, listBytes(t, f, 6)), want: ""},
		"another trust root":         {list: listBytes(t, other, 9), want: "bound to a different trust root"},
		"malformed":                  {list: []byte("{"), want: ""},
		"invalid answer":             {fetchErr: errors.New("revocation endpoint answered status 403"), want: "status 403"},
	} {
		t.Run(name, func(t *testing.T) {
			fetcher.list, fetcher.signature, fetcher.err = tc.list, tc.signature, tc.fetchErr
			if fetcher.signature == nil && tc.list != nil {
				fetcher.signature = testfixture.Sign(t, f.RecoverySigner, tc.list)
			}
			if _, err := checker.Check(t.Context(), snapshot); err == nil || (tc.want != "" && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("want refusal %q, got %v", tc.want, err)
			}
			// A refusal never replaces the remembered list.
			stored, readErr := os.ReadFile(filepath.Join(directory, "bridge-revocations-store.json"))
			if readErr != nil || string(stored) != string(remembered) {
				t.Fatalf("the stored list changed after a refusal: %v", readErr)
			}
		})
	}

	// A newer valid list advances the floor.
	fetcher.list, fetcher.signature, fetcher.err = nil, nil, nil
	fetcher.list, fetcher.signature = signedList(t, f, 6)
	result, err = checker.Check(t.Context(), snapshot)
	if err != nil || result.Sequence != 6 || len(result.Revoked) != 0 {
		t.Fatalf("a newer list advances: %+v %v", result, err)
	}
	fetcher.list, fetcher.signature = signedList(t, f, 5, bridge)
	if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
		t.Fatalf("a replay of the older list is refused: %v", err)
	}

	// A corrupted stored list is ignored, not fatal: the valid list below replaces it.
	if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-store.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher.list, fetcher.signature = signedList(t, f, 7)
	if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Sequence != 7 {
		t.Fatalf("a valid list replaces a corrupt stored one: %+v %v", result, err)
	}
}

// Stored state that exists but does not verify (a list beside another list's signature, a
// damaged file, a list signed by another key) must never widen admission: the
// revocations it held are lost, so bridge routes refuse under off, unreachable and stale.
// Only a fresh, non-stale, verified online list replaces it, and it keeps the recorded floor.
func TestUnusableStoredStateRefusesUntilAFreshVerifiedListReplacesIt(t *testing.T) {
	f := testfixture.New(t)
	other := testfixture.New(t)
	snapshot := f.Snapshot(t)
	bridge := releaseidentity.Hash([]byte("bridge"))
	tornList, _ := signedList(t, f, 3, bridge)
	_, tornSignature := signedList(t, f, 4)
	otherList, otherSignature := signedList(t, other, 9)
	for name, files := range map[string]map[string][]byte{
		"torn pair (list N beside signature N+1)": {"bridge-revocations-store.json": storeBytes(t, tornList, tornSignature)},
		"list without its signature":              {"bridge-revocations-store.json": storeBytes(t, tornList, nil)},
		"signed by another trust root's key":      {"bridge-revocations-store.json": storeBytes(t, otherList, otherSignature)},
		"damaged combined file":                   {"bridge-revocations-store.json": []byte("not json")},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			for file, raw := range files {
				if err := os.WriteFile(filepath.Join(directory, file), raw, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
			if _, err := off.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) || !strings.Contains(err.Error(), "remove") {
				t.Fatalf("off must refuse with the operator way out: %v", err)
			}
			fetcher := &scripted{err: bridgerevocation.ErrUnreachable}
			online := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
			if _, err := online.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) {
				t.Fatalf("an unreachable endpoint must refuse, not fall back: %v", err)
			}
			fetcher.err = nil
			fetcher.list, fetcher.signature = signedListAt(t, f, testNow.Add(-bridgerevocation.MaxListAge-time.Hour), 8)
			// The endpoint answered, and lists are not re-signed on a schedule, so the advice is to
			// wait for a newer list or remove the files, never to restore access.
			if _, err := online.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) || strings.Contains(err.Error(), "Restore access") || !strings.Contains(err.Error(), "wait for the release owner to sign and publish a newer list") || !strings.Contains(err.Error(), filepath.Join(directory, "bridge-revocations-floor.json")) || !strings.Contains(err.Error(), "accepts any validly signed list the endpoint serves, even an older one") {
				t.Fatalf("a stale list must not replace unusable state, with advice that can work: %v", err)
			}
			fetcher.list, fetcher.signature = signedList(t, f, 2)
			result, err := online.Check(t.Context(), snapshot)
			if err != nil || result.Outcome != bridgerevocation.Checked || result.Sequence != 2 {
				t.Fatalf("a fresh verified list replaces unusable state: %+v %v", result, err)
			}
			fetcher.list, fetcher.signature = signedList(t, f, 1)
			if _, err := online.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
				t.Fatalf("the replacement list is the new floor: %v", err)
			}
		})
	}
}

// The last check's recorded sequence is the floor still available once the stored list is
// unusable: a validly signed older list cannot replace it.
func TestUnusableStoredStateKeepsTheRecordedFloor(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	directory := t.TempDir()
	fetcher := &scripted{}
	fetcher.list, fetcher.signature = signedList(t, f, 5)
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	if _, err := checker.Check(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-store.json"), []byte("torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher.list, fetcher.signature = signedList(t, f, 4)
	if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
		t.Fatalf("a list below the recorded floor cannot replace unusable state: %v", err)
	}
	fetcher.list, fetcher.signature = signedList(t, f, 5)
	if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Sequence != 5 {
		t.Fatalf("a list at the floor replaces it: %+v %v", result, err)
	}
}

// The floor is its own monotonic record, written only when a list is stored: the outcome log
// can hold any sequence without lowering it, a different list at the floor's sequence is
// equivocation, a missing record is a first run, and an unreadable one refuses.
func TestSequenceFloorIsAMonotonicRecordSeparateFromTheOutcomeLog(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	bridge := releaseidentity.Hash([]byte("bridge"))
	setup := func(t *testing.T) (string, *scripted, bridgerevocation.Checker) {
		directory := t.TempDir()
		fetcher := &scripted{}
		fetcher.list, fetcher.signature = signedList(t, f, 5)
		checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
		if _, err := checker.Check(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}
		// The outcome log is overwritten by hand with a lower sequence, as a stale or
		// unreachable record would; no second check runs, so only persist wrote the floor.
		if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-check.json"), []byte(`{"outcome":"stale-fell-back-to-expiry","sequence":1,"at":"2026-01-01T00:00:00Z"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-store.json"), []byte("torn"), 0o600); err != nil {
			t.Fatal(err)
		}
		return directory, fetcher, checker
	}
	t.Run("a lower sequence never replaces it, whatever the outcome log says", func(t *testing.T) {
		_, fetcher, checker := setup(t)
		fetcher.list, fetcher.signature = signedList(t, f, 3)
		if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
			t.Fatalf("below the floor: %v", err)
		}
	})
	t.Run("the same sequence with a different list is equivocation", func(t *testing.T) {
		_, fetcher, checker := setup(t)
		fetcher.list, fetcher.signature = signedList(t, f, 5, bridge)
		if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "equivocation") {
			t.Fatalf("same sequence, other digest: %v", err)
		}
		fetcher.list, fetcher.signature = signedList(t, f, 5)
		if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Sequence != 5 {
			t.Fatalf("the same list at the floor replaces it: %+v %v", result, err)
		}
	})
	t.Run("a list bound to an older root does not lower it", func(t *testing.T) {
		directory, fetcher, checker := setup(t)
		other := testfixture.New(t)
		raw := testfixture.JSON(t, releasetrust.RevocationList{Schema: releasetrust.RevocationSchema, Sequence: 2, IssuedAt: testNow.Add(-time.Hour).UTC().Format(time.RFC3339), TrustRootSHA256: releaseidentity.Hash(other.Pinned.Root)})
		if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-store.json"), storeBytes(t, raw, testfixture.Sign(t, f.RecoverySigner, raw)), 0o600); err != nil {
			t.Fatal(err)
		}
		fetcher.list, fetcher.signature = signedList(t, f, 2)
		if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
			t.Fatalf("a list below the floor is refused even when the stored one is from an older root: %v", err)
		}
		fetcher.list, fetcher.signature = signedList(t, f, 5)
		if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Sequence != 5 {
			t.Fatalf("a list at the floor replaces it: %+v %v", result, err)
		}
	})
	t.Run("an absent floor record is a first run", func(t *testing.T) {
		directory, fetcher, checker := setup(t)
		if err := os.Remove(filepath.Join(directory, "bridge-revocations-floor.json")); err != nil {
			t.Fatal(err)
		}
		fetcher.list, fetcher.signature = signedList(t, f, 3)
		if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Sequence != 3 {
			t.Fatalf("no floor record, nothing to compare: %+v %v", result, err)
		}
	})
	t.Run("a corrupt floor record refuses", func(t *testing.T) {
		directory, fetcher, checker := setup(t)
		if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-floor.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		fetcher.list, fetcher.signature = signedList(t, f, 9)
		if _, err := checker.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) {
			t.Fatalf("an unreadable floor must refuse even a valid newer list: %v", err)
		}
	})
	t.Run("a stored list from before the floor existed gets a floor", func(t *testing.T) {
		directory := t.TempDir()
		list, signature := signedList(t, f, 5)
		if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-store.json"), storeBytes(t, list, signature), 0o600); err != nil {
			t.Fatal(err)
		}
		off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
		if _, err := off.Check(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-store.json"), []byte("torn"), 0o600); err != nil {
			t.Fatal(err)
		}
		fetcher := &scripted{}
		fetcher.list, fetcher.signature = signedList(t, f, 4)
		online := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
		if _, err := online.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
			t.Fatalf("the migrated floor holds: %v", err)
		}
	})
}

// A list bound to another trust root (after a root rotation, correctly signed or not) is
// never applied, and its absence of revocations is not an answer either: bridge routes
// refuse under off and when unreachable, and only a fresh list for this root replaces it.
func TestListBoundToAnotherRootIsUnusableUntilAFreshListReplacesIt(t *testing.T) {
	f := testfixture.New(t)
	other := testfixture.New(t)
	snapshot := f.Snapshot(t)
	raw := testfixture.JSON(t, releasetrust.RevocationList{Schema: releasetrust.RevocationSchema, Sequence: 3, IssuedAt: testNow.Add(-time.Hour).UTC().Format(time.RFC3339), TrustRootSHA256: releaseidentity.Hash(other.Pinned.Root)})
	for name, signature := range map[string][]byte{
		"signed by the current recovery key": testfixture.Sign(t, f.RecoverySigner, raw),
		"signed by another key":              testfixture.Sign(t, other.RecoverySigner, raw),
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-store.json"), storeBytes(t, raw, signature), 0o600); err != nil {
				t.Fatal(err)
			}
			off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
			if _, err := off.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) {
				t.Fatalf("off must refuse: %v", err)
			}
			fetcher := &scripted{err: bridgerevocation.ErrUnreachable}
			online := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
			if _, err := online.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) {
				t.Fatalf("an unreachable endpoint must refuse: %v", err)
			}
			fetcher.err = nil
			fetcher.list, fetcher.signature = signedList(t, f, 1)
			if result, err := online.Check(t.Context(), snapshot); err != nil || result.Sequence != 1 {
				t.Fatalf("a fresh list for this root replaces it, whatever its sequence: %+v %v", result, err)
			}
		})
	}
}

// The floor is bound to the trust root it was recorded under: after a rotation it does not
// hold the new root's lists to the old root's sequence.
func TestFloorBoundToAnotherRootDoesNotApply(t *testing.T) {
	f := testfixture.New(t)
	other := testfixture.New(t)
	directory := t.TempDir()
	digest := releaseidentity.Hash([]byte("old list"))
	floor := `{"sequence":9,"digest":"` + string(digest) + `","root":"` + string(releaseidentity.Hash(other.Pinned.Root)) + `"}`
	if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-floor.json"), []byte(floor), 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &scripted{}
	fetcher.list, fetcher.signature = signedList(t, f, 1)
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	if result, err := checker.Check(t.Context(), f.Snapshot(t)); err != nil || result.Sequence != 1 {
		t.Fatalf("another root's floor must not refuse this root's first list: %+v %v", result, err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, "bridge-revocations-floor.json"))
	if err != nil || !strings.Contains(string(raw), string(releaseidentity.Hash(f.Pinned.Root))) || !strings.Contains(string(raw), `"sequence":1`) {
		t.Fatalf("the floor is rebound to this root: %s %v", raw, err)
	}
}

// The floor holds whenever it exists, not only when the stored list is damaged: every way the
// store can fall behind it is unusable, and nothing below it can take the store's place.
func TestFloorIsEnforcedWheneverItIsPresent(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	bridge := releaseidentity.Hash([]byte("bridge"))
	accept := func(t *testing.T, sequence int64, revoked ...releaseidentity.Digest) (string, bridgerevocation.Checker, *scripted) {
		directory := t.TempDir()
		fetcher := &scripted{}
		fetcher.list, fetcher.signature = signedList(t, f, sequence, revoked...)
		checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
		if _, err := checker.Check(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}
		return directory, checker, fetcher
	}
	store := func(directory string) string { return filepath.Join(directory, "bridge-revocations-store.json") }
	t.Run("a removed store with a floor refuses a lower list", func(t *testing.T) {
		directory, checker, fetcher := accept(t, 5, bridge)
		if err := os.Remove(store(directory)); err != nil {
			t.Fatal(err)
		}
		fetcher.list, fetcher.signature = signedList(t, f, 3)
		if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
			t.Fatalf("sequence 3 would hide the revocation at 5: %v", err)
		}
		off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
		if _, err := off.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) {
			t.Fatalf("with the store gone and the floor present, off must refuse: %v", err)
		}
		fetcher.err = bridgerevocation.ErrUnreachable
		if _, err := checker.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) {
			t.Fatalf("with the store gone and the floor present, unreachable must refuse: %v", err)
		}
		fetcher.err = nil
		fetcher.list, fetcher.signature = signedList(t, f, 5, bridge)
		if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Sequence != 5 || !bridgerevocation.Contains(result.Revoked, []releaseidentity.Digest{bridge}) {
			t.Fatalf("the floor's own list restores the store: %+v %v", result, err)
		}
	})
	t.Run("a removed store refuses a stale list as well", func(t *testing.T) {
		directory, checker, fetcher := accept(t, 5, bridge)
		if err := os.Remove(store(directory)); err != nil {
			t.Fatal(err)
		}
		fetcher.list, fetcher.signature = signedListAt(t, f, testNow.Add(-bridgerevocation.MaxListAge-time.Hour), 2)
		if _, err := checker.Check(t.Context(), snapshot); err == nil {
			t.Fatal("a stale list at sequence 2 must not replace a store the floor says is gone")
		}
	})
	t.Run("a store behind the floor (crash between the two writes) refuses a list below the floor", func(t *testing.T) {
		directory, checker, fetcher := accept(t, 3)
		older, err := os.ReadFile(store(directory))
		if err != nil {
			t.Fatal(err)
		}
		fetcher.list, fetcher.signature = signedList(t, f, 5, bridge)
		if _, err := checker.Check(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}
		// The crash: the floor reached 5, the store is still the list at 3.
		if err := os.WriteFile(store(directory), older, 0o600); err != nil {
			t.Fatal(err)
		}
		off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
		if _, err := off.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) {
			t.Fatalf("a store behind its floor must refuse, not apply the list at 3: %v", err)
		}
		fetcher.list, fetcher.signature = signedList(t, f, 4)
		if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
			t.Fatalf("sequence 4 is below the floor at 5: %v", err)
		}
		fetcher.list, fetcher.signature = signedList(t, f, 5, bridge)
		if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Sequence != 5 {
			t.Fatalf("the list at the floor restores the store: %+v %v", result, err)
		}
	})
	t.Run("a store that differs from the floor at the same sequence refuses", func(t *testing.T) {
		directory, _, _ := accept(t, 5, bridge)
		other := &scripted{}
		other.list, other.signature = signedList(t, f, 5)
		// Swap in a validly signed list for the same sequence, as a tampered store would be.
		replacement := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: other, Directory: t.TempDir(), Now: func() time.Time { return testNow }}
		if _, err := replacement.Check(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}
		tampered, err := os.ReadFile(store(replacement.Directory))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(store(directory), tampered, 0o600); err != nil {
			t.Fatal(err)
		}
		off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
		if _, err := off.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) {
			t.Fatalf("a list that is not the floor's at the floor's sequence must refuse: %v", err)
		}
	})
	t.Run("the way out names the store and the floor file", func(t *testing.T) {
		directory, _, _ := accept(t, 5)
		if err := os.Remove(store(directory)); err != nil {
			t.Fatal(err)
		}
		off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
		_, err := off.Check(t.Context(), snapshot)
		if err == nil || !strings.Contains(err.Error(), "bridge-revocations-store.json") || !strings.Contains(err.Error(), "bridge-revocations-floor.json") {
			t.Fatalf("the operator way out must name both files: %v", err)
		}
	})
}

// The list and its signature are one file, so no state can hold one list beside another's signature.
func TestAcceptedListIsStoredAsOneFile(t *testing.T) {
	f := testfixture.New(t)
	directory := t.TempDir()
	fetcher := &scripted{}
	fetcher.list, fetcher.signature = signedList(t, f, 1)
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	if _, err := checker.Check(t.Context(), f.Snapshot(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(directory, "bridge-revocations-store.json")); err != nil {
		t.Fatal(err)
	}
}

// A crash while writing leaves only an unrenamed temporary file, which no reader consults,
// and a completed store leaves none.
func TestCrashLeavesAUsableStoreAndNoTemporaryFiles(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".revocation-12345"), []byte("half written"), 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &scripted{}
	fetcher.list, fetcher.signature = signedList(t, f, 6)
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	if _, err := checker.Check(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
	if result, err := off.Check(t.Context(), snapshot); err != nil || result.Sequence != 6 {
		t.Fatalf("the store is usable beside a crashed temporary file: %+v %v", result, err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	temporaries := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".revocation-") && entry.Name() != ".revocation-12345" {
			temporaries++
		}
	}
	if temporaries != 0 {
		t.Fatalf("a completed store leaves %d temporary files", temporaries)
	}
}

// storeBytes is the single-file store a Checker keeps: the list and its signature together.
func storeBytes(t *testing.T, list, signature []byte) []byte {
	t.Helper()
	return testfixture.JSON(t, struct {
		List      []byte `json:"list"`
		Signature []byte `json:"signature"`
	}{list, signature})
}

func listBytes(t *testing.T, f *testfixture.Fixture, sequence int64) []byte {
	raw, _ := signedList(t, f, sequence)
	return raw
}

func TestOperatorCanDisableTheNetworkCheckAndItIsRecorded(t *testing.T) {
	f := testfixture.New(t)
	fetcher := &scripted{err: errors.New("must not be called")}
	directory := t.TempDir()
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Off, Fetcher: fetcher, Directory: directory}
	result, err := checker.Check(t.Context(), f.Snapshot(t))
	if err != nil || result.Outcome != bridgerevocation.Disabled || fetcher.calls != 0 {
		t.Fatalf("off skips the fetch: %+v %v calls=%d", result, err, fetcher.calls)
	}
	if outcome, _, _, err := bridgerevocation.LastCheck(directory); err != nil || outcome != bridgerevocation.Disabled {
		t.Fatalf("the opt-out is recorded: %v %v", outcome, err)
	}
	if _, err := bridgerevocation.ParseMode("sometimes"); err == nil {
		t.Fatal("an unknown mode must be refused")
	}
	if mode, err := bridgerevocation.ParseMode(""); err != nil || mode != bridgerevocation.Online {
		t.Fatalf("the default is online: %v %v", mode, err)
	}
}

// Opting out of the network check does not unlearn a revocation already stored.
func TestOffStillAppliesAStoredRevocation(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	bridge := releaseidentity.Hash([]byte("bridge"))
	directory := t.TempDir()
	list, signature := signedList(t, f, 3, bridge)
	online := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: &scripted{list: list, signature: signature}, Directory: directory, Now: func() time.Time { return testNow }}
	if _, err := online.Check(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	fetcher := &scripted{err: errors.New("must not be called")}
	off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	revoked, err := off.Revoked(t.Context(), snapshot)
	if err != nil || fetcher.calls != 0 || !bridgerevocation.Contains(revoked, []releaseidentity.Digest{bridge}) {
		t.Fatalf("a stored revocation must still withdraw the bridge with the check off: %v %v calls=%d", revoked, err, fetcher.calls)
	}
}
