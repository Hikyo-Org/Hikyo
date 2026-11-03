package isolation

import (
	"bytes"
	"crypto/mldsa"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/transit"
	"github.com/Hikyo-Org/hikyo/internal/transit/transittest"
)

// The transit ML-DSA-65 signing key end to end (#1129, post-quantum migration
// DEC-02, transit ADR amendment): create, sign, verify with the standard library
// alone, rotate, apply version policy, compromise, retire, destroy, plus the
// custody, authorization, restart, re-encryption and rotation-race rules, on both
// engines and under both custody providers. The public HTTP surface is HOLD (the
// API freeze refuses the enum growth), so this drives the real service,
// chokepoint, store and audit trail; the handler-level refusal is tested in
// internal/server.

// independentVerify checks a transit signature value with nothing but the
// standard library and the published layout: "hikyo:v<N>:" then base64url of
// the two-byte identifier 0x0905 and the FIPS 204 signature, under the Transit
// label as context.
func independentVerify(t *testing.T, publicKey []byte, message []byte, value string) bool {
	t.Helper()
	_, enc, ok := strings.Cut(strings.TrimPrefix(value, "hikyo:v"), ":")
	if !ok {
		t.Fatalf("not a transit value: %.20q", value)
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(enc)
	if err != nil || len(payload) != 3311 || payload[0] != 0x09 || payload[1] != 0x05 {
		t.Fatalf("payload shape: len %d err %v", len(payload), err)
	}
	pk, err := mldsa.NewPublicKey(mldsa.MLDSA65(), publicKey)
	if err != nil {
		t.Fatalf("published public key is not a valid ML-DSA-65 key: %v", err)
	}
	return mldsa.Verify(pk, message, payload[2:], &mldsa.Options{Context: "hikyo/transit/sign/v1"}) == nil
}

func publicKeyOf(t *testing.T, view service.TransitKeyView, version uint32) []byte {
	t.Helper()
	for _, v := range view.Versions {
		if v.Version == version {
			if len(v.PublicKey) != 1952 {
				t.Fatalf("v%d public key is %d bytes", version, len(v.PublicKey))
			}
			return v.PublicKey
		}
	}
	t.Fatalf("no version %d in %+v", version, view.Versions)
	return nil
}

func TestTransitMLDSA65EndToEnd(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc, ext := transitEnv(t, db, "_pq")
		for _, custody := range []string{"software", "external"} {
			t.Run(custody, func(t *testing.T) { runTransitMLDSALifecycle(t, db, svc, ext, custody) })
		}
		t.Run("authorization_before_crypto", func(t *testing.T) { runTransitMLDSAAuthorization(t, db, svc, ext) })
		t.Run("external_unavailable_fails_closed", func(t *testing.T) { runTransitMLDSAExternalDown(t, svc, ext) })
		t.Run("concurrent_rotation", func(t *testing.T) { runTransitMLDSARotationRace(t, svc) })
		t.Run("restart_dek_rotation_and_reencrypt", func(t *testing.T) { runTransitMLDSAReencrypt(t, db, svc, ext) })
		t.Run("creation_refusals", func(t *testing.T) { runTransitMLDSACreationRefusals(t, svc) })
		t.Run("audit_never_carries_signatures_or_keys", func(t *testing.T) { assertTransitMLDSAAuditClean(t, db) })
	})
}

func runTransitMLDSALifecycle(t *testing.T, db *store.DB, svc *service.Transit, ext *transittest.External, custody string) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	name := func(s string) string { return "pq-" + custody + "-" + s }

	// --- create: a distinct, immutable identity -------------------------------
	key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: name("signer"), Algorithm: "mldsa65", Custody: custody})
	if key.Algorithm != "mldsa65" || key.State != "active" || key.LatestVersion != 1 {
		t.Fatalf("created key = %+v", key.TransitKeyRecord)
	}
	if strings.Join(key.AllowedOperations, ",") != "sign,verify" {
		t.Fatalf("default operations = %v", key.AllowedOperations)
	}
	if key.Versions[0].HasMaterial == (custody == "external") || key.Versions[0].ExternalHeld != (custody == "external") {
		t.Fatalf("version custody columns wrong: %+v", key.Versions[0])
	}
	pub1 := publicKeyOf(t, key, 1)

	// --- sign, then verify with the standard library alone --------------------
	msg := []byte("payload to sign")
	s1, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s1.Value, "hikyo:v1:") || s1.KeyVersion != 1 {
		t.Fatalf("signature = %.20s v%d", s1.Value, s1.KeyVersion)
	}
	if !independentVerify(t, pub1, msg, s1.Value) {
		t.Fatal("the standard library refused a transit signature")
	}
	if independentVerify(t, pub1, []byte("tampered"), s1.Value) {
		t.Fatal("the standard library verified another message")
	}
	got, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s1.Value)
	if err != nil || !got.Valid || got.KeyVersion != 1 {
		t.Fatalf("verify = %+v, %v", got, err)
	}
	if bad, err := svc.Verify(ctx, me, transitScope, key.Name, []byte("tampered"), s1.Value); err != nil || bad.Valid {
		t.Fatalf("tampered message = %+v, %v", bad, err)
	}
	// The 64 KiB bound is exact, for sign and verify.
	big := bytes.Repeat([]byte{0x5a}, crypto.MaxTransitPlaintextBytes)
	bs, err := svc.Sign(ctx, me, transitScope, key.Name, big, 0)
	if err != nil {
		t.Fatalf("message at the bound: %v", err)
	}
	if v, err := svc.Verify(ctx, me, transitScope, key.Name, big, bs.Value); err != nil || !v.Valid {
		t.Fatalf("verify at the bound = %+v, %v", v, err)
	}
	if _, err := svc.Sign(ctx, me, transitScope, key.Name, append(big, 0), 0); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("oversized message signed: %v", err)
	}
	if _, err := svc.Verify(ctx, me, transitScope, key.Name, append(big, 0), bs.Value); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("oversized message verified: %v", err)
	}
	// Operations the key cannot perform are refused by the key's own policy.
	for _, err := range []error{
		func() error { _, e := svc.Encrypt(ctx, me, transitScope, key.Name, msg, nil, 0); return e }(),
		func() error { _, e := svc.HMAC(ctx, me, transitScope, key.Name, msg, 0); return e }(),
		func() error { _, e := svc.DataKey(ctx, me, transitScope, key.Name, 256, nil, false); return e }(),
	} {
		if !errors.Is(err, service.ErrTransitForbidden) {
			t.Fatalf("signing key performed another operation: %v", err)
		}
	}

	// --- substitution and malformed values --------------------------------
	other := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: name("other"), Algorithm: "mldsa65", Custody: custody})
	otherSig, err := svc.Sign(ctx, me, transitScope, other.Name, msg, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := svc.Verify(ctx, me, transitScope, key.Name, msg, otherSig.Value); err != nil || v.Valid {
		t.Fatalf("another key's signature = %+v, %v", v, err)
	}
	classic := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: name("classic"), Algorithm: "ed25519", Custody: custody})
	classicSig, err := svc.Sign(ctx, me, transitScope, classic.Name, msg, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(classicSig.Value) != len("hikyo:v1:")+86 {
		t.Fatalf("ed25519 wire form changed: %d characters", len(classicSig.Value))
	}
	if v, err := svc.Verify(ctx, me, transitScope, key.Name, msg, classicSig.Value); err != nil || v.Valid {
		t.Fatalf("ed25519 signature under the ml-dsa key = %+v, %v", v, err)
	}
	if v, err := svc.Verify(ctx, me, transitScope, classic.Name, msg, s1.Value); err != nil || v.Valid {
		t.Fatalf("ml-dsa signature under the ed25519 key = %+v, %v", v, err)
	}
	_, payload, err := crypto.ParseTransitValue(s1.Value)
	if err != nil {
		t.Fatal(err)
	}
	flip := func(b []byte, i int) []byte { c := bytes.Clone(b); c[i] ^= 1; return c }
	for what, p := range map[string][]byte{
		"truncated":        payload[:len(payload)-1],
		"extended":         append(bytes.Clone(payload), 0),
		"no identifier":    payload[2:],
		"wrong identifier": flip(payload, 1),
		"flipped bit":      flip(payload, 500),
	} {
		v, err := svc.Verify(ctx, me, transitScope, key.Name, msg, crypto.FormatTransitValue(1, p))
		if err != nil || v.Valid {
			t.Errorf("%s signature = %+v, %v", what, v, err)
		}
	}
	if _, err := svc.Verify(ctx, me, transitScope, key.Name, msg, "hikyo:v1:not base64!"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("malformed value = %v", err)
	}
	if _, err := svc.Verify(ctx, me, transitScope, key.Name, msg, crypto.FormatTransitValue(2, payload)); err == nil {
		t.Error("a version the key does not have verified")
	} else {
		wantConflictCause(t, err, "version")
	}

	// --- rotation never converts the key; versions are independent ------------
	rotated, err := svc.RotateKey(ctx, me, transitScope, key.Name)
	if err != nil || rotated.Algorithm != "mldsa65" || rotated.LatestVersion != 2 {
		t.Fatalf("rotate = %+v, %v", rotated.TransitKeyRecord, err)
	}
	pub2 := publicKeyOf(t, rotated, 2)
	if bytes.Equal(pub1, pub2) {
		t.Fatal("rotation reused the public key")
	}
	s2, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0)
	if err != nil || s2.KeyVersion != 2 {
		t.Fatalf("post-rotation sign = v%d, %v", s2.KeyVersion, err)
	}
	if !independentVerify(t, pub2, msg, s2.Value) || independentVerify(t, pub1, msg, s2.Value) {
		t.Fatal("version 2 signature does not verify under exactly the version 2 key")
	}
	// Historical signatures keep verifying until the floor moves.
	if v, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s1.Value); err != nil || !v.Valid {
		t.Fatalf("historical v1 signature = %+v, %v", v, err)
	}
	// A v1 signature relabelled as v2 does not verify.
	if v, err := svc.Verify(ctx, me, transitScope, key.Name, msg, crypto.FormatTransitValue(2, payload)); err != nil || v.Valid {
		t.Fatalf("relabelled version = %+v, %v", v, err)
	}
	pinned, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 1)
	if err != nil || pinned.KeyVersion != 1 || !independentVerify(t, pub1, msg, pinned.Value) {
		t.Fatalf("explicit v1 sign = %+v, %v", pinned, err)
	}

	// --- minimum-version policy -------------------------------------------------
	two := uint32(2)
	if _, err := svc.ConfigureKey(ctx, me, transitScope, key.Name, service.ConfigureTransitKeyRequest{MinEncryptVersion: &two, MinDecryptVersion: &two}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s1.Value); err == nil {
		t.Fatal("v1 verified below min_decrypt_version")
	} else {
		wantConflictCause(t, err, "version")
	}
	if _, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 1); err == nil {
		t.Fatal("v1 signed below min_encrypt_version")
	} else {
		wantConflictCause(t, err, "version")
	}
	if v, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s2.Value); err != nil || !v.Valid {
		t.Fatalf("v2 at the floor = %+v, %v", v, err)
	}
	if _, deleted, err := svc.TrimKey(ctx, me, transitScope, key.Name); err != nil || deleted != 1 {
		t.Fatalf("trim = %d, %v", deleted, err)
	}
	if _, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s1.Value); err == nil {
		t.Fatal("a trimmed version verified")
	}

	// --- compromise: forgeable versions refuse both ways ----------------------
	change := func(action string) error {
		_, err := svc.ChangeKeyState(ctx, me, transitScope, key.Name, action, 0)
		return err
	}
	if err := change("compromise"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0); err == nil {
		t.Fatal("a compromised version signed")
	} else {
		wantConflictCause(t, err, "compromised")
	}
	if _, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s2.Value); err == nil {
		t.Fatal("a compromised version verified")
	} else {
		wantConflictCause(t, err, "compromised")
	}
	if _, err := svc.RotateKey(ctx, me, transitScope, key.Name); err != nil {
		t.Fatal(err)
	}
	s3, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0)
	if err != nil || s3.KeyVersion != 3 {
		t.Fatalf("sign after recovery = v%d, %v", s3.KeyVersion, err)
	}
	if v, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s3.Value); err != nil || !v.Valid {
		t.Fatalf("verify after recovery = %+v, %v", v, err)
	}
	if _, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s2.Value); err == nil {
		t.Fatal("the compromised version verifies again after rotation")
	}

	// --- disable, retire, enable --------------------------------------------------
	if err := change("disable"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s3.Value); err == nil {
		t.Fatal("a disabled key verified")
	} else {
		wantConflictCause(t, err, "state")
	}
	if err := change("retire"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0); err == nil {
		t.Fatal("a retired key signed")
	} else {
		wantConflictCause(t, err, "state")
	}
	if v, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s3.Value); err != nil || !v.Valid {
		t.Fatalf("a retired key must still verify: %+v, %v", v, err)
	}
	if err := change("enable"); err != nil {
		t.Fatal(err)
	}

	// --- no export path: the view carries public keys only ------------------
	view, err := svc.GetKey(ctx, me, transitScope, key.Name)
	if err != nil {
		t.Fatal(err)
	}
	if view.Algorithm != "mldsa65" {
		t.Fatalf("view = %+v", view.TransitKeyRecord)
	}

	// --- destroy ------------------------------------------------------------------
	start := time.Now().UTC()
	svc.Now = func() time.Time { return start }
	defer func() { svc.Now = nil }()
	if err := change("schedule-deletion"); err != nil {
		t.Fatal(err)
	}
	svc.Now = func() time.Time { return start.Add(8 * 24 * time.Hour) }
	if n, err := svc.PurgeDue(ctx); err != nil || n < 1 {
		t.Fatalf("purge = %d, %v", n, err)
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM transit_key_versions WHERE key_id='`+key.ID+`' AND (material_ciphertext IS NOT NULL OR external_ref IS NOT NULL)`); n != 0 {
		t.Fatalf("destroy left material for %d versions", n)
	}
	if _, err := svc.Verify(ctx, me, transitScope, key.Name, msg, s3.Value); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a destroyed key is still addressable: %v", err)
	}
}

// runTransitMLDSAAuthorization proves authorization runs before any private
// operation or custody call: a caller without crypto-use, a caller the key's
// entries exclude, and a caller outside the environment never reach the provider.
func runTransitMLDSAAuthorization(t *testing.T, db *store.DB, svc *service.Transit, ext *transittest.External) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "pq-authz", Algorithm: "mldsa65", Custody: "external"})
	msg := []byte("authz message body")
	sig, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0)
	if err != nil {
		t.Fatal(err)
	}
	before := ext.Calls.Load()
	for name, err := range map[string]error{
		"sign without crypto-use": func() error {
			_, e := svc.Sign(ctx, service.LocalPrincipal(reader), transitScope, key.Name, msg, 0)
			return e
		}(),
		"verify without crypto-use": func() error {
			_, e := svc.Verify(ctx, service.LocalPrincipal(reader), transitScope, key.Name, msg, sig.Value)
			return e
		}(),
		"sign from another org": func() error {
			_, e := svc.Sign(ctx, service.LocalPrincipal(bob), transitScope, key.Name, msg, 0)
			return e
		}(),
		"sign in a sibling environment": func() error {
			_, e := svc.Sign(ctx, me, domain.Scope{Org: orgA, Project: prjA1, Env: "env_prod"}, key.Name, msg, 0)
			return e
		}(),
		"rotate without crypto-manage": func() error {
			_, e := svc.RotateKey(ctx, service.LocalPrincipal(reader), transitScope, key.Name)
			return e
		}(),
	} {
		if !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s = %v, want the uniform not-found", name, err)
		}
	}
	if ext.Calls.Load() != before {
		t.Fatal("a refused caller reached the custody provider")
	}

	// A per-key entry narrows crypto-use: a workload that holds crypto-use may
	// verify but not sign.
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_pq_ident','usr_alice','manage-identities','org_a','prj_a1',NULL,`+ts+`)`)
	sa, err := identitySvc(db).CreateServiceAccount(ctx, me, prjScope(), "pq-workload", domain.ClassWorkload)
	if err != nil {
		t.Fatal(err)
	}
	minted, err := identitySvc(db).MintCredential(ctx, me, prjScope(), sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatal(err)
	}
	workload := service.Bearer(minted.Value)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_pq_sa','`+string(sa.Principal)+`','crypto-use','org_a','prj_a1','env_a1',`+ts+`)`)
	entries := []service.TransitCallerEntry{{PrincipalID: string(sa.Principal), Operations: []string{"verify"}}}
	if _, err := svc.ConfigureKey(ctx, me, transitScope, key.Name, service.ConfigureTransitKeyRequest{Callers: &entries}); err != nil {
		t.Fatal(err)
	}
	before = ext.Calls.Load()
	if _, err := svc.Sign(ctx, workload, transitScope, key.Name, msg, 0); !errors.Is(err, service.ErrTransitForbidden) {
		t.Fatalf("sign past the caller entry = %v, want forbidden", err)
	}
	if _, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0); !errors.Is(err, service.ErrTransitForbidden) {
		t.Fatalf("an unlisted caller signed = %v, want forbidden", err)
	}
	if ext.Calls.Load() != before {
		t.Fatal("a caller refused by the key's entries reached the custody provider")
	}
	if v, err := svc.Verify(ctx, workload, transitScope, key.Name, msg, sig.Value); err != nil || !v.Valid {
		t.Fatalf("verify within the caller entry = %+v, %v", v, err)
	}
	// Verification is local: it performs no private operation at the provider.
	if ext.Calls.Load() != before {
		t.Fatal("public verification called the custody provider")
	}
	bad := []service.TransitCallerEntry{{PrincipalID: string(sa.Principal), Operations: []string{"encrypt"}}}
	if _, err := svc.ConfigureKey(ctx, me, transitScope, key.Name, service.ConfigureTransitKeyRequest{Callers: &bad}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("caller entry naming an operation the key cannot do = %v", err)
	}
}

// runTransitMLDSAExternalDown proves D3 for the new algorithm: unavailable or
// unregistered custody refuses sign and verify alike, does no work, and is never
// served by software custody.
func runTransitMLDSAExternalDown(t *testing.T, svc *service.Transit, ext *transittest.External) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "pq-hsm", Algorithm: "mldsa65", Custody: "external"})
	msg := []byte("down")
	sig, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0)
	if err != nil {
		t.Fatal(err)
	}
	ext.SetAvailable(false)
	defer ext.SetAvailable(true)
	before := ext.Calls.Load()
	if _, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("sign with custody down = %v", err)
	}
	if _, err := svc.Verify(ctx, me, transitScope, key.Name, msg, sig.Value); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("verify with custody down = %v (D3: unavailable custody refuses every operation)", err)
	}
	if _, err := svc.RotateKey(ctx, me, transitScope, key.Name); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("rotate with custody down = %v", err)
	}
	if _, err := svc.CreateKey(ctx, me, transitScope, service.CreateTransitKeyRequest{Name: "pq-hsm-2", Algorithm: "mldsa65", Custody: "external"}); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("create with custody down = %v", err)
	}
	if ext.Calls.Load() != before {
		t.Fatal("an unavailable provider did work")
	}
	bare := &service.Transit{DB: svc.DB, Keyring: svc.Keyring, Custody: transit.NewRegistry(&transit.Software{Keyring: svc.Keyring})}
	ext.SetAvailable(true)
	if _, err := bare.Sign(ctx, me, transitScope, key.Name, msg, 0); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("unregistered external custody signed = %v", err)
	}
	if _, err := bare.Verify(ctx, me, transitScope, key.Name, msg, sig.Value); !errors.Is(err, transit.ErrUnavailable) {
		t.Fatalf("unregistered external custody verified = %v", err)
	}
	if _, err := bare.CreateKey(ctx, me, transitScope, service.CreateTransitKeyRequest{Name: "pq-hsm-3", Algorithm: "mldsa65", Custody: "external"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("create on an instance without external custody = %v", err)
	}
}

// runTransitMLDSARotationRace races rotations of an ML-DSA key: versions stay
// contiguous, every loser is a retryable conflict, and every surviving version
// has its own public key and signs verifiably.
func runTransitMLDSARotationRace(t *testing.T, svc *service.Transit) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "pq-raced", Algorithm: "mldsa65"})
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
	seen := map[string]bool{}
	for i, v := range view.Versions {
		if v.Version != uint32(i+1) || len(v.PublicKey) != 1952 || seen[string(v.PublicKey)] {
			t.Fatalf("version %d: gap, wrong public key size or duplicate public key", v.Version)
		}
		seen[string(v.PublicKey)] = true
		s, err := svc.Sign(ctx, me, transitScope, key.Name, []byte("race"), v.Version)
		if err != nil || !independentVerify(t, v.PublicKey, []byte("race"), s.Value) {
			t.Fatalf("version %d does not sign verifiably: %v", v.Version, err)
		}
	}
}

// runTransitMLDSAReencrypt proves a restart and the key hierarchy cover the
// sealed material of an ML-DSA key: the public key and its earlier signatures are
// unchanged after rotate-dek and reencrypt, and the same sealed material still
// derives the same key under the new project DEK.
func runTransitMLDSAReencrypt(t *testing.T, db *store.DB, svc *service.Transit, ext *transittest.External) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "pq-hierarchy", Algorithm: "mldsa65"})
	pub := publicKeyOf(t, key, 1)
	msg := []byte("sealed before rotate-dek")
	before, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0)
	if err != nil {
		t.Fatal(err)
	}
	sealed := queryString(t, db, sealedHexQuery(db, key.ID))

	restarted := &service.Transit{DB: db, Keyring: probeKeyring(t, db), Custody: transit.NewRegistry(&transit.Software{Keyring: probeKeyring(t, db)}, ext)}
	if v, err := restarted.Verify(ctx, me, transitScope, key.Name, msg, before.Value); err != nil || !v.Valid {
		t.Fatalf("restarted service cannot verify: %+v, %v", v, err)
	}
	after, err := restarted.Sign(ctx, me, transitScope, key.Name, msg, 0)
	if err != nil || !independentVerify(t, pub, msg, after.Value) {
		t.Fatalf("restarted service signs under another key: %v", err)
	}

	kr := probeKeyring(t, db)
	rotation := &service.Rotation{DB: db, Keyring: kr, RootKey: probeRootSource{db: db}}
	if _, err := rotation.RotateDEK(ctx, service.LocalPrincipal(root), service.DEKScope{OrgID: string(orgA), ProjectID: string(prjA1)}); err != nil {
		t.Fatal(err)
	}
	re := &service.Reencrypt{DB: db, Keyring: kr, ChunkPause: -1}
	if _, err := re.ReencryptProject(ctx, service.LocalPrincipal(root), string(orgA), string(prjA1)); err != nil {
		t.Fatalf("reencrypt with ML-DSA material present: %v", err)
	}
	if queryString(t, db, sealedHexQuery(db, key.ID)) == sealed {
		t.Fatal("reencrypt did not re-seal the ML-DSA material")
	}
	if v, err := svc.Verify(ctx, me, transitScope, key.Name, msg, before.Value); err != nil || !v.Valid {
		t.Fatalf("a signature from before rotate-dek no longer verifies: %+v, %v", v, err)
	}
	viewAfter, err := svc.GetKey(ctx, me, transitScope, key.Name)
	if err != nil || !bytes.Equal(publicKeyOf(t, viewAfter, 1), pub) {
		t.Fatalf("the public key moved across reencrypt: %v", err)
	}
	post, err := svc.Sign(ctx, me, transitScope, key.Name, msg, 0)
	if err != nil || !independentVerify(t, pub, msg, post.Value) {
		t.Fatalf("sealed material derives another key after reencrypt: %v", err)
	}
	if _, err := svc.RotateKey(ctx, me, transitScope, key.Name); err != nil {
		t.Fatalf("rotation under the new DEK version: %v", err)
	}
}

func sealedHexQuery(db *store.DB, keyID string) string {
	if db.Engine() == store.EnginePostgres {
		return `SELECT encode(material_ciphertext,'hex') FROM transit_key_versions WHERE key_id='` + keyID + `' AND version=1`
	}
	return `SELECT hex(material_ciphertext) FROM transit_key_versions WHERE key_id='` + keyID + `' AND version=1`
}

func runTransitMLDSACreationRefusals(t *testing.T, svc *service.Transit) {
	ctx := tctx(t)
	me := service.LocalPrincipal(alice)
	for _, req := range []service.CreateTransitKeyRequest{
		{Name: "pq-mixed", Algorithm: "mldsa65", AllowedOperations: []string{"sign", "encrypt"}},
		{Name: "pq-hmac", Algorithm: "mldsa65", AllowedOperations: []string{"hmac"}},
		{Name: "pq-dup-ops", Algorithm: "mldsa65", AllowedOperations: []string{"sign", "sign"}},
		{Name: "pq-export", Algorithm: "mldsa65", Exportable: true},
		{Name: "pq-hsm", Algorithm: "mldsa65", Custody: "hsm"},
		{Name: "pq-44", Algorithm: "mldsa44"},
		{Name: "pq-87", Algorithm: "mldsa87"},
		{Name: "pq-upper", Algorithm: "MLDSA65"},
		{Name: "pq-dashed", Algorithm: "ml-dsa-65"},
		{Name: "pq-padded", Algorithm: "mldsa65 "},
		{Name: "pq-empty-alg", Algorithm: ""},
		{Name: "pq-rot", Algorithm: "mldsa65", RotationPeriodSeconds: 60},
		{Name: "Bad Name", Algorithm: "mldsa65"},
	} {
		if _, err := svc.CreateKey(ctx, me, transitScope, req); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("create %+v = %v, want invalid", req, err)
		}
	}
	k := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "pq-dup", Algorithm: "mldsa65"})
	// A duplicate name is a conflict whatever the algorithm; an ed25519 key
	// cannot take over an ML-DSA key's name, and nothing converts a key.
	for _, alg := range []string{"mldsa65", "ed25519"} {
		if _, err := svc.CreateKey(ctx, me, transitScope, service.CreateTransitKeyRequest{Name: k.Name, Algorithm: alg}); !errors.Is(err, domain.ErrConflict) {
			t.Errorf("duplicate name under %s = %v, want conflict", alg, err)
		}
	}
	view, err := svc.GetKey(ctx, me, transitScope, k.Name)
	if err != nil || view.Algorithm != "mldsa65" || view.ID != k.ID {
		t.Fatalf("key identity changed: %+v %v", view.TransitKeyRecord, err)
	}
}

// assertTransitMLDSAAuditClean checks the durable audit trail of the whole run:
// no signature, public key or message body, and the decision-only records of the
// operations exist.
func assertTransitMLDSAAuditClean(t *testing.T, db *store.DB) {
	for _, needle := range []string{
		"hikyo:v", "payload to sign", "authz message body", "sealed before rotate-dek",
		base64.StdEncoding.EncodeToString([]byte("payload to sign")),
		"hikyo/transit/sign/v1",
	} {
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE payload LIKE '%`+needle+`%'`); n != 0 {
			t.Errorf("audit trail carries %q in %d events", needle, n)
		}
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='transit.operation' AND payload LIKE '%"operation":"sign"%'`); n == 0 {
		t.Error("no sign operations were audited")
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='transit.operation' AND payload LIKE '%"operation":"verify"%'`); n == 0 {
		t.Error("no verify operations were audited")
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='transit.key_created' AND payload LIKE '%mldsa65%'`); n == 0 {
		t.Error("no ML-DSA key creation was audited with its algorithm")
	}
}
