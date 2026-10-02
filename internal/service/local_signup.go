package service

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"net/url"
	"slices"
	"strings"
	"text/template"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

const SignupLifetime = 24 * time.Hour

//go:embed mail_templates/*.txt
var signupMailTemplates embed.FS

// SignupVerification contains form fields. Email and identity come only from the token row.
type SignupVerification struct{ Token, DisplayName, Password, OrgName, Landing string }

func (s *Auth) EnableLocalSignup(capture func(context.Context) (*runtimeconfig.Bundle, error)) {
	s.signupMail = capture
}

func localDomainAdmitted(p authz.RegistrationPolicy, email string) bool {
	return p.LocalEnabled && (len(p.Domains) == 0 || slices.Contains(p.Domains, email[strings.LastIndexByte(email, '@')+1:]))
}
func (s *Auth) localPolicy(ctx context.Context, az *authz.TxAuthorizer, org domain.OrgID, email string) (authz.RegistrationPolicy, string, error) {
	if s.registration == nil || s.signupMail == nil {
		return authz.RegistrationPolicy{}, "closed", nil
	}
	p, err := az.RegistrationPolicyFor(ctx, org)
	if errors.Is(err, domain.ErrNotFound) {
		return p, "closed", nil
	}
	if err != nil {
		return p, "", err
	}
	v, err := s.registration.evaluatePolicy(ctx, az, p)
	if err != nil {
		return p, "", err
	}
	switch v.InactiveCause {
	case "":
	case InactiveAuthorityLost, InactiveAuthorityUnassigned:
		return p, "authority-lost", nil
	default:
		return p, "precondition", nil
	}
	if !localDomainAdmitted(p, email) {
		return p, "predicate", nil
	}
	return p, "", nil
}
func signupRefused(ctx context.Context, az *authz.TxAuthorizer, org domain.OrgID, policyID, cause string) error {
	payload := audit.Payload{"scope": renderScope(domain.Scope{Org: org}), "cause": cause}
	if policyID != "" {
		payload["policy_id"] = policyID
	}
	ev, err := newAuditEvent(ctx, audit.EventRegistrationSignupRefused, "", audit.Object{Type: "registration-signup"}, audit.OutcomeFailure, "", payload)
	if err != nil {
		return err
	}
	return az.RecordAuthEvent(ctx, ev)
}
func (s *Auth) refuseSignup(ctx context.Context, org domain.OrgID, policyID, cause string) error {
	err := tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return signupRefused(ctx, az, org, policyID, cause)
	})
	if err != nil {
		return err
	}
	return domain.ErrUnauthenticated
}

// Signup commits intent before its single synchronous transport attempt. An admitted charge is never refunded.
func (s *Auth) Signup(ctx context.Context, email string, org domain.OrgID) error {
	release, err := s.Admission.Enter(ctx, audit.FromContext(ctx).SourceIP)
	if err != nil {
		return err
	}
	defer release()
	email, err = domain.CanonicalEmail(email)
	if err != nil {
		e := s.refuseSignup(ctx, org, "", "predicate")
		if errors.Is(e, domain.ErrUnauthenticated) {
			return nil
		}
		return e
	}
	var bundle *runtimeconfig.Bundle
	var token, intentID, landing string
	var existing, admitted, charged bool
	var refusal error
	err = tx.WriteSerialized(ctx, s.DB, "hikyo:local-signup", func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		admitted = false
		existing = false
		refusal = nil
		p, cause, e := s.localPolicy(ctx, az, org, email)
		if e != nil {
			return e
		}
		if cause != "" {
			return signupRefused(ctx, az, org, p.ID, cause)
		}
		bundle, e = s.signupMail(ctx)
		if errors.Is(e, ErrSelfConfigFenced) {
			return signupRefused(ctx, az, org, p.ID, "precondition")
		}
		if e != nil {
			return e
		}
		if !bundle.MailConfigured() {
			return signupRefused(ctx, az, org, p.ID, "precondition")
		}
		if !charged {
			if _, e = s.signupBudget.chargeSignup(); e != nil {
				refusal = e
				return signupRefused(ctx, az, org, p.ID, "budget")
			}
			charged = true
		}
		existing, e = az.VerifiedAccountEmailExists(ctx, email)
		if e != nil {
			return e
		}
		now := s.now()
		landing = p.Landing
		if LandingKind(p.Landing) == LandingOrgTemplate {
			landing = "org"
		}
		if !existing {
			token, _, e = crypto.NewArtifact(crypto.ArtifactSignup)
			if e != nil {
				return e
			}
			epoch, e := az.CredentialEpoch(ctx)
			if e != nil {
				return e
			}
			row, e := az.RegistrationSignupByEmail(ctx, email)
			if e != nil && !errors.Is(e, domain.ErrNotFound) {
				return e
			}
			if e == nil && !now.Before(row.ExpiresAt) {
				gone, e := az.PruneExpiredRegistrationSignup(ctx, row.ID, now)
				if e != nil {
					return e
				}
				if gone {
					if e = signupExpired(ctx, az, row.ID, row.PolicyID); e != nil {
						return e
					}
				}
				row.ID = ""
			}
			n := authz.NewRegistrationSignup{ID: row.ID, Email: email, TokenVerifier: crypto.ArtifactVerifier(token), PolicyID: p.ID, SignupScopeOrgID: org, CredentialEpoch: epoch, CreatedAt: now, ExpiresAt: now.Add(SignupLifetime)}
			if n.ID == "" {
				n.ID, e = newID("su")
				if e != nil {
					return e
				}
				e = az.CreateRegistrationSignup(ctx, n)
			} else {
				e = az.ReissueRegistrationSignup(ctx, n)
				if errors.Is(e, domain.ErrNotFound) {
					return signupRefused(ctx, az, org, p.ID, "unknown")
				}
			}
			if e != nil {
				return e
			}
			ev, e := newAuditEvent(ctx, audit.EventRegistrationSignupAdmitted, "", audit.Object{Type: "registration-policy", ID: p.ID}, audit.OutcomeSuccess, "", audit.Payload{"policy_id": p.ID, "scope": renderScope(domain.Scope{Org: org}), "landing": p.Landing})
			if e != nil {
				return e
			}
			if e = az.RecordAuthEvent(ctx, ev); e != nil {
				return e
			}
		} else {
			if e = signupRefused(ctx, az, org, p.ID, "email-exists"); e != nil {
				return e
			}
		}
		kind := "verification"
		if existing {
			kind = "existing-address"
		}
		ev, e := newAuditEvent(ctx, audit.EventRegistrationMailIntent, "", audit.Object{Type: "registration-policy", ID: p.ID}, audit.OutcomeIntent, "", audit.Payload{"kind": kind, "recipient": audit.SanitizeFreeText(email), "policy_id": p.ID})
		if e != nil {
			return e
		}
		intentID = ev.ID
		admitted = true
		return az.RecordAuthEvent(ctx, ev)
	})
	// Enter's release is idempotent and retains its already-charged per-IP
	// admission. SMTP and outcome persistence do not consume Argon2 memory.
	release()
	if err != nil {
		return err
	}
	if refusal != nil {
		return refusal
	}
	if !admitted {
		return nil
	}
	fragment := url.Values{"token": {token}, "landing": {landing}, "email": {email}}
	if org != "" {
		fragment.Set("org", string(org))
	}
	link := strings.TrimRight(s.ExternalOrigin, "/") + "/signup/verify#" + fragment.Encode()
	name, subject := "verification.txt", "Verify your Hikyo email"
	if existing {
		name, subject = "existing-address.txt", "Your Hikyo account"
		link = ""
	}
	tmpl, err := template.ParseFS(signupMailTemplates, "mail_templates/"+name)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	if err = tmpl.Execute(&body, struct{ Link string }{link}); err != nil {
		return err
	}
	sendErr := bundle.Send(ctx, email, subject, body.String())
	if sendErr != nil && s.MailFailed != nil {
		s.MailFailed()
	}
	// Record completion even when the caller disconnects during SMTP.
	outcomeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	return tx.Write(outcomeCtx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		status := audit.OutcomeSuccess
		if sendErr != nil {
			status = audit.OutcomeFailure
		}
		ev, e := newAuditEvent(ctx, audit.EventRegistrationMailOutcome, "", audit.Object{Type: "mail-intent", ID: intentID}, status, "", audit.Payload{"intent_id": intentID})
		if e != nil {
			return e
		}
		return az.RecordAuthEvent(ctx, ev)
	})
}

func (s *Auth) VerifySignup(ctx context.Context, in SignupVerification) error {
	if err := CheckPassword(in.Password); err != nil {
		return err
	}
	release, err := s.Admission.Enter(ctx, audit.FromContext(ctx).SourceIP)
	if err != nil {
		return err
	}
	defer release()
	if crypto.ParseArtifact(in.Token, crypto.ArtifactSignup) != nil {
		return s.refuseSignup(ctx, "", "", "malformed")
	}
	verifier := crypto.ArtifactVerifier(in.Token)
	var row authz.RegistrationSignup
	var cause string
	err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		var e error
		row, e = az.RegistrationSignupByVerifier(ctx, verifier)
		if errors.Is(e, domain.ErrNotFound) {
			cause = "unknown"
			return nil
		}
		if e != nil {
			return e
		}
		epoch, e := az.CredentialEpoch(ctx)
		if e != nil {
			return e
		}
		if !s.now().Before(row.ExpiresAt) {
			cause = "expired"
		} else if row.CredentialEpoch != epoch {
			cause = "epoch-superseded"
		}
		return nil
	})
	if err != nil {
		return err
	}
	if cause != "" {
		return s.refuseSignup(ctx, row.SignupScopeOrgID, row.PolicyID, cause)
	}
	accountID, err := newID("acc")
	if err != nil {
		return err
	}
	handle := "local-" + accountID
	if err = validateAccountProfile(ProfileUpdate{Username: handle, DisplayName: in.DisplayName}); err != nil {
		return err
	}
	if in.OrgName != "" {
		if err = checkName("org_name", in.OrgName); err != nil {
			return err
		}
	}
	sealed, params, dek, err := s.sealVerifier(accountID, in.Password)
	if err != nil {
		return err
	}
	var refused bool
	err = tx.WriteSerialized(ctx, s.DB, orgCreateSerialization, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		refused = false
		live, e := az.RegistrationSignupByVerifier(ctx, verifier)
		if errors.Is(e, domain.ErrNotFound) {
			refused = true
			return signupRefused(ctx, az, row.SignupScopeOrgID, row.PolicyID, "unknown")
		}
		if e != nil {
			return e
		}
		epoch, e := az.CredentialEpoch(ctx)
		if e != nil {
			return e
		}
		cause = ""
		if !s.now().Before(live.ExpiresAt) {
			cause = "expired"
		} else if live.CredentialEpoch != epoch {
			cause = "epoch-superseded"
		}
		p, c, e := s.localPolicy(ctx, az, live.SignupScopeOrgID, live.Email)
		if e != nil {
			return e
		}
		if cause == "" {
			cause = c
		}
		if cause == "" && p.ID != live.PolicyID {
			cause = "closed"
		}
		if cause == "" && LandingKind(p.Landing) == LandingFreshOrg {
			n, e := az.CountRegistrationPolicyOrgs(ctx, p.ID)
			if e != nil {
				return e
			}
			if n >= p.FreshOrgCap {
				cause = "cap"
			}
		}
		if cause != "" {
			refused = true
			return signupRefused(ctx, az, live.SignupScopeOrgID, live.PolicyID, cause)
		}
		consumed, e := az.ConsumeRegistrationSignup(ctx, live.ID, verifier)
		if e != nil {
			return e
		}
		if !consumed {
			refused = true
			return signupRefused(ctx, az, live.SignupScopeOrgID, live.PolicyID, "unknown")
		}
		now := s.now()
		principalID, e := newID("prn")
		if e != nil {
			return e
		}
		principal := domain.PrincipalID(principalID)
		if e = az.CreateHumanPrincipal(ctx, principal, now); e != nil {
			return e
		}
		if e = az.CreateAccount(ctx, authz.Account{ID: accountID, PrincipalID: principal, Username: handle, DisplayName: in.DisplayName, CreatedAt: now}); e != nil {
			return e
		}
		if e = az.SetLocalAccountEmail(ctx, accountID, live.Email, now); e != nil {
			return e
		}
		if e = az.AssertActiveInstanceDEKVersion(ctx, dek); e != nil {
			return e
		}
		if e = az.WritePasswordCredential(ctx, authz.PasswordCredential{AccountID: accountID, Verifier: sealed, KDF: params, DEKVersion: dek, CredentialEpoch: epoch}, now); e != nil {
			return e
		}
		lander := &federatedSignup{auth: s}
		if in.Landing == string(LandingFreshOrg) && LandingKind(p.Landing) == LandingFreshOrg {
			lander.orgName = in.OrgName
		}
		landed, e := lander.land(ctx, r, az, p, principal, now)
		if e != nil {
			if lander.orgName != "" && (isUniquenessRace(e) || errors.Is(e, store.ErrConflict)) {
				return refuseRegistration("org_name: that name is already in use")
			}
			return e
		}
		payload := audit.Payload{"policy_id": p.ID, "account_id": accountID, "landing": p.Landing}
		if landed.orgID != "" {
			payload["org_id"] = landed.orgID
		}
		ev, e := newAuditEvent(ctx, audit.EventRegistrationSignupCompleted, principal, audit.Object{Type: "account", ID: accountID}, audit.OutcomeSuccess, "", payload)
		if e != nil {
			return e
		}
		return az.RecordAuthEvent(ctx, ev)
	})
	if err != nil {
		return err
	}
	if refused {
		return domain.ErrUnauthenticated
	}
	return nil
}

func signupExpired(ctx context.Context, az *authz.TxAuthorizer, id, policyID string) error {
	ev, err := newAuditEvent(ctx, audit.EventRegistrationSignupExpired, "", audit.Object{Type: "registration-signup", ID: id}, audit.OutcomeSuccess, "", audit.Payload{"signup_id": id, "policy_id": policyID, "cause": "expired"})
	if err != nil {
		return err
	}
	return az.RecordAuthEvent(ctx, ev)
}

// ReapSignups prunes only expired pending rows, atomically with their trail events.
func (s *Auth) ReapSignups(ctx context.Context) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		rows, err := az.ExpiredRegistrationSignups(ctx, now)
		if err != nil {
			return err
		}
		for _, row := range rows {
			gone, err := az.PruneExpiredRegistrationSignup(ctx, row.ID, now)
			if err != nil {
				return err
			}
			if !gone {
				continue
			}
			if err = signupExpired(ctx, az, row.ID, row.PolicyID); err != nil {
				return err
			}
		}
		return nil
	})
}
