package service

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/webauthnrp"
)

// Login challenge (#760). A browser password login on an account with an
// enrolled factor mints no session: it issues a single-use, expiring challenge
// (localLogin 202), and the second factor is presented against it before any
// cookie is set. The factor that stands is never skippable — the challenge path
// does not consult the instance policy. The finish ops mint the browser session
// with the presented factor recorded in the assurance.
//
// The challenge is a random, single-use, expiring, account-bound continuation
// token — not a credential, so it stores no epoch; a restore-superseded FACTOR
// is refused at finish by the factor's own live-epoch check. A missing, expired
// or consumed challenge answers domain.ErrNotFound (404); a
// wrong code answers domain.ErrUnauthenticated (401); a replayed TOTP step
// answers the loud already-used conflict (409); the factor budget answers 429.

// LoginChallengeLifetime bounds a challenge's life, mirroring the WebAuthn
// ceremony: a challenge not finished inside it is inert.
const LoginChallengeLifetime = 5 * time.Minute

// enrolledFactors reports the factor classes an account can satisfy a login
// challenge with, in offer order (totp then webauthn). A confirmed TOTP factor
// yields "totp"; at least one enabled WebAuthn credential yields "webauthn". A
// clone-disabled credential is excluded (mirror rpCredentials), so an account
// whose only passkey is disabled reads as unenrolled rather than being offered a
// factor no ceremony could satisfy.
//
// A factor is only counted at the LIVE credential epoch: a restore bumps the
// epoch and renders every prior-epoch MFA seed inert (human-auth ADR § Restore),
// so a superseded TOTP seed or passkey must NOT read as a standing factor — else
// it would both mint an ungated session on a `required` instance and be
// presentable against a challenge. This mirrors the epoch check the webauthn
// finish already applies to the credential it verifies.
func (s *Auth) enrolledFactors(ctx context.Context, az *authz.TxAuthorizer, accountID string) ([]string, error) {
	epoch, err := az.CredentialEpoch(ctx)
	if err != nil {
		return nil, err
	}
	var factors []string
	if confirmed, err := az.ConfirmedTOTP(ctx, accountID); err == nil {
		if confirmed.CredentialEpoch == epoch {
			factors = append(factors, "totp")
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	creds, err := az.WebAuthnCredentialsForAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, c := range creds {
		if !c.Disabled && c.CredentialEpoch == epoch {
			factors = append(factors, "webauthn")
			break
		}
	}
	return factors, nil
}

// computeEnrolmentRequired decides the session's enrolment gate from live state:
// a local-password session on a `required` instance whose account holds no
// factor is gated (#760). Any other method, or a factor already standing, reads
// as false. It is evaluated at every mint so a new credential can never mint
// itself an ungated session.
func (s *Auth) computeEnrolmentRequired(ctx context.Context, az *authz.TxAuthorizer, method, accountID string) (bool, error) {
	if method != MethodLocalPassword || !s.secondFactorRequired() {
		return false, nil
	}
	factors, err := s.enrolledFactors(ctx, az, accountID)
	if err != nil {
		return false, err
	}
	return len(factors) == 0, nil
}

// issueLoginChallenge writes a single-use, expiring challenge and records that
// the password step passed for this account. The audit event is the login event
// carrying the challenge id; the session it will mint is recorded by its own
// session_created event on the finish op.
func (s *Auth) issueLoginChallenge(ctx context.Context, az *authz.TxAuthorizer, account authz.Account, factors []string, now time.Time) (*LoginChallengeIssued, error) {
	challengeID, err := newID("lch")
	if err != nil {
		return nil, err
	}
	factorsJSON, err := json.Marshal(factors)
	if err != nil {
		return nil, err
	}
	expires := now.Add(LoginChallengeLifetime)
	if err := az.CreateLoginChallenge(ctx, authz.NewLoginChallenge{
		ID: challengeID, AccountID: account.ID,
		Factors: string(factorsJSON), ExpiresAt: expires, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	e, err := newAuditEvent(ctx, audit.EventAuthLogin, account.PrincipalID,
		audit.Object{Type: "account", ID: account.ID}, audit.OutcomeSuccess, "",
		// The presence of `challenge_id` (and Object type "account", not "session")
		// is what distinguishes this challenge-issuance login event from the
		// session-minting login the finish op records; both are EventAuthLogin.
		audit.Payload{
			"method": MethodLocalPassword, "artifact": ArtifactBrowser.String(),
			"subject_resolved": true, "account_id": account.ID,
			"assurance": "single-factor", "challenge_id": challengeID,
		})
	if err != nil {
		return nil, err
	}
	if err := az.RecordAuthEvent(ctx, e); err != nil {
		return nil, err
	}
	return &LoginChallengeIssued{ID: challengeID, ExpiresAt: expires, Factors: factors}, nil
}

// liveChallenge refuses a challenge that is consumed, expired, or does not offer
// the factor the caller is presenting — all with the uniform domain.ErrNotFound
// (404), so presentation reveals nothing beyond "no such live challenge". Deny
// by default: a challenge offering only webauthn cannot be satisfied on the totp
// endpoint. Restore safety is enforced on the FACTOR at finish (its live-epoch
// check), not on this ephemeral token, which stores no epoch.
func (s *Auth) liveChallenge(ctx context.Context, az *authz.TxAuthorizer, c authz.LoginChallenge, factor string) error {
	if c.Consumed || !s.now().Before(c.ExpiresAt) {
		return domain.ErrNotFound
	}
	if !challengeOffers(c.Factors, factor) {
		return domain.ErrNotFound
	}
	return nil
}

// challengeOffers reports whether a challenge's stored factor list names factor.
// An unparseable list reads as no offer (fail closed).
func challengeOffers(factorsJSON, factor string) bool {
	var factors []string
	if json.Unmarshal([]byte(factorsJSON), &factors) != nil {
		return false
	}
	return slices.Contains(factors, factor)
}

// LoginChallengeTOTP satisfies a login challenge with a TOTP code and mints a
// browser session recording [password, totp]. It mirrors StepUpTOTP's verify
// discipline (admission budget, sealed-seed validation, single-use step CAS) but
// mints a new session instead of rotating one, and additionally consumes the
// challenge single-use. A wrong code leaves the challenge usable for a retry; a
// replayed step is the loud already-used conflict.
func (s *Auth) LoginChallengeTOTP(ctx context.Context, challengeID, code string) (LoginResult, error) {
	// Phase 1 — resolve the challenge, its account, and the confirmed factor.
	var (
		account   authz.Account
		confirmed authz.TOTPCredential
	)
	err := tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		challenge, err := az.LoginChallengeByID(ctx, challengeID)
		if err != nil {
			return err
		}
		if err := s.liveChallenge(ctx, az, challenge, "totp"); err != nil {
			return err
		}
		account, err = az.AccountByID(ctx, challenge.AccountID)
		if err != nil {
			return err
		}
		confirmed, err = az.ConfirmedTOTP(ctx, account.ID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrNotFound
		}
		if err != nil {
			return err
		}
		// The confirmed factor must stand at the LIVE epoch: a restore renders a
		// prior-epoch seed inert (human-auth ADR § Restore). Uniform 404 so a
		// superseded seed is indistinguishable from no factor.
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if confirmed.CredentialEpoch != epoch {
			return domain.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return LoginResult{}, err
	}

	// Admission: bound online brute force of the six-digit code, keyed on the
	// account resolved from the challenge.
	release, err := s.enterFactorBudget(ctx, account.ID)
	if err != nil {
		return LoginResult{}, err
	}
	defer release()

	// Phase 2 — verify the code against the sealed seed, outside any transaction.
	seed, err := s.Keyring.ForInstance().OpenField(totpSeedAAD(confirmed.ID), confirmed.Seed)
	if err != nil {
		s.logFault(ctx, "opening a TOTP seed failed", err, account.ID)
		return LoginResult{}, domain.ErrUnauthenticated
	}
	step, ok := crypto.ValidateTOTP(seed, code, s.now(), crypto.TOTPSkewSteps)
	crypto.Zero(seed)
	if !ok {
		s.recordFactorFailure(ctx, account.PrincipalID, account.ID)
		return LoginResult{}, domain.ErrUnauthenticated
	}
	s.Admission.RecordSuccess(account.ID)

	// Phase 3 — consume the step and the challenge, then mint a browser session.
	result, err := writeCommittedLoginResult(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, result *LoginResult) error {
		now := s.now()
		challenge, err := az.LoginChallengeByID(ctx, challengeID)
		if err != nil {
			return err
		}
		// Re-check against the write-tx clock/epoch: a request delayed past the
		// window or an epoch bump between the phases must not still mint.
		if err := s.liveChallenge(ctx, az, challenge, "totp"); err != nil {
			return err
		}
		// CAS on the row whose seed was VERIFIED in phase 1, not a freshly read
		// one, so a code proved against a since-removed-and-replaced factor cannot
		// be applied to its successor (finding HIGH-5).
		consumed, err := az.AdvanceTOTPStep(ctx, confirmed.ID, confirmed.RowVersion, step)
		if err != nil {
			return err
		}
		if !consumed {
			if s.totpStepConsumed(ctx, az, account.ID, confirmed.ID, step) {
				return totpStepAlreadyUsed()
			}
			return domain.ErrUnauthenticated
		}
		// Consume the challenge atomically: a losing claim fails closed.
		claimed, err := az.ConsumeLoginChallenge(ctx, challengeID, now)
		if err != nil {
			return err
		}
		if !claimed {
			return domain.ErrNotFound
		}
		*result, err = s.mintSession(ctx, az, account, ArtifactBrowser, []string{"password", "totp"}, "", now)
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}
	return result, nil
}

// LoginChallengeWebauthnStart opens a WebAuthn assertion ceremony scoped to the
// challenge's account, so the passkey is bound to THAT account rather than being
// discoverable. It carries the `login-2fa` purpose (the `login` purpose already
// means discoverable login) and is session-less. The challenge is not consumed
// here — only the finish op consumes it.
func (s *Auth) LoginChallengeWebauthnStart(ctx context.Context, challengeID string) ([]byte, error) {
	if err := s.requireRP(); err != nil {
		return nil, err
	}
	// Bound ceremony creation by the per-IP + instance-wide budget, as
	// PasskeyLoginStart does: this is a pre-authentication write path reachable by
	// anyone holding a live challenge id, so an unbudgeted start would let a
	// password holder flood webauthn_ceremonies for the challenge's lifetime. The
	// per-IP budget (not per-account backoff) is used deliberately so a start
	// cannot be abused to lock a victim's account out of its own login.
	release, err := s.Admission.Enter(ctx, audit.FromContext(ctx).SourceIP)
	if err != nil {
		return nil, err
	}
	defer release()
	var options []byte
	err = tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		challenge, err := az.LoginChallengeByID(ctx, challengeID)
		if err != nil {
			return err
		}
		if err := s.liveChallenge(ctx, az, challenge, "webauthn"); err != nil {
			return err
		}
		account, err := az.AccountByID(ctx, challenge.AccountID)
		if err != nil {
			return err
		}
		creds, err := az.WebAuthnCredentialsForAccount(ctx, account.ID)
		if err != nil {
			return err
		}
		handle, err := az.WebAuthnUserHandle(ctx, account.ID)
		if err != nil {
			return err
		}
		user := rpUser(handle, account, creds)
		if len(user.Credentials) == 0 {
			return domain.ErrNotFound
		}
		opts, sessionData, ceremonyChallenge, err := s.WebAuthn.BeginLogin(user)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		ceremonyID, err := newID("wac")
		if err != nil {
			return err
		}
		if err := az.CreateWebAuthnCeremony(ctx, authz.NewWebAuthnCeremony{
			ID: ceremonyID, ChallengeVerifier: challengeVerifier(ceremonyChallenge), SessionData: sessionData,
			AccountID: account.ID, Purpose: "login-2fa", CredentialEpoch: epoch,
			ExpiresAt: now.Add(WebAuthnCeremonyLifetime), CreatedAt: now,
		}); err != nil {
			return err
		}
		options = opts
		return nil
	})
	if err != nil {
		return nil, err
	}
	return options, nil
}

// LoginChallengeWebauthnFinish verifies an account-bound assertion against a live
// login challenge and mints a browser session recording [password, webauthn]. It
// mirrors PasskeyLoginFinish's sign-count/clone rule, but the account is KNOWN
// from the challenge (the assertion is bound to that account, not discoverable),
// and it consumes the login challenge single-use in the same write transaction.
func (s *Auth) LoginChallengeWebauthnFinish(ctx context.Context, challengeID string, responseJSON []byte) (LoginResult, error) {
	if err := s.requireRP(); err != nil {
		return LoginResult{}, err
	}
	assertionChallenge, err := webauthnrp.ChallengeFromAssertion(responseJSON)
	if err != nil {
		return LoginResult{}, domain.ErrUnauthenticated
	}

	// Phase 1 — resolve the login challenge, its account, and the ceremony.
	var (
		account  authz.Account
		ceremony authz.WebAuthnCeremony
		creds    []authz.WebAuthnCredential
		handle   []byte
	)
	err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		challenge, err := az.LoginChallengeByID(ctx, challengeID)
		if err != nil {
			return err
		}
		if err := s.liveChallenge(ctx, az, challenge, "webauthn"); err != nil {
			return err
		}
		account, err = az.AccountByID(ctx, challenge.AccountID)
		if err != nil {
			return err
		}
		ceremony, err = az.WebAuthnCeremonyByChallenge(ctx, challengeVerifier(assertionChallenge))
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrUnauthenticated
		}
		if err != nil {
			return err
		}
		creds, err = az.WebAuthnCredentialsForAccount(ctx, account.ID)
		if err != nil {
			return err
		}
		handle, err = az.WebAuthnUserHandle(ctx, account.ID)
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}
	if !validCeremony(ceremony, "login-2fa", account.ID, "", "", s.now()) {
		return LoginResult{}, domain.ErrUnauthenticated
	}

	// Admission: the account is known up front, so enter the budget before the
	// signature verification, exactly as the account-bound assertion paths do.
	release, err := s.enterFactorBudget(ctx, account.ID)
	if err != nil {
		return LoginResult{}, err
	}
	defer release()

	assertion, err := s.WebAuthn.FinishLogin(rpUser(handle, account, creds), ceremony.SessionData, responseJSON)
	if err != nil {
		s.recordFactorFailure(ctx, account.PrincipalID, account.ID)
		return LoginResult{}, domain.ErrUnauthenticated
	}

	// Phase 3 — apply the sign-count rule, consume the ceremony and the challenge,
	// then mint the browser session, atomically.
	attempt, err := writeCommittedSessionAttempt(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer, attempt *sessionCompletionAttempt) error {
		now := s.now()
		challenge, err := az.LoginChallengeByID(ctx, challengeID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				attempt.refused = sessionRefusedUnauthenticated
				return nil
			}
			return err
		}
		if err := s.liveChallenge(ctx, az, challenge, "webauthn"); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				attempt.refused = sessionRefusedUnauthenticated
				return nil
			}
			return err
		}
		// Reload + re-validate the ceremony against the tx clock/epoch (A3). The
		// reloaded row is what is consumed.
		fresh, err := az.WebAuthnCeremonyByChallenge(ctx, challengeVerifier(assertionChallenge))
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				attempt.refused = sessionRefusedUnauthenticated
				return nil
			}
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if !validCeremony(fresh, "login-2fa", account.ID, "", "", now) || fresh.CredentialEpoch != epoch {
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		stored, err := az.WebAuthnCredentialByCredentialID(ctx, assertion.CredentialID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				attempt.refused = sessionRefusedUnauthenticated
				return nil
			}
			return err
		}
		if stored.AccountID != account.ID || stored.Disabled || stored.CredentialEpoch != epoch {
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		consumed, err := az.ConsumeWebAuthnCeremony(ctx, fresh.ID, stored.ID, now)
		if err != nil {
			return err
		}
		if !consumed {
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		if s.isClone(stored, assertion.SignCount) {
			if err := s.respondToClone(ctx, az, account, stored, now); err != nil {
				return err
			}
			s.Admission.RecordFailure(account.ID)
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		advanced, err := az.AdvanceWebAuthnSignCount(ctx, stored.ID, stored.RowVersion, int64(assertion.SignCount), now)
		if err != nil {
			return err
		}
		if !advanced {
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		// Consume the login challenge atomically: a losing claim fails closed.
		claimed, err := az.ConsumeLoginChallenge(ctx, challengeID, now)
		if err != nil {
			return err
		}
		if !claimed {
			attempt.refused = sessionRefusedUnauthenticated
			return nil
		}
		// Mint against the RELOADED ceremony id (A3): it is what was consumed, so
		// the minted session's ceremony_id traces the credential that authored it
		// for a later clone sweep.
		attempt.result, err = s.mintSession(ctx, az, account, ArtifactBrowser, []string{"password", "webauthn"}, fresh.ID, now)
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}
	if refused := attempt.refusal(); refused != nil {
		return LoginResult{}, refused
	}
	s.Admission.RecordSuccess(account.ID)
	return attempt.result, nil
}
