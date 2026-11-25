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

// A stored list that no longer verifies (a torn two-file write from an earlier release,
// a corrupt file, or a list bound to a rotated root) must not block every bridge route:
// check=off ignores it and an online run replaces it with the valid newer list.
func TestUnusableStoredStateNeitherBlocksOffNorAValidNewerList(t *testing.T) {
	f := testfixture.New(t)
	other := testfixture.New(t)
	snapshot := f.Snapshot(t)
	tornList, _ := signedList(t, f, 3)
	_, tornSignature := signedList(t, f, 4)
	otherList, otherSignature := signedList(t, other, 9)
	for name, files := range map[string]map[string][]byte{
		"torn pair (list N beside signature N+1)": {"bridge-revocations.json": tornList, "bridge-revocations.sigstore.json": tornSignature},
		"list without its signature":              {"bridge-revocations.json": tornList},
		"stored under another trust root":         {"bridge-revocations.json": otherList, "bridge-revocations.sigstore.json": otherSignature},
		"damaged combined file":                   {"bridge-revocations-store.json": []byte("not json")},
	} {
		t.Run(name, func(t *testing.T) {
			write := func(directory string) {
				for file, raw := range files {
					if err := os.WriteFile(filepath.Join(directory, file), raw, 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			offDirectory := t.TempDir()
			write(offDirectory)
			off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: offDirectory}
			if result, err := off.Check(t.Context(), snapshot); err != nil || result.Outcome != bridgerevocation.Disabled {
				t.Fatalf("off must not depend on stored state: %+v %v", result, err)
			}
			onDirectory := t.TempDir()
			write(onDirectory)
			fetcher := &scripted{}
			fetcher.list, fetcher.signature = signedList(t, f, 2)
			online := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: onDirectory, Now: func() time.Time { return testNow }}
			if result, err := online.Check(t.Context(), snapshot); err != nil || result.Outcome != bridgerevocation.Checked || result.Sequence != 2 {
				t.Fatalf("a valid newer list replaces unusable stored state: %+v %v", result, err)
			}
			// The replacement is now the floor: an older list is a rollback again.
			fetcher.list, fetcher.signature = signedList(t, f, 1)
			if _, err := online.Check(t.Context(), snapshot); err == nil || !strings.Contains(err.Error(), "rollback") {
				t.Fatalf("the replacement list is the new floor: %v", err)
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
