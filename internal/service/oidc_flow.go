package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/oidcrp"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// ErrBadPurpose refuses an unknown OIDC transaction purpose loudly: it is a
// caller programming error, not a probe outcome.
var ErrBadPurpose = errors.New("service: OIDC purpose must be login, link or reauth")

// ErrAlreadyLinked refuses linking an identity that is already bound.
var ErrAlreadyLinked = errors.New("service: that identity is already linked")

// ErrReauthNoPolicy refuses starting a reauth against a provider with no
// assurance policy (A5, #588 d4): such a row is not reauth-capable, so the
// start refuses by name, before any round-trip and before any row is written,
// naming the remedy. The caller has already authenticated a session through
// this very provider, so the refusal discloses nothing to a prober.
var ErrReauthNoPolicy error = reauthNoPolicyError{}

type reauthNoPolicyError struct{}

const reauthNoPolicyRemedy = "this sign-in provider cannot confirm it's you again: enrol WebAuthn or TOTP and use it instead"

func (reauthNoPolicyError) Error() string {
	return "service: provider has no assurance policy; OIDC reauthentication is refused (" + reauthNoPolicyRemedy + ")"
}

// SafeDetail is the remedy the wire carries in the 409's detail.
func (reauthNoPolicyError) SafeDetail() string { return reauthNoPolicyRemedy }

// Login intents (#604): recorded on a purpose-login transaction, absent on
// the wire = sign-in.
const (
	IntentSignIn = "sign-in"
	IntentSignUp = "sign-up"
)

// ErrReauthNoEnvironment refuses a reauth with no environment scope.
var ErrReauthNoEnvironment = errors.New("service: OIDC reauthentication requires an environment_id")

// ErrEnvironmentNotForPurpose refuses an environment_id on a login or link
// start: only reauth scopes a window, and the transaction CHECK refuses the
// field on every other purpose. It depends on the request body alone, so it
// is the one start refusal that is not folded into the uniform 401.
var ErrEnvironmentNotForPurpose = fmt.Errorf("%w: environment_id is only valid with purpose reauth", domain.ErrInvalid)

// OIDCStartResult is the authorization URL plus the artifacts the transport
// carries: the state value (used to derive the per-transaction binding-cookie
// name, A16) and, for an anonymous login, the browser-binding cookie value the
// callback must present (A2).
type OIDCStartResult struct {
	AuthURL       string
	State         string
	BindingCookie string // "ob" artifact for an anonymous login; empty for a session-bound tx
	Purpose       string
}

// OIDCStart creates a single-use transaction and returns the IdP authorization
// URL. login is anonymous and browser-cookie-bound; link and reauth require an
// authenticated session and are session-bound, and link additionally verifies
// the account-security proof (the pre-existing password) up front, binding it to
// the transaction ceremony (A6). PKCE S256 is always used.
//
// A login records its intent (#604 d1, d2; absent = sign-in) and, for a
// sign-up, the addressed scope (signupOrg, absent = instance). Intent on any
// other purpose, and a scope without sign-up, refuse uniformly in the
// ErrBadPurpose shape. The start reads no registration policy (#604 d5).
// Reauth without an assurance policy returns ErrReauthNoPolicy before redirect;
// admission, authentication and transaction-write errors propagate, while a
// discovery failure returns ErrProviderDiscovery.
func (s *Auth) OIDCStart(ctx context.Context, slug, purpose, intent, signupOrg, environmentID, presented, proof string, browser bool) (OIDCStartResult, error) {
	// Admission is entered FIRST, uniformly for every purpose and BEFORE the
	// provider is resolved or the purpose/environment validated. An unknown
	// slug, a bad purpose, a missing environment and a fully resolved provider
	// then take one path with one per-IP admission cost, so a pre-auth prober
	// cannot enumerate provider config by the status, body or timing of the
	// refusal. link/reauth additionally ride the per-account backoff below; the
	// per-IP slot taken here already throttles their Argon2 proof.
	release, err := s.Admission.Enter(ctx, audit.FromContext(ctx).SourceIP)
	if err != nil {
		return OIDCStartResult{}, err
	}
	defer release()

	switch purpose {
	case purposeLogin, purposeLink, purposeReauth:
	default:
		return OIDCStartResult{}, ErrBadPurpose
	}
	switch {
	case intent != "" && purpose != purposeLogin:
		return OIDCStartResult{}, ErrBadPurpose
	case intent != "" && intent != IntentSignIn && intent != IntentSignUp:
		return OIDCStartResult{}, ErrBadPurpose
	case signupOrg != "" && intent != IntentSignUp:
		return OIDCStartResult{}, ErrBadPurpose
	}
	if purpose == purposeLogin && intent == "" {
		intent = IntentSignIn
	}
	// reauth scopes a window to one environment; without it the transaction
	// CHECK would reject the row as a raw fault. Refuse loudly up front.
	if purpose == purposeReauth && environmentID == "" {
		return OIDCStartResult{}, ErrReauthNoEnvironment
	}
	if purpose != purposeReauth && environmentID != "" {
		return OIDCStartResult{}, ErrEnvironmentNotForPurpose
	}

	// Phase 1 - resolve the provider and, for a session-bound purpose, the
	// acting session and account. Both run INSIDE the admission budget, and
	// link/reauth authenticate BEFORE the provider is resolved so an
	// unauthenticated caller refuses identically (uniform 401) whether the slug
	// is known or not: provider existence is never what a prober learns first.
	var (
		provider          authz.OIDCProvider
		epoch             int64
		account           authz.Account
		sessionID         string
		sessionMethod     string
		sessionProviderID string
	)
	err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		var e error
		if purpose != purposeLogin {
			id, e := az.Authenticate(ctx, presented, s.now())
			if e != nil {
				return e
			}
			account, e = az.AccountByPrincipal(ctx, id.Principal)
			if e != nil {
				return e
			}
			sessionID = id.SessionID
			sessionMethod = id.Assurance.Method
			sessionProviderID = id.ProviderID
		}
		provider, e = az.EnabledProviderBySlug(ctx, slug)
		if errors.Is(e, domain.ErrNotFound) {
			return ErrProviderNotFound
		}
		if e != nil {
			return e
		}
		if purpose == purposeReauth &&
			(sessionMethod != oidcMethod(provider.Issuer) || sessionProviderID != provider.ID) {
			return domain.ErrUnauthenticated
		}
		if epoch, e = az.CredentialEpoch(ctx); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		return OIDCStartResult{}, err
	}
	if purpose == purposeReauth && provider.AssurancePolicy == nil {
		return OIDCStartResult{}, ErrReauthNoPolicy // A5, fail fast (also enforced at callback)
	}

	// link/reauth ride the per-account backoff so a stolen session cannot be an
	// unthrottled Argon2 oracle; the per-IP admission slot is already held, so
	// this adds only the account-scoped delay, never a second slot.
	if purpose != purposeLogin {
		if s.Admission.AccountDelay(account.ID) > 0 {
			return OIDCStartResult{}, admission.ErrOverloaded
		}
	}

	// Link: verify the account-security proof (the pre-existing password, since
	// the credential being added never authorizes its own addition).
	if purpose == purposeLink {
		var cred authz.PasswordCredential
		perr := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			var e error
			cred, e = az.PasswordCredentialFor(ctx, account.ID)
			if errors.Is(e, domain.ErrNotFound) {
				return ErrNoProofCredential
			}
			return e
		})
		if perr != nil {
			return OIDCStartResult{}, perr
		}
		if !s.verifyPassword(ctx, account.ID, cred, proof) {
			s.recordFactorFailure(ctx, account.PrincipalID, account.ID)
			return OIDCStartResult{}, domain.ErrUnauthenticated
		}
		s.Admission.RecordSuccess(account.ID)
	}

	// Discovery reconstructs the provider (A20), then the authorization URL.
	rp, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return OIDCStartResult{}, ErrProviderDiscovery
	}
	state, stateVerifier, err := crypto.NewArtifact(crypto.ArtifactOIDCState)
	if err != nil {
		return OIDCStartResult{}, err
	}
	nonce, err := randToken()
	if err != nil {
		return OIDCStartResult{}, err
	}
	pkce, err := randToken()
	if err != nil {
		return OIDCStartResult{}, err
	}
	var extra map[string]string
	if purpose == purposeReauth {
		// prompt=login and max_age=0 force a fresh authentication; alone they
		// prove neither freshness nor multi-factor, which is why the callback
		// checks auth_time and acr/amr against policy.
		extra = map[string]string{"prompt": "login", "max_age": "0"}
	}
	authURL := rp.AuthCodeURL(provider.ClientID, provider.RedirectURI, provider.Scopes, state, nonce, pkce, extra)

	txID, err := newID("oidctx")
	if err != nil {
		return OIDCStartResult{}, err
	}
	newTx := authz.NewOIDCTransaction{
		ID: txID, StateVerifier: stateVerifier, Nonce: crypto.ArtifactVerifier(nonce), PKCEVerifier: pkce,
		ProviderID: provider.ID, Issuer: provider.Issuer, RedirectURI: provider.RedirectURI,
		Purpose: purpose, EnvironmentID: environmentID, Browser: browser, CredentialEpoch: epoch,
		Intent: intent, SignupScopeOrgID: signupOrg,
	}
	var bindingCookie string
	if purpose == purposeLogin {
		obVal, obVerifier, aerr := crypto.NewArtifact(crypto.ArtifactOIDCBinding)
		if aerr != nil {
			return OIDCStartResult{}, aerr
		}
		newTx.BindingKind = bindingBrowserCookie
		newTx.BrowserBindingVerifier = obVerifier
		bindingCookie = obVal
	} else {
		newTx.BindingKind = bindingSession
		newTx.InitiatingSessionID = sessionID
		newTx.AccountID = account.ID
	}
	if purpose == purposeLink {
		ceremonyID, cerr := newID("cer")
		if cerr != nil {
			return OIDCStartResult{}, cerr
		}
		newTx.CeremonyID = ceremonyID
	}

	err = tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		newTx.CreatedAt = now
		newTx.ExpiresAt = now.Add(oidcTxLifetime)
		return az.CreateOIDCTransaction(ctx, newTx)
	})
	if err != nil {
		return OIDCStartResult{}, err
	}
	return OIDCStartResult{AuthURL: authURL, State: state, BindingCookie: bindingCookie, Purpose: purpose}, nil
}

// OIDCCallbackResult carries the freshly minted or rotated session on success.
type OIDCCallbackResult struct {
	Login   LoginResult
	Purpose string
	State   string
	Browser bool
}

// OIDCCallback processes an IdP redirect against its own transaction. Mix-up is
// refused BEFORE any token is fetched (A1): the callback's provider slug must
// equal the transaction's provider, and the RFC 9207 iss (when present) must
// equal the pinned issuer. The transaction is consumed single-use; the exchange
// runs only at the recorded provider; the ID token is validated completely; and
// the purpose wall is structural - the branch is the transaction's purpose.
func (s *Auth) OIDCCallback(ctx context.Context, slug, code, stateValue, issParam, idpError, bindingCookie, presented string) (OIDCCallbackResult, error) {
	release, err := s.Admission.Enter(ctx, audit.FromContext(ctx).SourceIP)
	if err != nil {
		return OIDCCallbackResult{}, err
	}
	defer release()

	// An unparseable state cannot match any transaction; refuse uniformly.
	if crypto.ParseArtifact(stateValue, crypto.ArtifactOIDCState) != nil {
		return OIDCCallbackResult{}, s.refuseOIDC(ctx, causeState, "", stateValue)
	}
	stateVerifier := crypto.ArtifactVerifier(stateValue)

	// Phase A - resolve, validate, consume. No network here.
	var (
		txn     authz.OIDCTransaction
		prov    authz.OIDCProvider
		refused error
	)
	err = tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		refused = nil
		now := s.now()
		epoch, e := az.CredentialEpoch(ctx)
		if e != nil {
			return e
		}
		t, e := az.OIDCTransactionByState(ctx, stateVerifier)
		if errors.Is(e, domain.ErrNotFound) {
			if aerr := s.stageOIDCRefuse(ctx, az, causeState, ""); aerr != nil {
				return aerr
			}
			refused = domain.ErrUnauthenticated
			return nil
		}
		if e != nil {
			return e
		}
		txn = t
		cause := ""
		switch {
		case t.Consumed:
			cause = causeState
		case !now.Before(t.ExpiresAt):
			cause = causeExpired
		}
		var provider authz.OIDCProvider
		if cause == "" {
			provider, e = az.ProviderForCallback(ctx, t.ProviderID)
			switch {
			case errors.Is(e, domain.ErrNotFound):
				cause = causeMixup
			case e != nil:
				return e
			case provider.Slug != slug: // mix-up leg 1, BEFORE any token
				cause = causeMixup
			case issParam != "" && issParam != t.Issuer: // RFC 9207 leg 2
				cause = causeMixup
			case provider.Issuer != t.Issuer: // A11
				cause = causeIssuer
			case !provider.Enabled:
				cause = causeReconciliation
			case t.CredentialEpoch != epoch:
				cause = causeEpoch
			}
		}
		if cause == "" && idpError != "" { // A18
			cause = causeIDPError
		}
		if cause == "" {
			cause = s.checkBinding(ctx, az, t, bindingCookie, presented, now)
		}
		// A resolved transaction is spent by this callback, whatever the outcome:
		// single-use, no replay.
		claimed, e := az.ConsumeOIDCTransaction(ctx, t.ID, now)
		if e != nil {
			return e
		}
		if !claimed && cause == "" {
			cause = causeState
		}
		if cause != "" {
			if aerr := s.stageOIDCRefuse(ctx, az, cause, t.ProviderID); aerr != nil {
				return aerr
			}
			refused = domain.ErrUnauthenticated
			return nil
		}
		prov = provider
		return nil
	})
	metadata := OIDCCallbackResult{Purpose: txn.Purpose, State: stateValue, Browser: txn.Browser}
	if err != nil {
		return metadata, err
	}
	if refused != nil {
		return metadata, refused
	}

	// Phase B - exchange and validate, outside any transaction.
	claims, cause := s.exchangeAndVerify(ctx, prov, txn, code)
	if cause != "" {
		return metadata, s.refuseOIDC(ctx, cause, prov.ID, "")
	}

	// Phase C - purpose dispatch. The branch IS the transaction's purpose, so a
	// response obtained for one purpose cannot complete another.
	var result OIDCCallbackResult
	switch txn.Purpose {
	case purposeLogin:
		result, err = s.completeLogin(ctx, prov, txn, claims)
	case purposeLink:
		result, err = s.completeLink(ctx, prov, txn, claims, presented)
	case purposeReauth:
		result, err = s.completeReauth(ctx, prov, txn, claims, presented)
	default:
		return metadata, ErrBadPurpose
	}
	result.Purpose = txn.Purpose
	result.State = stateValue
	result.Browser = txn.Browser
	return result, err
}

// checkBinding enforces the transaction binding (A2). A browser-cookie tx
// requires the ob cookie whose hash was recorded; a session tx requires the
// presented session to still be the initiating one. No default branch trusts an
// unknown binding.
func (s *Auth) checkBinding(ctx context.Context, az *authz.TxAuthorizer, t authz.OIDCTransaction, bindingCookie, presented string, now time.Time) string {
	switch t.BindingKind {
	case bindingBrowserCookie:
		// Constant-time: the browser-binding cookie is a bearer secret, and
		// bytes.Equal is not the primitive for comparing one.
		if bindingCookie == "" ||
			subtle.ConstantTimeCompare(crypto.ArtifactVerifier(bindingCookie), t.BrowserBindingVerifier) != 1 {
			return causeBinding
		}
		return ""
	case bindingSession:
		id, err := az.Authenticate(ctx, presented, now)
		if err != nil || id.SessionID != t.InitiatingSessionID {
			return causeBinding
		}
		return ""
	default:
		return causeBinding
	}
}

// exchangeAndVerify opens the client secret, exchanges the code at the recorded
// provider only, validates the ID token completely, and compares the nonce hash
// (A19). It returns a closed refusal cause on any failure, never a downgrade.
func (s *Auth) exchangeAndVerify(ctx context.Context, prov authz.OIDCProvider, txn authz.OIDCTransaction, code string) (oidcrp.Claims, string) {
	plainSecret, err := s.Keyring.ForInstance().OpenField(providerSecretAAD(prov.ID), prov.ClientSecret)
	if err != nil {
		s.logFault(ctx, "opening an OIDC client secret failed", err, prov.ID)
		return oidcrp.Claims{}, causeSignature
	}
	defer crypto.Zero(plainSecret)

	rp, err := s.discover(ctx, txn.Issuer)
	if err != nil {
		return oidcrp.Claims{}, causeIDPError
	}
	rawIDToken, err := rp.Exchange(ctx, prov.ClientID, plainSecret, txn.RedirectURI, prov.Scopes, code, txn.PKCEVerifier)
	if err != nil {
		return oidcrp.Claims{}, causeIDPError
	}
	claims, err := rp.Verify(ctx, prov.ClientID, rawIDToken, s.now)
	if err != nil {
		// An ID-token issuer mismatch is go-oidc's refusal (#588 d3, no Hikyo
		// belt) and audits under the default token-validation cause; `issuer`
		// is the row-versus-transaction check (A11) alone.
		if errors.Is(err, oidcrp.ErrAudience) {
			return oidcrp.Claims{}, causeAudience
		}
		return oidcrp.Claims{}, causeSignature
	}
	// Constant-time: the nonce is a bearer-class secret stored as its
	// artifact verifier, and the ID token replays it. Same primitive as every
	// other verifier comparison in this codebase.
	if subtle.ConstantTimeCompare(crypto.ArtifactVerifier(claims.Nonce), txn.Nonce) != 1 {
		return oidcrp.Claims{}, causeNonce
	}
	return claims, ""
}

// revalidateProvider locks the pinned provider row inside a Phase-C write tx
// and returns a refusal cause if it moved since the Phase-A snapshot. The
// snapshot was taken before the network exchange, so a concurrent reconfigure
// could have swept sessions and narrowed policy while the exchange was in
// flight. A plain re-read is not enough under postgres READ COMMITTED: the
// callback could read a stale snapshot and mint after the sweep committed. The
// guard is a no-op CAS UPDATE on the provider row (row_version + enabled +
// issuer against the snapshot), so it deterministically CONFLICTS with a
// concurrent provider-change UPDATE on the same row — whichever commits first,
// the other refuses. 0 rows means the provider was disabled, deleted, re-issued
// or reconfigured (every reconfigure bumps row_version), so the sweep always
// wins (A4): a stale evaluation cannot mint an account, identity, session or
// window. On sqlite the single writer already serializes; the guard keeps one
// code path across engines.
//
// ponytail: every phase-C mint now takes the provider row lock, so concurrent
// logins through one provider serialize at phase C. Acceptable — the locked
// window is a small local write tx (no network, which was phase B), and login
// is not a hot enough path here to want per-provider lock striping.
func (s *Auth) revalidateProvider(ctx context.Context, az *authz.TxAuthorizer, snapshot authz.OIDCProvider) (string, error) {
	ok, err := az.GuardProviderForMint(ctx, snapshot.ID, snapshot.RowVersion, snapshot.Issuer)
	if err != nil {
		return "", err
	}
	if !ok {
		return causeReconciliation, nil
	}
	return "", nil
}

// completeLogin resolves the identity three ways (live / epoch-inert /
// unknown, A8) and mints a browser session or refuses uniformly, in the
// order of spec section 4: provider revalidation, epoch, the instance-wide
// (kind, issuer, subject) resolution (intent-blind: the tenant-isolation
// third member's one query path), then a known identity signs in under
// either intent (#604 d3). An unknown identity under `sign-in` refuses
// `unknown-identity` with no registration event and no charge; under
// `sign-up` it enters the registration leg (registration_signup.go), in this
// same transaction.
//
// A sign-up-intent callback runs under WriteSerialized on the org-create key
// whatever the landing: the landing is known only after the policy read
// inside the transaction, and a fresh-org mint must serialize its cap count
// and admin grant against operator creates. A sign-in callback keeps
// tx.Write. Committed registration refusals return domain.ErrUnauthenticated,
// except budget overflow, which returns admission.ErrOverloaded. Transaction
// and audit errors propagate without publishing a session.
func (s *Auth) completeLogin(ctx context.Context, prov authz.OIDCProvider, txn authz.OIDCTransaction, claims oidcrp.Claims) (OIDCCallbackResult, error) {
	signup := newOIDCSignup(s, prov, txn, claims)
	fn := func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, attempt *sessionCompletionAttempt) error {
		// A rerun means the previous attempt rolled back: its charge goes.
		signup.rollback()
		now := s.now()
		refuse := func(cause string) error {
			if aerr := s.stageLoginRefuse(ctx, az, cause, prov.ID, txn); aerr != nil {
				return aerr
			}
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		if cause, e := s.revalidateProvider(ctx, az, prov); e != nil {
			return e
		} else if cause != "" {
			return refuse(cause)
		}
		epoch, e := az.CredentialEpoch(ctx)
		if e != nil {
			return e
		}
		var account authz.Account
		identity, e := az.ExternalIdentityByKey(ctx, OIDCKind, txn.Issuer, claims.Subject)
		switch {
		case errors.Is(e, domain.ErrNotFound):
			// Login never creates accounts; only a sign-up intent enters a
			// registration policy (#604 d3). Invitation and explicit linking
			// remain the other ways this identity may authenticate.
			if txn.Intent != IntentSignUp {
				return refuse(causeUnknownIdentity)
			}
			account, e = signup.run(ctx, r, az, attempt, epoch, now)
			if e != nil || attempt.refused != sessionNotRefused {
				return e
			}
		case e != nil:
			return e
		default:
			// A8: epoch-inert is terminal, never provisioned.
			if identity.CredentialEpoch != epoch {
				return refuse(causeEpoch)
			}
			// A3: the recorded provider must be the currently enabled one for
			// this issuer (which is prov, since we exchanged there and it is
			// enabled). A mismatch is a restored/superseded link: refuse to
			// operator reconciliation.
			if identity.ProviderID != prov.ID {
				return refuse(causeReconciliation)
			}
			account, e = az.AccountByID(ctx, identity.AccountID)
			if e != nil {
				return e
			}
		}
		mfa, e := evaluateAssurance(prov.AssurancePolicy, claims.ACR, claims.AMR)
		if e != nil {
			return e
		}
		attempt.result, e = s.mintOIDCSession(ctx, az, account, prov, txn, claims, mfa, now)
		return e
	}
	var (
		attempt sessionCompletionAttempt
		err     error
	)
	if txn.Intent == IntentSignUp {
		err = tx.WriteSerialized(ctx, s.DB, orgCreateSerialization, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
			attempt = sessionCompletionAttempt{}
			return fn(ctx, r, az, &attempt)
		})
	} else {
		attempt, err = writeCommittedSessionAttempt(ctx, s.DB, fn)
	}
	if err != nil {
		signup.rollback()
	}
	if errors.Is(err, errSignupIdentityRace) {
		// The UNIQUE key arbitrated a concurrent bind of the same identity:
		// the failed insert aborted the transaction (nothing of this sign-up
		// survives, no org row either), so the refusal commits on its own.
		return OIDCCallbackResult{}, signup.refuseAfterRace(ctx)
	}
	if err != nil {
		return OIDCCallbackResult{}, err
	}
	if refused := attempt.refused.err(); refused != nil {
		return OIDCCallbackResult{}, refused
	}
	return OIDCCallbackResult{Login: attempt.result, Purpose: purposeLogin}, nil
}

// completeLink binds a new identity to the transaction's account as an
// account-security mutation: the proof ceremony was consumed with the
// transaction (A6), so here the mutation reissues the acting session from that
// proof, deleting every prior session.
func (s *Auth) completeLink(ctx context.Context, prov authz.OIDCProvider, txn authz.OIDCTransaction, claims oidcrp.Claims, presented string) (OIDCCallbackResult, error) {
	attempt, err := writeCommittedSessionAttempt(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, attempt *sessionCompletionAttempt) error {
		now := s.now()
		if cause, e := s.revalidateProvider(ctx, az, prov); e != nil {
			return e
		} else if cause != "" {
			if aerr := s.stageOIDCRefuse(ctx, az, cause, prov.ID); aerr != nil {
				return aerr
			}
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		epoch, e := az.CredentialEpoch(ctx)
		if e != nil {
			return e
		}
		account, e := az.AccountByID(ctx, txn.AccountID)
		if e != nil {
			return e
		}
		if _, e := az.ExternalIdentityByKey(ctx, OIDCKind, txn.Issuer, claims.Subject); e == nil {
			return ErrAlreadyLinked
		} else if !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		identityID, e := newID("eid")
		if e != nil {
			return e
		}
		if e := az.CreateExternalIdentity(ctx, authz.NewExternalIdentity{
			ID: identityID, AccountID: account.ID, Kind: OIDCKind, Issuer: txn.Issuer,
			Subject: claims.Subject, ProviderID: prov.ID, CredentialEpoch: epoch, CreatedAt: now,
		}); e != nil {
			return e
		}
		// Re-authenticate the acting session inside the write tx, for the same
		// two reasons its siblings do: a session revoked between the binding
		// check and here must not link an identity and reissue itself, and the
		// replacement must be the SAME artifact kind — a browser that linked an
		// identity and got a `cli` token back would be logged out on the spot,
		// with a long-lived credential handed to script.
		live, e := az.Authenticate(ctx, presented, now)
		if e != nil {
			return e
		}
		if live.Principal != account.PrincipalID {
			return domain.ErrUnauthenticated
		}
		attempt.result, e = s.reissueSession(ctx, az, account, "password", MethodLocalPassword, Artifact(live.Artifact), now)
		if e != nil {
			return e
		}
		ev, e := newAuditEvent(ctx, audit.EventIdentityLinked, account.PrincipalID,
			audit.Object{Type: "external_identity", ID: identityID}, audit.OutcomeSuccess, "",
			audit.Payload{"kind": OIDCKind, "account_id": account.ID, "identity_id": identityID, "provider_id": prov.ID, "authorizing_credential": "password"})
		if e != nil {
			return e
		}
		return az.RecordAuthEvent(ctx, ev)
	})
	if err != nil {
		return OIDCCallbackResult{}, err
	}
	if refused := attempt.refused.err(); refused != nil {
		return OIDCCallbackResult{}, refused
	}
	return OIDCCallbackResult{Login: attempt.result, Purpose: purposeLink}, nil
}

// completeReauth validates a fresh federated authentication and opens a
// reauthentication window over the transaction's environment. It is at least as
// strict as completeLogin: it refuses when the pinned provider moved during the
// exchange (A4 TOCTOU), when the provider has no assurance policy (A5), when
// auth_time is absent or stale (A7), when the returned identity is not the
// account's (B15), when the identity is epoch-inert (B2) or recorded against a
// superseded provider (A3), when the policy is unsatisfied or the amr carries no
// recognized possession factor (A5/B5), when the evidence is weaker than the
// session it re-authorizes (a downgrade), or when the effective window is 0
// (only WebAuthn opens a 0-window gate). On success the acting session rotates.
func (s *Auth) completeReauth(ctx context.Context, prov authz.OIDCProvider, txn authz.OIDCTransaction, claims oidcrp.Claims, presented string) (OIDCCallbackResult, error) {
	attempt, err := writeCommittedSessionAttempt(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, attempt *sessionCompletionAttempt) error {
		now := s.now()
		reject := func(cause string) error {
			if aerr := s.stageOIDCRefuse(ctx, az, cause, prov.ID); aerr != nil {
				return aerr
			}
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		if cause, e := s.revalidateProvider(ctx, az, prov); e != nil {
			return e
		} else if cause != "" {
			return reject(cause)
		}
		if prov.AssurancePolicy == nil {
			return reject(causeNoPolicy)
		}
		if !claims.HasAuthTime {
			return reject(causeNoAuthTime)
		}
		if now.Sub(claims.AuthTime) > oidcAuthTimeBound || claims.AuthTime.After(now.Add(time.Minute)) {
			return reject(causeNoAuthTime)
		}
		identity, e := az.ExternalIdentityByKey(ctx, OIDCKind, txn.Issuer, claims.Subject)
		if errors.Is(e, domain.ErrNotFound) || (e == nil && identity.AccountID != txn.AccountID) {
			return reject(causeUnknownIdentity) // B15: not the identity the session used
		}
		if e != nil && !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		epoch, e := az.CredentialEpoch(ctx)
		if e != nil {
			return e
		}
		// B2: an epoch-inert (restored) identity is terminal, exactly as
		// completeLogin refuses it — never opens a window.
		if identity.CredentialEpoch != epoch {
			return reject(causeEpoch)
		}
		// A3: the identity must be recorded against the currently enabled
		// provider for this issuer (which is prov). A mismatch is a
		// restored/superseded link after a provider replacement.
		if identity.ProviderID != prov.ID {
			return reject(causeReconciliation)
		}
		mfa, e := evaluateAssurance(prov.AssurancePolicy, claims.ACR, claims.AMR)
		if e != nil {
			return e
		}
		if !mfa {
			return reject(causeNoPolicy) // policy not satisfied
		}
		// Possession is checked INDEPENDENTLY of policy satisfaction (A5/B5): an
		// acr match or a knowledge-only amr set can satisfy a policy without any
		// possession factor, and a reveal reauth demands one.
		if !hasPossessionAMR(claims.AMR) {
			return reject(causeNoPossession)
		}

		// Authenticate the initiating session inside this tx and refuse a
		// downgrade: a reauth may not re-authorize a session established with a
		// stronger (phishing-resistant) credential than this federated evidence.
		id, e := az.Authenticate(ctx, presented, now)
		if e != nil || id.SessionID != txn.InitiatingSessionID {
			return reject(causeBinding)
		}
		if id.Assurance.Method != oidcMethod(prov.Issuer) || id.ProviderID != prov.ID {
			return reject(causeBinding)
		}
		evidence := authz.Assurance{Factors: oidcFactors(mfa)}
		if authz.AssuranceRank(id.Assurance) > authz.AssuranceRank(evidence) {
			return reject(causeDowngrade)
		}
		// The environment's effective window is resolved through the one seam,
		// never the global s.ReauthWindow directly (A2), so a lowered environment
		// is honoured on the OIDC path exactly as on the TOTP path.
		effWin, e := s.effectiveReauthWindow(ctx, az, txn.EnvironmentID)
		if e != nil {
			return e
		}
		if effWin <= 0 {
			// A 0-window gate requires WebAuthn: OIDC cannot bind the enumerated
			// unit, so it opens nothing here (B18). Refuse naming the remedy.
			attempt.refused = sessionRefusedWindowClosed
			if aerr := s.stageOIDCRefuse(ctx, az, causeWindowClosed, prov.ID); aerr != nil {
				return aerr
			}
			return nil
		}

		// Rotate the acting session token (every reauth rotates) preserving its
		// factor set, and open the window over the recorded environment.
		account, e := az.AccountByID(ctx, txn.AccountID)
		if e != nil {
			return e
		}
		completion, e := s.completeSession(ctx, az, RotateSession{
			session: id, account: account, factors: id.Assurance.Factors,
		}, now)
		if e != nil {
			return e
		}
		completion.Assurance.Provider = prov.Slug
		windowID, e := newID("raw")
		if e != nil {
			return e
		}
		hardCap := s.hardCap()
		hardExpires := now.Add(hardCap)
		windowExpires := now.Add(effWin)
		if windowExpires.After(hardExpires) {
			windowExpires = hardExpires
		}
		if e := az.OpenReauthWindow(ctx, authz.NewReauthWindow{
			ID: windowID, SessionID: id.SessionID, EnvironmentID: txn.EnvironmentID, CeremonyID: txn.ID,
			FactorClass: "oidc", AuthenticatedAt: now, WindowExpiresAt: windowExpires,
			HardExpiresAt: hardExpires, CredentialEpoch: epoch, CreatedAt: now,
		}); e != nil {
			return e
		}
		for _, ev := range []struct {
			typ     audit.EventType
			payload audit.Payload
		}{
			{audit.EventAuthReauthenticated, audit.Payload{"session_id": id.SessionID, "factor": "oidc"}},
			{audit.EventOIDCLogin, audit.Payload{
				"method": oidcMethod(txn.Issuer), "purpose": purposeReauth, "account_id": txn.AccountID,
				"assurance": "multi-factor", "provider_id": prov.ID, "acr": claims.ACR,
				"amr": joinAMR(claims.AMR), "provider_row_version": int(prov.RowVersion),
			}},
		} {
			e, aerr := newAuditEvent(ctx, ev.typ, id.Principal, audit.Object{Type: "session", ID: id.SessionID}, audit.OutcomeSuccess, "", ev.payload)
			if aerr != nil {
				return aerr
			}
			if aerr := az.RecordAuthEvent(ctx, e); aerr != nil {
				return aerr
			}
		}
		attempt.result = completion
		return nil
	})
	if err != nil {
		return OIDCCallbackResult{}, err
	}
	if refused := attempt.refused.err(); refused != nil {
		return OIDCCallbackResult{}, refused
	}
	return OIDCCallbackResult{Login: attempt.result, Purpose: purposeReauth}, nil
}

// mintOIDCSession mints a browser session for a federated login, recording the
// assurance the provider policy yielded. The raw acr/amr and the provider
// row_version are recorded in the audit event, read in this same mint tx (A12);
// the provider-change sweep by provider_id keeps a stale evaluation from
// lingering (A4).
func (s *Auth) mintOIDCSession(ctx context.Context, az *authz.TxAuthorizer, account authz.Account, prov authz.OIDCProvider, txn authz.OIDCTransaction, claims oidcrp.Claims, mfa bool, now time.Time) (LoginResult, error) {
	issuer := txn.Issuer
	factorClasses := oidcFactors(mfa)
	assuranceLabel := "single-factor"
	if mfa {
		assuranceLabel = "multi-factor"
	}
	result, err := s.completeSession(ctx, az, CreateSession{
		account: account, artifact: ArtifactBrowser,
		assurance: Assurance{Method: oidcMethod(issuer), Provider: prov.Slug, Factors: factorClasses, AuthenticatedAt: now},
		csrf:      sessionWithCSRF, providerID: prov.ID,
	}, now)
	if err != nil {
		return LoginResult{}, err
	}
	for _, ev := range []struct {
		typ     audit.EventType
		payload audit.Payload
	}{
		{audit.EventOIDCLogin, withLoginIntent(audit.Payload{
			"method": oidcMethod(issuer), "purpose": purposeLogin, "account_id": account.ID,
			"assurance": assuranceLabel, "provider_id": prov.ID, "acr": claims.ACR,
			"amr": joinAMR(claims.AMR), "provider_row_version": int(prov.RowVersion),
		}, txn)},
		{audit.EventAuthSessionCreated, audit.Payload{
			"session_id": result.SessionID, "artifact": ArtifactBrowser.String(),
			"method": oidcMethod(issuer), "assurance": assuranceLabel,
		}},
	} {
		e, err := newAuditEvent(ctx, ev.typ, account.PrincipalID, audit.Object{Type: "session", ID: result.SessionID}, audit.OutcomeSuccess, "", ev.payload)
		if err != nil {
			return LoginResult{}, err
		}
		if err := az.RecordAuthEvent(ctx, e); err != nil {
			return LoginResult{}, err
		}
	}
	return result, nil
}

// refuseOIDC records an oidc_refused event with its cause and answers the
// uniform refusal. The event is committed with the refusal (fail-closed): a
// denial without its durable record is exactly what the discipline forbids.
func (s *Auth) refuseOIDC(ctx context.Context, cause, providerID, backoffKey string) error {
	if backoffKey != "" {
		s.Admission.RecordFailure(backoffKey)
	}
	if err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return s.stageOIDCRefuse(ctx, az, cause, providerID)
	}); err != nil {
		return err
	}
	if cause == causeWindowClosed {
		return ErrReauthWindowClosed
	}
	return domain.ErrUnauthenticated
}

// stageOIDCRefuse stages the refusal event in the caller's transaction and
// returns only the audit-write error, on the same fail-closed contract as
// failLogin.
func (s *Auth) stageOIDCRefuse(ctx context.Context, az *authz.TxAuthorizer, cause, providerID string) error {
	return s.stageOIDCRefusePayload(ctx, az, cause, providerID, audit.Payload{})
}

// stageLoginRefuse is stageOIDCRefuse for a resolved login transaction: the
// refusal carries the recorded intent and sign-up scope (#604 d8).
func (s *Auth) stageLoginRefuse(ctx context.Context, az *authz.TxAuthorizer, cause, providerID string, txn authz.OIDCTransaction) error {
	return s.stageOIDCRefusePayload(ctx, az, cause, providerID, withLoginIntent(audit.Payload{}, txn))
}

func (s *Auth) stageOIDCRefusePayload(ctx context.Context, az *authz.TxAuthorizer, cause, providerID string, payload audit.Payload) error {
	payload["cause"] = cause
	if providerID != "" {
		payload["provider_id"] = providerID
	}
	e, err := newAuditEvent(ctx, audit.EventOIDCRefused, "",
		audit.Object{Type: "oidc_transaction"}, audit.OutcomeFailure, "", payload)
	if err != nil {
		return err
	}
	return az.RecordAuthEvent(ctx, e)
}

// withLoginIntent adds a login transaction's intent and sign-up scope to an
// auth.oidc_login / auth.oidc_refused payload.
func withLoginIntent(payload audit.Payload, txn authz.OIDCTransaction) audit.Payload {
	if txn.Intent != "" {
		payload["intent"] = txn.Intent
	}
	if txn.SignupScopeOrgID != "" {
		payload["signup_org"] = txn.SignupScopeOrgID
	}
	return payload
}

func joinAMR(amr []string) string {
	out := ""
	for i, m := range amr {
		if i > 0 {
			out += ","
		}
		out += m
	}
	return out
}

// AuthMethodProvider is one enabled provider for the public methods list.
type AuthMethodProvider struct {
	Slug        string
	DisplayName string
	Kind        string
	// Brand names the provider whose published button rules the sign-in
	// surface follows (#587 d4): "google", "microsoft", or "" for generic.
	Brand string
}

// Brand issuers (docs/research/social-providers.md 2.1, 3.1): Google's one
// issuer, and the Entra authority host of every tenant-specific row.
const (
	googleIssuer         = "https://accounts.google.com"
	microsoftIssuerStart = "https://login.microsoftonline.com/"
)

// providerBrand derives the presentation brand from a pinned issuer. It is
// display only; nothing admits or refuses on it.
func providerBrand(issuer string) string {
	switch {
	case issuer == googleIssuer:
		return "google"
	case strings.HasPrefix(issuer, microsoftIssuerStart):
		return "microsoft"
	default:
		return ""
	}
}

// AuthMethods returns enabled OIDC and SAML providers and reports local login
// enabled. OIDC brands are derived from the pinned issuer. This is proof-free,
// instance-level discovery; repository read errors propagate.
func (s *Auth) AuthMethods(ctx context.Context) ([]AuthMethodProvider, bool, error) {
	var out []AuthMethodProvider
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		rows, e := az.ListProviders(ctx)
		if e != nil {
			return e
		}
		for _, p := range rows {
			if p.Enabled {
				out = append(out, AuthMethodProvider{Slug: p.Slug, DisplayName: p.DisplayName, Kind: OIDCKind, Brand: providerBrand(p.Issuer)})
			}
		}
		samlProviders, e := az.ListSAMLProviders(ctx)
		if e != nil {
			return e
		}
		for _, p := range samlProviders {
			if p.Enabled {
				out = append(out, AuthMethodProvider{Slug: p.Slug, DisplayName: p.DisplayName, Kind: SAMLKind})
			}
		}
		return nil
	})
	return out, true, err
}

// ExternalIdentityView is the transport-facing shape of a linked identity, so
// internal/server needs no authz import.
type ExternalIdentityView struct {
	ID         string
	Kind       string
	Issuer     string
	Subject    string
	ProviderID string
	CreatedAt  time.Time
}

// ListIdentities returns an account's linked external identities.
func (s *Auth) ListIdentities(ctx context.Context, presented string) ([]ExternalIdentityView, error) {
	var out []ExternalIdentityView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		id, e := az.Authenticate(ctx, presented, s.now())
		if e != nil {
			return e
		}
		account, e := az.AccountByPrincipal(ctx, id.Principal)
		if e != nil {
			return e
		}
		rows, e := az.ExternalIdentitiesForAccount(ctx, account.ID)
		if e != nil {
			return e
		}
		for _, r := range rows {
			out = append(out, ExternalIdentityView{
				ID: r.ID, Kind: r.Kind, Issuer: r.Issuer, Subject: r.Subject, ProviderID: r.ProviderID, CreatedAt: r.CreatedAt,
			})
		}
		return nil
	})
	return out, err
}

// UnlinkIdentity removes a linked identity as an account-security mutation. It
// refuses removing the last remaining credential (in the lockout-invariant
// shape), verifies the pre-existing password, and reissues the acting session.
func (s *Auth) UnlinkIdentity(ctx context.Context, presented, identityID, proof string) (LoginResult, error) {
	var (
		account      authz.Account
		cred         authz.PasswordCredential
		identityKind string
	)
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		id, e := az.Authenticate(ctx, presented, s.now())
		if e != nil {
			return e
		}
		account, e = az.AccountByPrincipal(ctx, id.Principal)
		if e != nil {
			return e
		}
		identity, e := az.ExternalIdentityByID(ctx, identityID)
		if errors.Is(e, domain.ErrNotFound) || (e == nil && identity.AccountID != account.ID) {
			return ErrIdentityNotFound
		}
		if e != nil {
			return e
		}
		identityKind = identity.Kind
		cred, e = az.PasswordCredentialFor(ctx, account.ID)
		if errors.Is(e, domain.ErrNotFound) {
			return ErrNoProofCredential
		}
		return e
	})
	if err != nil {
		return LoginResult{}, err
	}

	release, err := s.enterFactorBudget(ctx, account.ID)
	if err != nil {
		return LoginResult{}, err
	}
	defer release()
	if !s.verifyPassword(ctx, account.ID, cred, proof) {
		s.recordFactorFailure(ctx, account.PrincipalID, account.ID)
		return LoginResult{}, domain.ErrUnauthenticated
	}
	s.Admission.RecordSuccess(account.ID)

	result, err := writeCommittedLoginResult(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, result *LoginResult) error {
		now := s.now()
		live, e := az.Authenticate(ctx, presented, now)
		if e != nil {
			return e
		}
		if live.Principal != account.PrincipalID {
			return domain.ErrUnauthenticated
		}
		// Last-credential refusal: unlinking must not strand the account. A
		// password stands here (the proof), so an identity unlink is always
		// safe while a password exists; the check is kept for the passwordless
		// future and stays in the lockout-invariant shape.
		identities, e := az.ExternalIdentitiesForAccount(ctx, account.ID)
		if e != nil {
			return e
		}
		if _, e := az.PasswordCredentialFor(ctx, account.ID); errors.Is(e, domain.ErrNotFound) && len(identities) <= 1 {
			return ErrLastCredential
		} else if e != nil && !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		if e := az.RemoveExternalIdentity(ctx, identityID); e != nil {
			return e
		}
		*result, e = s.reissueSession(ctx, az, account, "password", MethodLocalPassword, Artifact(live.Artifact), now)
		if e != nil {
			return e
		}
		ev, e := newAuditEvent(ctx, audit.EventIdentityUnlinked, account.PrincipalID,
			audit.Object{Type: "external_identity", ID: identityID}, audit.OutcomeSuccess, "",
			audit.Payload{"kind": identityKind, "account_id": account.ID, "identity_id": identityID, "authorizing_credential": "password"})
		if e != nil {
			return e
		}
		return az.RecordAuthEvent(ctx, ev)
	})
	if err != nil {
		return LoginResult{}, err
	}
	return result, nil
}
