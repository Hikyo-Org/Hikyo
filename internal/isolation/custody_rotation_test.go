package isolation

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
)

const custodyInstance = "ins_11111111111111111111111111111111"

// directCustody is the service seam over the real vault primitives.
type directCustody struct {
	directory   string
	stageError  error
	onCommit    func(context.Context)
	commitError error
}

func (c directCustody) Stage(_ context.Context, current, next []byte) error {
	if c.stageError != nil {
		return c.stageError
	}
	return upgradecustody.StageRootRotation(c.directory, current, next, custodyInstance)
}

func (c directCustody) Check(_ context.Context, root []byte) error {
	return upgradecustody.CheckRootRotation(c.directory, root, custodyInstance)
}

func (c directCustody) Commit(ctx context.Context, root []byte) error {
	if c.onCommit != nil {
		c.onCommit(ctx)
	}
	if c.commitError != nil {
		return c.commitError
	}
	return upgradecustody.CommitRootRotation(c.directory, root, custodyInstance)
}

type custodyRotation struct {
	t        *testing.T
	rotation *service.Rotation
	src      *mutableRootSource
	custody  string
	oldRoot  []byte
	newRoot  []byte
}

func newCustodyRotation(t *testing.T, custody func(directory string) service.RootCustody) *custodyRotation {
	t.Helper()
	db := seededDB(t, openSQLite)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(base, "custody")
	oldRoot, err := (probeRootSource{db: db}).Current(tctx(t))
	if err != nil {
		t.Fatal(err)
	}
	vault, err := upgradecustody.CreateLocal(directory, oldRoot, custodyInstance)
	if err != nil {
		t.Fatal(err)
	}
	vault.Close()
	newRoot, err := crypto.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	src := &mutableRootSource{current: bytes.Clone(oldRoot), next: newRoot}
	return &custodyRotation{t: t, src: src, custody: directory, oldRoot: oldRoot, newRoot: newRoot,
		rotation: &service.Rotation{DB: db, Keyring: probeKeyring(t, db), RootKey: src, Custody: custody(directory)}}
}

func (c *custodyRotation) phase(phase service.RootKeyRotationPhase) error {
	_, err := c.rotation.RotateRootKey(tctx(c.t), service.LocalPrincipal(root), phase)
	return err
}

func (c *custodyRotation) opensUnder(rootKey []byte) error {
	v, err := upgradecustody.OpenLocal(c.custody, rootKey, custodyInstance)
	if v != nil {
		defer v.Close()
		if !bytes.Equal(v.RootKey(), rootKey) {
			c.t.Fatal("vault root escrow is not the root that opened it")
		}
	}
	return err
}

func (c *custodyRotation) mustPhase(phase service.RootKeyRotationPhase) {
	c.t.Helper()
	if err := c.phase(phase); err != nil {
		c.t.Fatalf("%s: %v", phase, err)
	}
}

// #1154: a completed root rotation must leave the custody vault openable under
// the new root, and under the old root until the rotation is finalized. Without
// the custody seam this fails with ErrUnlock, the error `sudo hikyo upgrade`
// reports as a passphrase vault.
func TestRootRotationReSealsCustodyAtEveryPhase(t *testing.T) {
	c := newCustodyRotation(t, func(d string) service.RootCustody { return directCustody{directory: d} })
	if err := c.opensUnder(c.oldRoot); err != nil {
		t.Fatal(err)
	}
	c.mustPhase(service.RootRotatePrepare)
	// Prepared, operator has not swapped the root: the old root still boots.
	if err := c.opensUnder(c.oldRoot); err != nil {
		t.Fatalf("after prepare, old root: %v", err)
	}
	// The operator installed the new root but has not verified or finalized: the
	// master is still dual-wrapped, and the vault must open under the new root.
	c.src.install()
	if err := c.opensUnder(c.newRoot); err != nil {
		t.Fatalf("after install before verify, new root: %v", err)
	}
	c.mustPhase(service.RootRotateVerify)
	if err := c.opensUnder(c.newRoot); err != nil {
		t.Fatalf("after verify, new root: %v", err)
	}
	c.mustPhase(service.RootRotateFinalize)
	if err := c.opensUnder(c.newRoot); err != nil {
		t.Fatalf("after finalize, new root: %v", err)
	}
	if err := c.opensUnder(c.oldRoot); !errors.Is(err, upgradecustody.ErrUnlock) {
		t.Fatalf("retired root still opens custody after finalize: %v", err)
	}
}

// A finalize whose custody commit did not complete (crash or error after the
// keyring retired the old wrapper) cannot be retried, but the vault still opens
// under the new root through its staged file, and the next rotation completes
// the interrupted promotion before staging.
func TestRootRotationInterruptedCommitHealsOnNextRotation(t *testing.T) {
	boom := errors.New("promote failed")
	c := newCustodyRotation(t, func(d string) service.RootCustody { return directCustody{directory: d, commitError: boom} })
	c.mustPhase(service.RootRotatePrepare)
	c.src.install()
	c.mustPhase(service.RootRotateVerify)
	if err := c.phase(service.RootRotateFinalize); !errors.Is(err, boom) {
		t.Fatalf("finalize = %v, want the commit failure", err)
	}
	if err := c.opensUnder(c.newRoot); err != nil {
		t.Fatalf("new root after interrupted commit: %v", err)
	}
	third, err := crypto.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	c.src.mu.Lock()
	c.src.next = third
	c.src.mu.Unlock()
	c.rotation.Custody = directCustody{directory: c.custody}
	c.mustPhase(service.RootRotatePrepare)
	c.src.install()
	c.mustPhase(service.RootRotateVerify)
	c.mustPhase(service.RootRotateFinalize)
	if err := c.opensUnder(third); err != nil {
		t.Fatal(err)
	}
	if err := c.opensUnder(c.newRoot); !errors.Is(err, upgradecustody.ErrUnlock) {
		t.Fatalf("second retired root still opens custody: %v", err)
	}
}

func TestRootRotationRefusesPrepareWhenCustodyCannotBeStaged(t *testing.T) {
	boom := errors.New("disk full")
	c := newCustodyRotation(t, func(d string) service.RootCustody { return directCustody{directory: d, stageError: boom} })
	if err := c.phase(service.RootRotatePrepare); !errors.Is(err, boom) {
		t.Fatalf("prepare = %v, want the staging failure", err)
	}
	// Nothing was dual-wrapped: verify has no rotation to confirm.
	if err := c.phase(service.RootRotateVerify); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("verify after refused prepare = %v, want conflict", err)
	}
	if err := c.opensUnder(c.oldRoot); err != nil {
		t.Fatal(err)
	}
}

// Finalize is destructive, so custody that cannot open under the installed root
// keeps the old wrapper: the operator can repair and retry.
func TestRootRotationFinalizeRefusedWhenCustodyDoesNotOpen(t *testing.T) {
	c := newCustodyRotation(t, func(d string) service.RootCustody { return directCustody{directory: d} })
	c.mustPhase(service.RootRotatePrepare)
	c.src.install()
	// Simulate lost staged custody: re-create the live vault under an unrelated root.
	other := bytes.Repeat([]byte{0x5a}, 32)
	if err := removeCustody(c.custody); err != nil {
		t.Fatal(err)
	}
	vault, err := upgradecustody.CreateLocal(c.custody, other, custodyInstance)
	if err != nil {
		t.Fatal(err)
	}
	vault.Close()
	err = c.phase(service.RootRotateFinalize)
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("finalize = %v, want conflict", err)
	}
	// The old wrapper survived: verify still sees the dual-wrapped rotation.
	c.mustPhase(service.RootRotateVerify)
}

// Installations without reachable custody rotate exactly as before.
func TestRootRotationWithoutCustodySeam(t *testing.T) {
	c := newCustodyRotation(t, func(string) service.RootCustody { return nil })
	c.mustPhase(service.RootRotatePrepare)
	c.src.install()
	c.mustPhase(service.RootRotateVerify)
	c.mustPhase(service.RootRotateFinalize)
}

func removeCustody(directory string) error { return os.RemoveAll(directory) }

// The retired root keeps opening custody until the keyring has actually retired
// its wrapper: the store can refuse finalize, so the promotion runs only after
// that commit.
func TestRootRotationPromotesCustodyOnlyAfterKeyringFinalize(t *testing.T) {
	var c *custodyRotation
	c = newCustodyRotation(t, func(d string) service.RootCustody {
		return directCustody{directory: d, onCommit: func(ctx context.Context) {
			if _, err := c.rotation.Keyring.VerifyRootKeyRotation(ctx, c.newRoot); !errors.Is(err, crypto.ErrNotDualWrapped) {
				t.Errorf("custody committed while the master was still dual-wrapped: %v", err)
			}
		}}
	})
	c.mustPhase(service.RootRotatePrepare)
	c.src.install()
	c.mustPhase(service.RootRotateVerify)
	if err := c.opensUnder(c.oldRoot); err != nil {
		t.Fatalf("old root while the rotation is pending: %v", err)
	}
	c.mustPhase(service.RootRotateFinalize)
	if err := c.opensUnder(c.oldRoot); !errors.Is(err, upgradecustody.ErrUnlock) {
		t.Fatalf("old root after finalize: %v", err)
	}
}

// A second prepare while one is in flight is refused before it can restage, so
// the rotation that is in flight still completes.
func TestRootRotationRefusedPrepareKeepsStagedCustody(t *testing.T) {
	c := newCustodyRotation(t, func(d string) service.RootCustody { return directCustody{directory: d} })
	c.mustPhase(service.RootRotatePrepare)
	other, err := crypto.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	c.src.mu.Lock()
	c.src.next = other
	c.src.mu.Unlock()
	if err := c.phase(service.RootRotatePrepare); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second prepare = %v, want conflict", err)
	}
	c.src.mu.Lock()
	c.src.next = c.newRoot
	c.src.mu.Unlock()
	c.src.install()
	c.mustPhase(service.RootRotateVerify)
	c.mustPhase(service.RootRotateFinalize)
	if err := c.opensUnder(c.newRoot); err != nil {
		t.Fatal(err)
	}
}
