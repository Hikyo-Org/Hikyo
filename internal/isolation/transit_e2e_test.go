package isolation

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/transit"
	"github.com/Hikyo-Org/hikyo/internal/transit/transittest"
)

// The transit surface end to end (#156, transit ADR invariants 5 and 7): every
// operation, every lifecycle refusal and the negative authorization paths,
// through the real service, chokepoint, store and audit trail, on both engines
// and under both custody providers. The software provider seals under the
// fixture's real project DEK; the mock external provider stands in for an HSM.

// transitEnv is the transit fixture: alice manages and uses keys in env_a1.
func transitEnv(t *testing.T, db *store.DB, suffix string) (*service.Transit, *transittest.External) {
	t.Helper()
	for _, g := range []struct{ id, cap, env string }{
		{"g_tr_manage" + suffix, "crypto-manage", ""},
		{"g_tr_use" + suffix, "crypto-use", "env_a1"},
	} {
		env := "NULL"
		if g.env != "" {
			env = "'" + g.env + "'"
		}
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('`+g.id+`','usr_alice','`+g.cap+`','org_a','prj_a1',`+env+`,`+ts+`)`)
	}
	ext := transittest.NewExternal()
	kr := probeKeyring(t, db)
	return &service.Transit{
		DB: db, Keyring: kr,
		Custody: transit.NewRegistry(&transit.Software{Keyring: kr}, ext),
	}, ext
}

var transitScope = domain.Scope{Org: orgA, Project: prjA1, Env: envA1}

func mustTransitKey(t *testing.T, svc *service.Transit, req service.CreateTransitKeyRequest) service.TransitKeyView {
	t.Helper()
	k, err := svc.CreateKey(tctx(t), service.LocalPrincipal(alice), transitScope, req)
	if err != nil {
		t.Fatalf("create transit key %s: %v", req.Name, err)
	}
	return k
}

func wantConflictCause(t *testing.T, err error, cause string) {
	t.Helper()
	var sd interface{ SafeDetail() string }
	if !errors.Is(err, domain.ErrConflict) || !errors.As(err, &sd) || sd.SafeDetail() != cause {
		t.Fatalf("err = %v, want a %q conflict", err, cause)
	}
}

func TestTransitEndToEnd(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc, ext := transitEnv(t, db, "")
		for _, custody := range []string{"software", "external"} {
			t.Run(custody, func(t *testing.T) {
				runTransitCustodySuite(t, db, svc, ext, custody)
			})
		}
		t.Run("authorization", func(t *testing.T) { runTransitAuthorization(t, db, svc) })
		t.Run("external_unavailable_fails_closed", func(t *testing.T) { runTransitExternalDown(t, svc, ext) })
		t.Run("concurrent_rotation", func(t *testing.T) { runTransitConcurrentRotation(t, svc) })
		t.Run("scheduler", func(t *testing.T) { runTransitScheduler(t, db, svc, ext) })
		t.Run("scheduler_pages_past_batch", func(t *testing.T) { runTransitSchedulerPaging(t, db, svc) })
		t.Run("dek_rotation_and_reencrypt", func(t *testing.T) { runTransitReencrypt(t, db, svc) })
		t.Run("audit_never_carries_material", func(t *testing.T) { assertTransitAuditClean(t, db) })
	})
}

func runTransitCustodySuite(t *testing.T, db *store.DB, svc *service.Transit, ext *transittest.External, custody string) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	name := func(s string) string { return custody + "-" + s }

	// --- encryption key: encrypt, decrypt, context, rewrap, data keys -----
	enc := mustTransitKey(t, svc, service.CreateTransitKeyRequest{
		Name: name("payments"), Algorithm: "xchacha20-poly1305", Custody: custody,
		AllowedOperations: []string{"encrypt", "decrypt", "rewrap", "datakey", "datakey-plaintext"},
	})
	if enc.LatestVersion != 1 || enc.State != "active" || enc.Custody != custody {
		t.Fatalf("created key = %+v", enc.TransitKeyRecord)
	}
	if enc.Versions[0].HasMaterial == (custody == "external") || enc.Versions[0].ExternalHeld != (custody == "external") {
		t.Fatalf("version custody columns wrong: %+v", enc.Versions[0])
	}
	ct, err := svc.Encrypt(ctx, me, transitScope, enc.Name, []byte("4111-1111-1111-1111"), []byte("order-7"), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ct.Value, "hikyo:v1:") {
		t.Fatalf("ciphertext = %s", ct.Value)
	}
	pt, err := svc.Decrypt(ctx, me, transitScope, enc.Name, ct.Value, []byte("order-7"))
	if err != nil || string(pt.Plaintext) != "4111-1111-1111-1111" {
		t.Fatalf("decrypt = %q, %v", pt.Plaintext, err)
	}
	if _, err := svc.Decrypt(ctx, me, transitScope, enc.Name, ct.Value, []byte("order-8")); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("wrong context = %v, want invalid", err)
	}
	// A ciphertext of one key never opens under another key.
	other := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: name("other"), Algorithm: "xchacha20-poly1305", Custody: custody})
	if _, err := svc.Decrypt(ctx, me, transitScope, other.Name, ct.Value, []byte("order-7")); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("cross-key decrypt = %v", err)
	}
	// Default allowed operations exclude datakey-plaintext.
	if _, err := svc.DataKey(ctx, me, transitScope, other.Name, 256, nil, true); !errors.Is(err, service.ErrTransitForbidden) {
		t.Fatalf("plaintext data key on a default key = %v, want forbidden", err)
	}
	wrapped, err := svc.DataKey(ctx, me, transitScope, enc.Name, 256, []byte("dk"), false)
	if err != nil || wrapped.Plaintext != nil {
		t.Fatalf("wrapped data key = %+v, %v", wrapped, err)
	}
	dk, err := svc.DataKey(ctx, me, transitScope, enc.Name, 512, []byte("dk"), true)
	if err != nil || len(dk.Plaintext) != 64 {
		t.Fatalf("plaintext data key: len=%d err=%v", len(dk.Plaintext), err)
	}
	opened, err := svc.Decrypt(ctx, me, transitScope, enc.Name, dk.Value, []byte("dk"))
	if err != nil || !bytes.Equal(opened.Plaintext, dk.Plaintext) {
		t.Fatalf("wrapped data key does not unwrap to the revealed one: %v", err)
	}
	if _, err := svc.DataKey(ctx, me, transitScope, enc.Name, 192, nil, false); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("192-bit data key = %v", err)
	}

	// Rotation, rewrap and the version window.
	if _, err := svc.RotateKey(ctx, me, transitScope, enc.Name); err != nil {
		t.Fatal(err)
	}
	ct2, err := svc.Encrypt(ctx, me, transitScope, enc.Name, []byte("new"), nil, 0)
	if err != nil || ct2.KeyVersion != 2 {
		t.Fatalf("post-rotation encrypt = v%d, %v", ct2.KeyVersion, err)
	}
	old, err := svc.Encrypt(ctx, me, transitScope, enc.Name, []byte("pinned"), nil, 1)
	if err != nil || old.KeyVersion != 1 {
		t.Fatalf("explicit v1 encrypt = %+v, %v", old, err)
	}
	moved, err := svc.Rewrap(ctx, me, transitScope, enc.Name, ct.Value, []byte("order-7"))
	if err != nil || moved.KeyVersion != 2 || moved.Plaintext != nil {
		t.Fatalf("rewrap = %+v, %v", moved, err)
	}
	if again, _ := svc.Decrypt(ctx, me, transitScope, enc.Name, moved.Value, []byte("order-7")); string(again.Plaintext) != "4111-1111-1111-1111" {
		t.Fatal("rewrapped ciphertext does not decrypt to the original")
	}
	two := uint32(2)
	if _, err := svc.ConfigureKey(ctx, me, transitScope, enc.Name, service.ConfigureTransitKeyRequest{MinEncryptVersion: &two, MinDecryptVersion: &two}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decrypt(ctx, me, transitScope, enc.Name, ct.Value, []byte("order-7")); err == nil {
		t.Fatal("v1 ciphertext decrypted below min_decrypt_version")
	} else {
		wantConflictCause(t, err, "version")
	}
	if _, err := svc.Encrypt(ctx, me, transitScope, enc.Name, []byte("x"), nil, 1); err == nil {
		t.Fatal("v1 encrypt below min_encrypt_version")
	} else {
		wantConflictCause(t, err, "version")
	}
	three := uint32(3)
	if _, err := svc.ConfigureKey(ctx, me, transitScope, enc.Name, service.ConfigureTransitKeyRequest{MinEncryptVersion: &three}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("min_encrypt above latest = %v", err)
	}
	if _, deleted, err := svc.TrimKey(ctx, me, transitScope, enc.Name); err != nil || deleted != 1 {
		t.Fatalf("trim = %d, %v", deleted, err)
	}
	view, err := svc.GetKey(ctx, me, transitScope, enc.Name)
	if err != nil || len(view.Versions) != 1 || view.Versions[0].Version != 2 {
		t.Fatalf("after trim: %+v, %v", view.Versions, err)
	}

	// --- lifecycle, fail closed ---------------------------------------------
	change := func(action string, delay time.Duration) error {
		_, err := svc.ChangeKeyState(ctx, me, transitScope, enc.Name, action, delay)
		return err
	}
	if err := change("disable", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decrypt(ctx, me, transitScope, enc.Name, moved.Value, []byte("order-7")); err == nil {
		t.Fatal("disabled key decrypted")
	} else {
		wantConflictCause(t, err, "state")
	}
	if err := change("retire", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Encrypt(ctx, me, transitScope, enc.Name, []byte("x"), nil, 0); err == nil {
		t.Fatal("retired key encrypted")
	} else {
		wantConflictCause(t, err, "state")
	}
	if _, err := svc.Decrypt(ctx, me, transitScope, enc.Name, moved.Value, []byte("order-7")); err != nil {
		t.Fatalf("retired key must still decrypt: %v", err)
	}
	if err := change("enable", 0); err != nil {
		t.Fatal(err)
	}
	if err := change("compromise", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Encrypt(ctx, me, transitScope, enc.Name, []byte("x"), nil, 0); err == nil {
		t.Fatal("compromised version encrypted")
	} else {
		wantConflictCause(t, err, "compromised")
	}
	// Recovery path: rotate, then rewrap off the compromised version.
	if _, err := svc.RotateKey(ctx, me, transitScope, enc.Name); err != nil {
		t.Fatal(err)
	}
	recovered, err := svc.Rewrap(ctx, me, transitScope, enc.Name, moved.Value, []byte("order-7"))
	if err != nil || recovered.KeyVersion != 3 {
		t.Fatalf("rewrap off a compromised version = %+v, %v", recovered, err)
	}
	if err := change("schedule-deletion", time.Hour); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("deletion delay below the floor = %v", err)
	}
	if err := change("schedule-deletion", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Decrypt(ctx, me, transitScope, enc.Name, recovered.Value, []byte("order-7")); err == nil {
		t.Fatal("key pending deletion decrypted")
	}
	if _, err := svc.RotateKey(ctx, me, transitScope, enc.Name); err == nil {
		t.Fatal("key pending deletion rotated")
	}
	if err := change("enable", 0); err == nil {
		t.Fatal("pending deletion went straight back to active")
	}
	if err := change("cancel-deletion", 0); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.GetKey(ctx, me, transitScope, enc.Name); got.State != "disabled" {
		t.Fatalf("cancel-deletion landed in %q, want disabled", got.State)
	}

	// --- signing key ------------------------------------------------------------
	sig := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: name("webhooks"), Algorithm: "ed25519", Custody: custody})
	if len(sig.Versions[0].PublicKey) != 32 {
		t.Fatal("signing key has no public key")
	}
	s1, err := svc.Sign(ctx, me, transitScope, sig.Name, []byte("payload"), 0)
	if err != nil {
		t.Fatal(err)
	}
	ok, err := svc.Verify(ctx, me, transitScope, sig.Name, []byte("payload"), s1.Value)
	if err != nil || !ok.Valid {
		t.Fatalf("verify = %+v, %v", ok, err)
	}
	if bad, _ := svc.Verify(ctx, me, transitScope, sig.Name, []byte("tampered"), s1.Value); bad.Valid {
		t.Fatal("tampered message verified")
	}
	if _, err := svc.Encrypt(ctx, me, transitScope, sig.Name, []byte("x"), nil, 0); !errors.Is(err, service.ErrTransitForbidden) {
		t.Fatalf("signing key encrypt = %v, want forbidden", err)
	}
	if _, err := svc.ChangeKeyState(ctx, me, transitScope, sig.Name, "compromise", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, me, transitScope, sig.Name, []byte("payload"), s1.Value); err == nil {
		t.Fatal("a compromised version still verifies")
	} else {
		wantConflictCause(t, err, "compromised")
	}

	// --- MAC key ------------------------------------------------------------------
	mac := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: name("tokens"), Algorithm: "hmac-sha256", Custody: custody})
	m1, err := svc.HMAC(ctx, me, transitScope, mac.Name, []byte("session-9"), 0)
	if err != nil {
		t.Fatal(err)
	}
	m2, _ := svc.HMAC(ctx, me, transitScope, mac.Name, []byte("session-9"), 0)
	if m1.Value != m2.Value {
		t.Fatal("HMAC is not deterministic")
	}
	if v, err := svc.VerifyHMAC(ctx, me, transitScope, mac.Name, []byte("session-9"), m1.Value); err != nil || !v.Valid {
		t.Fatalf("hmac-verify = %+v, %v", v, err)
	}
	if v, _ := svc.VerifyHMAC(ctx, me, transitScope, mac.Name, []byte("session-10"), m1.Value); v.Valid {
		t.Fatal("wrong message MAC verified")
	}
	if _, err := svc.VerifyHMAC(ctx, me, transitScope, mac.Name, []byte("x"), "hikyo:v1:not base64!"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("malformed mac = %v", err)
	}

	// --- restart: a fresh service over the same datastore decrypts ---------------
	restarted := &service.Transit{DB: db, Keyring: probeKeyring(t, db), Custody: transit.NewRegistry(&transit.Software{Keyring: probeKeyring(t, db)}, ext)}
	if _, err := svc.ChangeKeyState(ctx, me, transitScope, other.Name, "retire", 0); err != nil {
		t.Fatal(err)
	}
	otherCT, err := restarted.Encrypt(ctx, me, transitScope, mac.Name, nil, nil, 0)
	if err == nil || otherCT.Value != "" {
		t.Fatal("a MAC key encrypted after restart")
	}
	if got, err := restarted.VerifyHMAC(ctx, me, transitScope, mac.Name, []byte("session-9"), m1.Value); err != nil || !got.Valid {
		t.Fatalf("restarted service lost the MAC key: %+v %v", got, err)
	}
}

// runTransitAuthorization walks the negative paths: no crypto-use, no
// crypto-manage, another environment, another org, per-key caller entries and
// a workload credential that holds crypto-use and nothing else.
func runTransitAuthorization(t *testing.T, db *store.DB, svc *service.Transit) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "authz-key", Algorithm: "xchacha20-poly1305"})
	ct, err := svc.Encrypt(ctx, me, transitScope, key.Name, []byte("x"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	// reader holds `read` only: metadata yes, crypto no.
	if _, err := svc.GetKey(ctx, service.LocalPrincipal(reader), transitScope, key.Name); err != nil {
		t.Fatalf("read@env cannot inspect: %v", err)
	}
	for name, err := range map[string]error{
		"encrypt": func() error {
			_, e := svc.Encrypt(ctx, service.LocalPrincipal(reader), transitScope, key.Name, []byte("x"), nil, 0)
			return e
		}(),
		"decrypt": func() error {
			_, e := svc.Decrypt(ctx, service.LocalPrincipal(reader), transitScope, key.Name, ct.Value, nil)
			return e
		}(),
		"rotate": func() error {
			_, e := svc.RotateKey(ctx, service.LocalPrincipal(reader), transitScope, key.Name)
			return e
		}(),
		"create": func() error {
			_, e := svc.CreateKey(ctx, service.LocalPrincipal(reader), transitScope, service.CreateTransitKeyRequest{Name: "nope", Algorithm: "ed25519"})
			return e
		}(),
		"cross-org": func() error {
			_, e := svc.Encrypt(ctx, service.LocalPrincipal(bob), transitScope, key.Name, []byte("x"), nil, 0)
			return e
		}(),
	} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s without authority = %v, want the uniform not-found", name, err)
		}
	}
	// crypto-use is environment-scoped: env_prod has no grant, and the key is
	// invisible there anyway.
	prod := domain.Scope{Org: orgA, Project: prjA1, Env: "env_prod"}
	if _, err := svc.Encrypt(ctx, me, prod, key.Name, []byte("x"), nil, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("sibling environment = %v", err)
	}

	// A workload holding crypto-use and nothing else.
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_tr_ident','usr_alice','manage-identities','org_a','prj_a1',NULL,`+ts+`)`)
	ident := identitySvc(db)
	sa, err := ident.CreateServiceAccount(ctx, me, prjScope(), "transit-workload", domain.ClassWorkload)
	if err != nil {
		t.Fatal(err)
	}
	minted, err := ident.MintCredential(ctx, me, prjScope(), sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatal(err)
	}
	workload := service.Bearer(minted.Value)
	if _, err := svc.Encrypt(ctx, workload, transitScope, key.Name, []byte("x"), nil, 0); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("workload without crypto-use = %v", err)
	}
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_tr_sa','`+string(sa.Principal)+`','crypto-use','org_a','prj_a1','env_a1',`+ts+`)`)
	if _, err := svc.Encrypt(ctx, workload, transitScope, key.Name, []byte("x"), nil, 0); err != nil {
		t.Fatalf("workload with crypto-use: %v", err)
	}
	if _, err := svc.RotateKey(ctx, workload, transitScope, key.Name); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("workload rotated a key: %v", err)
	}

	// Per-key caller entries narrow crypto-use: only the workload, encrypt only.
	entries := []service.TransitCallerEntry{{PrincipalID: string(sa.Principal), Operations: []string{"encrypt"}}}
	if _, err := svc.ConfigureKey(ctx, me, transitScope, key.Name, service.ConfigureTransitKeyRequest{Callers: &entries}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Encrypt(ctx, me, transitScope, key.Name, []byte("x"), nil, 0); !errors.Is(err, service.ErrTransitForbidden) {
		t.Fatalf("unlisted caller = %v, want forbidden", err)
	}
	if _, err := svc.Decrypt(ctx, workload, transitScope, key.Name, ct.Value, nil); !errors.Is(err, service.ErrTransitForbidden) {
		t.Fatalf("listed caller, unlisted operation = %v, want forbidden", err)
	}
	if _, err := svc.Encrypt(ctx, workload, transitScope, key.Name, []byte("x"), nil, 0); err != nil {
		t.Fatalf("listed caller, listed operation: %v", err)
	}
	bad := []service.TransitCallerEntry{{PrincipalID: string(sa.Principal), Operations: []string{"sign"}}}
	if _, err := svc.ConfigureKey(ctx, me, transitScope, key.Name, service.ConfigureTransitKeyRequest{Callers: &bad}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("caller entry naming an operation the key cannot do = %v", err)
	}

	// Creation-time refusals.
	for _, req := range []service.CreateTransitKeyRequest{
		{Name: "Bad Name", Algorithm: "ed25519"},
		{Name: "aes", Algorithm: "aes-256-gcm"},
		{Name: "export", Algorithm: "ed25519", Exportable: true},
		{Name: "mixed", Algorithm: "ed25519", AllowedOperations: []string{"sign", "encrypt"}},
		{Name: "rot", Algorithm: "ed25519", RotationPeriodSeconds: 60},
		{Name: "hsm", Algorithm: "ed25519", Custody: "hsm"},
	} {
		if _, err := svc.CreateKey(ctx, me, transitScope, req); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("create %+v = %v, want invalid", req, err)
		}
	}
	if _, err := svc.CreateKey(ctx, me, transitScope, service.CreateTransitKeyRequest{Name: key.Name, Algorithm: "ed25519"}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate name = %v, want conflict", err)
	}
}

// runTransitExternalDown proves D3's fail-closed rule: an external key whose
// provider is down refuses every operation and is never served by software
// custody, and creation of external keys is refused too.
func runTransitExternalDown(t *testing.T, svc *service.Transit, ext *transittest.External) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "hsm-backed", Algorithm: "xchacha20-poly1305", Custody: "external"})
	ct, err := svc.Encrypt(ctx, me, transitScope, key.Name, []byte("x"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ext.SetAvailable(false)
	defer ext.SetAvailable(true)
	before := ext.Calls.Load()
	if _, err := svc.Encrypt(ctx, me, transitScope, key.Name, []byte("x"), nil, 0); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("encrypt with custody down = %v", err)
	}
	if _, err := svc.Decrypt(ctx, me, transitScope, key.Name, ct.Value, nil); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("decrypt with custody down = %v", err)
	}
	if _, err := svc.RotateKey(ctx, me, transitScope, key.Name); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("rotate with custody down = %v", err)
	}
	if _, err := svc.CreateKey(ctx, me, transitScope, service.CreateTransitKeyRequest{Name: "hsm-2", Algorithm: "ed25519", Custody: "external"}); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("create with custody down = %v", err)
	}
	if ext.Calls.Load() != before {
		t.Fatal("an unavailable provider did work")
	}
	// A registry with no external provider at all: unavailable, never software.
	bare := &service.Transit{DB: svc.DB, Keyring: svc.Keyring, Custody: transit.NewRegistry(&transit.Software{Keyring: svc.Keyring})}
	ext.SetAvailable(true)
	if _, err := bare.Encrypt(ctx, me, transitScope, key.Name, []byte("x"), nil, 0); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("unregistered external custody = %v", err)
	}
	if _, err := bare.CreateKey(ctx, me, transitScope, service.CreateTransitKeyRequest{Name: "hsm-3", Algorithm: "ed25519", Custody: "external"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("create on an instance without external custody = %v", err)
	}
}

// runTransitConcurrentRotation races rotations: versions stay contiguous and
// every loser is a retryable conflict, never a duplicate or a gap.
func runTransitConcurrentRotation(t *testing.T, svc *service.Transit) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "raced", Algorithm: "hmac-sha256"})
	const n = 6
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.RotateKey(ctx, me, transitScope, key.Name)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, domain.ErrConflict):
			default:
				t.Errorf("rotation failed with a non-conflict: %v", err)
			}
		}()
	}
	wg.Wait()
	view, err := svc.GetKey(ctx, me, transitScope, key.Name)
	if err != nil {
		t.Fatal(err)
	}
	if int(view.LatestVersion) != 1+wins || len(view.Versions) != 1+wins {
		t.Fatalf("latest=%d versions=%d wins=%d: rotation lost or duplicated a version", view.LatestVersion, len(view.Versions), wins)
	}
	for i, v := range view.Versions {
		if v.Version != uint32(i+1) {
			t.Fatalf("version gap: %+v", view.Versions)
		}
	}
}

// runTransitScheduler drives automatic rotation and the deletion purge with an
// injected clock, for both custody kinds.
func runTransitScheduler(t *testing.T, db *store.DB, svc *service.Transit, ext *transittest.External) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	start := time.Now().UTC()
	svc.Now = func() time.Time { return start }
	defer func() { svc.Now = nil }()
	auto := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "auto-rotated", Algorithm: "ed25519", RotationPeriodSeconds: 3600})
	svc.Now = func() time.Time { return start.Add(30 * time.Minute) }
	if n, err := svc.RotateDue(ctx); err != nil || n != 0 {
		t.Fatalf("rotated %d before the period elapsed: %v", n, err)
	}
	svc.Now = func() time.Time { return start.Add(2 * time.Hour) }
	if got, _ := svc.GetKey(ctx, me, transitScope, auto.Name); !got.RotationDue {
		t.Fatal("key past its period not reported rotation-due")
	}
	if n, err := svc.RotateDue(ctx); err != nil || n < 1 {
		t.Fatalf("scheduled rotation = %d, %v", n, err)
	}
	if got, _ := svc.GetKey(ctx, me, transitScope, auto.Name); got.LatestVersion != 2 || got.RotationDue {
		t.Fatalf("after scheduled rotation: v%d due=%v", got.LatestVersion, got.RotationDue)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='transit.key_rotated' AND object_id='`+auto.ID+`'`); got != 1 {
		t.Fatalf("scheduled rotation events = %d", got)
	}

	for _, custody := range []string{"software", "external"} {
		k := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "doomed-" + custody, Algorithm: "xchacha20-poly1305", Custody: custody})
		ref := ""
		if custody == "external" {
			ref = queryString(t, db, `SELECT external_ref FROM transit_key_versions WHERE key_id='`+k.ID+`'`)
			if !ext.Holds(ref) {
				t.Fatal("provider does not hold the new key's material")
			}
		}
		if _, err := svc.ChangeKeyState(ctx, me, transitScope, k.Name, "schedule-deletion", 24*time.Hour); err != nil {
			t.Fatal(err)
		}
		if n, err := svc.PurgeDue(ctx); err != nil || n != 0 {
			t.Fatalf("purged %d before the delay: %v", n, err)
		}
		svc.Now = func() time.Time { return start.Add(3*time.Hour + 25*time.Hour) }
		// Once the delay has elapsed the purge may already be destroying
		// material, so the cancel window is closed.
		_, err := svc.ChangeKeyState(ctx, me, transitScope, k.Name, "cancel-deletion", 0)
		wantConflictCause(t, err, "state")
		if n, err := svc.PurgeDue(ctx); err != nil || n < 1 {
			t.Fatalf("purge = %d, %v", n, err)
		}
		if _, err := svc.GetKey(ctx, me, transitScope, k.Name); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("destroyed key still resolves: %v", err)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM transit_key_versions WHERE key_id='`+k.ID+`' AND (material_ciphertext IS NOT NULL OR external_ref IS NOT NULL)`); got != 0 {
			t.Fatalf("destroyed key kept %d material rows", got)
		}
		if ref != "" && ext.Holds(ref) {
			t.Fatal("external material survived the purge")
		}
		// The name is free again; the tombstone keeps the old id.
		mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: k.Name, Algorithm: "ed25519"})
		svc.Now = func() time.Time { return start.Add(3 * time.Hour) }
	}
}

// runTransitSchedulerPaging pins that the rotation sweep reads past a full page
// of candidates that are not due: more than one sweep batch (100) of rotating
// keys sit before a due key in id order, and the due key still rotates.
func runTransitSchedulerPaging(t *testing.T, db *store.DB, svc *service.Transit) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	start := time.Now().UTC()
	defer func() { svc.Now = nil }()
	svc.Now = func() time.Time { return start.Add(2 * time.Hour) }
	for i := range 101 {
		mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: fmt.Sprintf("paging-fresh-%03d", i), Algorithm: "ed25519", RotationPeriodSeconds: 3600})
	}
	// Created last (highest UUIDv7 id) but stamped earliest: the only due key.
	svc.Now = func() time.Time { return start }
	due := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "paging-due", Algorithm: "ed25519", RotationPeriodSeconds: 3600})
	if got := queryInt(t, db, `SELECT COUNT(*) FROM transit_keys WHERE state='active' AND rotation_period_seconds>0 AND id<'`+due.ID+`'`); got <= 100 {
		t.Fatalf("only %d rotating keys precede the due key; the test needs more than one batch", got)
	}
	svc.Now = func() time.Time { return start.Add(90 * time.Minute) }
	if n, err := svc.RotateDue(ctx); err != nil || n < 1 {
		t.Fatalf("scheduled rotation = %d, %v", n, err)
	}
	if got, err := svc.GetKey(ctx, me, transitScope, due.Name); err != nil || got.LatestVersion != 2 {
		t.Fatalf("due key past the first page was not rotated: v%d, %v", got.LatestVersion, err)
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM transit_keys WHERE name LIKE 'paging-fresh-%' AND latest_version<>1`); got != 0 {
		t.Fatalf("%d keys rotated before their period elapsed", got)
	}
}

// assertTransitAuditClean greps both audit tables for the plaintexts, contexts
// and outputs the suite used: none may appear in any form.
func assertTransitAuditClean(t *testing.T, db *store.DB) {
	for _, secret := range []string{"4111-1111-1111-1111", "order-7", "session-9", "payload", "hikyo:v"} {
		for _, needle := range []string{secret, base64.StdEncoding.EncodeToString([]byte(secret))} {
			n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE payload LIKE '%`+needle+`%'`)
			if n != 0 {
				t.Errorf("audit trail carries %q in %d events", needle, n)
			}
		}
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='transit.operation'`); got == 0 {
		t.Error("no transit.operation events were written")
	}
	if got := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='transit.operation' AND outcome='denied'`); got == 0 {
		t.Error("no denied transit.operation events survived their rollback")
	}
}

// runTransitLifecycle gives every transit.* audit type a real emitter for the
// registry-emitter closure check (audit_e2e_test.go).
func runTransitLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	ctx := tctx(t)
	svc, _ := transitEnv(t, db, "_audit")
	me := service.LocalPrincipal(alice)
	start := time.Now().UTC()
	svc.Now = func() time.Time { return start }
	k := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "audit-lifecycle", Algorithm: "xchacha20-poly1305", RotationPeriodSeconds: 3600})
	if _, err := svc.Encrypt(ctx, me, transitScope, k.Name, []byte("x"), nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sign(ctx, me, transitScope, k.Name, []byte("x"), 0); err == nil {
		t.Fatal("encryption key signed")
	}
	if _, err := svc.RotateKey(ctx, me, transitScope, k.Name); err != nil {
		t.Fatal(err)
	}
	two := uint32(2)
	if _, err := svc.ConfigureKey(ctx, me, transitScope, k.Name, service.ConfigureTransitKeyRequest{MinDecryptVersion: &two, MinEncryptVersion: &two}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.TrimKey(ctx, me, transitScope, k.Name); err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return start.Add(2 * time.Hour) }
	if _, err := svc.RotateDue(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ChangeKeyState(ctx, me, transitScope, k.Name, "schedule-deletion", 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return start.Add(30 * time.Hour) }
	if n, err := svc.PurgeDue(ctx); err != nil || n != 1 {
		t.Fatalf("transit purge = %d, %v", n, err)
	}
}

// runTransitReencrypt proves the key hierarchy covers transit material (ADR
// D3): after rotate-dek and reencrypt, every version's material is sealed under
// the new project DEK version, the retired DEK version holds zero references,
// and ciphertext produced before the rotation still decrypts.
func runTransitReencrypt(t *testing.T, db *store.DB, svc *service.Transit) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "hierarchy-covered", Algorithm: "xchacha20-poly1305"})
	before, err := svc.Encrypt(ctx, me, transitScope, key.Name, []byte("sealed before rotate-dek"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	material := func() []byte {
		var raw string
		if db.Engine() == store.EnginePostgres {
			raw = queryString(t, db, `SELECT encode(material_ciphertext,'hex') FROM transit_key_versions WHERE key_id='`+key.ID+`'`)
		} else {
			raw = queryString(t, db, `SELECT hex(material_ciphertext) FROM transit_key_versions WHERE key_id='`+key.ID+`'`)
		}
		return []byte(strings.ToLower(raw))
	}
	sealedBefore := material()
	kr := probeKeyring(t, db)
	rotation := &service.Rotation{DB: db, Keyring: kr, RootKey: probeRootSource{db: db}}
	if _, err := rotation.RotateDEK(ctx, service.LocalPrincipal(root), service.DEKScope{OrgID: string(orgA), ProjectID: string(prjA1)}); err != nil {
		t.Fatal(err)
	}
	re := &service.Reencrypt{DB: db, Keyring: kr, ChunkPause: -1}
	if _, err := re.ReencryptProject(ctx, service.LocalPrincipal(root), string(orgA), string(prjA1)); err != nil {
		t.Fatalf("reencrypt with transit material present: %v", err)
	}
	if bytes.Equal(material(), sealedBefore) {
		t.Fatal("reencrypt did not re-seal the transit material")
	}
	after, err := svc.Decrypt(ctx, me, transitScope, key.Name, before.Value, nil)
	if err != nil || string(after.Plaintext) != "sealed before rotate-dek" {
		t.Fatalf("ciphertext from before rotate-dek no longer decrypts: %v", err)
	}
	if _, err := svc.RotateKey(ctx, me, transitScope, key.Name); err != nil {
		t.Fatalf("transit rotation under the new DEK version: %v", err)
	}
}
