package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/oauth2rp"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

var ErrOAuth2ProviderExists = fmt.Errorf("%w: service: an OAuth2 provider with that slug already exists", domain.ErrConflict)

func oauth2ProviderSecretAAD(id string) crypto.InstanceFieldAAD {
	return crypto.InstanceFieldAAD{OwnerTable: "oauth2_providers", OwnerRowID: id, FieldTag: "client_secret"}
}

type OAuth2Providers struct {
	DB      *store.DB
	Keyring *crypto.Keyring
	// ExternalOrigin is the instance's public origin, used to build the
	// per-provider redirect URI (A1). Never derived from a request header.
	ExternalOrigin string
	Now            func() time.Time
}

func (s *OAuth2Providers) now() time.Time {
	return nowOr(s.Now)
}

// OAuth2ProviderView is a provider as returned to an administrator. The client secret
// never leaves the server, so it is absent here.
type OAuth2ProviderView struct {
	Profile     string
	Slug        string
	DisplayName string
	Issuer      string
	ClientID    string
	RedirectURI string
	Enabled     bool
	RowVersion  int64
}

func oauth2ProviderView(p authz.OAuth2Provider) OAuth2ProviderView {
	return OAuth2ProviderView{
		Slug: p.Slug, DisplayName: p.DisplayName, Issuer: p.Issuer, ClientID: p.ClientID, Profile: p.Profile,
		RedirectURI: p.RedirectURI,
		Enabled:     p.Enabled,
		RowVersion:  p.RowVersion,
	}
}

// OAuth2ProviderInput is a create-or-update request body.
type OAuth2ProviderInput struct {
	Profile            string
	DisplayName        string
	Issuer             string
	ClientID           string
	ClientSecret       string
	Enabled            bool
	CreateOnly         bool
	ExpectedRowVersion *int64
}

func (s *OAuth2Providers) redirectURI(slug string) string {
	return strings.TrimRight(s.ExternalOrigin, "/") + "/api/v1/auth/oauth2/" + slug + "/callback"
}

// Put validates the closed profile locally, then seals configuration. No discovery runs.
func (s *OAuth2Providers) Put(ctx context.Context, actor Actor, slug string, in OAuth2ProviderInput) (OAuth2ProviderView, error) {
	var out OAuth2ProviderView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpOAuth2ProviderPut, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		if _, err := oauth2rp.New(in.Profile, in.Issuer, nil); err != nil {
			return errors.Join(domain.ErrInvalid, err)
		}
		if slug == "" || in.DisplayName == "" || in.ClientID == "" || in.ClientSecret == "" {
			return domain.ErrInvalid
		}

		existing, err := az.OAuth2ProviderBySlug(ctx, slug)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			if in.ExpectedRowVersion != nil {
				return ErrProviderRace
			}
			return s.create(ctx, r, az, p, caller.Principal, slug, in, &out)
		case err != nil:
			return err
		}
		if in.CreateOnly {
			return ErrOAuth2ProviderExists
		}
		if in.ExpectedRowVersion != nil && *in.ExpectedRowVersion != existing.RowVersion {
			return ErrProviderRace
		}
		if existing.Issuer != in.Issuer {
			return ErrIssuerImmutable
		}
		return s.update(ctx, r, az, p, caller.Principal, existing, in, &out)
	})
	if err != nil {
		return OAuth2ProviderView{}, err
	}
	return out, nil
}

// fence:delegated : returns the sealed secret and its instance DEK version to
// create/update, which fence on that version (fenceInstanceVersion) in the
// write transaction before the provider row is written.
func (s *OAuth2Providers) sealSecret(providerID, secret string) ([]byte, int64, error) {
	sealer := s.Keyring.ForInstance()
	sealed, err := sealer.SealField(oauth2ProviderSecretAAD(providerID), []byte(secret))
	if err != nil {
		return nil, 0, err
	}
	return sealed, int64(sealer.Version()), nil
}

func (s *OAuth2Providers) create(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, principal domain.PrincipalID, slug string, in OAuth2ProviderInput, out *OAuth2ProviderView) error {
	id, err := newID("oauth2p")
	if err != nil {
		return err
	}
	sealed, dek, err := s.sealSecret(id, in.ClientSecret)
	if err != nil {
		return err
	}
	now := s.now()
	prov := authz.NewOAuth2Provider{
		ID: id, Slug: slug, DisplayName: in.DisplayName, Kind: OAuth2Kind, Profile: in.Profile, Issuer: in.Issuer,
		ClientID: in.ClientID, ClientSecret: sealed, RedirectURI: s.redirectURI(slug),
		Enabled:    in.Enabled,
		DEKVersion: dek, CreatedAt: now, UpdatedAt: now,
	}
	// Writer fence (invariant 7): refuse if a rotate-dek --instance retired the
	// version the secret was sealed under since sealSecret snapshotted it.
	if err := fenceInstanceVersion(ctx, r, p, uint32(dek)); err != nil {
		return err
	}
	if err := az.CreateOAuth2Provider(ctx, prov); err != nil {
		return err
	}
	if err := s.auditChanged(ctx, r, p, principal, id, "created", 0); err != nil {
		return err
	}
	*out = oauth2ProviderView(authz.OAuth2Provider{
		Slug: slug, DisplayName: in.DisplayName, Issuer: in.Issuer, ClientID: in.ClientID, Profile: in.Profile,
		RedirectURI: prov.RedirectURI,
		Enabled:     in.Enabled,
		RowVersion:  1,
	})
	return nil
}

func (s *OAuth2Providers) update(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, p authz.Proof, principal domain.PrincipalID, existing authz.OAuth2Provider, in OAuth2ProviderInput, out *OAuth2ProviderView) error {
	sealed, dek, err := s.sealSecret(existing.ID, in.ClientSecret)
	if err != nil {
		return err
	}
	upd := authz.OAuth2ProviderUpdate{
		ID: existing.ID, DisplayName: in.DisplayName, ClientID: in.ClientID, ClientSecret: sealed,
		RedirectURI: s.redirectURI(existing.Slug),
		Enabled:     in.Enabled,
		DEKVersion:  dek, RowVersion: existing.RowVersion, UpdatedAt: s.now(),
	}
	// Writer fence (invariant 7): refuse a secret sealed under a version a
	// concurrent rotate-dek --instance retired.
	if err := fenceInstanceVersion(ctx, r, p, uint32(dek)); err != nil {
		return err
	}
	swapped, err := az.UpdateOAuth2Provider(ctx, upd)
	if err != nil {
		return err
	}
	if !swapped {
		return ErrProviderRace
	}
	// Any reconfigure changes security material (client, secret, assurance
	// policy, or enabled state; the issuer cannot change), so every session
	// authenticated through this provider is swept in the same tx (A4).
	swept, err := az.DeleteSessionsForOAuth2Provider(ctx, existing.ID)
	if err != nil {
		return err
	}
	if err := s.auditChanged(ctx, r, p, principal, existing.ID, "updated", swept); err != nil {
		return err
	}
	*out = oauth2ProviderView(authz.OAuth2Provider{
		Slug: existing.Slug, DisplayName: in.DisplayName, Issuer: existing.Issuer, ClientID: in.ClientID, Profile: existing.Profile,
		RedirectURI: upd.RedirectURI,
		Enabled:     in.Enabled,
		RowVersion:  existing.RowVersion + 1,
	})
	return nil
}

// Get returns one provider by slug.
func (s *OAuth2Providers) Get(ctx context.Context, actor Actor, slug string) (OAuth2ProviderView, error) {
	var out OAuth2ProviderView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpOAuth2ProviderGet, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		prov, err := az.OAuth2ProviderBySlug(ctx, slug)
		if errors.Is(err, domain.ErrNotFound) {
			return ErrProviderNotFound
		}
		if err != nil {
			return err
		}
		out = oauth2ProviderView(prov)
		return s.auditRead(ctx, r, p, caller.Principal, "get", 1)
	})
	return out, err
}

// List returns every configured provider.
func (s *OAuth2Providers) List(ctx context.Context, actor Actor) ([]OAuth2ProviderView, error) {
	var out []OAuth2ProviderView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpOAuth2ProviderList, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		rows, err := az.ListOAuth2Providers(ctx)
		if err != nil {
			return err
		}
		out = make([]OAuth2ProviderView, 0, len(rows))
		for _, row := range rows {
			out = append(out, oauth2ProviderView(row))
		}
		return s.auditRead(ctx, r, p, caller.Principal, "list", len(rows))
	})
	return out, err
}

// Delete removes a provider and sweeps its federated sessions (A4). Its
// transaction rows and federated sessions cascade on the FK (A14); the sweep
// runs after the provider row is locked so the count is accurate and no mint
// can race the delete.
func (s *OAuth2Providers) Delete(ctx context.Context, actor Actor, slug string) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpOAuth2ProviderDelete, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		prov, err := az.OAuth2ProviderBySlug(ctx, slug)
		if errors.Is(err, domain.ErrNotFound) {
			return ErrProviderNotFound
		}
		if err != nil {
			return err
		}
		// Lock the provider row BEFORE sweeping, so the sweep runs with the row
		// held: a concurrent Phase-C mint guard either already committed (the
		// sweep then catches its session) or blocks on this lock and finds the
		// row gone once we commit (mint refused). The FK cascade (A14) is the
		// atomic backstop; this ordering keeps the sweep count accurate and is
		// race-safe even if FK enforcement were off.
		if err := az.LockOAuth2ProviderForDelete(ctx, prov.ID); errors.Is(err, domain.ErrNotFound) {
			return ErrProviderNotFound
		} else if err != nil {
			return err
		}
		swept, err := az.DeleteSessionsForOAuth2Provider(ctx, prov.ID)
		if err != nil {
			return err
		}
		if err := az.DeleteOAuth2Provider(ctx, prov.ID); err != nil {
			return err
		}
		return s.auditChanged(ctx, r, p, caller.Principal, prov.ID, "deleted", swept)
	})
}

func (s *OAuth2Providers) auditChanged(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID, providerID, change string, swept int64) error {
	e, err := newAuditEvent(ctx, audit.EventOIDCProviderChanged, principal,
		audit.Object{Type: "oauth2_provider", ID: providerID}, audit.OutcomeSuccess, "",
		audit.Payload{"kind": OAuth2Kind, "provider_id": providerID, "change": change, "sessions_swept": int(swept)})
	if err != nil {
		return err
	}
	return r.Audit().InsertInstance(ctx, p, e)
}

func (s *OAuth2Providers) auditRead(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID, query string, count int) error {
	e, err := newAuditEvent(ctx, audit.EventOIDCProviderRead, principal,
		audit.Object{Type: "oauth2_provider"}, audit.OutcomeSuccess, "",
		audit.Payload{"kind": OAuth2Kind, "query": query, "row_count": count})
	if err != nil {
		return err
	}
	return r.Audit().InsertInstance(ctx, p, e)
}
