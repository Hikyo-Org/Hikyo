package service

import (
	"context"
	stdcrypto "crypto"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	wencrypto "github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/samlsp"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

const (
	SAMLKind = "saml"

	samlNameIDPersistent  = "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent"
	samlNameIDUnspecified = "urn:oasis:names:tc:SAML:1.1:nameid-format:unspecified"
	samlNameIDTransient   = "urn:oasis:names:tc:SAML:2.0:nameid-format:transient"
	samlNameIDEmail       = "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"

	samlTransactionLifetime = 10 * time.Minute
	samlReauthFreshness     = 5 * time.Minute
	samlClockSkew           = time.Minute
)

var (
	ErrSAMLTransientNameID     = errors.New("service: transient SAML NameID is unsupported")
	ErrSAMLEmailNameIDDisabled = errors.New("service: emailAddress SAML NameID requires provider opt-in")
	ErrSAMLNameIDFormat        = errors.New("service: unsupported SAML NameID format")
	ErrSAMLProviderNotFound    = errors.New("service: no such SAML provider")
	ErrSAMLMetadataExpired     = errors.New("service: SAML provider metadata has expired")
	ErrSAMLResponse            = errors.New("service: SAML response refused")
	ErrSAMLRelayState          = errors.New("service: invalid SAML RelayState")
	ErrSAMLSigningKey          = errors.New("service: SAML request signing key is unavailable")
	ErrSAMLReauthNoPolicy      = errors.New("service: SAML provider has no assurance policy; reauthentication is refused")
	ErrSAMLReauthNoEnvironment = errors.New("service: SAML reauthentication requires an environment_id")
)

type samlAssurancePolicy struct {
	AuthnContextClassRefs []string `json:"authn_context_class_refs"`
}

func evaluateSAMLAssurance(policy *string, contextClassRef *string) (bool, error) {
	if policy == nil || contextClassRef == nil {
		return false, nil
	}
	var parsed samlAssurancePolicy
	if err := json.Unmarshal([]byte(*policy), &parsed); err != nil {
		var refs []string
		if listErr := json.Unmarshal([]byte(*policy), &refs); listErr != nil {
			return false, fmt.Errorf("service: parsing a SAML assurance policy: %w", err)
		}
		parsed.AuthnContextClassRefs = refs
	}
	for _, accepted := range parsed.AuthnContextClassRefs {
		if accepted == *contextClassRef {
			return true, nil
		}
	}
	return false, nil
}

// samlSubject enforces the provider's NameID-format policy, then turns the
// ADR's arbitrary-byte injective encoding into an equally injective text key.
// Base64 is representation only: it performs no normalization and is safe for
// PostgreSQL TEXT (which cannot store the encoding's NUL presence bytes).
func samlSubject(nameID samlsp.NameID, allowEmail bool) (string, error) {
	// SAML Core 2.2.2 treats an omitted Format as unspecified. Accept it under
	// that policy while retaining nil in the injective encoding: the ADR's
	// byte-exact identity rule still distinguishes omitted from explicitly set.
	if nameID.Format != nil {
		switch *nameID.Format {
		case samlNameIDPersistent, samlNameIDUnspecified:
		case samlNameIDTransient:
			return "", ErrSAMLTransientNameID
		case samlNameIDEmail:
			if !allowEmail {
				return "", ErrSAMLEmailNameIDDisabled
			}
		default:
			return "", ErrSAMLNameIDFormat
		}
	}
	encoded, err := samlsp.EncodeNameID(nameID)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(encoded), nil
}

func samlMethod(entityID string) string { return "saml:" + entityID }

func samlFactors(mfa bool) []string {
	if mfa {
		return []string{"saml", "saml-mfa"}
	}
	return []string{"saml"}
}

func samlSPEntityID(origin string) string {
	return strings.TrimRight(origin, "/") + "/api/v1/auth/saml"
}

func samlSPKeyAAD(id string) wencrypto.InstanceFieldAAD {
	return wencrypto.InstanceFieldAAD{OwnerTable: "saml_sp_keys", OwnerRowID: id, FieldTag: "private_key"}
}

// SAMLStartResult carries the redirect and the anonymous-login binding value.
// Transport returns only RedirectURL in JSON and sets InitiatorCookie as the
// path-scoped Secure/HttpOnly/SameSite=None cookie the ACS must present.
type SAMLStartResult struct {
	RedirectURL     string
	InitiatorCookie string
	Purpose         string
}

// samlCeremony owns the provider, transaction and validated claims that form
// one SAML audit identity. A zero value represents a refusal before lookup.
type samlCeremony struct {
	provider    authz.SAMLProvider
	transaction authz.SAMLTransaction
	claims      *samlsp.Claims
}

type samlAuditCause string

const (
	samlCauseRelayState                   samlAuditCause = "relay-state"
	samlCauseConsumedTransaction          samlAuditCause = "consumed-transaction"
	samlCauseExpiredTransaction           samlAuditCause = "expired-transaction"
	samlCauseProviderMixup                samlAuditCause = "provider-mixup"
	samlCauseProviderReconciliation       samlAuditCause = "provider-reconciliation"
	samlCauseMetadataExpired              samlAuditCause = "metadata-expired"
	samlCauseEpoch                        samlAuditCause = "epoch"
	samlCauseInitiatorMismatch            samlAuditCause = "initiator-mismatch"
	samlCausePurposeMismatch              samlAuditCause = "purpose-mismatch"
	samlCauseMalformed                    samlAuditCause = "malformed"
	samlCauseReplayedAssertion            samlAuditCause = "replayed-assertion"
	samlCauseUnknownIdentity              samlAuditCause = "unknown-identity"
	samlCauseAlreadyLinked                samlAuditCause = "already-linked"
	samlCauseNoAssurancePolicy            samlAuditCause = "no-assurance-policy"
	samlCauseStaleAuthnInstant            samlAuditCause = "stale-authn-instant"
	samlCauseDowngrade                    samlAuditCause = "downgrade"
	samlCauseWindowZero                   samlAuditCause = "window-zero"
	samlCauseDuplicateID                  samlAuditCause = "duplicate-id"
	samlCauseEmptyID                      samlAuditCause = "empty-id"
	samlCauseAssertionCardinality         samlAuditCause = "assertion-cardinality"
	samlCauseAssertionPosition            samlAuditCause = "assertion-position"
	samlCauseEncryptedAssertion           samlAuditCause = "encrypted-assertion"
	samlCauseSignatureAlgorithm           samlAuditCause = "signature-algorithm"
	samlCauseDigestAlgorithm              samlAuditCause = "digest-algorithm"
	samlCauseCanonicalizationAlgorithm    samlAuditCause = "canonicalization-algorithm"
	samlCauseTransformAlgorithm           samlAuditCause = "transform-algorithm"
	samlCauseUnknownCertificate           samlAuditCause = "unknown-certificate"
	samlCauseAssertionSignature           samlAuditCause = "assertion-signature"
	samlCauseResponseSignature            samlAuditCause = "response-signature"
	samlCauseSignatureReference           samlAuditCause = "signature-reference"
	samlCauseSignatureStructure           samlAuditCause = "signature-structure"
	samlCauseResponseStatus               samlAuditCause = "response-status"
	samlCauseResponseIssuerMismatch       samlAuditCause = "response-issuer-mismatch"
	samlCauseAssertionIssuerMismatch      samlAuditCause = "assertion-issuer-mismatch"
	samlCauseRequestMismatch              samlAuditCause = "request-mismatch"
	samlCauseDestinationMismatch          samlAuditCause = "destination-mismatch"
	samlCauseAudienceMissing              samlAuditCause = "audience-missing"
	samlCauseAudienceMismatch             samlAuditCause = "audience-mismatch"
	samlCauseAudienceStructure            samlAuditCause = "audience-structure"
	samlCauseSubjectConfirmationMissing   samlAuditCause = "subject-confirmation-missing"
	samlCauseConfirmationMethod           samlAuditCause = "confirmation-method"
	samlCauseConfirmationRecipient        samlAuditCause = "confirmation-recipient"
	samlCauseConfirmationRequestMismatch  samlAuditCause = "confirmation-request-mismatch"
	samlCauseConfirmationExpiryMissing    samlAuditCause = "confirmation-expiry-missing"
	samlCauseConfirmationExpired          samlAuditCause = "confirmation-expired"
	samlCauseSubjectConfirmationStructure samlAuditCause = "subject-confirmation-structure"
	samlCauseConditionsMissing            samlAuditCause = "conditions-missing"
	samlCauseConditionsTooEarly           samlAuditCause = "conditions-too-early"
	samlCauseConditionsExpiryMissing      samlAuditCause = "conditions-expiry-missing"
	samlCauseConditionsExpired            samlAuditCause = "conditions-expired"
	samlCauseConditionsStructure          samlAuditCause = "conditions-structure"
	samlCauseResponseIssueInstant         samlAuditCause = "response-issue-instant"
	samlCauseAssertionIssueInstant        samlAuditCause = "assertion-issue-instant"
	samlCauseIssueInstant                 samlAuditCause = "issue-instant"
	samlCauseAuthnContextCardinality      samlAuditCause = "authn-context-cardinality"
	samlCauseAuthnStatementCardinality    samlAuditCause = "authn-statement-cardinality"
	samlCauseAuthnInstant                 samlAuditCause = "authn-instant"
	samlCauseDTD                          samlAuditCause = "dtd"
	samlCauseDocumentSize                 samlAuditCause = "document-size"
	samlCauseDocumentDepth                samlAuditCause = "document-depth"
	samlCauseDocumentTokenCount           samlAuditCause = "document-token-count"
	samlCauseXMLRoundTrip                 samlAuditCause = "xml-roundtrip"
	samlCauseResponseRoot                 samlAuditCause = "response-root"
	samlCauseTransientNameID              samlAuditCause = "transient-nameid"
	samlCauseEmailNameIDDisabled          samlAuditCause = "email-nameid-disabled"
	samlCauseNameID                       samlAuditCause = "nameid"
)

// SAMLStart creates the SP-initiated request and its durable purpose binding.
func (s *Auth) SAMLStart(ctx context.Context, slug, purpose, environmentID, presented, proof string) (SAMLStartResult, error) {
	release, err := s.Admission.Enter(ctx, audit.FromContext(ctx).SourceIP)
	if err != nil {
		return SAMLStartResult{}, err
	}
	defer release()

	switch purpose {
	case purposeLogin, purposeLink, purposeReauth:
	default:
		return SAMLStartResult{}, ErrBadPurpose
	}
	if purpose == purposeReauth && environmentID == "" {
		return SAMLStartResult{}, ErrSAMLReauthNoEnvironment
	}

	var (
		provider  authz.SAMLProvider
		account   authz.Account
		sessionID string
		epoch     int64
	)
	err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		var readErr error
		if purpose != purposeLogin {
			identity, authErr := az.Authenticate(ctx, presented, s.now())
			if authErr != nil {
				return authErr
			}
			account, readErr = az.AccountByPrincipal(ctx, identity.Principal)
			if readErr != nil {
				return readErr
			}
			sessionID = identity.SessionID
		}
		provider, readErr = az.SAMLProviderBySlug(ctx, slug)
		if errors.Is(readErr, domain.ErrNotFound) || (readErr == nil && !provider.Enabled) {
			return ErrSAMLProviderNotFound
		}
		if readErr != nil {
			return readErr
		}
		if provider.MetadataValidUntil != nil && !s.now().Before(*provider.MetadataValidUntil) {
			return ErrSAMLMetadataExpired
		}
		epoch, readErr = az.CredentialEpoch(ctx)
		return readErr
	})
	if err != nil {
		return SAMLStartResult{}, err
	}
	if purpose == purposeReauth && provider.AssurancePolicy == nil {
		return SAMLStartResult{}, ErrSAMLReauthNoPolicy
	}
	if purpose != purposeLogin && s.Admission.AccountDelay(account.ID) > 0 {
		return SAMLStartResult{}, admission.ErrOverloaded
	}
	if purpose == purposeLink {
		var credential authz.PasswordCredential
		err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			var readErr error
			credential, readErr = az.PasswordCredentialFor(ctx, account.ID)
			if errors.Is(readErr, domain.ErrNotFound) {
				return ErrNoProofCredential
			}
			return readErr
		})
		if err != nil {
			return SAMLStartResult{}, err
		}
		if !s.verifyPassword(ctx, account.ID, credential, proof) {
			s.recordFactorFailure(ctx, account.PrincipalID, account.ID)
			return SAMLStartResult{}, domain.ErrUnauthenticated
		}
		s.Admission.RecordSuccess(account.ID)
	}

	relayState, err := randToken()
	if err != nil {
		return SAMLStartResult{}, err
	}
	initiator, err := randToken()
	if err != nil {
		return SAMLStartResult{}, err
	}
	config := samlsp.AuthnRequestConfig{
		IDPSSOURL: provider.SSORedirectURL, SPEntityID: samlSPEntityID(s.ExternalOrigin),
		ACSURL: provider.ACSURL, RelayState: relayState, ForceAuthn: purpose == purposeReauth,
		Sign: provider.ForceSignRequests || provider.MetadataWantAuthnRequestsSigned, Now: s.now(),
	}
	if config.Sign {
		config.Signer, config.Certificate, err = s.samlSigningMaterial(ctx)
		if err != nil {
			return SAMLStartResult{}, err
		}
	}
	request, err := samlsp.BuildAuthnRequest(config)
	if err != nil {
		return SAMLStartResult{}, err
	}
	transactionID, err := newID("samltx")
	if err != nil {
		return SAMLStartResult{}, err
	}
	transaction := newSAMLTransaction(transactionID, request.RequestID, relayState, initiator,
		provider, purpose, sessionID, account.ID, environmentID, epoch)
	if purpose == purposeLink {
		transaction.CeremonyID, err = newID("cer")
		if err != nil {
			return SAMLStartResult{}, err
		}
	}
	err = tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		transaction.CreatedAt = now
		transaction.ExpiresAt = now.Add(samlTransactionLifetime)
		return az.CreateSAMLTransaction(ctx, transaction)
	})
	if err != nil {
		return SAMLStartResult{}, err
	}
	return SAMLStartResult{RedirectURL: request.URL, InitiatorCookie: initiator, Purpose: purpose}, nil
}

func newSAMLTransaction(id, requestID, relayState, initiator string, provider authz.SAMLProvider,
	purpose, sessionID, accountID, environmentID string, epoch int64,
) authz.NewSAMLTransaction {
	return authz.NewSAMLTransaction{
		ID: id, RequestID: requestID,
		RelayStateVerifier: wencrypto.ArtifactVerifier(relayState),
		InitiatorVerifier:  wencrypto.ArtifactVerifier(initiator),
		ProviderID:         provider.ID, EntityID: provider.EntityID, ACSURL: provider.ACSURL,
		Purpose: purpose, InitiatingSessionID: sessionID, AccountID: accountID,
		EnvironmentID: environmentID, CredentialEpoch: epoch,
	}
}

func (s *Auth) samlSigningMaterial(ctx context.Context) (stdcrypto.Signer, []byte, error) {
	var key authz.SAMLSPKey
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		var readErr error
		key, readErr = az.ActiveSAMLSPKey(ctx)
		return readErr
	})
	if err != nil {
		return nil, nil, ErrSAMLSigningKey
	}
	plain, err := s.Keyring.ForInstance().OpenField(samlSPKeyAAD(key.ID), key.EncryptedPrivateKey)
	if err != nil {
		return nil, nil, ErrSAMLSigningKey
	}
	defer wencrypto.Zero(plain)
	parsed, err := x509.ParsePKCS8PrivateKey(plain)
	if err != nil {
		return nil, nil, ErrSAMLSigningKey
	}
	signer, ok := parsed.(stdcrypto.Signer)
	if !ok {
		return nil, nil, ErrSAMLSigningKey
	}
	return signer, key.CertificateDER, nil
}

// SAMLACS validates one HTTP-POST response and completes the transaction's
// recorded purpose. Transaction consumption, replay insertion and the session
// or reauth write commit together; a concurrent replay can win only once.
func (s *Auth) SAMLACS(ctx context.Context, slug, encodedResponse, relayState, initiatorCookie string) (LoginResult, error) {
	release, err := s.Admission.Enter(ctx, audit.FromContext(ctx).SourceIP)
	if err != nil {
		return LoginResult{}, err
	}
	defer release()
	if !validSAMLHandle(relayState) {
		return LoginResult{}, (samlCeremony{}).refuseCommitted(ctx, s, samlCauseRelayState)
	}
	raw, responseDecodeErr := base64.StdEncoding.DecodeString(encodedResponse)

	attempt, err := writeCommittedSessionAttempt(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, attempt *sessionCompletionAttempt) error {
		now := s.now()
		transaction, lookupErr := az.SAMLTransactionByRelayState(ctx, wencrypto.ArtifactVerifier(relayState))
		if errors.Is(lookupErr, domain.ErrNotFound) {
			return commitSAMLRefusal(attempt, (samlCeremony{}).refuse(ctx, az, samlCauseRelayState))
		}
		if lookupErr != nil {
			return lookupErr
		}
		provider, providerErr := az.SAMLProviderForCallback(ctx, transaction.ProviderID)
		ceremony := samlCeremony{provider: provider, transaction: transaction}
		var cause samlAuditCause
		switch {
		case transaction.Consumed:
			cause = samlCauseConsumedTransaction
		case !now.Before(transaction.ExpiresAt):
			cause = samlCauseExpiredTransaction
		case errors.Is(providerErr, domain.ErrNotFound):
			cause = samlCauseProviderMixup
		case providerErr != nil:
			return providerErr
		case provider.Slug != slug || provider.ID != transaction.ProviderID:
			cause = samlCauseProviderMixup
		case !provider.Enabled || provider.EntityID != transaction.EntityID || provider.ACSURL != transaction.ACSURL:
			cause = samlCauseProviderReconciliation
		case provider.MetadataValidUntil != nil && !now.Before(*provider.MetadataValidUntil):
			cause = samlCauseMetadataExpired
		}
		epoch, epochErr := az.CredentialEpoch(ctx)
		if epochErr != nil {
			return epochErr
		}
		if cause == "" && transaction.CredentialEpoch != epoch {
			cause = samlCauseEpoch
		}

		var initiating authz.Identity
		if cause == "" && (initiatorCookie == "" || !equalVerifier(transaction.InitiatorVerifier, initiatorCookie)) {
			cause = samlCauseInitiatorMismatch
		}
		if cause == "" {
			switch transaction.Purpose {
			case purposeLogin:
			case purposeLink, purposeReauth:
				initiating, lookupErr = az.AuthenticateSessionByID(ctx, transaction.InitiatingSessionID, now)
				if lookupErr != nil {
					cause = samlCauseInitiatorMismatch
				}
			default:
				cause = samlCausePurposeMismatch
			}
		}

		claimed, consumeErr := az.ConsumeSAMLTransaction(ctx, transaction.ID, now)
		if consumeErr != nil {
			return consumeErr
		}
		if !claimed && cause == "" {
			cause = samlCauseConsumedTransaction
		}
		if cause == "" && responseDecodeErr != nil {
			cause = samlCauseMalformed
		}
		if cause != "" {
			return commitSAMLRefusal(attempt, ceremony.refuse(ctx, az, cause))
		}

		certificates, certErr := parseSAMLCertificates(provider.SigningCertificates)
		if certErr != nil {
			return certErr
		}
		claims, validationErr := samlsp.ValidateResponse(raw, certificates, samlsp.ValidationExpectations{
			ProviderEntityID: transaction.EntityID, SPEntityID: samlSPEntityID(s.ExternalOrigin),
			ACSURL: transaction.ACSURL, RequestID: transaction.RequestID, Now: now,
			ClockSkew: samlClockSkew, MaxIssueAge: samlReauthFreshness,
		})
		if validationErr != nil {
			return commitSAMLRefusal(attempt, ceremony.refuse(ctx, az, samlValidationCause(validationErr)))
		}
		ceremony.claims = &claims
		subject, subjectErr := samlSubject(claims.NameID, provider.AllowEmailNameID)
		if subjectErr != nil {
			return commitSAMLRefusal(attempt, ceremony.refuse(ctx, az, samlValidationCause(subjectErr)))
		}
		if _, gcErr := az.DeleteExpiredSAMLReplay(ctx, now); gcErr != nil {
			return gcErr
		}
		replayClaimed, replayErr := az.ClaimSAMLReplay(ctx, authz.NewSAMLReplay{
			Issuer: transaction.EntityID, AssertionID: claims.AssertionID,
			ExpiresAt: claims.Conditions.NotOnOrAfter.Add(samlClockSkew), CreatedAt: now,
		})
		if replayErr != nil {
			return replayErr
		}
		if !replayClaimed {
			return commitSAMLRefusal(attempt, ceremony.refuse(ctx, az, samlCauseReplayedAssertion))
		}
		guarded, guardErr := az.GuardSAMLProviderForMint(ctx, provider.ID, provider.RowVersion, provider.EntityID)
		if guardErr != nil {
			return guardErr
		}
		if !guarded {
			return commitSAMLRefusal(attempt, ceremony.refuse(ctx, az, samlCauseProviderReconciliation))
		}

		var completeErr error
		switch transaction.Purpose {
		case purposeLogin:
			attempt.result, completeErr = s.completeSAMLLogin(ctx, az, ceremony, subject, epoch, now)
		case purposeLink:
			attempt.result, completeErr = s.completeSAMLLink(ctx, az, ceremony, subject, initiating, epoch, now)
		case purposeReauth:
			attempt.result, completeErr = s.completeSAMLReauth(ctx, az, ceremony, subject, initiating, epoch, now)
		}
		if errors.Is(completeErr, domain.ErrUnauthenticated) {
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		if errors.Is(completeErr, ErrReauthWindowClosed) {
			attempt.refused = sessionRefusedWindowClosed
			return nil
		}
		if errors.Is(completeErr, ErrAlreadyLinked) {
			attempt.refused = sessionRefusedAlreadyLinked
			return nil
		}
		return completeErr
	})
	if err != nil {
		return LoginResult{}, err
	}
	if refused := attempt.refused.err(); refused != nil {
		return LoginResult{}, refused
	}
	return attempt.result, nil
}

func validSAMLHandle(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func equalVerifier(want []byte, value string) bool {
	return len(want) != 0 && subtle.ConstantTimeCompare(want, wencrypto.ArtifactVerifier(value)) == 1
}

func parseSAMLCertificates(encoded []byte) ([]*x509.Certificate, error) {
	var ders [][]byte
	if err := json.Unmarshal(encoded, &ders); err != nil {
		return nil, fmt.Errorf("service: parsing SAML certificate set: %w", err)
	}
	if len(ders) == 0 {
		return nil, errors.New("service: SAML certificate set is empty")
	}
	certificates := make([]*x509.Certificate, 0, len(ders))
	for _, der := range ders {
		certificate, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("service: parsing pinned SAML certificate: %w", err)
		}
		certificates = append(certificates, certificate)
	}
	return certificates, nil
}

func (s *Auth) completeSAMLLogin(ctx context.Context, az *authz.TxAuthorizer, ceremony samlCeremony, subject string, epoch int64, now time.Time) (LoginResult, error) {
	provider, transaction, claims := ceremony.provider, ceremony.transaction, *ceremony.claims
	identity, err := az.ExternalIdentityByKey(ctx, SAMLKind, transaction.EntityID, subject)
	if errors.Is(err, domain.ErrNotFound) {
		return LoginResult{}, ceremony.refuse(ctx, az, samlCauseUnknownIdentity)
	}
	if err != nil {
		return LoginResult{}, err
	}
	if identity.CredentialEpoch != epoch {
		return LoginResult{}, ceremony.refuse(ctx, az, samlCauseProviderReconciliation)
	}
	if identity.ProviderID != provider.ID {
		rebound, rebindErr := az.RebindSAMLExternalIdentityProvider(ctx, identity.ID, identity.ProviderID, provider.ID)
		if rebindErr != nil {
			return LoginResult{}, rebindErr
		}
		if !rebound {
			return LoginResult{}, ceremony.refuse(ctx, az, samlCauseProviderReconciliation)
		}
	}
	account, err := az.AccountByID(ctx, identity.AccountID)
	if err != nil {
		return LoginResult{}, err
	}
	mfa, err := evaluateSAMLAssurance(provider.AssurancePolicy, claims.Authn.ContextClassRef)
	if err != nil {
		return LoginResult{}, err
	}
	return s.mintSAMLSession(ctx, az, account, ceremony, mfa, now)
}

func (s *Auth) completeSAMLLink(ctx context.Context, az *authz.TxAuthorizer, ceremony samlCeremony, subject string, initiating authz.Identity, epoch int64, now time.Time) (LoginResult, error) {
	provider, transaction := ceremony.provider, ceremony.transaction
	if _, err := az.ExternalIdentityByKey(ctx, SAMLKind, transaction.EntityID, subject); err == nil {
		if auditErr := ceremony.stage(ctx, az, "", audit.OutcomeFailure, samlCauseAlreadyLinked); auditErr != nil {
			return LoginResult{}, auditErr
		}
		return LoginResult{}, ErrAlreadyLinked
	} else if !errors.Is(err, domain.ErrNotFound) {
		return LoginResult{}, err
	}
	account, err := az.AccountByID(ctx, transaction.AccountID)
	if err != nil || initiating.Principal != account.PrincipalID {
		if err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, ceremony.refuse(ctx, az, samlCauseInitiatorMismatch)
	}
	identityID, err := newID("eid")
	if err != nil {
		return LoginResult{}, err
	}
	if err := az.CreateExternalIdentity(ctx, authz.NewExternalIdentity{
		ID: identityID, AccountID: account.ID, Kind: SAMLKind, Issuer: transaction.EntityID,
		Subject: subject, ProviderID: provider.ID, CredentialEpoch: epoch, CreatedAt: now,
	}); err != nil {
		return LoginResult{}, err
	}
	result, err := s.reissueSession(ctx, az, account, "password", MethodLocalPassword, Artifact(initiating.Artifact), now)
	if err != nil {
		return LoginResult{}, err
	}
	event, err := newAuditEvent(ctx, audit.EventIdentityLinked, account.PrincipalID,
		audit.Object{Type: "external_identity", ID: identityID}, audit.OutcomeSuccess, "",
		audit.Payload{"kind": SAMLKind, "account_id": account.ID, "identity_id": identityID, "provider_id": provider.ID, "authorizing_credential": "password"})
	if err != nil {
		return LoginResult{}, err
	}
	if err := az.RecordAuthEvent(ctx, event); err != nil {
		return LoginResult{}, err
	}
	if err := ceremony.stage(ctx, az, account.PrincipalID, audit.OutcomeSuccess, ""); err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

func (s *Auth) completeSAMLReauth(ctx context.Context, az *authz.TxAuthorizer, ceremony samlCeremony, subject string, initiating authz.Identity, epoch int64, now time.Time) (LoginResult, error) {
	provider, transaction, claims := ceremony.provider, ceremony.transaction, *ceremony.claims
	if provider.AssurancePolicy == nil || claims.Authn.ContextClassRef == nil {
		return LoginResult{}, ceremony.refuse(ctx, az, samlCauseNoAssurancePolicy)
	}
	if claims.Authn.Instant.After(now.Add(samlClockSkew)) || now.Sub(claims.Authn.Instant) > samlReauthFreshness+samlClockSkew {
		return LoginResult{}, ceremony.refuse(ctx, az, samlCauseStaleAuthnInstant)
	}
	mfa, err := evaluateSAMLAssurance(provider.AssurancePolicy, claims.Authn.ContextClassRef)
	if err != nil {
		return LoginResult{}, err
	}
	if !mfa {
		return LoginResult{}, ceremony.refuse(ctx, az, samlCauseNoAssurancePolicy)
	}
	identity, err := az.ExternalIdentityByKey(ctx, SAMLKind, transaction.EntityID, subject)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && identity.AccountID != transaction.AccountID) {
		return LoginResult{}, ceremony.refuse(ctx, az, samlCauseUnknownIdentity)
	}
	if err != nil {
		return LoginResult{}, err
	}
	if identity.CredentialEpoch != epoch {
		return LoginResult{}, ceremony.refuse(ctx, az, samlCauseProviderReconciliation)
	}
	if identity.ProviderID != provider.ID {
		rebound, rebindErr := az.RebindSAMLExternalIdentityProvider(ctx, identity.ID, identity.ProviderID, provider.ID)
		if rebindErr != nil {
			return LoginResult{}, rebindErr
		}
		if !rebound {
			return LoginResult{}, ceremony.refuse(ctx, az, samlCauseProviderReconciliation)
		}
	}
	if initiating.SessionID != transaction.InitiatingSessionID {
		return LoginResult{}, ceremony.refuse(ctx, az, samlCauseInitiatorMismatch)
	}
	evidence := authz.Assurance{Factors: samlFactors(true)}
	if authz.AssuranceRank(initiating.Assurance) > authz.AssuranceRank(evidence) {
		return LoginResult{}, ceremony.refuse(ctx, az, samlCauseDowngrade)
	}
	effectiveWindow, err := s.effectiveReauthWindow(ctx, az, transaction.EnvironmentID)
	if err != nil {
		return LoginResult{}, err
	}
	if effectiveWindow <= 0 {
		if err := ceremony.stage(ctx, az, "", audit.OutcomeFailure, samlCauseWindowZero); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrReauthWindowClosed
	}
	account, err := az.AccountByID(ctx, identity.AccountID)
	if err != nil {
		return LoginResult{}, err
	}
	completion, err := s.completeSession(ctx, az, RotateSession{
		session: initiating, account: account, factors: initiating.Assurance.Factors,
	}, now)
	if err != nil {
		return LoginResult{}, err
	}
	windowID, err := newID("raw")
	if err != nil {
		return LoginResult{}, err
	}
	hardCap := s.ReauthHardCap
	if hardCap <= 0 {
		hardCap = effectiveWindow
	}
	hardExpires := now.Add(hardCap)
	windowExpires := now.Add(effectiveWindow)
	if windowExpires.After(hardExpires) {
		windowExpires = hardExpires
	}
	if err := az.OpenReauthWindow(ctx, authz.NewReauthWindow{
		ID: windowID, SessionID: initiating.SessionID, EnvironmentID: transaction.EnvironmentID,
		CeremonyID: transaction.ID, FactorClass: SAMLKind, AuthenticatedAt: claims.Authn.Instant,
		WindowExpiresAt: windowExpires, HardExpiresAt: hardExpires, CredentialEpoch: epoch, CreatedAt: now,
	}); err != nil {
		return LoginResult{}, err
	}
	event, err := newAuditEvent(ctx, audit.EventAuthReauthenticated, initiating.Principal,
		audit.Object{Type: "session", ID: initiating.SessionID}, audit.OutcomeSuccess, "",
		audit.Payload{"session_id": initiating.SessionID, "factor": SAMLKind})
	if err != nil {
		return LoginResult{}, err
	}
	if err := az.RecordAuthEvent(ctx, event); err != nil {
		return LoginResult{}, err
	}
	if err := ceremony.stage(ctx, az, initiating.Principal, audit.OutcomeSuccess, ""); err != nil {
		return LoginResult{}, err
	}
	return completion, nil
}

func (s *Auth) mintSAMLSession(ctx context.Context, az *authz.TxAuthorizer, account authz.Account, ceremony samlCeremony, mfa bool, now time.Time) (LoginResult, error) {
	provider, transaction, claims := ceremony.provider, ceremony.transaction, *ceremony.claims
	factorClasses := samlFactors(mfa)
	result, err := s.completeSession(ctx, az, CreateSession{
		account: account, artifact: ArtifactBrowser,
		assurance: Assurance{
			Method: samlMethod(transaction.EntityID), Factors: factorClasses,
			AuthenticatedAt: claims.Authn.Instant, CeremonyID: transaction.ID,
		},
		csrf: sessionWithCSRF,
	}, now)
	if err != nil {
		return LoginResult{}, err
	}
	bound, err := az.BindSessionToSAMLProvider(ctx, result.SessionID, provider.ID)
	if err != nil {
		return LoginResult{}, err
	}
	if !bound {
		return LoginResult{}, errors.New("service: failed to bind SAML session provider")
	}
	assuranceLabel := "single-factor"
	if mfa {
		assuranceLabel = "multi-factor"
	}
	if err := ceremony.stage(ctx, az, account.PrincipalID, audit.OutcomeSuccess, ""); err != nil {
		return LoginResult{}, err
	}
	event, err := newAuditEvent(ctx, audit.EventAuthSessionCreated, account.PrincipalID,
		audit.Object{Type: "session", ID: result.SessionID}, audit.OutcomeSuccess, "",
		audit.Payload{"session_id": result.SessionID, "artifact": ArtifactBrowser.String(), "method": samlMethod(transaction.EntityID), "assurance": assuranceLabel})
	if err != nil {
		return LoginResult{}, err
	}
	if err := az.RecordAuthEvent(ctx, event); err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

func (ceremony samlCeremony) stage(ctx context.Context, az *authz.TxAuthorizer, principal domain.PrincipalID, outcome audit.Outcome, cause samlAuditCause) error {
	eventType, object, payload := ceremony.auditDetails(outcome, cause)
	event, err := newAuditEvent(ctx, eventType, principal, object, outcome, "", payload)
	if err != nil {
		return err
	}
	return az.RecordAuthEvent(ctx, event)
}

func (ceremony samlCeremony) auditDetails(outcome audit.Outcome, cause samlAuditCause) (audit.EventType, audit.Object, audit.Payload) {
	eventType := audit.EventSAMLLogin
	if ceremony.transaction.Purpose == purposeReauth {
		eventType = audit.EventSAMLReauth
	}
	payload := audit.Payload{
		"provider_id": ceremony.provider.ID, "entity_id": ceremony.transaction.EntityID,
		"purpose": ceremony.transaction.Purpose, "transaction_id": ceremony.transaction.ID,
	}
	if outcome == audit.OutcomeFailure {
		payload["cause"] = string(cause)
	}
	if ceremony.claims != nil {
		if ceremony.claims.ExpiredPinnedCertificate {
			payload["pinned_certificate_expired"] = true
		}
		if ceremony.claims.NameID.Format != nil {
			payload["name_id_format"] = *ceremony.claims.NameID.Format
		}
		if ceremony.claims.Authn.ContextClassRef != nil {
			payload["authn_context_class_ref"] = *ceremony.claims.Authn.ContextClassRef
		}
	}
	return eventType, audit.Object{Type: "saml_transaction", ID: ceremony.transaction.ID}, payload
}

func (ceremony samlCeremony) refuse(ctx context.Context, az *authz.TxAuthorizer, cause samlAuditCause) error {
	if err := ceremony.stage(ctx, az, "", audit.OutcomeFailure, cause); err != nil {
		return err
	}
	return domain.ErrUnauthenticated
}

func (ceremony samlCeremony) refuseCommitted(ctx context.Context, s *Auth, cause samlAuditCause) error {
	if err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return ceremony.stage(ctx, az, "", audit.OutcomeFailure, cause)
	}); err != nil {
		return err
	}
	return domain.ErrUnauthenticated
}

func commitSAMLRefusal(attempt *sessionCompletionAttempt, err error) error {
	if !errors.Is(err, domain.ErrUnauthenticated) {
		return err
	}
	attempt.refused = sessionRefusedUnauthenticated
	return nil
}

func samlValidationCause(err error) samlAuditCause {
	switch {
	case errors.Is(err, samlsp.ErrDuplicateID):
		return samlCauseDuplicateID
	case errors.Is(err, samlsp.ErrEmptyID):
		return samlCauseEmptyID
	case errors.Is(err, samlsp.ErrAssertionCardinality):
		return samlCauseAssertionCardinality
	case errors.Is(err, samlsp.ErrAssertionPosition):
		return samlCauseAssertionPosition
	case errors.Is(err, samlsp.ErrEncryptedAssertion):
		return samlCauseEncryptedAssertion
	case errors.Is(err, samlsp.ErrSignatureAlgorithm):
		return samlCauseSignatureAlgorithm
	case errors.Is(err, samlsp.ErrDigestAlgorithm):
		return samlCauseDigestAlgorithm
	case errors.Is(err, samlsp.ErrCanonicalizationAlgorithm):
		return samlCauseCanonicalizationAlgorithm
	case errors.Is(err, samlsp.ErrTransformAlgorithm):
		return samlCauseTransformAlgorithm
	case errors.Is(err, samlsp.ErrNoPinnedCertificate):
		return samlCauseUnknownCertificate
	case errors.Is(err, samlsp.ErrAssertionSignature):
		return samlCauseAssertionSignature
	case errors.Is(err, samlsp.ErrResponseSignature):
		return samlCauseResponseSignature
	case errors.Is(err, samlsp.ErrSignatureReference):
		return samlCauseSignatureReference
	case errors.Is(err, samlsp.ErrSignatureStructure):
		return samlCauseSignatureStructure
	case errors.Is(err, samlsp.ErrResponseStatus):
		return samlCauseResponseStatus
	case errors.Is(err, samlsp.ErrResponseIssuer):
		return samlCauseResponseIssuerMismatch
	case errors.Is(err, samlsp.ErrAssertionIssuer):
		return samlCauseAssertionIssuerMismatch
	case errors.Is(err, samlsp.ErrInResponseTo):
		return samlCauseRequestMismatch
	case errors.Is(err, samlsp.ErrDestination):
		return samlCauseDestinationMismatch
	case errors.Is(err, samlsp.ErrAudienceMissing):
		return samlCauseAudienceMissing
	case errors.Is(err, samlsp.ErrAudienceMismatch):
		return samlCauseAudienceMismatch
	case errors.Is(err, samlsp.ErrAudience):
		return samlCauseAudienceStructure
	case errors.Is(err, samlsp.ErrSubjectConfirmationMissing):
		return samlCauseSubjectConfirmationMissing
	case errors.Is(err, samlsp.ErrSubjectConfirmationMethod):
		return samlCauseConfirmationMethod
	case errors.Is(err, samlsp.ErrSubjectConfirmationRecipient):
		return samlCauseConfirmationRecipient
	case errors.Is(err, samlsp.ErrSubjectConfirmationInResponseTo):
		return samlCauseConfirmationRequestMismatch
	case errors.Is(err, samlsp.ErrSubjectConfirmationExpiryMissing):
		return samlCauseConfirmationExpiryMissing
	case errors.Is(err, samlsp.ErrSubjectConfirmationExpired):
		return samlCauseConfirmationExpired
	case errors.Is(err, samlsp.ErrSubjectConfirmation):
		return samlCauseSubjectConfirmationStructure
	case errors.Is(err, samlsp.ErrConditionsMissing):
		return samlCauseConditionsMissing
	case errors.Is(err, samlsp.ErrConditionsNotBefore):
		return samlCauseConditionsTooEarly
	case errors.Is(err, samlsp.ErrConditionsExpiryMissing):
		return samlCauseConditionsExpiryMissing
	case errors.Is(err, samlsp.ErrConditionsExpired):
		return samlCauseConditionsExpired
	case errors.Is(err, samlsp.ErrConditions):
		return samlCauseConditionsStructure
	case errors.Is(err, samlsp.ErrResponseIssueInstant):
		return samlCauseResponseIssueInstant
	case errors.Is(err, samlsp.ErrAssertionIssueInstant):
		return samlCauseAssertionIssueInstant
	case errors.Is(err, samlsp.ErrIssueInstant):
		return samlCauseIssueInstant
	case errors.Is(err, samlsp.ErrAuthnStatementCardinality), errors.Is(err, samlsp.ErrAuthnContextCardinality):
		if errors.Is(err, samlsp.ErrAuthnContextCardinality) {
			return samlCauseAuthnContextCardinality
		}
		return samlCauseAuthnStatementCardinality
	case errors.Is(err, samlsp.ErrInvalidAuthnInstant):
		return samlCauseAuthnInstant
	case errors.Is(err, samlsp.ErrDTD):
		return samlCauseDTD
	case errors.Is(err, samlsp.ErrDocumentTooLarge):
		return samlCauseDocumentSize
	case errors.Is(err, samlsp.ErrDocumentTooDeep):
		return samlCauseDocumentDepth
	case errors.Is(err, samlsp.ErrTooManyTokens):
		return samlCauseDocumentTokenCount
	case errors.Is(err, samlsp.ErrRoundTrip):
		return samlCauseXMLRoundTrip
	case errors.Is(err, samlsp.ErrResponseRoot):
		return samlCauseResponseRoot
	case errors.Is(err, ErrSAMLTransientNameID):
		return samlCauseTransientNameID
	case errors.Is(err, ErrSAMLEmailNameIDDisabled):
		return samlCauseEmailNameIDDisabled
	case errors.Is(err, ErrSAMLNameIDFormat), errors.Is(err, samlsp.ErrEmptyNameID), errors.Is(err, samlsp.ErrNameIDCardinality):
		return samlCauseNameID
	default:
		return samlCauseMalformed
	}
}
