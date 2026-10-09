package authz

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

type DeveloperCredential = authn.DeveloperCredential

func (a *TxAuthorizer) authenticateDeveloper(ctx context.Context, presented string, now time.Time) (Identity, error) {
	if err := operation.AdmitPreauthentication(ctx); err != nil {
		return Identity{}, err
	}
	c, err := a.r.DeveloperCredentialByVerifier(ctx, crypto.ArtifactVerifier(presented))
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return Identity{}, err
	}
	generation, genErr := a.r.PrincipalGeneration(ctx, c.AuthorityPrincipal)
	if genErr != nil && !errors.Is(genErr, domain.ErrNotFound) {
		return Identity{}, genErr
	}
	epoch, epochErr := a.r.CredentialEpoch(ctx)
	if epochErr != nil {
		return Identity{}, epochErr
	}
	_, accountErr := a.r.AccountByPrincipal(ctx, c.AuthorityPrincipal)
	if accountErr != nil && !errors.Is(accountErr, domain.ErrNotFound) {
		return Identity{}, accountErr
	}
	if err != nil || genErr != nil || accountErr != nil || generation != c.AuthorityGeneration || !c.Live(now, epoch) {
		_ = a.refuseAdmission(ctx, Identity{Principal: c.PrincipalID, AuthorityPrincipal: c.AuthorityPrincipal, CredentialID: c.ID, Class: domain.ClassDeveloper}, "delivery.fetch", "developer-credential", "invalid-developer-credential")
		return Identity{}, domain.ErrUnauthenticated
	}
	return Identity{Principal: c.PrincipalID, Class: domain.ClassDeveloper, CredentialID: c.ID, Artifact: string(crypto.ArtifactDeveloper), CredentialExpiresAt: c.ExpiresAt, CreatedAt: c.CreatedAt, AuthorityPrincipal: c.AuthorityPrincipal, DeveloperScope: c.Scope}, nil
}
func (a *TxAuthorizer) DeveloperCredentialPolicy(ctx context.Context) (time.Duration, error) {
	return a.r.DeveloperCredentialPolicy(ctx)
}
func (a *TxAuthorizer) SetDeveloperCredentialPolicy(ctx context.Context, d time.Duration) error {
	return a.r.SetDeveloperCredentialPolicy(ctx, d)
}
func (a *TxAuthorizer) ListDeveloperCredentials(ctx context.Context) ([]DeveloperCredential, error) {
	return a.r.ListDeveloperCredentials(ctx)
}
func (a *TxAuthorizer) CreateDeveloperCredential(ctx context.Context, c DeveloperCredential, v []byte) error {
	if err := a.r.CreateMachinePrincipal(ctx, c.PrincipalID, domain.ClassDeveloper, c.CreatedAt); err != nil {
		return err
	}
	return a.r.InsertDeveloperCredential(ctx, c, v)
}
func (a *TxAuthorizer) ClampDeveloperCredentialExpiry(ctx context.Context, id string, at time.Time) error {
	return a.r.ClampDeveloperCredentialExpiry(ctx, id, at)
}
func (a *TxAuthorizer) RevokeDeveloperCredentials(ctx context.Context, authority domain.PrincipalID, scope domain.Scope, id string, at time.Time) ([]DeveloperCredential, error) {
	rows, err := a.r.ListDeveloperCredentials(ctx)
	if err != nil {
		return nil, err
	}
	out := []DeveloperCredential{}
	for _, c := range rows {
		if authority != "" && c.AuthorityPrincipal != authority || scope.Org != "" && c.Scope.Org != scope.Org || scope.Project != "" && c.Scope.Project != scope.Project || scope.Env != "" && c.Scope.Env != scope.Env || id != "" && c.ID != id {
			continue
		}
		if !c.RevokedAt.IsZero() {
			continue
		}
		if err := a.r.RevokeDeveloperCredential(ctx, c.ID, at); err != nil {
			return nil, err
		}
		c.RevokedAt = at
		out = append(out, c)
	}
	return out, nil
}
func (a *TxAuthorizer) RevokeDeveloperCredentialsByProvenance(ctx context.Context, kind, id string, at time.Time) ([]DeveloperCredential, error) {
	rows, err := a.r.ListDeveloperCredentials(ctx)
	if err != nil {
		return nil, err
	}
	out := []DeveloperCredential{}
	for _, c := range rows {
		match := false
		switch kind {
		case "session":
			match = c.ParentSessionID == id
		case "provider":
			match = c.ProviderID == id
		case "oauth2-provider":
			match = c.OAuth2ProviderID == id
		case "saml-provider":
			match = c.SAMLProviderID == id
		default:
			return nil, domain.ErrInvalid
		}
		if match && c.RevokedAt.IsZero() {
			if err := a.r.RevokeDeveloperCredential(ctx, c.ID, at); err != nil {
				return nil, err
			}
			c.RevokedAt = at
			out = append(out, c)
		}
	}
	return out, nil
}
func (a *TxAuthorizer) RevokeInvalidDeveloperCredentials(ctx context.Context, authority domain.PrincipalID, org domain.OrgID, at time.Time) ([]DeveloperCredential, error) {
	rows, err := a.r.ListDeveloperCredentials(ctx)
	if err != nil {
		return nil, err
	}
	out := []DeveloperCredential{}
	for _, c := range rows {
		if c.AuthorityPrincipal != authority || c.Scope.Org != org || !c.RevokedAt.IsZero() {
			continue
		}
		chain, chainErr := a.r.ResolveChain(ctx, c.Scope)
		if chainErr != nil && !errors.Is(chainErr, domain.ErrNotFound) {
			return nil, chainErr
		}
		grants, err := a.r.Grants(ctx, authority)
		if err != nil {
			return nil, err
		}
		rules, err := a.r.Rules(ctx, authority)
		if err != nil {
			return nil, err
		}
		if chainErr != nil || !developerEnvironmentAuthority(chain, grants, rules) {
			if err := a.r.RevokeDeveloperCredential(ctx, c.ID, at); err != nil {
				return nil, err
			}
			c.RevokedAt = at
			out = append(out, c)
		}
	}
	return out, nil
}

type developerDeliveryContextKey struct{}

// WithDeveloperDelivery marks the sole below-network credential delivery surface.
func WithDeveloperDelivery(ctx context.Context) context.Context {
	return context.WithValue(ctx, developerDeliveryContextKey{}, true)
}
func developerDeliveryContext(ctx context.Context) bool {
	v, _ := ctx.Value(developerDeliveryContextKey{}).(bool)
	return v
}

// developerEnvironmentAuthority requires authority over the whole environment.
// With no key target, restricted-key or folder rules cannot confer consent to
// all current and future keys. Mint, delivery and terminal SCIM revocation use
// this same formula over the human's current records.
func developerEnvironmentAuthority(scope domain.Scope, grants []domain.Grant, rules []domain.Rule) bool {
	return evaluateWithRules(Formula{{Cap: domain.CapRead, At: domain.LevelEnv}, {Cap: domain.CapReveal, At: domain.LevelEnv}}, scope, grants, rules, nil)
}

// DeveloperDeliveryGrants projects only the two capabilities delegated by this
// artifact. The human's historical, write and cross-environment grants never
// travel into developer delivery, including when whole-environment rules supply
// the human's current authority.
func (a *TxAuthorizer) DeveloperDeliveryGrants(ctx context.Context, caller Identity) ([]domain.Grant, error) {
	if caller.Class != domain.ClassDeveloper || caller.AuthorityPrincipal == "" {
		return nil, fmt.Errorf("authz: developer delivery grants require a resolved developer identity")
	}
	chain, err := a.r.ResolveChain(ctx, caller.DeveloperScope)
	if err != nil {
		return nil, err
	}
	grants, err := a.r.Grants(ctx, caller.AuthorityPrincipal)
	if err != nil {
		return nil, err
	}
	rules, err := a.r.Rules(ctx, caller.AuthorityPrincipal)
	if err != nil {
		return nil, err
	}
	if !developerEnvironmentAuthority(chain, grants, rules) {
		return nil, domain.ErrNotFound
	}
	return []domain.Grant{
		{Capability: domain.CapRead, Scope: chain},
		{Capability: domain.CapReveal, Scope: chain},
	}, nil
}
