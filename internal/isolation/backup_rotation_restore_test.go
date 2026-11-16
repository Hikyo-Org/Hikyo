package isolation

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/app"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
)

// #1126: a backup is restorable after the root key is rotated, for the
// classical and the hybrid backup identity alike, on both engines. The archive
// carries the secret fields wrapped under the root that stood when it was
// written, so it boots only under THAT root; the identity opens the container
// and never the data. The drill rotates the root through every real phase.
func TestRestoreAfterRootRotationSQLite(t *testing.T) {
	for name, generate := range rotationIdentities() {
		t.Run(name, func(t *testing.T) {
			c := newCustodyWith(t, generate)
			runRestoreAfterRootRotation(t, sqliteTarget(t, t.TempDir(), c.recipient(t)), c)
		})
	}
}

func TestRestoreAfterRootRotationPostgres(t *testing.T) {
	for name, generate := range rotationIdentities() {
		t.Run(name, func(t *testing.T) {
			c := newCustodyWith(t, generate)
			runRestoreAfterRootRotation(t, postgresTarget(t, t.TempDir(), c.recipient(t)), c)
		})
	}
}

func rotationIdentities() map[string]func() (string, string, error) {
	return map[string]func() (string, string, error){
		"x25519": backup.GenerateIdentity,
		"hybrid": backup.GenerateHybridIdentity,
	}
}

func runRestoreAfterRootRotation(t *testing.T, target drillTarget, c custody) {
	db, _ := buildInstance(t, target, c)
	oldRoot := c.rootKey(t)
	defer crypto.Zero(oldRoot)

	before := exportArchive(t, target)
	orgs := queryStrings(t, db, "SELECT name FROM orgs ORDER BY id")

	newRoot := rotateRootThroughEveryPhase(t, db, c, oldRoot)
	defer crypto.Zero(newRoot)
	after := exportArchive(t, target)

	// The booted fixture reads the root from the custody file, so the file
	// holds the root that wrapped the archive being restored.
	restoreFrom := func(t *testing.T, archive string, root []byte) *store.DB {
		t.Helper()
		if err := os.WriteFile(filepath.Join(c.rootStore, "rootkey"), []byte(crypto.EncodeRootKey(root)+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		target.destroy(t)
		if err := app.RunRestore(t.Context(), target.cfg, drillLogger(),
			[]string{"run", "--from", archive, "--identity-file", c.identityFile()},
			io.Discard, nil, nil); err != nil {
			t.Fatalf("restore %s: %v", filepath.Base(archive), err)
		}
		recoverRestoredTarget(t, target, c)
		return target.open(t)
	}
	boots := func(t *testing.T, restored *store.DB, root []byte) error {
		t.Helper()
		_, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: restored}, append([]byte(nil), root...))
		return err
	}

	t.Run("archive_written_after_rotation_boots_under_the_new_root_only", func(t *testing.T) {
		restored := restoreFrom(t, after, newRoot)
		if got := queryStrings(t, restored, "SELECT name FROM orgs ORDER BY id"); got != orgs {
			t.Fatalf("restored organisations = %q, want %q", got, orgs)
		}
		if err := boots(t, restored, newRoot); err != nil {
			t.Fatalf("restored datastore did not boot under the rotated root: %v", err)
		}
		if err := boots(t, restored, oldRoot); err == nil {
			t.Fatal("restored datastore booted under the retired root")
		}
	})

	t.Run("archive_written_before_rotation_still_restores_and_needs_its_own_root", func(t *testing.T) {
		restored := restoreFrom(t, before, oldRoot)
		if got := queryStrings(t, restored, "SELECT name FROM orgs ORDER BY id"); got != orgs {
			t.Fatalf("restored organisations = %q, want %q", got, orgs)
		}
		if err := boots(t, restored, oldRoot); err != nil {
			t.Fatalf("pre-rotation archive did not boot under the root that stood at export: %v", err)
		}
		if err := boots(t, restored, newRoot); err == nil {
			t.Fatal("pre-rotation archive booted under a root that never wrapped it")
		}
	})

	archive, err := os.ReadFile(after)
	if err != nil {
		t.Fatal(err)
	}
	refuses := func(t *testing.T, name string, content []byte, identity string, wantNoMatch bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			from := filepath.Join(t.TempDir(), "archive.age")
			if err := os.WriteFile(from, content, 0o600); err != nil {
				t.Fatal(err)
			}
			target.destroy(t)
			err := app.RunRestore(t.Context(), target.cfg, drillLogger(),
				[]string{"run", "--from", from, "--identity-file", identity}, io.Discard, nil, nil)
			if err == nil {
				t.Fatal("the restore was accepted")
			}
			// boundary confines filippo.io/age to the crypto packages, so the
			// refusal is pinned by age's own message text.
			if wantNoMatch && !strings.Contains(err.Error(), "identity did not match any of the recipients") {
				t.Fatalf("restore error = %v, want age's no-identity-match refusal", err)
			}
			assertTargetUntouched(t, target)
		})
	}

	flip := func(at int) []byte {
		tampered := bytes.Clone(archive)
		tampered[at] ^= 0x01
		return tampered
	}
	// The stanza, the header MAC and the payload fail for different reasons:
	// a stanza byte stops the file key unwrapping, a MAC byte fails the header
	// check, a payload byte fails that chunk's authentication tag.
	mac := bytes.Index(archive, []byte("\n--- "))
	if mac < 0 {
		t.Fatal("no age header MAC line in the archive")
	}
	stanza := bytes.IndexByte(archive, '\n') + 1 // first byte of the first stanza line
	refuses(t, "tampered_stanza_refused_before_any_state", flip(stanza+4), c.identityFile(), false)
	refuses(t, "tampered_header_mac_refused_before_any_state", flip(mac+6), c.identityFile(), false)
	refuses(t, "tampered_payload_refused_before_any_state", flip(len(archive)-24), c.identityFile(), false)

	foreign := map[string]func() (string, string, error){
		"foreign_x25519_identity": backup.GenerateIdentity,
		"foreign_hybrid_identity": backup.GenerateHybridIdentity,
	}
	for name, generate := range foreign {
		identity, _, err := generate()
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "identity")
		if err := os.WriteFile(file, []byte(identity+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		refuses(t, name+"_is_refused_before_any_state", archive, file, true)
	}
	// The root key is not a backup identity in either direction.
	rootFile := filepath.Join(c.rootStore, "rootkey")
	if raw, err := os.ReadFile(rootFile); err != nil || strings.TrimSpace(string(raw)) == "" {
		t.Fatalf("root key store: %v", err)
	}
	refuses(t, "root_key_file_is_not_an_identity", archive, rootFile, false)
}

// rotateRootThroughEveryPhase runs prepare, install, verify and finalize
// against the live datastore, writes the new root where the booted fixture
// reads it, and returns it.
func rotateRootThroughEveryPhase(t *testing.T, db *store.DB, c custody, oldRoot []byte) []byte {
	t.Helper()
	ctx := t.Context()
	kr, err := crypto.LoadKeyring(ctx, &keyring.Store{DB: db}, append([]byte(nil), oldRoot...))
	if err != nil {
		t.Fatal(err)
	}
	if err := (&service.Escrow{DB: db}).Verify(ctx, append([]byte(nil), oldRoot...), true); err != nil {
		t.Fatalf("escrow verify: %v", err)
	}
	newRoot, err := crypto.GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	src := &mutableRootSource{current: append([]byte(nil), oldRoot...), next: append([]byte(nil), newRoot...)}
	rotation := &service.Rotation{DB: db, Keyring: kr, RootKey: src}
	phase := func(p service.RootKeyRotationPhase) {
		t.Helper()
		if _, err := rotation.RotateRootKey(ctx, service.LocalPrincipal(root), p); err != nil {
			t.Fatalf("root rotation %s: %v", p, err)
		}
	}
	phase(service.RootRotatePrepare)
	src.install()
	if err := os.WriteFile(filepath.Join(c.rootStore, "rootkey"), []byte(crypto.EncodeRootKey(newRoot)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	phase(service.RootRotateVerify)
	phase(service.RootRotateFinalize)
	return newRoot
}
