package bridgerevocation_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

type fetchFunc func() ([]byte, []byte, error)

func (f fetchFunc) Fetch(context.Context) ([]byte, []byte, error) { return f() }

// Two runs read an empty directory. The first fetched an older list, the second a newer one
// that revokes the bridge, and the second stores it while the first is still fetching. The
// first must report the stored (newer) list's revocations, never its own older list's.
func TestARunThatLosesTheRaceReportsTheNewerStoredList(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	bridge := releaseidentity.Hash([]byte("bridge"))
	directory := t.TempDir()
	newerList, newerSignature := signedList(t, f, 2, bridge)
	newer := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetchFunc(func() ([]byte, []byte, error) { return newerList, newerSignature, nil }), Directory: directory, Now: func() time.Time { return testNow }}
	olderList, olderSignature := signedList(t, f, 1)
	older := bridgerevocation.Checker{Mode: bridgerevocation.Online, Directory: directory, Now: func() time.Time { return testNow }, Fetcher: fetchFunc(func() ([]byte, []byte, error) {
		if _, err := newer.Check(t.Context(), snapshot); err != nil {
			t.Error(err)
		}
		return olderList, olderSignature, nil
	})}
	result, err := older.Check(t.Context(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sequence != 2 || !slices.Contains(result.Revoked, bridge) {
		t.Fatalf("the stored list is sequence 2 and revokes the bridge; got %+v", result)
	}
}

// A floor record that exists but cannot be used refuses bridge routes even when the store is
// fine, under off as well as online, and the message says what only the operator can do: no
// fetched list can replace it, because storing one first needs the floor.
func TestUnreadableOrMalformedFloorRefusesAndSaysWhatToDo(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	digest := releaseidentity.Hash([]byte("d"))
	root := releaseidentity.Hash(f.Pinned.Root)
	type floor struct {
		Sequence int64                  `json:"sequence"`
		Digest   releaseidentity.Digest `json:"digest"`
		Root     releaseidentity.Digest `json:"root"`
	}
	for name, content := range map[string][]byte{
		"not json":         []byte("{"),
		"sequence zero":    testfixture.JSON(t, floor{0, digest, root}),
		"invalid digest":   testfixture.JSON(t, floor{3, "nope", root}),
		"invalid root":     testfixture.JSON(t, floor{3, digest, "nope"}),
		"unknown property": []byte(`{"sequence":3,"extra":true}`),
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			fetcher := &scripted{}
			fetcher.list, fetcher.signature = signedList(t, f, 3)
			online := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: fetcher, Directory: directory, Now: func() time.Time { return testNow }}
			if _, err := online.Check(t.Context(), snapshot); err != nil {
				t.Fatal(err)
			}
			floorPath := filepath.Join(directory, "bridge-revocations-floor.json")
			if err := os.WriteFile(floorPath, content, 0o600); err != nil {
				t.Fatal(err)
			}
			off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
			_, err := off.Check(t.Context(), snapshot)
			if !errors.Is(err, bridgerevocation.ErrUnusableState) || !strings.Contains(err.Error(), floorPath) || !strings.Contains(err.Error(), "repair or remove") || strings.Contains(err.Error(), "Restore access") {
				t.Fatalf("off must refuse and name the operator action on the floor file: %v", err)
			}
			fetcher.list, fetcher.signature = signedList(t, f, 9)
			if _, err := online.Check(t.Context(), snapshot); !errors.Is(err, bridgerevocation.ErrUnusableState) || !strings.Contains(err.Error(), "repair or remove") {
				t.Fatalf("a fetched list cannot replace an unreadable floor: %v", err)
			}
		})
	}
}

// The slow run read sequence 1, a concurrent run stores sequence 2 revoking the bridge, and
// the slow run then finishes without a fresh list of its own: its fetch returns the stored
// list again, or the endpoint is unreachable. It still reports sequence 2.
func TestARunWithoutAFreshListReportsTheNewerStoredList(t *testing.T) {
	for _, mode := range []string{"same-list", "unreachable"} {
		t.Run(mode, func(t *testing.T) {
			f := testfixture.New(t)
			snapshot := f.Snapshot(t)
			directory := t.TempDir()
			bridge := releaseidentity.Hash([]byte("bridge"))
			l1, s1 := signedList(t, f, 1)
			l2, s2 := signedList(t, f, 2, bridge)
			clock := func() time.Time { return testNow }
			seed := bridgerevocation.Checker{Mode: bridgerevocation.Online, Directory: directory, Now: clock, Fetcher: fetchFunc(func() ([]byte, []byte, error) { return l1, s1, nil })}
			if _, err := seed.Check(t.Context(), snapshot); err != nil {
				t.Fatal(err)
			}
			fast := bridgerevocation.Checker{Mode: bridgerevocation.Online, Directory: directory, Now: clock, Fetcher: fetchFunc(func() ([]byte, []byte, error) { return l2, s2, nil })}
			slow := bridgerevocation.Checker{Mode: bridgerevocation.Online, Directory: directory, Now: clock, Fetcher: fetchFunc(func() ([]byte, []byte, error) {
				if _, err := fast.Check(t.Context(), snapshot); err != nil {
					t.Error(err)
				}
				if mode == "unreachable" {
					return nil, nil, bridgerevocation.ErrUnreachable
				}
				return l1, s1, nil
			})}
			result, err := slow.Check(t.Context(), snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if result.Sequence != 2 || !slices.Contains(result.Revoked, bridge) {
				t.Fatalf("the stored list is sequence 2 and revokes the bridge; got %+v", result)
			}
		})
	}
}

// With the store and the floor both damaged, the remedy names the floor, not the endpoint, and
// with the check off it names the setting rather than restoring access.
func TestUnusableStateAdviceMatchesWhatCanHelp(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	directory := t.TempDir()
	for _, name := range []string{"bridge-revocations-store.json", "bridge-revocations-floor.json"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	unreachable := bridgerevocation.Checker{Mode: bridgerevocation.Online, Directory: directory, Now: func() time.Time { return testNow }, Fetcher: fetchFunc(func() ([]byte, []byte, error) { return nil, nil, bridgerevocation.ErrUnreachable })}
	_, err := unreachable.Check(t.Context(), snapshot)
	if !errors.Is(err, bridgerevocation.ErrUnusableState) || !strings.Contains(err.Error(), "repair or remove") || strings.Contains(err.Error(), "Restore access") {
		t.Fatalf("both damaged and unreachable must name the floor repair: %v", err)
	}
	// Only the store damaged, with the check off: nothing is fetched, so switching to online is the way.
	if err := os.Remove(filepath.Join(directory, "bridge-revocations-floor.json")); err != nil {
		t.Fatal(err)
	}
	off := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory}
	_, err = off.Check(t.Context(), snapshot)
	if !errors.Is(err, bridgerevocation.ErrUnusableState) || strings.Contains(err.Error(), "Restore access") || !strings.Contains(err.Error(), "HIKYO_UPGRADE_REVOCATION_CHECK") {
		t.Fatalf("off must say to switch to online or remove the files: %v", err)
	}
}
