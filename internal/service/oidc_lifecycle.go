package service

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"

	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Directory lifecycle events (human-auth ADR, 2026-11-17 amendment). An OIDC
// provider row with a lifecycle profile receives its directory's signed
// webhooks at POST /api/v1/auth/oidc/{provider}/lifecycle. The raw body is
// verified by the Standard Webhooks reference library BEFORE it is parsed; once
// it verifies, every outcome is a success, so an unknown subject, an ignored
// event type and a replay are indistinguishable.

// MaxLifecycleBodyBytes bounds a lifecycle event body, read before the
// signature is checked. Directory user events are a few KiB.
const MaxLifecycleBodyBytes = 64 << 10

// ErrLifecycleRetry reports a verified event that raced a provider change; the
// sender should redeliver.
var ErrLifecycleRetry = errors.New("service: provider changed while handling a lifecycle event")

const (
	lifecycleUserDeleted = "user.deleted"
	lifecycleUserUpdated = "user.updated"
)

// lifecycleDummy verifies refused slugs against a fixed key, so an unknown,
// disabled or profile-less slug costs the same HMAC as a real one.
var lifecycleDummy = func() *standardwebhooks.Webhook {
	wh, err := standardwebhooks.NewWebhookRaw(make([]byte, 32))
	if err != nil {
		panic(err)
	}
	return wh
}()

// lifecycleHeaders maps a profile's header names to the Standard Webhooks
// names. The clerk profile (Svix) sends `svix-*`; a standard header already
// present wins.
func lifecycleHeaders(profile string, h http.Header) http.Header {
	out := h.Clone()
	if out == nil {
		out = http.Header{}
	}
	if profile != LifecycleProfileClerk {
		return out
	}
	for _, pair := range [][2]string{
		{"Svix-Id", standardwebhooks.HeaderWebhookID},
		{"Svix-Timestamp", standardwebhooks.HeaderWebhookTimestamp},
		{"Svix-Signature", standardwebhooks.HeaderWebhookSignature},
	} {
		if out.Get(pair[1]) == "" {
			if v := out.Get(pair[0]); v != "" {
				out.Set(pair[1], v)
			}
		}
	}
	return out
}

// lifecycleEvent is what the clerk profile reads of an event body. Unknown
// members are ignored.
type lifecycleEvent struct {
	Type string `json:"type"`
	Data struct {
		ID     string `json:"id"`
		Banned *bool  `json:"banned"`
	} `json:"data"`
}

// OIDCLifecycle verifies and applies one directory lifecycle event. A slug that
// is unknown, disabled or has no profile, and a body that fails verification
// (signature, headers, timestamp tolerance), all return domain.ErrUnauthenticated.
// After verification it returns nil for every event: an ignored type, an
// unparseable body, an unknown subject and a replay change nothing. Admission,
// storage and sealing faults propagate.
func (s *Auth) OIDCLifecycle(ctx context.Context, slug string, header http.Header, body []byte) error {
	release, err := s.Admission.Enter(ctx, audit.FromContext(ctx).SourceIP)
	if err != nil {
		return err
	}
	defer release()

	var prov authz.OIDCProvider
	found := false
	err = tx.Read(ctx, s.DB, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		p, e := az.EnabledProviderBySlug(ctx, slug)
		if errors.Is(e, domain.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		prov, found = p, true
		return nil
	})
	if err != nil {
		return err
	}
	wh, profile := lifecycleDummy, LifecycleProfileClerk
	usable := found && prov.LifecycleProfile != nil && len(prov.LifecycleSecret) > 0
	if usable {
		wh, profile, usable = s.lifecycleVerifier(ctx, prov)
		if !usable {
			wh, profile = lifecycleDummy, LifecycleProfileClerk
		}
	}
	// Verify unconditionally, so the refused slug classes do the same work.
	if err := wh.Verify(body, lifecycleHeaders(profile, header)); err != nil || !usable {
		return domain.ErrUnauthenticated
	}

	var event lifecycleEvent
	if err := json.Unmarshal(body, &event); err != nil || event.Data.ID == "" {
		return nil
	}
	banned := event.Type == lifecycleUserUpdated && event.Data.Banned != nil && *event.Data.Banned
	if event.Type != lifecycleUserDeleted && !banned {
		return nil
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		// The provider may have been disabled or reconfigured since the read.
		cause, err := s.revalidateProvider(ctx, az, prov)
		if err != nil {
			return err
		}
		if cause != "" {
			// The signature already verified, so a 401 would drop a genuine event
			// and break "204 or retryable"; the directory redelivers on 503.
			return ErrLifecycleRetry
		}
		return s.applyDirectoryEvent(ctx, az, prov, event.Data.ID, banned)
	})
}

// lifecycleVerifier opens the provider's sealed signing secret. An unreadable
// record is a server fault logged here and answered like any refusal.
func (s *Auth) lifecycleVerifier(ctx context.Context, prov authz.OIDCProvider) (*standardwebhooks.Webhook, string, bool) {
	secret, err := s.Keyring.ForInstance().OpenField(providerLifecycleAAD(prov.ID), prov.LifecycleSecret)
	if err != nil {
		s.logFault(ctx, "opening an OIDC lifecycle secret failed", err, prov.ID)
		return nil, "", false
	}
	defer crypto.Zero(secret)
	wh, err := standardwebhooks.NewWebhook(string(secret))
	if err != nil {
		s.logFault(ctx, "a stored OIDC lifecycle secret is malformed", err, prov.ID)
		return nil, "", false
	}
	return wh, *prov.LifecycleProfile, true
}

// applyDirectoryEvent acts on a verified, relevant event. The identity is
// resolved by (oidc, the row's pinned issuer, subject), never by provider id,
// and an identity from an earlier credential epoch counts: removing one is
// always safe. Sessions are swept BEFORE the identity is removed, since the
// credential-establishment evidence join needs the identity row.
func (s *Auth) applyDirectoryEvent(ctx context.Context, az *authz.TxAuthorizer, prov authz.OIDCProvider, subject string, ban bool) error {
	identity, err := az.ExternalIdentityByKey(ctx, OIDCKind, prov.Issuer, subject)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	account, err := az.AccountByID(ctx, identity.AccountID)
	if err != nil {
		return err
	}
	if err := az.LockTargetPrincipal(ctx, account.PrincipalID); err != nil {
		return err
	}
	swept, err := az.SweepSessionsForPrincipalAndIssuer(ctx, string(account.PrincipalID), prov.Issuer)
	if err != nil {
		return err
	}
	// Developer credentials minted from a swept session, or through any provider
	// row of this issuer, die with it; ones minted from other sessions survive.
	providers, err := az.ListProviders(ctx)
	if err != nil {
		return err
	}
	var providerIDs []string
	for _, p := range providers {
		if p.Kind == OIDCKind && p.Issuer == prov.Issuer {
			providerIDs = append(providerIDs, p.ID)
		}
	}
	cause := "directory-" + lifecycleUserDeleted
	if ban {
		cause = "directory-banned"
	}
	if err := revokeDeveloperBySessionsBySystem(ctx, az, account.PrincipalID, swept, providerIDs, cause, s.now()); err != nil {
		return err
	}
	if ban {
		if len(swept) == 0 {
			return nil
		}
		return recordSystemEvent(ctx, az, audit.EventSessionsSweptByDirectory, audit.Object{Type: "external_identity", ID: identity.ID},
			audit.Payload{"account_id": account.ID, "identity_id": identity.ID, "provider_id": prov.ID,
				"lifecycle_event": "user.updated.banned", "sessions_swept": len(swept)})
	}
	if err := removeIdentityLink(ctx, az, account, identity.ID, true, s.now()); err != nil {
		return err
	}
	if err := recordSystemEvent(ctx, az, audit.EventIdentityUnlinkedByDirectory, audit.Object{Type: "external_identity", ID: identity.ID},
		audit.Payload{"kind": identity.Kind, "account_id": account.ID, "identity_id": identity.ID, "provider_id": prov.ID,
			"lifecycle_event": lifecycleUserDeleted, "sessions_swept": len(swept)}); err != nil {
		return err
	}
	// An account left with no login credential is in the same state as an
	// unclaimed invitation; say so. The interactive last-credential refusal
	// does not apply: the identity no longer exists upstream.
	switch err := s.assertLoginCredential(ctx, az, account.ID, ""); {
	case errors.Is(err, ErrLastCredential):
		// assertLoginCredential's passkey floor treats one passkey as none;
		// here any passkey is still a way in.
		passkeys, err := az.WebAuthnCredentialsForAccount(ctx, account.ID)
		if err != nil || len(passkeys) > 0 {
			return err
		}
		return recordSystemEvent(ctx, az, audit.EventAccountNoLoginCredential, audit.Object{Type: "account", ID: account.ID},
			audit.Payload{"account_id": account.ID, "cause": "directory-user-deleted"})
	default:
		return err
	}
}

// recordSystemEvent writes an instance-trail authentication event attributed to
// the system: the directory caused it, no Hikyo principal did.
func recordSystemEvent(ctx context.Context, az *authz.TxAuthorizer, typ audit.EventType, obj audit.Object, payload audit.Payload) error {
	ev, err := newAuditEvent(ctx, typ, "", obj, audit.OutcomeSuccess, "", payload)
	if err != nil {
		return err
	}
	ev.Actor.Class = audit.ActorSystem
	return az.RecordAuthEvent(ctx, ev)
}
