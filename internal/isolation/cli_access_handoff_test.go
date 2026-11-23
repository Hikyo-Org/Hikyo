package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestCLIAccessReauthHandoff(t *testing.T) {
	forEngines(t, runCLIAccessReauthHandoff)
}

// cliSessionFor mints a CLI session for a principal with no reauthentication
// window, as `hikyo login` would leave it.
func cliSessionFor(t *testing.T, db *store.DB, principal domain.PrincipalID, id string) string {
	t.Helper()
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactCLISession)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	generation := int64(queryInt(t, db, "SELECT session_generation FROM principals WHERE id = '"+string(principal)+"'"))
	if err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		return az.MintSession(ctx, authz.NewSession{
			ID: id, PrincipalID: principal, Verifier: verifier, Artifact: "cli",
			SessionGeneration: generation, CredentialEpoch: epoch,
			AuthMethod: "local-passkey", Factors: `["webauthn","mfa"]`,
			AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour),
			AbsoluteExpiresAt: now.Add(24 * time.Hour), SourceIP: "127.0.0.1", UserAgent: "access-handoff",
		})
	}); err != nil {
		t.Fatalf("mint session %s: %v", id, err)
	}
	return artifact
}

// runCLIAccessReauthHandoff is the 0-window handoff for emergency temporary
// access (#1531). Its unit is the environment alone: the handoff carries no key
// ids and refuses any, a reveal decision never satisfies it, the browser's
// single decision is spent by approval, and redemption hands the CLI a window
// that satisfies the ACCESS intent exactly once. Starting one authorizes
// `access.bypass` over the environment and nothing wider.
func runCLIAccessReauthHandoff(t *testing.T, db *store.DB) {
	ctx := t.Context()
	identityFixtures(t, db)
	ceremony := ceremonyFixture(t, db, "access-handoff")
	auth, browserToken, dev := ceremony.admin.auth, ceremony.admin.token, ceremony.device
	auth.ReauthWindow = 0
	auth.ReauthHardCap = time.Hour
	seedOrigins(t, db)

	// An enabled policy at the environment names the ceremony's principal a
	// bypasser, so the emergency request itself is admissible.
	officer := ceremony.admin.boot.PrincipalID
	access := &service.Access{DB: db, Auth: auth}
	if _, err := access.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), domain.Scope{Org: orgA, Project: prjA1}, service.AccessPolicyInput{
		EnvironmentID: string(envA1), Capabilities: []string{"read"}, MaxDurationSeconds: 3600, MinApprovals: 1,
		RequestTTLSeconds: 3600, Enabled: true, Bypassers: []string{string(officer)},
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}); err != nil {
		t.Fatalf("create the access policy: %v", err)
	}
	scope := scopeEnv(orgA, prjA1, envA1)

	browser, err := discoverableLogin(t, auth, ctx, dev)
	if err != nil {
		t.Fatal(err)
	}
	cliLogin := cliLoginFromBrowser(t, auth, browser)
	sopts, err := auth.StepUpPasskeyStart(ctx, cliLogin.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	sresp, err := dev.Assert(sopts)
	if err != nil {
		t.Fatal(err)
	}
	stepped, err := auth.StepUpPasskeyFinish(ctx, cliLogin.SessionToken, sresp)
	if err != nil {
		t.Fatal(err)
	}
	cliToken := stepped.SessionToken
	pkce := func(seed string) (string, string) {
		v := sha256.Sum256([]byte(seed))
		verifier := base64.RawURLEncoding.EncodeToString(v[:])
		c := sha256.Sum256([]byte(verifier))
		return verifier, base64.RawURLEncoding.EncodeToString(c[:])
	}
	const redirect = "http://127.0.0.1:40128/callback"
	env := string(envA1)
	accessIntent := disclosureReauthIntent(t, service.PurposeAccess, env, nil)
	emergency := service.AccessRequestInput{Capabilities: []string{"read"}, Reason: "production is down"}

	// The request is refused until a window over the environment is held.
	if _, err := access.EmergencyAccess(ctx, service.Bearer(cliToken), scope, emergency); !errors.Is(err, service.ErrNoReauthWindow) {
		t.Fatalf("emergency access without a window = %v, want ErrNoReauthWindow", err)
	}

	// An access handoff names no keys: a key set is refused at start.
	_, challenge := pkce("access-keys")
	if _, err := auth.StartCLIReauth(ctx, cliToken, disclosureReauthIntent(t, service.PurposeAccess, env, []string{"key_" + ceremonySecretA}), challenge, redirect); !errors.Is(err, service.ErrReauthUnitMismatch) {
		t.Fatalf("access handoff with key ids = %v, want unit mismatch", err)
	}

	// Starting authorizes `access.bypass` over the environment: a principal with
	// no grant there is not offered the ceremony, and one with read is, whether
	// or not the request later admits them as a bypasser.
	outsider := cliSessionFor(t, db, bob, "ses_access_outsider")
	if _, err := auth.StartCLIReauth(ctx, outsider, accessIntent, challenge, redirect); err == nil {
		t.Fatal("a principal with no grant at the environment started an access handoff")
	}
	if _, err := auth.StartCLIReauth(ctx, cliSessionFor(t, db, reader, "ses_access_reader"), accessIntent, challenge, redirect); err != nil {
		t.Fatalf("a principal holding read at the environment was refused the handoff: %v", err)
	}

	// A reveal decision over a key is another decision: it does not satisfy an
	// access handoff, and the refusal does not spend it.
	res := passkeyCeremony(t, auth, ctx, browserToken, service.PurposeReveal, env, []string{"key_" + ceremonySecretA}, dev)
	browserToken = res.SessionToken
	_, wrongChallenge := pkce("access-wrong")
	wrong, err := auth.StartCLIReauth(ctx, cliToken, accessIntent, wrongChallenge, redirect)
	if err != nil {
		t.Fatalf("start access handoff: %v", err)
	}
	if _, err := auth.ApproveCLIReauth(ctx, service.Bearer(browserToken), wrong.State); !errors.Is(err, service.ErrReauthUnitMismatch) {
		t.Fatalf("a reveal decision satisfying an access handoff = %v, want unit mismatch", err)
	}

	// The browser's access decision (no keys) is approved, redeemed and spent once.
	res = passkeyCeremony(t, auth, ctx, browserToken, service.PurposeAccess, env, nil, dev)
	browserToken = res.SessionToken
	verifier, challenge := pkce("access-exact")
	start, err := auth.StartCLIReauth(ctx, cliToken, accessIntent, challenge, redirect)
	if err != nil {
		t.Fatalf("start access handoff: %v", err)
	}
	transaction, err := auth.CLIReauthTransaction(ctx, service.Bearer(browserToken), start.State)
	if err != nil {
		t.Fatal(err)
	}
	if transaction.Purpose != "access" || transaction.Operation != "access.bypass" || len(transaction.KeyIDs) != 0 || len(transaction.Environments) != 1 || !transaction.Environments[0].RequiresWebAuthn {
		t.Fatalf("transaction = %+v", transaction)
	}
	approved, err := auth.ApproveCLIReauth(ctx, service.Bearer(browserToken), start.State)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	redeemed, err := auth.RedeemCLIReauth(ctx, approved.Code, verifier)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if len(redeemed.Windows) != 1 || !redeemed.Windows[0].SingleDecision {
		t.Fatalf("redeemed windows = %+v, want one single-decision window", redeemed.Windows)
	}

	// The redeemed window is an access decision: a reveal cannot spend it, and
	// the attempt leaves it intact for the emergency request.
	if _, err := ceremony.values.Get(ctx, service.Bearer(redeemed.SessionToken), scope, ceremonySecretA, true); !errors.Is(err, service.ErrReauthUnitMismatch) {
		t.Fatalf("a reveal under an access window = %v, want unit mismatch", err)
	}
	granted, err := access.EmergencyAccess(ctx, service.Bearer(redeemed.SessionToken), scope, emergency)
	if err != nil {
		t.Fatalf("emergency access under the redeemed window: %v", err)
	}
	if !granted.Bypassed || granted.State != "granted" {
		t.Fatalf("emergency access = %+v", granted)
	}
	if _, err := access.EmergencyAccess(ctx, service.Bearer(redeemed.SessionToken), scope, emergency); !errors.Is(err, service.ErrReauthWindowSpent) {
		t.Fatalf("second request on the single-decision window = %v, want ErrReauthWindowSpent", err)
	}
}
