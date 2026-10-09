package service

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type DeveloperCredential = authz.DeveloperCredential
type MintDeveloperCredentialRequest struct {
	Lifetime                time.Duration
	ConsentCurrentAndFuture bool
	KeyIDs                  []string
}
type MintedDeveloperCredential struct {
	Credential DeveloperCredential
	Value      string
}
type DeveloperCredentials struct {
	DB   *store.DB
	Auth *Auth
	Now  func() time.Time
}

func (s *DeveloperCredentials) now() time.Time { return nowOr(s.Now) }

const MaxLiveDeveloperCredentials = 4

func (s *DeveloperCredentials) Mint(ctx context.Context, actor Actor, scope domain.Scope, request MintDeveloperCredentialRequest) (MintedDeveloperCredential, error) {
	if !request.ConsentCurrentAndFuture || request.Lifetime < 0 || request.Lifetime%time.Second != 0 {
		return MintedDeveloperCredential{}, domain.ErrInvalid
	}
	intent, err := NewDeveloperCredentialReauthIntent(string(scope.Env), request.KeyIDs, request.Lifetime, request.ConsentCurrentAndFuture)
	if err != nil {
		return MintedDeveloperCredential{}, err
	}
	var out MintedDeveloperCredential
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		out = MintedDeveloperCredential{}
		now := s.now()
		caller, p, err := authorize(ctx, az, actor, authz.OpDeveloperCredentialMint, scope, now)
		if err != nil {
			return err
		}
		refuse := func(cause string, failure error) error {
			e, err := newAuditEvent(ctx, audit.EventDeveloperCredentialMintRefused, caller.Principal, audit.Object{Type: "developer-credential"}, audit.OutcomeFailure, "", audit.Payload{"cause": cause, "scope": renderScope(scope)})
			if err != nil {
				return err
			}
			e.Actor.CredentialID = caller.SessionID
			e.AuthorityID = string(caller.Principal)
			az.CaptureAudit(audit.TrailInstance, domain.Scope{}, e)
			return failure
		}
		if caller.Class != domain.ClassHuman || caller.SessionID == "" || caller.Artifact == authz.WorkspaceArtifact {
			return refuse("human-session-required", domain.ErrNotFound)
		}
		settings, err := az.EnvironmentReauthSettings(ctx, string(scope.Env))
		if err != nil {
			return err
		}
		if settings.Protected {
			return refuse("protected-environment", domain.ErrNotFound)
		}
		ceiling, err := az.DeveloperCredentialPolicy(ctx)
		if err != nil {
			return err
		}
		ttl := request.Lifetime
		if ttl == 0 {
			ttl = ceiling
		}
		if ttl <= 0 || ttl > ceiling {
			return refuse("lifetime-ceiling", invalidDetail("developer credential lifetime exceeds the instance ceiling"))
		}
		if s.Auth == nil {
			return refuse("missing-ceremony", ErrNoReauthWindow)
		}
		if err := s.Auth.ConsumeDeveloperCredentialReauth(ctx, az, caller, intent, now); err != nil {
			return refuse("reauth-required", err)
		}
		snapshot, err := r.Snapshots().Latest(ctx, p)
		if errors.Is(err, store.ErrNotFound) {
			return refuse("not-materialized", ErrNotMaterialized)
		}
		if err != nil {
			return err
		}
		entries, err := r.Snapshots().Entries(ctx, p, snapshot)
		if err != nil {
			return err
		}
		current := make([]string, 0, len(entries))
		for _, entry := range entries {
			current = append(current, entry.KeyID)
		}
		if !slices.Equal(canonicalSet(current), canonicalSet(request.KeyIDs)) {
			return refuse("changed-key-set", ErrReauthUnitMismatch)
		}
		rows, err := az.ListDeveloperCredentials(ctx)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		generation, err := az.PrincipalGeneration(ctx, caller.Principal)
		if err != nil {
			return err
		}
		count := 0
		for _, c := range rows {
			if c.AuthorityPrincipal == caller.Principal && c.AuthorityGeneration == generation && c.Live(now, epoch) {
				count++
			}
		}
		if count >= MaxLiveDeveloperCredentials {
			return refuse("live-credential-cap", invalidDetail("four live developer credentials already exist; list and revoke one before minting"))
		}
		id, err := newID("devc")
		if err != nil {
			return err
		}
		principal, err := newID("devp")
		if err != nil {
			return err
		}
		value, verifier, err := crypto.NewArtifact(crypto.ArtifactDeveloper)
		if err != nil {
			return err
		}
		c := DeveloperCredential{ID: id, PrincipalID: domain.PrincipalID(principal), AuthorityPrincipal: caller.Principal, Scope: scope, PrefixHint: value[:15], ParentSessionID: caller.SessionID, ProviderID: caller.ProviderID, OAuth2ProviderID: caller.OAuth2ProviderID, SAMLProviderID: caller.SAMLProviderID, AuthMethod: caller.Assurance.Method, AuthorityGeneration: generation, CredentialEpoch: epoch, CreatedAt: now, ExpiresAt: now.Add(ttl)}
		if err := az.CreateDeveloperCredential(ctx, c, verifier); err != nil {
			return err
		}
		e, err := domainEvent(ctx, audit.EventDeveloperCredentialMinted, caller.Principal, audit.Object{Type: "developer-credential", ID: id}, audit.Payload{"credential_id": id, "target_principal": principal, "human_authority": string(c.AuthorityPrincipal), "lifetime_seconds": int64(ttl / time.Second), "consent_current_and_future": true, "expires_at": c.ExpiresAt.Format(time.RFC3339Nano)})
		if err != nil {
			return err
		}
		e.AuthorityID = string(c.AuthorityPrincipal)
		if err := r.Audit().InsertTenant(ctx, p, e); err != nil {
			return err
		}
		out = MintedDeveloperCredential{c, value}
		return nil
	})
	if err != nil {
		return MintedDeveloperCredential{}, err
	}
	return out, nil
}
func (s *DeveloperCredentials) List(ctx context.Context, actor Actor, scope domain.Scope) ([]DeveloperCredential, error) {
	var out []DeveloperCredential
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		var caller authz.Identity
		var p authz.Proof
		var err error
		if scope == (domain.Scope{}) {
			caller, err = actor.resolveSelf(ctx, az, s.now())
		} else {
			caller, p, err = authorize(ctx, az, actor, authz.OpDeveloperCredentialListProject, scope, s.now())
		}
		if err != nil {
			return err
		}
		if caller.SessionID == "" || caller.Class != domain.ClassHuman || caller.Artifact == authz.WorkspaceArtifact {
			return domain.ErrNotFound
		}
		rows, err := az.ListDeveloperCredentials(ctx)
		if err != nil {
			return err
		}
		out = []DeveloperCredential{}
		for _, c := range rows {
			if scope == (domain.Scope{}) {
				if c.AuthorityPrincipal != caller.Principal {
					continue
				}
			} else if c.Scope.Org != scope.Org || c.Scope.Project != scope.Project {
				continue
			}
			out = append(out, c)
		}
		e, err := domainEvent(ctx, audit.EventDeveloperCredentialsListed, caller.Principal, audit.Object{Type: "developer-credential"}, audit.Payload{"row_count": len(out)})
		if err != nil {
			return err
		}
		if p != nil {
			return r.Audit().InsertTenant(ctx, p, e)
		}
		return az.RecordAuthEvent(ctx, e)
	})
	return out, err
}
func (s *DeveloperCredentials) Revoke(ctx context.Context, actor Actor, scope domain.Scope, id string, all bool) error {
	if id == "" && !all || id != "" && all {
		return domain.ErrInvalid
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		var caller authz.Identity
		var p authz.Proof
		var err error
		var authority domain.PrincipalID
		if scope == (domain.Scope{}) {
			caller, err = actor.resolveSelf(ctx, az, now)
			authority = caller.Principal
		} else {
			caller, p, err = authorize(ctx, az, actor, authz.OpDeveloperCredentialRevokeProject, scope, now)
		}
		if err != nil {
			return err
		}
		if caller.SessionID == "" || caller.Class != domain.ClassHuman || caller.Artifact == authz.WorkspaceArtifact {
			return domain.ErrNotFound
		}
		if id != "" {
			rows, err := az.ListDeveloperCredentials(ctx)
			if err != nil {
				return err
			}
			found := false
			for _, c := range rows {
				if c.ID == id && (authority == "" || c.AuthorityPrincipal == authority) && (scope.Org == "" || c.Scope.Org == scope.Org) && (scope.Project == "" || c.Scope.Project == scope.Project) {
					found = true
				}
			}
			if !found {
				return domain.ErrNotFound
			}
		}
		rows, err := az.RevokeDeveloperCredentials(ctx, authority, scope, id, now)
		if err != nil {
			return err
		}
		for _, c := range rows {
			e, err := developerRevocationEvent(ctx, caller.Principal, c, "explicit-revocation")
			if err != nil {
				return err
			}
			if p != nil {
				err = r.Audit().InsertTenant(ctx, p, e)
			} else {
				err = az.RecordAuthEvent(ctx, e)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}
func developerRevocationEvent(ctx context.Context, actor domain.PrincipalID, c DeveloperCredential, cause string) (audit.Event, error) {
	e, err := domainEvent(ctx, audit.EventDeveloperCredentialRevoked, actor, audit.Object{Type: "developer-credential", ID: c.ID}, audit.Payload{"credential_id": c.ID, "target_principal": string(c.PrincipalID), "human_authority": string(c.AuthorityPrincipal), "cause": cause, "scope": renderScope(c.Scope)})
	if err == nil {
		e.AuthorityID = string(c.AuthorityPrincipal)
	}
	return e, err
}
func (s *DeveloperCredentials) Policy(ctx context.Context, actor Actor) (time.Duration, error) {
	var out time.Duration
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpDeveloperCredentialPolicyRead, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		out, err = az.DeveloperCredentialPolicy(ctx)
		if err != nil {
			return err
		}
		e, err := domainEvent(ctx, audit.EventDeveloperCredentialPolicyRead, caller.Principal, audit.Object{Type: "developer-credential-policy"}, audit.Payload{})
		if err != nil {
			return err
		}
		return r.Audit().InsertInstance(ctx, p, e)
	})
	return out, err
}
func (s *DeveloperCredentials) SetPolicy(ctx context.Context, actor Actor, ceiling time.Duration) (time.Duration, error) {
	if ceiling < time.Second || ceiling > 8*time.Hour || ceiling%time.Second != 0 {
		return 0, domain.ErrInvalid
	}
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpDeveloperCredentialPolicyUpdate, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		old, err := az.DeveloperCredentialPolicy(ctx)
		if err != nil {
			return err
		}
		count := 0
		if ceiling < old {
			rows, err := az.ListDeveloperCredentials(ctx)
			if err != nil {
				return err
			}
			for _, c := range rows {
				target := c.CreatedAt.Add(ceiling)
				if target.Before(c.ExpiresAt) {
					if err := az.ClampDeveloperCredentialExpiry(ctx, c.ID, target); err != nil {
						return err
					}
					count++
				}
			}
		}
		if err := az.SetDeveloperCredentialPolicy(ctx, ceiling); err != nil {
			return err
		}
		e, err := domainEvent(ctx, audit.EventDeveloperCredentialPolicyChanged, caller.Principal, audit.Object{Type: "developer-credential-policy"}, audit.Payload{"max_lifetime_seconds": int64(ceiling / time.Second), "clamped_count": count})
		if err != nil {
			return err
		}
		return r.Audit().InsertInstance(ctx, p, e)
	})
	return ceiling, err
}
