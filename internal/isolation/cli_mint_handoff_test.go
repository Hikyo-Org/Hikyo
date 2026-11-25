package isolation

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestCLIMintReauthHandoff(t *testing.T) {
	forEngines(t, runCLIMintReauthHandoff)
}

// runCLIMintReauthHandoff is the 0-window handoff for a service-account mint
// (#1516). A mint's unit is its environment alone: the handoff carries no key
// ids and refuses any, the browser's single decision is spent by approval,
// redemption hands the CLI a window that satisfies the MINT intent exactly
// once, and a reveal-bound browser decision never satisfies a mint handoff.
func runCLIMintReauthHandoff(t *testing.T, db *store.DB) {
	ctx := t.Context()
	identityFixtures(t, db)
	ceremony := ceremonyFixture(t, db, "mint-handoff")
	auth, browserToken, dev := ceremony.admin.auth, ceremony.admin.token, ceremony.device
	auth.ReauthWindow = 0
	auth.ReauthHardCap = time.Hour
	minter := ceremony.admin.boot.PrincipalID
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_mh_ident', '`+string(minter)+`', 'manage-identities', 'org_a', NULL, NULL, `+ts+`)`)
	seedOrigins(t, db)

	ident := &service.Identities{DB: db, Auth: auth}
	sa, err := ident.CreateServiceAccount(ctx, service.LocalPrincipal(identAdmin), prjScope(), "mint-handoff-sa", domain.ClassWorkload)
	if err != nil {
		t.Fatal(err)
	}
	seedMachineReveal(t, db, "g_mh_read", sa.Principal, domain.CapRead, envA1)
	seedMachineReveal(t, db, "g_mh_rv", sa.Principal, domain.CapReveal, envA1)

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
	const redirect = "http://127.0.0.1:40126/callback"
	env := string(envA1)
	mintIntent := disclosureReauthIntent(t, service.PurposeMint, env, nil)

	// The mint is refused until a window over the environment is held, and the
	// refusal names the environment the handoff must cover.
	_, err = ident.MintCredential(ctx, service.Bearer(cliToken), prjScope(), sa.ID, service.MintRequest{})
	if !errors.Is(err, service.ErrReauthRequired) || !strings.Contains(err.Error(), env) {
		t.Fatalf("mint without a window = %v, want ErrReauthRequired naming %s", err, env)
	}

	// A mint handoff names no keys: a key set is refused at start.
	_, challenge := pkce("mint-keys")
	if _, err := auth.StartCLIReauth(ctx, cliToken, disclosureReauthIntent(t, service.PurposeMint, env, []string{"key_" + ceremonySecretA}), challenge, redirect); !errors.Is(err, service.ErrReauthUnitMismatch) {
		t.Fatalf("mint handoff with key ids = %v, want unit mismatch", err)
	}

	// A reveal decision over a key is another decision: it does not satisfy a
	// mint handoff, and the refusal does not spend it.
	res := passkeyCeremony(t, auth, ctx, browserToken, service.PurposeReveal, env, []string{"key_" + ceremonySecretA}, dev)
	browserToken = res.SessionToken
	_, wrongChallenge := pkce("mint-wrong")
	wrong, err := auth.StartCLIReauth(ctx, cliToken, mintIntent, wrongChallenge, redirect)
	if err != nil {
		t.Fatalf("start mint handoff: %v", err)
	}
	if _, err := auth.ApproveCLIReauth(ctx, service.Bearer(browserToken), wrong.State); !errors.Is(err, service.ErrReauthUnitMismatch) {
		t.Fatalf("a reveal decision satisfying a mint handoff = %v, want unit mismatch", err)
	}

	// The browser's mint decision (no keys) is approved, redeemed and spent once.
	res = passkeyCeremony(t, auth, ctx, browserToken, service.PurposeMint, env, nil, dev)
	browserToken = res.SessionToken
	verifier, challenge := pkce("mint-exact")
	start, err := auth.StartCLIReauth(ctx, cliToken, mintIntent, challenge, redirect)
	if err != nil {
		t.Fatalf("start mint handoff: %v", err)
	}
	transaction, err := auth.CLIReauthTransaction(ctx, service.Bearer(browserToken), start.State)
	if err != nil {
		t.Fatal(err)
	}
	if transaction.Purpose != "mint" || transaction.Operation != "identity.credential-mint" || len(transaction.KeyIDs) != 0 || len(transaction.Environments) != 1 || !transaction.Environments[0].RequiresWebAuthn {
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

	// The redeemed window is a mint decision: a reveal cannot spend it, and the
	// attempt leaves it intact for the mint.
	if _, err := ceremony.values.Get(ctx, service.Bearer(redeemed.SessionToken), scopeEnv(orgA, prjA1, envA1), ceremonySecretA, true); !errors.Is(err, service.ErrReauthUnitMismatch) {
		t.Fatalf("a reveal under a mint window = %v, want unit mismatch", err)
	}
	minted, err := ident.MintCredential(ctx, service.Bearer(redeemed.SessionToken), prjScope(), sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatalf("mint under the redeemed window: %v", err)
	}
	if minted.Value == "" {
		t.Fatal("a successful mint returns the value once")
	}
	if _, err := ident.MintCredential(ctx, service.Bearer(redeemed.SessionToken), prjScope(), sa.ID, service.MintRequest{}); !errors.Is(err, service.ErrReauthRequired) {
		t.Fatalf("second mint on the single-decision window = %v, want ErrReauthRequired", err)
	}
}

func TestCLIMintReauthHandoffTwoZeroWindowEnvironments(t *testing.T) {
	forEngines(t, runCLIMintReauthHandoffTwoEnvironments)
}

// An account that reaches two 0-window environments needs one browser decision
// per environment. The refused mint must roll back, so the first decision stays
// live and the retry names only the environment still missing (#1516).
func runCLIMintReauthHandoffTwoEnvironments(t *testing.T, db *store.DB) {
	ctx := t.Context()
	identityFixtures(t, db)
	ceremony := ceremonyFixture(t, db, "mint-two-env")
	auth, browserToken, dev := ceremony.admin.auth, ceremony.admin.token, ceremony.device
	auth.ReauthWindow = 0
	auth.ReauthHardCap = time.Hour
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_m2_ident', '`+string(ceremony.admin.boot.PrincipalID)+`', 'manage-identities', 'org_a', NULL, NULL, `+ts+`)`)
	// A second environment in the account's own project.
	const second = domain.EnvID("env_stage_a")
	execRaw(t, db, `INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) VALUES ('env_stage_a', 'org_a', 'prj_a1', 'stage', '', `+ts+`, 1)`)
	seedOrigins(t, db)
	ident := &service.Identities{DB: db, Auth: auth}
	sa, err := ident.CreateServiceAccount(ctx, service.LocalPrincipal(identAdmin), prjScope(), "mint-two-env-sa", domain.ClassWorkload)
	if err != nil {
		t.Fatal(err)
	}
	for i, env := range []domain.EnvID{envA1, second} {
		seedMachineReveal(t, db, fmt.Sprintf("g_m2_read_%d", i), sa.Principal, domain.CapRead, env)
		seedMachineReveal(t, db, fmt.Sprintf("g_m2_rv_%d", i), sa.Principal, domain.CapReveal, env)
	}
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
	const redirect = "http://127.0.0.1:40127/callback"
	// handoff is one CLI ceremony: the browser decides over env, the CLI redeems
	// and keeps the rotated session, as the real command does.
	handoff := func(env string) {
		t.Helper()
		res := passkeyCeremony(t, auth, ctx, browserToken, service.PurposeMint, env, nil, dev)
		browserToken = res.SessionToken
		v := sha256.Sum256([]byte("two-env-" + env))
		verifier := base64.RawURLEncoding.EncodeToString(v[:])
		c := sha256.Sum256([]byte(verifier))
		start, err := auth.StartCLIReauth(ctx, cliToken, disclosureReauthIntent(t, service.PurposeMint, env, nil), base64.RawURLEncoding.EncodeToString(c[:]), redirect)
		if err != nil {
			t.Fatalf("start handoff over %s: %v", env, err)
		}
		approved, err := auth.ApproveCLIReauth(ctx, service.Bearer(browserToken), start.State)
		if err != nil {
			t.Fatalf("approve over %s: %v", env, err)
		}
		redeemed, err := auth.RedeemCLIReauth(ctx, approved.Code, verifier)
		if err != nil {
			t.Fatalf("redeem over %s: %v", env, err)
		}
		cliToken = redeemed.SessionToken
	}
	a, b := string(envA1), string(second)
	_, err = ident.MintCredential(ctx, service.Bearer(cliToken), prjScope(), sa.ID, service.MintRequest{})
	if !errors.Is(err, service.ErrReauthRequired) || !strings.Contains(err.Error(), a) {
		t.Fatalf("first mint = %v, want ErrReauthRequired naming %s", err, a)
	}
	handoff(a)
	_, err = ident.MintCredential(ctx, service.Bearer(cliToken), prjScope(), sa.ID, service.MintRequest{})
	if !errors.Is(err, service.ErrReauthRequired) || !strings.Contains(err.Error(), b) || strings.Contains(err.Error(), a) {
		t.Fatalf("retry after the %s handoff = %v, want ErrReauthRequired naming only %s (the refused mint must not spend %s's decision)", a, err, b, a)
	}
	handoff(b)
	minted, err := ident.MintCredential(ctx, service.Bearer(cliToken), prjScope(), sa.ID, service.MintRequest{})
	if err != nil || minted.Value == "" {
		t.Fatalf("mint after both handoffs = %v", err)
	}
}
