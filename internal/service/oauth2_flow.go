package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/federationhttp"
	"github.com/Hikyo-Org/hikyo/internal/oauth2rp"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

var ErrOAuth2Reauth = errors.New("service: OAuth2 cannot reauthenticate; enrol WebAuthn or TOTP")
var errOAuth2EmailNeeded = errors.New("service: OAuth2 email assertion needed outside transaction")
var errOAuth2UserInfo = errors.New("service: OAuth2 email assertion failed")

func (s *Auth) oauth2RP(prov authz.OAuth2Provider) (*oauth2rp.Provider, error) {
	client := s.OAuth2HTTPClient
	if client == nil {
		var err error
		client, err = federationhttp.NewClient(s.FederationPolicy, federationhttp.TokenBytes)
		if err != nil {
			return nil, err
		}
	}
	return oauth2rp.New(prov.Profile, prov.Issuer, client)
}
func newOAuth2Signup(s *Auth, prov authz.OAuth2Provider, txn authz.OAuth2Transaction, claims oauth2rp.User) *federatedSignup {
	return &federatedSignup{auth: s, kind: OAuth2Kind, providerID: prov.ID, issuer: txn.Issuer, signupOrg: txn.SignupScopeOrgID, subject: claims.Subject, claims: claims.Claims}
}
func (s *Auth) mintOAuth2Session(ctx context.Context, az *authz.TxAuthorizer, account authz.Account, prov authz.OAuth2Provider, txn authz.OAuth2Transaction, now time.Time) (LoginResult, error) {
	result, err := s.completeSession(ctx, az, CreateSession{account: account, artifact: ArtifactBrowser, assurance: Assurance{Method: "oauth2:" + txn.Issuer, Provider: prov.Slug, Factors: []string{"oauth2"}, AuthenticatedAt: now}, csrf: sessionWithCSRF}, now)
	if err != nil {
		return LoginResult{}, err
	}
	bound, err := az.BindSessionToOAuth2Provider(ctx, result.SessionID, prov.ID)
	if err != nil {
		return LoginResult{}, err
	}
	if !bound {
		return LoginResult{}, domain.ErrConflict
	}
	for _, row := range []struct {
		typ     audit.EventType
		payload audit.Payload
	}{
		{audit.EventOAuth2Login, withOAuth2LoginIntent(audit.Payload{"method": "oauth2:" + txn.Issuer, "purpose": txn.Purpose, "account_id": account.ID, "assurance": "single-factor", "provider_id": prov.ID, "provider_row_version": int(prov.RowVersion)}, txn)},
		{audit.EventAuthSessionCreated, audit.Payload{"session_id": result.SessionID, "artifact": ArtifactBrowser.String(), "method": "oauth2:" + txn.Issuer, "assurance": "single-factor"}},
	} {
		ev, err := newAuditEvent(ctx, row.typ, account.PrincipalID, audit.Object{Type: "session", ID: result.SessionID}, audit.OutcomeSuccess, "", row.payload)
		if err != nil {
			return LoginResult{}, err
		}
		if err := az.RecordAuthEvent(ctx, ev); err != nil {
			return LoginResult{}, err
		}
	}
	return result, nil
}
func (s *Auth) OAuth2Start(ctx context.Context, slug, purpose, intent, signupOrg, environmentID, presented, proof string, browser bool) (OIDCStartResult, error) {
	// Admission is entered FIRST, uniformly for every purpose and BEFORE the
	// provider is resolved or the purpose/environment validated. An unknown
	// slug, a bad purpose, a missing environment and a fully resolved provider
	// then take one path with one per-IP admission cost, so a pre-auth prober
	// cannot enumerate provider config by the status, body or timing of the
	// refusal. link additionally ride the per-account backoff below; the
	// per-IP slot taken here already throttles their Argon2 proof.
	release, err := s.Admission.Enter(ctx, audit.FromContext(ctx).SourceIP)
	if err != nil {
		return OIDCStartResult{}, err
	}
	defer release()

	if purpose == purposeReauth {
		return OIDCStartResult{}, ErrOAuth2Reauth
	}
	switch purpose {
	case purposeLogin, purposeLink, "establish", purposeClaim:
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
	if environmentID != "" {
		return OIDCStartResult{}, ErrEnvironmentNotForPurpose
	}

	// Phase 1 - resolve the provider and, for a session-bound purpose, the
	// acting session and account. Both run INSIDE the admission budget, and
	// link authenticate BEFORE the provider is resolved so an
	// unauthenticated caller refuses identically (uniform 401) whether the slug
	// is known or not: provider existence is never what a prober learns first.
	var (
		authority  authz.CredentialAuthority
		provider   authz.OAuth2Provider
		epoch      int64
		account    authz.Account
		sessionID  string
		claimCause string
	)
	err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		var e error
		if purpose != purposeLogin && purpose != purposeClaim {
			id, e := az.Authenticate(ctx, presented, s.now())
			if e != nil {
				return e
			}
			account, e = az.AccountByPrincipal(ctx, id.Principal)
			if e != nil {
				return e
			}
			sessionID = id.SessionID
		}
		if purpose == purposeClaim {
			// The authority is the proof (#610): phase-1 checks without
			// consumption, refused by cause before the provider is resolved.
			if epoch, e = az.CredentialEpoch(ctx); e != nil {
				return e
			}
			authority, account, claimCause, e = s.claimAuthority(ctx, az, proof, epoch)
			if e != nil || claimCause != "" {
				return e
			}
		}
		provider, e = az.OAuth2ProviderBySlug(ctx, slug)
		if errors.Is(e, domain.ErrNotFound) {
			return ErrProviderNotFound
		}
		if e != nil {
			return e
		}
		if !provider.Enabled {
			return ErrProviderNotFound
		}
		if epoch, e = az.CredentialEpoch(ctx); e != nil {
			return e
		}
		if purpose == "establish" {
			local, err := hasLocalProof(ctx, az, account.ID)
			if err != nil {
				return err
			}
			if local {
				return ErrBadPurpose
			}
		}
		return nil
	})
	if err != nil {
		return OIDCStartResult{}, err
	}
	if claimCause != "" {
		return OIDCStartResult{}, s.refuseAuthority(ctx, claimCause)
	}
	// link ride the per-account backoff so a stolen session cannot be an
	// unthrottled Argon2 oracle; the per-IP admission slot is already held, so
	// this adds only the account-scoped delay, never a second slot.
	if purpose != purposeLogin && purpose != purposeClaim {
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

	rp, err := s.oauth2RP(provider)
	if err != nil {
		return OIDCStartResult{}, err
	}

	state, stateVerifier, err := crypto.NewArtifact(crypto.ArtifactOIDCState)
	if err != nil {
		return OIDCStartResult{}, err
	}
	pkce, err := randToken()
	if err != nil {
		return OIDCStartResult{}, err
	}
	authURL := rp.AuthCodeURL(provider.ClientID, provider.RedirectURI, state, pkce)

	txID, err := newID("oidctx")
	if err != nil {
		return OIDCStartResult{}, err
	}
	newTx := authz.NewOAuth2Transaction{
		ID: txID, StateVerifier: stateVerifier, PKCEVerifier: pkce,
		ProviderID: provider.ID, Issuer: provider.Issuer, RedirectURI: provider.RedirectURI,
		Purpose: purpose, Browser: browser, CredentialEpoch: epoch,
		Intent: intent, SignupScopeOrgID: signupOrg,
	}
	var bindingCookie string
	if purpose == purposeLogin || purpose == purposeClaim {
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
	if purpose == purposeClaim {
		newTx.AccountID = account.ID
		newTx.AuthorityID = authority.ID
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
		return az.CreateOAuth2Transaction(ctx, newTx)
	})
	if err != nil {
		return OIDCStartResult{}, err
	}
	return OIDCStartResult{AuthURL: authURL, State: state, BindingCookie: bindingCookie, Purpose: purpose}, nil
}

func (s *Auth) OAuth2Callback(ctx context.Context, slug, code, stateValue, issParam, idpError, bindingCookie, presented string) (OIDCCallbackResult, error) {
	release, err := s.Admission.Enter(ctx, audit.FromContext(ctx).SourceIP)
	if err != nil {
		return OIDCCallbackResult{}, err
	}
	defer release()

	// An unparseable state cannot match any transaction; refuse uniformly.
	if crypto.ParseArtifact(stateValue, crypto.ArtifactOIDCState) != nil {
		return OIDCCallbackResult{}, s.refuseOAuth2(ctx, causeState, "", stateValue)
	}
	stateVerifier := crypto.ArtifactVerifier(stateValue)

	// Phase A - resolve, validate, consume. No network here.
	var (
		txn     authz.OAuth2Transaction
		prov    authz.OAuth2Provider
		refused error
	)
	err = tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		refused = nil
		now := s.now()
		epoch, e := az.CredentialEpoch(ctx)
		if e != nil {
			return e
		}
		t, e := az.OAuth2TransactionByState(ctx, stateVerifier)
		if errors.Is(e, domain.ErrNotFound) {
			cause := causeState
			if _, err := az.OIDCTransactionByState(ctx, stateVerifier); err == nil {
				cause = causeMixup
			} else if !errors.Is(err, domain.ErrNotFound) {
				return err
			}
			if aerr := s.stageOAuth2Refuse(ctx, az, cause, ""); aerr != nil {
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
		var provider authz.OAuth2Provider
		if cause == "" {
			provider, e = az.OAuth2ProviderForCallback(ctx, t.ProviderID)
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
				cause = causeMixup
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
			cause = s.checkOAuth2Binding(ctx, az, t, bindingCookie, presented, now)
		}
		// A resolved transaction is spent by this callback, whatever the outcome:
		// single-use, no replay.
		claimed, e := az.ConsumeOAuth2Transaction(ctx, t.ID, now)
		if e != nil {
			return e
		}
		if !claimed && cause == "" {
			cause = causeState
		}
		if cause != "" {
			if aerr := s.stageOAuth2LoginRefuse(ctx, az, cause, t.ProviderID, t); aerr != nil {
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
	rp, err := s.oauth2RP(prov)
	if err != nil {
		return metadata, err
	}
	secret, err := s.Keyring.ForInstance().OpenField(oauth2ProviderSecretAAD(prov.ID), prov.ClientSecret)
	if err != nil {
		return metadata, err
	}
	token, err := rp.Exchange(ctx, prov.ClientID, string(secret), txn.RedirectURI, code, txn.PKCEVerifier)
	crypto.Zero(secret)
	if err != nil {
		return metadata, s.refuseOAuth2Transaction(ctx, causeIDPError, prov.ID, txn)
	}
	claims, err := rp.User(ctx, token)
	cause := ""
	if err != nil {
		cause = "userinfo-error"
	}
	if cause != "" {
		return metadata, s.refuseOAuth2Transaction(ctx, cause, prov.ID, txn)
	}

	// Phase C - purpose dispatch. The branch IS the transaction's purpose, so a
	// response obtained for one purpose cannot complete another.
	var result OIDCCallbackResult
	switch txn.Purpose {
	case purposeLogin:
		result, err = s.completeOAuth2Login(ctx, prov, txn, claims, func(ctx context.Context) (string, error) { return rp.VerifiedEmail(ctx, token) })
	case purposeLink:
		result, err = s.completeOAuth2Link(ctx, prov, txn, claims, presented)
	case "establish":
		result, err = s.completeOAuth2Establish(ctx, prov, txn, claims, presented)
	case purposeClaim:
		result, err = s.completeOAuth2Claim(ctx, prov, txn, claims)
	default:
		return metadata, ErrBadPurpose
	}
	result.Purpose = txn.Purpose
	result.State = stateValue
	result.Browser = txn.Browser
	return result, err
}

func (s *Auth) checkOAuth2Binding(ctx context.Context, az *authz.TxAuthorizer, t authz.OAuth2Transaction, bindingCookie, presented string, now time.Time) string {
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

func (s *Auth) revalidateOAuth2Provider(ctx context.Context, az *authz.TxAuthorizer, snapshot authz.OAuth2Provider) (string, error) {
	ok, err := az.GuardOAuth2ProviderForMint(ctx, snapshot.ID, snapshot.RowVersion, snapshot.Issuer)
	if err != nil {
		return "", err
	}
	if !ok {
		return causeReconciliation, nil
	}
	return "", nil
}

func (s *Auth) completeOAuth2Login(ctx context.Context, prov authz.OAuth2Provider, txn authz.OAuth2Transaction, claims oauth2rp.User, fetchEmail func(context.Context) (string, error)) (OIDCCallbackResult, error) {
	signup := newOAuth2Signup(s, prov, txn, claims)
	var email string
	var emailErr error
	fetched := false
	resumeEmail := false
	signup.emailAssertion = func(context.Context) (string, string, bool, error) {
		if !fetched {
			return "", "", false, errOAuth2EmailNeeded
		}
		if emailErr != nil {
			return "", "", false, errOAuth2UserInfo
		}
		return email, "github-primary", email != "", nil
	}
	fn := func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, attempt *sessionCompletionAttempt) error {
		// Preserve the admitted budget reservation across the network leg.
		// A database retry refunds and reacquires it as usual.
		if !resumeEmail {
			signup.rollback()
		}
		resumeEmail = false
		now := s.now()
		refuse := func(cause string) error {
			if aerr := s.stageOAuth2LoginRefuse(ctx, az, cause, prov.ID, txn); aerr != nil {
				return aerr
			}
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		if cause, e := s.revalidateOAuth2Provider(ctx, az, prov); e != nil {
			return e
		} else if cause != "" {
			return refuse(cause)
		}
		epoch, e := az.CredentialEpoch(ctx)
		if e != nil {
			return e
		}
		if epoch != txn.CredentialEpoch {
			return refuse(causeEpoch)
		}
		var account authz.Account
		identity, e := az.ExternalIdentityByKey(ctx, OAuth2Kind, txn.Issuer, claims.Subject)
		switch {
		case errors.Is(e, domain.ErrNotFound):
			// Login never creates accounts; only a sign-up intent enters a
			// registration policy (#604 d3). An invitation claim (purpose
			// claim, #610) and explicit linking are the other ways this
			// identity may come to authenticate.
			if txn.Intent != IntentSignUp {
				return refuse(causeUnknownIdentity)
			}
			account, e = signup.run(ctx, r, az, attempt, epoch, now)
			if errors.Is(e, errOAuth2UserInfo) {
				return refuse("userinfo-error")
			}
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
		attempt.result, e = s.mintOAuth2Session(ctx, az, account, prov, txn, now)

		return e
	}
	var (
		attempt sessionCompletionAttempt
		err     error
	)
	for {

		if txn.Intent == IntentSignUp {
			err = tx.WriteSerialized(ctx, s.DB, orgCreateSerialization, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				attempt = sessionCompletionAttempt{}
				return fn(ctx, r, az, &attempt)
			})
		} else {
			attempt, err = writeCommittedSessionAttempt(ctx, s.DB, fn)
		}
		if !errors.Is(err, errOAuth2EmailNeeded) {
			break
		}
		resumeEmail = true
		email, emailErr = fetchEmail(ctx)
		fetched = true
	}
	if errors.Is(err, errOAuth2UserInfo) {
		signup.rollback()
		return OIDCCallbackResult{}, s.refuseOAuth2Transaction(ctx, "userinfo-error", prov.ID, txn)
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
	if refused := attempt.refusal(); refused != nil {
		return OIDCCallbackResult{}, refused
	}
	return OIDCCallbackResult{Login: attempt.result, Purpose: purposeLogin}, nil
}

func (s *Auth) completeOAuth2Link(ctx context.Context, prov authz.OAuth2Provider, txn authz.OAuth2Transaction, claims oauth2rp.User, presented string) (OIDCCallbackResult, error) {
	attempt, err := writeCommittedSessionAttempt(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, attempt *sessionCompletionAttempt) error {
		now := s.now()
		if cause, e := s.revalidateOAuth2Provider(ctx, az, prov); e != nil {
			return e
		} else if cause != "" {
			if aerr := s.stageOAuth2LoginRefuse(ctx, az, cause, prov.ID, txn); aerr != nil {
				return aerr
			}
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		epoch, e := az.CredentialEpoch(ctx)
		if e != nil {
			return e
		}
		if epoch != txn.CredentialEpoch {
			attempt.refused = sessionRefusedUnauthenticated
			return s.stageOAuth2LoginRefuse(ctx, az, causeEpoch, prov.ID, txn)
		}
		account, e := az.AccountByID(ctx, txn.AccountID)
		if e != nil {
			return e
		}
		if _, e := az.ExternalIdentityByKey(ctx, OAuth2Kind, txn.Issuer, claims.Subject); e == nil {
			return ErrAlreadyLinked
		} else if !errors.Is(e, domain.ErrNotFound) {
			return e
		}
		identityID, e := newID("eid")
		if e != nil {
			return e
		}
		if e := az.CreateExternalIdentity(ctx, authz.NewExternalIdentity{
			ID: identityID, AccountID: account.ID, Kind: OAuth2Kind, Issuer: txn.Issuer,
			Subject: claims.Subject, ProviderID: prov.ID, CredentialEpoch: epoch, CreatedAt: now,
		}); e != nil {
			return e
		}
		// Re-authenticate the acting session inside the write tx, for the same
		// two reasons its siblings do: a session revoked between the binding
		// check and here must not link an identity and reissue itself, and the
		// replacement must be the same artifact kind: a browser that linked an
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
			audit.Payload{"kind": OAuth2Kind, "account_id": account.ID, "identity_id": identityID, "provider_id": prov.ID, "authorizing_credential": "password"})
		if e != nil {
			return e
		}
		return az.RecordAuthEvent(ctx, ev)
	})
	if err != nil {
		return OIDCCallbackResult{}, err
	}
	if refused := attempt.refusal(); refused != nil {
		return OIDCCallbackResult{}, refused
	}
	return OIDCCallbackResult{Login: attempt.result, Purpose: purposeLink}, nil
}

func (s *Auth) refuseOAuth2(ctx context.Context, cause, providerID, backoffKey string) error {
	if backoffKey != "" {
		s.Admission.RecordFailure(backoffKey)
	}
	if err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return s.stageOAuth2Refuse(ctx, az, cause, providerID)
	}); err != nil {
		return err
	}
	if cause == causeWindowClosed {
		return ErrReauthWindowClosed
	}
	return domain.ErrUnauthenticated
}

// stageOAuth2Refuse stages the refusal event in the caller's transaction and
// returns only the audit-write error, on the same fail-closed contract as
// failLogin.
func (s *Auth) stageOAuth2Refuse(ctx context.Context, az *authz.TxAuthorizer, cause, providerID string) error {
	return s.stageOAuth2RefusePayload(ctx, az, cause, providerID, audit.Payload{})
}

// stageOAuth2LoginRefuse is stageOAuth2Refuse for a resolved login transaction: the
// refusal carries the recorded intent and sign-up scope (#604 d8).
func (s *Auth) stageOAuth2LoginRefuse(ctx context.Context, az *authz.TxAuthorizer, cause, providerID string, txn authz.OAuth2Transaction) error {
	return s.stageOAuth2RefusePayload(ctx, az, cause, providerID, withOAuth2LoginIntent(audit.Payload{}, txn))
}

func (s *Auth) stageOAuth2RefusePayload(ctx context.Context, az *authz.TxAuthorizer, cause, providerID string, payload audit.Payload) error {
	payload["cause"] = cause
	if providerID != "" {
		payload["provider_id"] = providerID
	}
	e, err := newAuditEvent(ctx, audit.EventOAuth2Refused, "",
		audit.Object{Type: "oauth2_transaction"}, audit.OutcomeFailure, "", payload)
	if err != nil {
		return err
	}
	return az.RecordAuthEvent(ctx, e)
}

// withOAuth2LoginIntent adds a login transaction's intent and sign-up scope to an
// auth.oauth2_login / auth.oauth2_refused payload.
func withOAuth2LoginIntent(payload audit.Payload, txn authz.OAuth2Transaction) audit.Payload {
	if txn.Purpose != "" {
		payload["purpose"] = txn.Purpose
	}
	if txn.Intent != "" {
		payload["intent"] = txn.Intent
	}
	if txn.SignupScopeOrgID != "" {
		payload["signup_org"] = txn.SignupScopeOrgID
	}
	return payload
}

// hasLocalProof is live credential state, never GitHub account enrollment metadata.
func hasLocalProof(ctx context.Context, az *authz.TxAuthorizer, accountID string) (bool, error) {
	_, err := az.PasswordCredentialFor(ctx, accountID)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return false, err
	}
	_, err = az.ConfirmedTOTP(ctx, accountID)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return false, err
	}
	keys, err := az.WebAuthnCredentialsForAccount(ctx, accountID)
	if err != nil {
		return false, err
	}
	for _, key := range keys {
		if !key.Disabled {
			return true, nil
		}
	}
	return false, nil
}
func (s *Auth) completeOAuth2Establish(ctx context.Context, prov authz.OAuth2Provider, txn authz.OAuth2Transaction, claims oauth2rp.User, presented string) (OIDCCallbackResult, error) {
	var result OIDCCallbackResult
	attempt, err := writeCommittedSessionAttempt(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, a *sessionCompletionAttempt) error {
		refuse := func(cause string) error {
			a.refused = sessionRefusedUnauthenticated
			return s.stageOAuth2LoginRefuse(ctx, az, cause, prov.ID, txn)
		}
		cause, err := s.revalidateOAuth2Provider(ctx, az, prov)
		if err != nil {
			return err
		}
		if cause != "" {
			return refuse(cause)
		}
		live, err := az.Authenticate(ctx, presented, s.now())
		if err != nil || live.SessionID != txn.InitiatingSessionID {
			return refuse(causeBinding)
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if epoch != txn.CredentialEpoch {
			return refuse(causeEpoch)
		}
		identity, err := az.ExternalIdentityByKey(ctx, OAuth2Kind, txn.Issuer, claims.Subject)
		if errors.Is(err, domain.ErrNotFound) {
			return refuse(causeUnknownIdentity)
		}
		if err != nil {
			return err
		}
		account, err := az.AccountByID(ctx, txn.AccountID)
		if err != nil {
			return err
		}
		if identity.AccountID != account.ID || account.PrincipalID != live.Principal {
			return refuse(causeUnknownIdentity)
		}
		if identity.CredentialEpoch != epoch {
			return refuse(causeEpoch)
		}
		if identity.ProviderID != prov.ID {
			return refuse(causeReconciliation)
		}
		local, err := hasLocalProof(ctx, az, account.ID)
		if err != nil {
			return err
		}
		if local {
			return refuse(causePurpose)
		}
		a.result, err = s.completeSession(ctx, az, RotateSession{session: live, account: account, factors: []string{"oauth2"}}, s.now())
		if err != nil {
			return err
		}
		if err := az.StampCredentialEstablish(ctx, live.SessionID, identity.ID, s.now().Add(5*time.Minute)); err != nil {
			return err
		}
		ev, err := newAuditEvent(ctx, audit.EventCredentialEstablish, account.PrincipalID, audit.Object{Type: "session", ID: live.SessionID}, audit.OutcomeSuccess, "", audit.Payload{"account_id": account.ID, "identity_id": identity.ID, "provider_id": prov.ID, "kind": OAuth2Kind, "purpose": "account-security"})
		if err != nil {
			return err
		}
		return az.RecordAuthEvent(ctx, ev)
	})
	if err != nil {
		return result, err
	}
	return OIDCCallbackResult{Login: attempt.result}, attempt.refusal()
}

// completeOAuth2Claim spends the transaction's authority on the shared claim
// core (claim.go); the session is single-factor, as every OAuth2 login.
func (s *Auth) completeOAuth2Claim(ctx context.Context, prov authz.OAuth2Provider, txn authz.OAuth2Transaction, claims oauth2rp.User) (OIDCCallbackResult, error) {
	login, err := s.completeClaim(ctx, federatedClaim{
		kind: OAuth2Kind, providerID: prov.ID, issuer: txn.Issuer, subject: claims.Subject,
		epoch: txn.CredentialEpoch, authorityID: txn.AuthorityID, accountID: txn.AccountID,
		revalidate: func(ctx context.Context, az *authz.TxAuthorizer) (string, error) {
			return s.revalidateOAuth2Provider(ctx, az, prov)
		},
		refuse: func(ctx context.Context, az *authz.TxAuthorizer, cause string) error {
			return s.stageOAuth2LoginRefuse(ctx, az, cause, prov.ID, txn)
		},
		mint: func(ctx context.Context, az *authz.TxAuthorizer, account authz.Account, now time.Time) (LoginResult, error) {
			return s.mintOAuth2Session(ctx, az, account, prov, txn, now)
		},
	})
	if err != nil {
		return OIDCCallbackResult{}, err
	}
	return OIDCCallbackResult{Login: login}, nil
}

func (s *Auth) refuseOAuth2Transaction(ctx context.Context, cause, providerID string, txn authz.OAuth2Transaction) error {
	if err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return s.stageOAuth2LoginRefuse(ctx, az, cause, providerID, txn)
	}); err != nil {
		return err
	}
	return domain.ErrUnauthenticated
}
