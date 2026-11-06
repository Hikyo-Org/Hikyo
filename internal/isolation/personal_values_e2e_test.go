package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/delivery"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/webauthntest"
)

// Personal values (#1235, personal-values ADR). The owner is the ceremony
// fixture's human; a second, invited project administrator holds every
// administrative capability and must never read the owner's value.

const (
	personalPlain = "personal-synthetic-owner-value"
	personalOnly  = "PERSONAL_ONLY"
)

type personalEnv struct {
	ceremonyEnv
	db       *store.DB
	pv       *service.PersonalValues
	adminTok string
	adminDev *webauthntest.Device
	keyA     string
}

func personalFixture(t *testing.T, db *store.DB, username string) personalEnv {
	t.Helper()
	ctx := t.Context()
	f := ceremonyFixture(t, db, username)
	invite, err := (&service.Grants{DB: db, Auth: f.admin.auth}).InviteMember(ctx, service.LocalPrincipal(f.admin.boot.PrincipalID),
		service.InviteSpec{Scope: domain.Scope{Org: orgA}, Username: username + "-admin", Template: domain.TemplateAdmin, Delivery: "response"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.admin.auth.EstablishCredential(ctx, invite.Authority, ceremonyPassword); err != nil {
		t.Fatal(err)
	}
	login, err := f.admin.auth.LocalLogin(ctx, username+"-admin", ceremonyPassword, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	dev := webauthntest.New(ceremonyRPID, ceremonyOrigin)
	adminTok := enrolPasskeyAndStepUp(t, f.admin.auth, ctx, login.SessionToken, ceremonyPassword, dev)
	return personalEnv{
		ceremonyEnv: f, db: db,
		pv:       &service.PersonalValues{DB: db, Keyring: f.values.Keyring, Auth: f.admin.auth},
		adminTok: adminTok, adminDev: dev, keyA: "key_" + ceremonySecretA,
	}
}

// reauth runs the per-key passkey ceremony personal set and reveal require and
// returns the rotated session token.
func personalReauth(t *testing.T, auth *service.Auth, token string, dev *webauthntest.Device, keyID string) string {
	t.Helper()
	intent, err := service.NewDisclosureReauthIntent(service.PurposeReveal, []string{string(envA1)}, []string{keyID})
	if err != nil {
		t.Fatal(err)
	}
	opts, err := auth.ReauthPasskeyStart(t.Context(), token, intent)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := dev.Assert(opts)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := auth.ReauthPasskeyFinish(t.Context(), token, resp)
	if err != nil {
		t.Fatal(err)
	}
	return fresh.SessionToken
}

func (e *personalEnv) setOwn(t *testing.T, keyName, keyID, value string) {
	t.Helper()
	e.admin.token = personalReauth(t, e.admin.auth, e.admin.token, e.device, keyID)
	if _, err := e.pv.Set(t.Context(), service.Bearer(e.admin.token), envScope(envA1), keyName, value); err != nil {
		t.Fatalf("personal set %s: %v", keyName, err)
	}
}

func deliveredKey(t *testing.T, r service.FetchResult, name string) service.DeliveredKey {
	t.Helper()
	for _, k := range r.Keys {
		if k.Name == name {
			return k
		}
	}
	t.Fatalf("delivery lacks key %s", name)
	return service.DeliveredKey{}
}

// valueIs reports a delivered value without ever printing it.
func valueIs(k service.DeliveredKey, want string) bool { return k.Value != nil && *k.Value == want }

func TestPersonalValueOverlayAndAdministratorBlindness(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-overlay")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)

		own, err := e.pv.List(ctx, service.Bearer(e.admin.token), envScope(envA1))
		if err != nil || len(own) != 1 || own[0].Name != ceremonySecretA || !own[0].Set || own[0].Value != "" {
			t.Fatalf("own list: err=%v entries=%d", err, len(own))
		}

		minted, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, true)
		del := deliverySvc(t, db)
		first, err := del.Fetch(ctx, minted.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		a := deliveredKey(t, first, ceremonySecretA)
		if !valueIs(a, personalPlain) || a.Personal != service.PersonalDelivered {
			t.Fatalf("personal value did not take precedence (personal=%q)", a.Personal)
		}
		if b := deliveredKey(t, first, ceremonySecretB); !valueIs(b, "plaintext-"+ceremonySecretB) || b.Personal != "" {
			t.Fatal("a key without a personal value lost its shared value")
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='personal_value.delivered'`) != 1 {
			t.Fatal("personal delivery not audited exactly once")
		}
		again, err := del.Fetch(ctx, minted.Value, envScope(envA1), first.Cursor, service.FetchOptions{})
		if err != nil || !again.Current {
			t.Fatalf("unchanged personal state not current: err=%v", err)
		}
		// The owner replacing their own value is never answered current.
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain+"-replaced")
		replaced, err := del.Fetch(ctx, minted.Value, envScope(envA1), first.Cursor, service.FetchOptions{})
		if err != nil || replaced.Current || !valueIs(deliveredKey(t, replaced, ceremonySecretA), personalPlain+"-replaced") {
			t.Fatalf("owner replacement answered current=%v err=%v", replaced.Current, err)
		}
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)

		// The administrator: every route answers with their own (empty) state
		// or the shared value, never the owner's.
		admin := service.Bearer(e.adminTok)
		if mine, err := e.pv.List(ctx, admin, envScope(envA1)); err != nil || len(mine) != 0 {
			t.Fatalf("administrator list: err=%v entries=%d", err, len(mine))
		}
		e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
		if _, err := e.pv.Reveal(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("administrator reveal of a value they do not own = %v", err)
		}
		holders, err := e.pv.Members(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA)
		if err != nil || len(holders) != 1 || holders[0].Principal != e.admin.boot.PrincipalID || !holders[0].Set {
			t.Fatalf("members: err=%v holders=%d", err, len(holders))
		}
		// The administrator's own value for the same key is theirs alone.
		e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
		if _, err := e.pv.Set(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, "administrator-own-value"); err != nil {
			t.Fatal(err)
		}
		if mine, err := del.Fetch(ctx, minted.Value, envScope(envA1), "", service.FetchOptions{}); err != nil || !valueIs(deliveredKey(t, mine, ceremonySecretA), personalPlain) {
			t.Fatalf("administrator set overwrote the owner's value: err=%v", err)
		}
		if err := e.pv.Delete(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA); err != nil {
			t.Fatal(err)
		}
		e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
		shared, err := e.values.Get(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, true)
		if err != nil || shared.Value != "plaintext-"+ceremonySecretA {
			t.Fatalf("administrator value reveal did not return the shared value: err=%v", err)
		}
		adminDelivery, err := del.FetchAs(ctx, service.LocalPrincipal(custodian), envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if k := deliveredKey(t, adminDelivery, ceremonySecretA); k.Personal != "" || valueIs(k, personalPlain) {
			t.Fatal("non-developer delivery carried a personal value")
		}
		// No stored audit row, and no stored ciphertext, carries the plaintext.
		for _, table := range []string{"audit_tenant_events", "audit_instance_events"} {
			if n := queryInt(t, db, `SELECT COUNT(*) FROM `+table+` WHERE payload LIKE '%`+personalPlain+`%'`); n != 0 {
				t.Fatalf("%s holds the personal plaintext", table)
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM personal_values WHERE ciphertext LIKE '%`+personalPlain+`%'`); n != 0 {
			t.Fatal("personal value stored in plaintext")
		}

		// An unknown member and a nonholder are answered alike.
		if err := e.pv.DeleteMember(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, "nobody-at-all"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("delete of nonexistent member = %v", err)
		}
		if err := e.pv.DeleteMember(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, "personal-overlay-admin"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("delete of nonholder = %v", err)
		}
		// Administrator removal leaves the owner a notice, and run is refused
		// rather than silently falling back to the shared value.
		if err := e.pv.DeleteMember(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, "personal-overlay"); err != nil {
			t.Fatal(err)
		}
		after, err := del.Fetch(ctx, minted.Value, envScope(envA1), first.Cursor, service.FetchOptions{})
		if err != nil || after.Current {
			t.Fatalf("removal answered as current: err=%v", err)
		}
		if k := deliveredKey(t, after, ceremonySecretA); k.Value != nil || k.Personal != service.PersonalNotice || k.PersonalReason != service.PersonalCauseAdministrator {
			t.Fatalf("notice not delivered (personal=%q reason=%q)", k.Personal, k.PersonalReason)
		}
		// Dismissing the notice restores the shared value, and moves the cursor.
		if err := e.pv.Delete(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA); err != nil {
			t.Fatal(err)
		}
		restored, err := del.Fetch(ctx, minted.Value, envScope(envA1), after.Cursor, service.FetchOptions{})
		if err != nil || restored.Current {
			t.Fatalf("dismissal answered as current: err=%v", err)
		}
		if k := deliveredKey(t, restored, ceremonySecretA); !valueIs(k, "plaintext-"+ceremonySecretA) || k.Personal != "" {
			t.Fatal("shared value not restored after dismissal")
		}
		// The developer credential reaches personal values only through delivery.
		if _, err := e.pv.List(ctx, service.Bearer(minted.Value), envScope(envA1)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("developer credential on the personal surface = %v", err)
		}
	})
}

func TestPersonalValueWithheldWithoutConsentConfigOnlyOrQualification(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-withheld")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		del := deliverySvc(t, db)

		unconsented, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, false)
		r, err := del.Fetch(ctx, unconsented.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if k := deliveredKey(t, r, ceremonySecretA); k.Value != nil || k.Personal != service.PersonalWithheld || k.PersonalReason != service.PersonalReasonConsent {
			t.Fatalf("unconsented credential: personal=%q reason=%q", k.Personal, k.PersonalReason)
		}
		consented, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, true)
		r, err = del.Fetch(ctx, consented.Value, envScope(envA1), "", service.FetchOptions{Projection: delivery.ModeConfigOnly})
		if err != nil {
			t.Fatal(err)
		}
		if k := deliveredKey(t, r, ceremonySecretA); k.Value != nil || k.Personal != service.PersonalWithheld || k.PersonalReason != service.PersonalReasonConfigOnly {
			t.Fatalf("config-only: personal=%q reason=%q", k.Personal, k.PersonalReason)
		}
		// A change with no credential write (here the passkey row vanishing
		// underneath) suspends rather than deletes.
		before, err := del.Fetch(ctx, consented.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil || !valueIs(deliveredKey(t, before, ceremonySecretA), personalPlain) {
			t.Fatalf("consented delivery: err=%v", err)
		}
		execRaw(t, db, `DELETE FROM webauthn_credentials WHERE account_id='`+e.admin.accountID+`'`)
		if again, err := del.Fetch(ctx, consented.Value, envScope(envA1), before.Cursor, service.FetchOptions{}); err != nil || again.Current {
			t.Fatalf("suspension answered as current: err=%v", err)
		}
		r, err = del.Fetch(ctx, consented.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if k := deliveredKey(t, r, ceremonySecretA); k.Value != nil || k.PersonalReason != service.PersonalReasonSuspended {
			t.Fatalf("unqualified owner: personal=%q reason=%q", k.Personal, k.PersonalReason)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 {
			t.Fatal("suspension deleted the value")
		}
	})
}

func TestPersonalValueFactorLossDeletesAndLeavesNotice(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-factor-loss")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		keys, err := e.admin.auth.ListPasskeys(ctx, e.admin.token)
		if err != nil || len(keys) != 1 {
			t.Fatalf("passkeys: err=%v n=%d", err, len(keys))
		}
		removed, err := e.admin.auth.RemovePasskey(ctx, e.admin.token, keys[0].ID, ceremonyPassword, "")
		if err != nil {
			t.Fatal(err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 0 {
			t.Fatal("removing the only factor kept the personal value")
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices WHERE cause='qualification_lost' AND key_name='`+ceremonySecretA+`'`) != 1 {
			t.Fatal("factor loss left no notice")
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type='personal_value.deleted'`) != 1 {
			t.Fatal("factor-loss purge not audited")
		}
		// The unqualified owner can no longer set one: the factorless session
		// is refused for assurance or qualification, both 403 classes.
		if _, err := e.pv.Set(ctx, service.Bearer(removed.SessionToken), envScope(envA1), ceremonySecretA, "x"); !errors.Is(err, domain.ErrUnauthorized) {
			t.Fatalf("unqualified owner set = %v", err)
		}
	})
}

func TestPersonalValueKeyRenameAndDeleteCascade(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-cascade")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		// A key with no shared value, so deleting it is not refused for values.
		execRaw(t, db, `INSERT INTO keys (id, org_id, project_id, name, folder_path, classification, description, deprecated, deprecation_note, declaration, required_mode, forbidden_mode, created_at) VALUES ('key_personal_only', 'org_a', 'prj_a1', '`+personalOnly+`', '', 'secret', '', FALSE, '', '{"rule":{"type":"string"}}', 'none', 'none', `+ts+`)`)
		e.setOwn(t, personalOnly, "key_personal_only", personalPlain)
		// A key whose only value is personal still crosses under consent.
		consented, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, true)
		if got, err := deliverySvc(t, db).Fetch(ctx, consented.Value, envScope(envA1), "", service.FetchOptions{}); err != nil || !valueIs(deliveredKey(t, got, personalOnly), personalPlain) {
			t.Fatalf("personal-only key not delivered: err=%v", err)
		}
		keys := &service.Keys{DB: db, Keyring: e.values.Keyring}
		actor := service.LocalPrincipal(custodian)
		if _, err := keys.Rename(ctx, actor, scopeProject(orgA, prjA1), e.keyA, "CEREMONY_SECRET_RENAMED", nil); err != nil {
			t.Fatal(err)
		}
		if err := keys.Delete(ctx, actor, scopeProject(orgA, prjA1), "key_personal_only"); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM personal_values`); n != 0 {
			t.Fatalf("cascade left %d personal values", n)
		}
		for name, cause := range map[string]string{ceremonySecretA: "key_renamed", personalOnly: "key_deleted"} {
			if queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices WHERE key_name='`+name+`' AND cause='`+cause+`'`) != 1 {
				t.Fatalf("no %s notice for %s", cause, name)
			}
		}
		entries, err := e.pv.List(ctx, service.Bearer(e.admin.token), envScope(envA1))
		if err != nil {
			t.Fatal(err)
		}
		causes := map[string]string{}
		for _, en := range entries {
			causes[en.Name] = en.Notice
		}
		if causes[ceremonySecretA] != service.PersonalCauseKeyRenamed || causes[personalOnly] != service.PersonalCauseKeyDeleted {
			t.Fatalf("owner notices = %v", causes)
		}
		// A key re-declared under the deleted name inherits the notice, so a
		// shared value an administrator controls is refused, not served.
		execRaw(t, db, `INSERT INTO keys (id, org_id, project_id, name, folder_path, classification, description, deprecated, deprecation_note, declaration, required_mode, forbidden_mode, created_at) VALUES ('key_redeclared', 'org_a', 'prj_a1', '`+personalOnly+`', '', 'secret', '', FALSE, '', '{"rule":{"type":"string"}}', 'none', 'none', `+ts+`)`)
		publishValue(t, e.values, service.LocalPrincipal(custodian), envScope(envA1), personalOnly, "administrator-controlled")
		minted, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, true)
		r, err := deliverySvc(t, db).Fetch(ctx, minted.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if k := deliveredKey(t, r, personalOnly); k.Value != nil || k.Personal != service.PersonalNotice || k.PersonalReason != service.PersonalCauseKeyDeleted {
			t.Fatalf("re-declared key: personal=%q reason=%q", k.Personal, k.PersonalReason)
		}
	})
}

// A planted recovery code can destroy personal values but never read them:
// consuming it deletes them in the same transaction that retires the factors,
// before any new factor can be enrolled.
func TestPersonalValueRecoveryCodeConsumptionPurges(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-recovery")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		codes, _, err := e.admin.auth.GenerateRecoveryCodes(ctx, e.admin.token, ceremonyPassword)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.admin.auth.ConsumeRecoveryCode(ctx, "personal-recovery", codes[0]); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM personal_values`); n != 0 {
			t.Fatalf("recovery consumption kept %d personal values", n)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices WHERE cause='qualification_lost'`) != 1 {
			t.Fatal("recovery consumption left no notice")
		}
	})
}

// The envelope binds the owner: ciphertext moved onto another person's row
// does not open for them.
func TestPersonalValueMovedToAnotherOwnerDoesNotOpen(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-moved")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		adminPrincipal := queryString(t, db, `SELECT principal_id FROM accounts WHERE username='personal-moved-admin'`)
		execRaw(t, db, `UPDATE personal_values SET owner_principal_id='`+adminPrincipal+`'`)
		e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
		got, err := e.pv.Reveal(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA)
		if !errors.Is(err, crypto.ErrDecrypt) || got.Value == personalPlain {
			t.Fatal("a moved personal value opened for another owner")
		}
	})
}

// DEK rotation and the reencrypt walk cover personal values, and the moved
// row still opens for its owner afterwards.
func TestPersonalValueReencryptAfterDEKRotation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-reencrypt")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		kr := e.values.Keyring
		rotation := &service.Rotation{DB: db, Keyring: kr, RootKey: probeRootSource{db: db}}
		if _, err := rotation.RotateDEK(ctx, service.LocalPrincipal(root), service.DEKScope{OrgID: string(orgA), ProjectID: string(prjA1)}); err != nil {
			t.Fatal(err)
		}
		walked := false
		re := &service.Reencrypt{DB: db, Keyring: kr, AfterChunk: func(_ context.Context, table string) error {
			walked = walked || table == "personal_value"
			return nil
		}}
		if _, err := re.ReencryptProject(ctx, service.LocalPrincipal(root), string(orgA), string(prjA1)); err != nil {
			t.Fatal(err)
		}
		if !walked {
			t.Fatal("reencrypt walk skipped personal values")
		}
		minted, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, true)
		r, err := deliverySvc(t, db).Fetch(ctx, minted.Value, envScope(envA1), "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if !valueIs(deliveredKey(t, r, ceremonySecretA), personalPlain) {
			t.Fatal("personal value did not open after reencrypt")
		}
	})
}

// exportValues serves the shared environment only; a personal value never
// rides along, revealed or not.
func TestPersonalValueStaysOutOfExport(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-export")
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		values, _, err := revisionSvc(t, db).ExportWithParameters(t.Context(), service.LocalPrincipal(custodian), envScope(envA1), 0, true, nil)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range values {
			if v.Value == personalPlain {
				t.Fatalf("export carried the personal value of %s", v.Name)
			}
			found = found || (v.Name == ceremonySecretA && v.Value == "plaintext-"+ceremonySecretA)
		}
		if !found {
			t.Fatal("export lost the shared value")
		}
	})
}

// Only the owner's own possession factor opens the gate: a window an identity
// provider opened does not, and protected environments take no personal value.
func TestPersonalValueGateRefusesFederatedWindowAndProtectedEnvironment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-federated")
		ctx := t.Context()
		e.admin.token = personalReauth(t, e.admin.auth, e.admin.token, e.device, e.keyA)
		execRaw(t, db, `UPDATE reauth_windows SET factor_class='oidc' WHERE environment_id='env_a1'`)
		if _, err := e.pv.Set(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA, personalPlain); !errors.Is(err, service.ErrPersonalFactorRequired) {
			t.Fatalf("federated window set = %v", err)
		}
		execRaw(t, db, `UPDATE environments SET protected=TRUE WHERE id='env_a1'`)
		e.admin.token = personalReauth(t, e.admin.auth, e.admin.token, e.device, e.keyA)
		if _, err := e.pv.Set(ctx, service.Bearer(e.admin.token), envScope(envA1), ceremonySecretA, personalPlain); !errors.Is(err, service.ErrPersonalProtected) {
			t.Fatalf("protected environment set = %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 0 {
			t.Fatal("a refused set stored a value")
		}
	})
}

// A rename to the same name keeps values; deleting the environment removes
// them and audits the loss per owner.
func TestPersonalValueSameNameRenameKeepsAndEnvironmentDeleteAudits(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-envdelete")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		keys := &service.Keys{DB: db, Keyring: e.values.Keyring}
		actor := service.LocalPrincipal(custodian)
		if _, err := keys.Rename(ctx, actor, scopeProject(orgA, prjA1), e.keyA, ceremonySecretA, nil); err != nil {
			t.Fatal(err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 || queryInt(t, db, `SELECT COUNT(*) FROM personal_value_notices`) != 0 {
			t.Fatal("a same-name rename removed the personal value")
		}
		execRaw(t, db, `DELETE FROM value_entries WHERE environment_id='env_a1'`)
		if err := (&service.Environments{DB: db, Keyring: e.values.Keyring}).Delete(ctx, actor, envScope(envA1)); err != nil {
			t.Fatal(err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 0 {
			t.Fatal("environment delete kept the personal value")
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='personal_value.deleted'`) != 1 {
			t.Fatal("environment delete did not audit the personal value loss")
		}
	})
}
