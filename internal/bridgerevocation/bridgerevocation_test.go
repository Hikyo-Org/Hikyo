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

// Stored state that exists but does not verify (a torn two-file write from an earlier
// release, a damaged file, a list signed by another key) must never widen admission: the
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
		"torn pair (list N beside signature N+1)": {"bridge-revocations.json": tornList, "bridge-revocations.sigstore.json": tornSignature},
		"list without its signature":              {"bridge-revocations.json": tornList},
		"signed by another trust root's key":      {"bridge-revocations.json": otherList, "bridge-revocations.sigstore.json": otherSignature},
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
			if _, err := online.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) {
				t.Fatalf("a stale list must not replace unusable state: %v", err)
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

// After a root rotation a list that is correctly signed but bound to the previous root is
// tolerated as "none"; the same list under a wrong signature is unusable.
func TestListBoundToAnotherRootIsToleratedOnlyWhenCorrectlySigned(t *testing.T) {
	f := testfixture.New(t)
	other := testfixture.New(t)
	snapshot := f.Snapshot(t)
	raw := testfixture.JSON(t, releasetrust.RevocationList{Schema: releasetrust.RevocationSchema, Sequence: 3, IssuedAt: testNow.Add(-time.Hour).UTC().Format(time.RFC3339), TrustRootSHA256: releaseidentity.Hash(other.Pinned.Root)})
	for name, tc := range map[string]struct {
		signature []byte
		tolerated bool
	}{
		"signed by the current recovery key": {testfixture.Sign(t, f.RecoverySigner, raw), true},
		"signed by another key":              {testfixture.Sign(t, other.RecoverySigner, raw), false},
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			for file, content := range map[string][]byte{"bridge-revocations.json": raw, "bridge-revocations.sigstore.json": tc.signature} {
				if err := os.WriteFile(filepath.Join(directory, file), content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
			_, err := off.Check(t.Context(), snapshot)
			if tc.tolerated != (err == nil) || (!tc.tolerated && !errors.Is(err, bridgerevocation.ErrUnusableState)) {
				t.Fatalf("tolerated=%v, got %v", tc.tolerated, err)
			}
		})
	}
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
	for _, gone := range []string{"bridge-revocations.json", "bridge-revocations.sigstore.json"} {
		if _, err := os.Stat(filepath.Join(directory, gone)); err == nil {
			t.Fatalf("%s is the superseded two-file form and must not be written", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(directory, "bridge-revocations-store.json")); err != nil {
		t.Fatal(err)
	}
}

// An installation upgraded from the two-file form keeps its sequence floor, and the next
// accepted list moves it to the single file and removes the pair.
func TestLegacyTwoFileStateIsReadAndMigrated(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	directory := t.TempDir()
	list, signature := signedList(t, f, 5)
	for name, raw := range map[string][]byte{"bridge-revocations.json": list, "bridge-revocations.sigstore.json": signature} {
		if err := os.WriteFile(filepath.Join(directory, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fetcher := &scripted{}
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	fetcher.list, fetcher.signature = signedList(t, f, 4)
	if _, err := checker.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
		t.Fatalf("the earlier form's floor still refuses a lower sequence: %v", err)
	}
	fetcher.list, fetcher.signature = signedList(t, f, 6)
	if result, err := checker.Check(t.Context(), snapshot); err != nil || result.Sequence != 6 {
		t.Fatalf("a newer list is accepted: %+v %v", result, err)
	}
	for _, gone := range []string{"bridge-revocations.json", "bridge-revocations.sigstore.json"} {
		if _, err := os.Stat(filepath.Join(directory, gone)); err == nil {
			t.Fatalf("%s must be removed once the single file holds the list", gone)
		}
	}
}

// A crash after the single-file store is durable but before the earlier pair is removed
// leaves both: the store wins and keeps the floor. A crash while writing leaves only an
// unrenamed temporary file, which no reader consults, and a completed store leaves none.
func TestCrashLeavesAUsableStoreAndNoTemporaryFiles(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	directory := t.TempDir()
	oldList, oldSignature := signedList(t, f, 2)
	for name, raw := range map[string][]byte{"bridge-revocations.json": oldList, "bridge-revocations.sigstore.json": oldSignature, ".revocation-12345": []byte("half written")} {
		if err := os.WriteFile(filepath.Join(directory, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fetcher := &scripted{}
	fetcher.list, fetcher.signature = signedList(t, f, 6)
	checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
	if _, err := checker.Check(t.Context(), snapshot); err != nil {
		t.Fatal(err)
	}
	// Put the superseded pair back, as if the crash came before its removal.
	for name, raw := range map[string][]byte{"bridge-revocations.json": oldList, "bridge-revocations.sigstore.json": oldSignature} {
		if err := os.WriteFile(filepath.Join(directory, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
	if result, err := off.Check(t.Context(), snapshot); err != nil || result.Sequence != 6 {
		t.Fatalf("the store wins over a leftover older pair: %+v %v", result, err)
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
