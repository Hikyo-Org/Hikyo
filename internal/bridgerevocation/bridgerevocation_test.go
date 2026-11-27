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
// revocations it held are lost, so bridge routes refuse under off and with an unreachable
// endpoint. A verified online list replaces it whatever its age (lists are signed only when
// something is revoked), and it is the new floor.
func TestUnusableStoredStateRefusesUntilAVerifiedListReplacesIt(t *testing.T) {
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
			fetcher.list, fetcher.signature = signedListAt(t, f, testNow.Add(-bridgerevocation.MaxListAge-time.Hour), 2)
			result, err := online.Check(t.Context(), snapshot)
			if err != nil || result.Outcome != bridgerevocation.Stale || result.Sequence != 2 {
				t.Fatalf("a stale verified list replaces unusable state and is reported stale: %+v %v", result, err)
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

// A list may replace unusable state whatever its age, provided it is at or above a readable
// floor and, at the floor's sequence, has the floor's digest. Every list here is older than
// MaxListAge, so age decides none of the outcomes.
func TestAnOldListReplacesUnusableStateOnlyAtOrAboveAReadableFloor(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	bridge := releaseidentity.Hash([]byte("bridge"))
	old := testNow.Add(-bridgerevocation.MaxListAge - 30*24*time.Hour)
	setup := func(t *testing.T) (string, *scripted, bridgerevocation.Checker) {
		directory := t.TempDir()
		fetcher := &scripted{}
		fetcher.list, fetcher.signature = signedListAt(t, f, old, 5)
		checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
		if _, err := checker.Check(t.Context(), snapshot); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-store.json"), []byte("torn"), 0o600); err != nil {
			t.Fatal(err)
		}
		return directory, fetcher, checker
	}
	t.Run("below the floor is refused", func(t *testing.T) {
		_, fetcher, checker := setup(t)
		fetcher.list, fetcher.signature = signedListAt(t, f, old, 4)
		if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
			t.Fatalf("an old list below the floor must not replace unusable state: %v", err)
		}
	})
	t.Run("an unreadable floor is refused", func(t *testing.T) {
		directory, fetcher, checker := setup(t)
		if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-floor.json"), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		fetcher.list, fetcher.signature = signedListAt(t, f, old, 9)
		if _, err := checker.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) || !strings.Contains(err.Error(), "repair or remove") {
			t.Fatalf("an old list must not replace unusable state while the floor cannot be read: %v", err)
		}
	})
	t.Run("another digest at the floor's sequence is refused", func(t *testing.T) {
		_, fetcher, checker := setup(t)
		fetcher.list, fetcher.signature = signedListAt(t, f, old, 5, bridge)
		if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "equivocation") {
			t.Fatalf("an old list at the floor's sequence with another digest must be refused: %v", err)
		}
	})
	t.Run("the floor's own list replaces it", func(t *testing.T) {
		_, _, checker := setup(t)
		if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Outcome != bridgerevocation.Stale || result.Sequence != 5 {
			t.Fatalf("the list the floor records replaces unusable state, reported stale: %+v %v", result, err)
		}
	})
	t.Run("an old list above the floor replaces it", func(t *testing.T) {
		_, fetcher, checker := setup(t)
		fetcher.list, fetcher.signature = signedListAt(t, f, old, 7, bridge)
		result, err := checker.Check(t.Context(), snapshot)
		if err != nil || result.Outcome != bridgerevocation.Stale || result.Sequence != 7 || len(result.Revoked) != 1 {
			t.Fatalf("an old list above the floor replaces unusable state, reported stale: %+v %v", result, err)
		}
		fetcher.list, fetcher.signature = signedListAt(t, f, old, 6)
		if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
			t.Fatalf("the replacement is the new floor: %v", err)
		}
	})
}

// A writer that crashed after raising the floor to list 2 but before storing it leaves list 1
// behind floor 2. Lists are signed only when something is revoked, so the endpoint keeps
// serving that same, now stale, list 2: it is exactly what the floor records, so it recovers.
func TestACrashAfterTheFloorRecoversFromTheSameStaleList(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	stale := testNow.Add(-bridgerevocation.MaxListAge - 24*time.Hour)
	check := func(directory string, fetcher *scripted) (bridgerevocation.Result, error) {
		return (bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}).Check(t.Context(), snapshot)
	}
	two := &scripted{}
	two.list, two.signature = signedListAt(t, f, stale, 2)
	ahead := t.TempDir()
	if _, err := check(ahead, two); err != nil {
		t.Fatal(err)
	}
	one := &scripted{}
	one.list, one.signature = signedListAt(t, f, stale, 1)
	directory := t.TempDir()
	if _, err := check(directory, one); err != nil {
		t.Fatal(err)
	}
	floor, err := os.ReadFile(filepath.Join(ahead, "bridge-revocations-floor.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-floor.json"), floor, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := check(directory, &scripted{err: bridgerevocation.ErrUnreachable}); !errors.Is(err, bridgerevocation.ErrUnusableState) {
		t.Fatalf("the store behind the floor is unusable until a list replaces it: %v", err)
	}
	result, err := check(directory, two)
	if err != nil || result.Outcome != bridgerevocation.Stale || result.Sequence != 2 {
		t.Fatalf("the stale list the floor records must recover the crashed state: %+v %v", result, err)
	}
	if result, err := check(directory, &scripted{err: bridgerevocation.ErrUnreachable}); err != nil || result.Outcome != bridgerevocation.Unreachable || result.Sequence != 2 {
		t.Fatalf("after recovery an unreachable endpoint falls back to the stored list 2: %+v %v", result, err)
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
// refuse under off and when unreachable, and only a verified list for this root replaces it.
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
				t.Fatalf("a verified list for this root replaces it, whatever its sequence (no floor for this root yet): %+v %v", result, err)
			}
		})
	}
}

// Each trust root keeps its own floor. A floor recorded under another root (here in the
// earlier single-record form, which is read as the floor of the root it names) does not hold
// this root's lists to its sequence, and storing this root's list keeps that root's floor.
func TestFloorBoundToAnotherRootDoesNotApply(t *testing.T) {
	f := testfixture.New(t)
	other := testfixture.New(t)
	directory := t.TempDir()
	otherList, otherSignature := signedList(t, other, 9)
	floor := `{"sequence":9,"digest":"` + string(releaseidentity.Hash(otherList)) + `","root":"` + string(releaseidentity.Hash(other.Pinned.Root)) + `"}`
	if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-floor.json"), []byte(floor), 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &scripted{}
	fetcher.list, fetcher.signature = signedList(t, f, 1)
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	if result, err := checker.Check(t.Context(), f.Snapshot(t)); err != nil || result.Sequence != 1 {
		t.Fatalf("another root's floor must not refuse this root's first list: %+v %v", result, err)
	}
	otherChecker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: &scripted{}, Directory: directory, Now: func() time.Time { return testNow }}
	otherChecker.Fetcher.(*scripted).list, otherChecker.Fetcher.(*scripted).signature = signedListAt(t, other, testNow.Add(-90*24*time.Hour), 8)
	if _, err := otherChecker.Check(t.Context(), other.Snapshot(t)); err == nil || !strings.Contains(err.Error(), "rollback") || !strings.Contains(err.Error(), "floor (sequence 9)") {
		t.Fatalf("storing this root's list must keep the other root's legacy floor at 9: %v", err)
	}
	otherChecker.Fetcher.(*scripted).list, otherChecker.Fetcher.(*scripted).signature = otherList, otherSignature
	if result, err := otherChecker.Check(t.Context(), other.Snapshot(t)); err != nil || result.Sequence != 9 {
		t.Fatalf("the other root's own list at its floor replaces the state: %+v %v", result, err)
	}
}

// The earlier single-record floor file is this root's floor when it names this root: a stored
// list lost under it cannot be replaced by a lower one.
func TestALegacySingleFloorIsReadAsTheFloorOfItsRoot(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	directory := t.TempDir()
	five, fiveSignature := signedList(t, f, 5)
	floor := `{"sequence":5,"digest":"` + string(releaseidentity.Hash(five)) + `","root":"` + string(releaseidentity.Hash(f.Pinned.Root)) + `"}`
	if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-floor.json"), []byte(floor), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bridge-revocations-store.json"), []byte("torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &scripted{}
	fetcher.list, fetcher.signature = signedListAt(t, f, testNow.Add(-90*24*time.Hour), 4)
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") || !strings.Contains(err.Error(), "floor (sequence 5)") {
		t.Fatalf("a legacy floor at 5 must refuse list 4: %v", err)
	}
	fetcher.list, fetcher.signature = five, fiveSignature
	if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Sequence != 5 {
		t.Fatalf("the list the legacy floor records replaces the state: %+v %v", result, err)
	}
	fetcher.list, fetcher.signature = signedList(t, f, 4)
	if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
		t.Fatalf("the migrated floor still holds at 5: %v", err)
	}
}

// Two binaries pinning different roots share one state directory (for example a security
// rollback across a root rotation, then a bridge upgrade). Root B stored list 3, which revokes
// X; a root-A run stores an old A list. Back on root B, the old B list 1 must not replace the
// state and drop X: B's floor survives the A run, so B refuses until it fetches list 3 or above.
func TestARunUnderAnotherRootNeverLowersThisRootsFloor(t *testing.T) {
	a, b := testfixture.New(t), testfixture.New(t)
	x := releaseidentity.Hash([]byte("revoked bridge"))
	old := testNow.Add(-90 * 24 * time.Hour)
	// The A list's sequence is below B's floor in the reviewer's probe, and above it in the
	// second case, so a run that overwrote another root's record would lower B's floor too.
	for name, aSequence := range map[string]int64{"the A list is below B's floor": 1, "the A list is above B's floor": 5} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			check := func(mode bridgerevocation.Mode, f *testfixture.Fixture, list, signature []byte, err error) (bridgerevocation.Result, error) {
				fetcher := &scripted{list: list, signature: signature, err: err}
				return (bridgerevocation.Checker{Mode: mode, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}).Check(t.Context(), f.Snapshot(t))
			}
			b1, b1Signature := signedListAt(t, b, old, 1)
			b2, b2Signature := signedListAt(t, b, old, 2)
			b3, b3Signature := signedList(t, b, 3, x)
			if r, err := check(bridgerevocation.Online, b, b3, b3Signature, nil); err != nil || !bridgerevocation.Contains(r.Revoked, []releaseidentity.Digest{x}) {
				t.Fatalf("setup: B stores list 3 revoking X: %+v %v", r, err)
			}
			aList, aSignature := signedListAt(t, a, old, aSequence)
			if r, err := check(bridgerevocation.Online, a, aList, aSignature, nil); err != nil || r.Sequence != aSequence {
				t.Fatalf("the first run under root A has no floor and stores its list: %+v %v", r, err)
			}
			for _, low := range []struct{ list, signature []byte }{{b1, b1Signature}, {b2, b2Signature}} {
				if r, err := check(bridgerevocation.Online, b, low.list, low.signature, nil); err == nil || !strings.Contains(err.Error(), "rollback") || !strings.Contains(err.Error(), "floor (sequence 3)") {
					t.Fatalf("an old B list below B's floor must not replace the state and drop X: %+v %v", r, err)
				}
			}
			floorPath := filepath.Join(directory, "bridge-revocations-floor.json")
			if _, err := check(bridgerevocation.Off, b, nil, nil, nil); !errors.Is(err, bridgerevocation.ErrUnusableState) || !strings.Contains(err.Error(), "this trust root's sequence floor") || !strings.Contains(err.Error(), floorPath) {
				t.Fatalf("off, B must refuse the A list it finds, naming the floor: %v", err)
			}
			if _, err := check(bridgerevocation.Online, b, nil, nil, bridgerevocation.ErrUnreachable); !errors.Is(err, bridgerevocation.ErrUnusableState) || !strings.Contains(err.Error(), "unreachable") {
				t.Fatalf("unreachable, B must refuse until it fetches a list at or above its floor: %v", err)
			}
			r, err := check(bridgerevocation.Online, b, b3, b3Signature, nil)
			if err != nil || r.Sequence != 3 || !bridgerevocation.Contains(r.Revoked, []releaseidentity.Digest{x}) {
				t.Fatalf("B's list at its floor replaces the state and X is revoked again: %+v %v", r, err)
			}
		})
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
