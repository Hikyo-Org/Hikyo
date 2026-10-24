package service

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

const CLILoginLifetime = 10 * time.Minute
const CLIDeviceLifetime = 15 * time.Minute
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var ErrCLILoginInvalid = fmt.Errorf("%w: invalid or spent CLI login transaction", domain.ErrUnauthorized)

// A successful password proof may disclose the remedy, but may never mint a
// password-only CLI session for an enrolled account. Keep the existing browser
// challenge artifact fixed instead of letting a finish request choose it.
type cliLoginFactorRequired struct{}

func (e cliLoginFactorRequired) Error() string { return "service: " + e.SafeDetail() }
func (cliLoginFactorRequired) Unwrap() error   { return domain.ErrInvalid }
func (cliLoginFactorRequired) SafeDetail() string {
	return "an enrolled factor is required; use hikyo login <instance-url> or hikyo login <instance-url> --device and complete browser sign-in"
}

type cliLoginBrowserFactorRequired struct{}

func (e cliLoginBrowserFactorRequired) Error() string { return "service: " + e.SafeDetail() }
func (cliLoginBrowserFactorRequired) Unwrap() error   { return domain.ErrInvalid }
func (cliLoginBrowserFactorRequired) SafeDetail() string {
	return "Complete your enrolled authenticator or passkey before authorizing the CLI. Sign out and sign in again."
}

// requireCLILoginAssurance enforces the local password sign-in rule against
// live enrollment. Provider methods retain their own assurance contract.
// Call under the principal lock at approval AND mint, because enrollment may
// change between them. Operation-specific fresh proof never crosses this join.
func (s *Auth) requireCLILoginAssurance(ctx context.Context, az *authz.TxAuthorizer, accountID string, assurance authz.Assurance) error {
	if assurance.Method != MethodLocalPassword || authz.AdequateAssurance(assurance) {
		return nil
	}
	factors, err := s.enrolledFactors(ctx, az, accountID)
	if err != nil {
		return err
	}
	if len(factors) > 0 {
		return ErrCLILoginInvalid
	}
	return nil
}

type CLILoginStart struct {
	State, DeviceCode, UserCode, VerificationURI string
	ExpiresAt                                    time.Time
	Interval                                     int
}
type CLILoginTransaction struct {
	Reference, Transport, RedirectURI, Instance string
	ExpiresAt                                   time.Time
}
type cliLoginSnapshot struct {
	Assurance        Assurance `json:"assurance"`
	ProviderID       string    `json:"provider_id"`
	OAuth2ProviderID string    `json:"oauth2_provider_id"`
	SAMLProviderID   string    `json:"saml_provider_id"`
}

func (s *Auth) StartCLILogin(ctx context.Context, transport, state, challenge, redirect string) (CLILoginStart, error) {
	if transport != "loopback" && transport != "device" {
		return CLILoginStart{}, domain.ErrInvalid
	}
	if transport == "loopback" && (!validPKCEChallenge(state) || !validPKCEChallenge(challenge) || !validCLILoopbackRedirect(redirect)) {
		return CLILoginStart{}, domain.ErrInvalid
	}
	origin, originErr := CanonicalOrigin(s.ExternalOrigin)
	if originErr != nil {
		return CLILoginStart{}, fmt.Errorf("%w: CLI browser login requires a configured http loopback or https instance origin", domain.ErrInvalid)
	}
	if transport == "device" && (state != "" || challenge != "" || redirect != "") {
		return CLILoginStart{}, domain.ErrInvalid
	}
	id, err := newID("clh")
	if err != nil {
		return CLILoginStart{}, err
	}
	now := s.now()
	expires := now.Add(CLILoginLifetime)
	h := authz.CLILogin{ID: id, Transport: transport, PKCEChallenge: challenge, RedirectURI: redirect, CreatedAt: now}
	out := CLILoginStart{State: state, Interval: 5}
	if transport == "device" {
		expires = now.Add(CLIDeviceLifetime)
		state, _, err = crypto.NewArtifact(crypto.ArtifactHandoffState)
		if err != nil {
			return CLILoginStart{}, err
		}
		var raw [8]byte
		if _, err = rand.Read(raw[:]); err != nil {
			return CLILoginStart{}, err
		}
		code := make([]byte, 8)
		for i, b := range raw {
			code[i] = crockford[int(b)&31]
		}
		out.UserCode = string(code)
		h.UserCodeVerifier = crypto.ArtifactVerifier(out.UserCode)
		out.DeviceCode, h.CodeVerifier, err = crypto.NewArtifact(crypto.ArtifactHandoffCode)
		if err != nil {
			return CLILoginStart{}, err
		}
		out.VerificationURI = origin + "/login/cli?device=1"
	}
	h.StateVerifier = crypto.ArtifactVerifier(state)
	h.ExpiresAt = expires
	out.ExpiresAt = expires
	err = tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		if err := az.PruneCLILogins(ctx, now); err != nil {
			return err
		}
		return az.CreateCLILogin(ctx, h)
	})
	if err != nil {
		return CLILoginStart{}, err
	}
	return out, nil
}

func cliUserCode(reference string) bool {
	if len(reference) != 8 {
		return false
	}
	for _, c := range reference {
		if !strings.ContainsRune(crockford, c) {
			return false
		}
	}
	return true
}
func loginByReference(ctx context.Context, az *authz.TxAuthorizer, reference string) (authz.CLILogin, error) {
	if cliUserCode(reference) {
		return az.CLILoginByUserCode(ctx, crypto.ArtifactVerifier(reference))
	}
	if !validPKCEChallenge(reference) {
		return authz.CLILogin{}, ErrCLILoginInvalid
	}
	return az.CLILoginByState(ctx, crypto.ArtifactVerifier(reference))
}
func (s *Auth) CLILoginTransaction(ctx context.Context, actor Actor, reference string) (CLILoginTransaction, error) {
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) (CLILoginTransaction, error) {
		caller, err := actor.resolveSelf(ctx, az, s.now())
		if err != nil {
			return CLILoginTransaction{}, err
		}
		if caller.Artifact != ArtifactBrowser.String() {
			return CLILoginTransaction{}, domain.ErrUnauthenticated
		}
		h, err := loginByReference(ctx, az, reference)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) || errors.Is(err, ErrCLILoginInvalid) {
				return CLILoginTransaction{}, ErrCLILoginInvalid
			}
			return CLILoginTransaction{}, err
		}
		if !h.Live(s.now()) || h.BrowserSessionID != "" {
			return CLILoginTransaction{}, ErrCLILoginInvalid
		}
		return CLILoginTransaction{Reference: reference, Transport: h.Transport, RedirectURI: h.RedirectURI, Instance: s.ExternalOrigin, ExpiresAt: h.ExpiresAt}, nil
	})
}
func (s *Auth) ApproveCLILogin(ctx context.Context, actor Actor, reference string) (CLIReauthApproval, error) {
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) (CLIReauthApproval, error) {
		now := s.now()
		caller, err := actor.resolveSelf(ctx, az, now)
		if err != nil {
			return CLIReauthApproval{}, err
		}
		if caller.Artifact != ArtifactBrowser.String() {
			return CLIReauthApproval{}, domain.ErrUnauthenticated
		}
		if err = az.LockTargetPrincipal(ctx, caller.Principal); err != nil {
			return CLIReauthApproval{}, err
		}
		caller, err = actor.resolveSelf(ctx, az, now)
		if err != nil {
			return CLIReauthApproval{}, err
		}
		h, err := loginByReference(ctx, az, reference)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) || errors.Is(err, ErrCLILoginInvalid) {
				return CLIReauthApproval{}, ErrCLILoginInvalid
			}
			return CLIReauthApproval{}, err
		}
		if !h.Live(now) || h.BrowserSessionID != "" {
			return CLIReauthApproval{}, ErrCLILoginInvalid
		}
		account, err := az.AccountByPrincipal(ctx, caller.Principal)
		if err != nil {
			return CLIReauthApproval{}, err
		}
		if err := s.requireCLILoginAssurance(ctx, az, account.ID, caller.Assurance); err != nil {
			if errors.Is(err, ErrCLILoginInvalid) {
				return CLIReauthApproval{}, cliLoginBrowserFactorRequired{}
			}
			return CLIReauthApproval{}, err
		}
		snapshot, err := json.Marshal(cliLoginSnapshot{Assurance: Assurance{Method: caller.Assurance.Method, Factors: caller.Assurance.Factors, AuthenticatedAt: caller.Assurance.AuthenticatedAt, CeremonyID: caller.Assurance.CeremonyID}, ProviderID: caller.ProviderID, OAuth2ProviderID: caller.OAuth2ProviderID, SAMLProviderID: caller.SAMLProviderID})
		if err != nil {
			return CLIReauthApproval{}, err
		}
		code := ""
		verifier := h.CodeVerifier
		if h.Transport == "loopback" {
			code, verifier, err = crypto.NewArtifact(crypto.ArtifactHandoffCode)
			if err != nil {
				return CLIReauthApproval{}, err
			}
		}
		ok, err := az.ApproveCLILogin(ctx, h.ID, caller.SessionID, string(snapshot), verifier)
		if err != nil {
			return CLIReauthApproval{}, err
		}
		if !ok {
			return CLIReauthApproval{}, ErrCLILoginInvalid
		}
		return CLIReauthApproval{Code: code, State: reference, RedirectURI: h.RedirectURI}, nil
	})
}

// mintCLILogin takes only an approved snapshot, revalidates the approving session
// under its principal lock, and creates independent clocks and no proof windows.
func (s *Auth) mintCLILogin(ctx context.Context, az *authz.TxAuthorizer, h authz.CLILogin, now time.Time) (LoginResult, error) {
	browser, err := az.AuthenticateSessionByID(ctx, h.BrowserSessionID, now)
	if err != nil {
		return LoginResult{}, err
	}
	if err = az.LockTargetPrincipal(ctx, browser.Principal); err != nil {
		return LoginResult{}, err
	}
	browser, err = az.AuthenticateSessionByID(ctx, h.BrowserSessionID, now)
	if err != nil {
		return LoginResult{}, err
	}
	if browser.Artifact != ArtifactBrowser.String() {
		return LoginResult{}, ErrCLILoginInvalid
	}
	var snapshot cliLoginSnapshot
	if err = json.Unmarshal([]byte(h.Snapshot), &snapshot); err != nil {
		return LoginResult{}, err
	}
	account, err := az.AccountByPrincipal(ctx, browser.Principal)
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.requireCLILoginAssurance(ctx, az, account.ID, browser.Assurance); err != nil {
		return LoginResult{}, err
	}
	if err := s.requireCLILoginAssurance(ctx, az, account.ID, authz.Assurance{Method: snapshot.Assurance.Method, Factors: snapshot.Assurance.Factors}); err != nil {
		return LoginResult{}, err
	}
	consumed, err := az.ConsumeCLILogin(ctx, h.ID, now)
	if err != nil {
		return LoginResult{}, err
	}
	if !consumed {
		return LoginResult{}, ErrCLILoginInvalid
	}
	result, err := s.completeSession(ctx, az, CreateSession{account: account, artifact: ArtifactCLI, assurance: snapshot.Assurance, csrf: sessionWithoutCSRF, providerID: snapshot.ProviderID, cliHandoffID: h.ID}, now)
	if err != nil {
		return LoginResult{}, err
	}
	if snapshot.OAuth2ProviderID != "" {
		ok, e := az.BindSessionToOAuth2Provider(ctx, result.SessionID, snapshot.OAuth2ProviderID)
		if e != nil {
			return LoginResult{}, e
		}
		if !ok {
			return LoginResult{}, domain.ErrConflict
		}
	}
	if snapshot.SAMLProviderID != "" {
		ok, e := az.BindSessionToSAMLProvider(ctx, result.SessionID, snapshot.SAMLProviderID)
		if e != nil {
			return LoginResult{}, e
		}
		if !ok {
			return LoginResult{}, domain.ErrConflict
		}
	}
	for _, ev := range []struct {
		typ     audit.EventType
		payload audit.Payload
	}{
		{audit.EventAuthLogin, audit.Payload{"method": snapshot.Assurance.Method, "artifact": "cli", "subject_resolved": true, "account_id": account.ID, "assurance": assuranceLabelFor(snapshot.Assurance.Factors)}},
		{audit.EventAuthSessionCreated, audit.Payload{"session_id": result.SessionID, "artifact": "cli", "method": snapshot.Assurance.Method, "assurance": assuranceLabelFor(snapshot.Assurance.Factors)}},
	} {
		e, eerr := newAuditEvent(ctx, ev.typ, browser.Principal, audit.Object{Type: "session", ID: result.SessionID}, audit.OutcomeSuccess, "", ev.payload)
		if eerr != nil {
			return LoginResult{}, eerr
		}
		if eerr = az.RecordAuthEvent(ctx, e); eerr != nil {
			return LoginResult{}, eerr
		}
	}
	return result, nil
}
func (s *Auth) RedeemCLILogin(ctx context.Context, code, verifier, redirect string) (LoginResult, error) {
	if crypto.ParseArtifact(code, crypto.ArtifactHandoffCode) != nil || !validPKCEVerifier(verifier) {
		return LoginResult{}, s.rejectCLILogin(ctx, "invalid-code-or-proof", ErrCLILoginInvalid)
	}
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) (LoginResult, error) {
		h, err := az.CLILoginByCode(ctx, crypto.ArtifactVerifier(code))
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return LoginResult{}, captureCLILoginFailure(ctx, az, authz.CLILogin{}, "unknown-code", ErrCLILoginInvalid)
			}
			return LoginResult{}, err
		}
		cause := ""
		switch {
		case h.Transport != "loopback" || !h.Live(s.now()) || h.BrowserSessionID == "":
			cause = "expired-or-consumed"
		case h.PKCEChallenge != pkceS256(verifier):
			cause = "pkce-mismatch"
		case h.RedirectURI != redirect:
			cause = "redirect-mismatch"
		}
		if cause != "" {
			return LoginResult{}, captureCLILoginFailure(ctx, az, h, cause, ErrCLILoginInvalid)
		}
		result, err := s.mintCLILogin(ctx, az, h, s.now())
		if errors.Is(err, domain.ErrUnauthenticated) || errors.Is(err, ErrCLILoginInvalid) {
			return LoginResult{}, captureCLILoginFailure(ctx, az, h, "approving-session-invalid", err)
		}
		return result, err
	})
}

type CLIDeviceAttempt struct {
	Login    LoginResult
	Status   string
	Interval int
}

func (s *Auth) PollCLIDevice(ctx context.Context, code string) (CLIDeviceAttempt, error) {
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) (CLIDeviceAttempt, error) {
		if crypto.ParseArtifact(code, crypto.ArtifactHandoffCode) != nil {
			return CLIDeviceAttempt{Status: "expired_token"}, recordCLILoginFailure(ctx, az, authz.CLILogin{Transport: "device"}, "invalid-code")
		}
		now := s.now()
		h, err := az.CLILoginByCode(ctx, crypto.ArtifactVerifier(code))
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return CLIDeviceAttempt{Status: "expired_token"}, recordCLILoginFailure(ctx, az, authz.CLILogin{Transport: "device"}, "unknown-code")
			}
			return CLIDeviceAttempt{}, err
		}
		if h.Transport != "device" || !h.Live(now) {
			return CLIDeviceAttempt{Status: "expired_token"}, recordCLILoginFailure(ctx, az, h, "expired-or-consumed")
		}
		interval := h.PollInterval
		status := "authorization_pending"
		if now.Before(h.LastPollAt.Add(time.Duration(interval) * time.Second)) {
			interval += 5
			status = "slow_down"
		}
		if err = az.PollCLILogin(ctx, h.ID, now, interval); err != nil {
			return CLIDeviceAttempt{}, err
		}
		if status == "slow_down" || h.BrowserSessionID == "" {
			return CLIDeviceAttempt{Status: status, Interval: int(interval)}, nil
		}
		login, err := s.mintCLILogin(ctx, az, h, now)
		if errors.Is(err, domain.ErrUnauthenticated) || errors.Is(err, ErrCLILoginInvalid) {
			return CLIDeviceAttempt{Status: "access_denied", Interval: int(interval)}, recordCLILoginFailure(ctx, az, h, "approving-session-invalid")
		}
		if err != nil {
			return CLIDeviceAttempt{}, err
		}
		return CLIDeviceAttempt{Login: login}, nil
	})
}

// Transport failures stay in the existing login family. No state, code, proof
// or browser bearer enters the event, and the presenting actor is unauthenticated.
func cliLoginFailureEvent(ctx context.Context, h authz.CLILogin, cause string) (audit.Event, error) {
	method := "cli-login-handoff"
	if h.Transport == "device" {
		method = "cli-device"
	}
	if h.Snapshot != "" {
		var snapshot cliLoginSnapshot
		if err := json.Unmarshal([]byte(h.Snapshot), &snapshot); err != nil {
			return audit.Event{}, err
		}
		method = snapshot.Assurance.Method
	}
	return newAuditEvent(ctx, audit.EventAuthLogin, "", audit.Object{Type: "cli-login-handoff", ID: h.ID}, audit.OutcomeFailure, "", audit.Payload{"method": method, "artifact": "cli", "subject_resolved": false, "cause": cause})
}
func captureCLILoginFailure(ctx context.Context, az *authz.TxAuthorizer, h authz.CLILogin, cause string, failure error) error {
	event, err := cliLoginFailureEvent(ctx, h, cause)
	if err != nil {
		return err
	}
	az.CaptureAudit(audit.TrailInstance, domain.Scope{}, event)
	return failure
}
func recordCLILoginFailure(ctx context.Context, az *authz.TxAuthorizer, h authz.CLILogin, cause string) error {
	event, err := cliLoginFailureEvent(ctx, h, cause)
	if err != nil {
		return err
	}
	return az.RecordAuthEvent(ctx, event)
}
func (s *Auth) rejectCLILogin(ctx context.Context, cause string, failure error) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return captureCLILoginFailure(ctx, az, authz.CLILogin{}, cause, failure)
	})
}
