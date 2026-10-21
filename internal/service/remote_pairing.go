package service

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/pairing"
	"github.com/Hikyo-Org/hikyo/internal/remotefetch"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

const pairingRequestLifetime = time.Hour
const pairingExchangeLifetime = 2 * time.Minute

var ErrPairingInvalid = fmt.Errorf("%w: pairing invitation or proof is not valid", domain.ErrUnauthenticated)

const PairingOriginConfigurationMessage = "Set this instance's public HTTPS address in instance settings before creating an invitation."
const PairingTLSConfigurationMessage = "The public TLS identity could not be verified. Check this instance's public HTTPS address and certificate, or set its pairing TLS fingerprint in instance settings."

// These closed, post-authorization refusals expose operator remedies without
// disclosing resolver errors, certificate contents or proxy credentials.
var ErrPairingOriginConfiguration = pairingIdentityRefusal{origin: true}
var ErrPairingTLSConfiguration = pairingIdentityRefusal{}

type pairingIdentityRefusal struct{ origin bool }

func (e pairingIdentityRefusal) Error() string { return "service: " + e.SafeDetail() }
func (pairingIdentityRefusal) Unwrap() error   { return domain.ErrInvalid }
func (e pairingIdentityRefusal) SafeDetail() string {
	if e.origin {
		return PairingOriginConfigurationMessage
	}
	return PairingTLSConfigurationMessage
}

// RemotePairings owns only directory pairing. It never consents to workspace
// origins and never issues a human workspace session.
type RemotePairings struct {
	DB      *store.DB
	Keyring *crypto.Keyring
	Remotes *Remotes
	Now     func() time.Time
	// Serving identity is captured from the active application generation,
	// never from invitation input or request headers. Resolution may perform a
	// bounded TLS handshake, so CreateInvite authorizes before invoking it.
	Origin             string
	ConfigurationError string
	ResolveSPKIPin     func(context.Context) (string, error)
}

func (s *RemotePairings) now() time.Time { return nowOr(s.Now) }

type PreparePairingRequest struct {
	Phrase    *string
	WordCount *int
}

type PairingPrepared struct {
	Phrase, RequesterState string
	ExpiresAt              time.Time
}
type PairingInviteView struct {
	ID, Label, Origin, SPKIPin                                       string
	CreatedAt, ExpiresAt, CredentialExpiresAt, RevokedAt, ConsumedAt time.Time
}
type CreatePairingInvite struct {
	Phrase, Label                      string
	InviteLifetime, CredentialLifetime time.Duration
}
type PairingInviteList struct {
	Items              []PairingInviteView
	Origin             string
	ConfigurationError string
}
type PairingInviteCreated struct {
	Invite       PairingInviteView
	InviteBundle string
}
type PairingStarted struct{ Exchange, ServerMessage string }
type PairingFinished struct {
	ServerMessage, Credential string
	Connection                ConnectionView
}
type PairRemoteRequest struct{ Name, RequesterState, InviteBundle string }
type requesterCapsule struct {
	Version    int       `json:"version"`
	Principal  string    `json:"principal"`
	Epoch      int64     `json:"epoch"`
	ExpiresAt  time.Time `json:"expires_at"`
	Phrase     string    `json:"phrase"`
	PrivateKey []byte    `json:"private_key"`
}
type invitationBundle struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Origin     string    `json:"origin"`
	SPKIPin    string    `json:"spki_pin"`
	Capability string    `json:"capability"`
	ExpiresAt  time.Time `json:"expires_at"`
}
type invitationSecret struct {
	Phrase string `json:"phrase"`
	Salt   []byte `json:"salt"`
}
type exchangeSecret struct {
	Enrollment pairing.Enrollment `json:"enrollment"`
	Secret     []byte             `json:"secret"`
	PublicJWK  string             `json:"public_jwk"`
	Thumbprint string             `json:"thumbprint"`
	Nonce      string             `json:"nonce"`
}
type pairingChallenge struct {
	Context         pairing.Context `json:"context"`
	ServerEphemeral []byte          `json:"server_ephemeral"`
	Nonce           string          `json:"nonce"`
}
type pairingClientProof struct {
	ClientEphemeral []byte `json:"client_ephemeral"`
	ClientProof     []byte `json:"client_proof"`
}

func pairingAAD(table, id, field string) crypto.InstanceFieldAAD {
	return crypto.InstanceFieldAAD{OwnerTable: table, OwnerRowID: id, FieldTag: field}
}
func decodePairingJSON(raw []byte, limit int, out any) error {
	if len(raw) == 0 || len(raw) > limit {
		return ErrPairingInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return ErrPairingInvalid
	}
	if d.Decode(new(any)) != io.EOF {
		return ErrPairingInvalid
	}
	return nil
}
func pairingInviteView(i authz.RemotePairingInvite) PairingInviteView {
	return PairingInviteView{ID: i.ID, Label: i.Label, Origin: i.Origin, SPKIPin: i.SPKIPin, CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt, CredentialExpiresAt: i.CredentialExpiresAt, RevokedAt: i.RevokedAt, ConsumedAt: i.ConsumedAt}
}
func pairingAudit(ctx context.Context, az *authz.TxAuthorizer, kind audit.EventType, actor domain.PrincipalID, id, exchange string) error {
	e, err := domainEvent(ctx, kind, actor, audit.Object{Type: "remote_pairing_invite", ID: id}, audit.Payload{"invite_id": id, "exchange_id": exchange})
	if err != nil {
		return err
	}
	return az.RecordAuthEvent(ctx, e)
}
func pairingFailure(ctx context.Context, az *authz.TxAuthorizer, id, exchange string) error {
	e, err := newAuditEvent(ctx, audit.EventRemotePairingFailed, "", audit.Object{Type: "remote_pairing_invite", ID: id}, audit.OutcomeFailure, "", audit.Payload{"invite_id": id, "exchange_id": exchange})
	if err != nil {
		return err
	}
	az.CaptureAudit(audit.TrailInstance, domain.Scope{}, e)
	return ErrPairingInvalid
}

func (s *RemotePairings) Prepare(ctx context.Context, actor Actor, req PreparePairingRequest) (PairingPrepared, error) {
	now := s.now()
	if req.Phrase != nil && req.WordCount != nil {
		return PairingPrepared{}, fmt.Errorf("%w: choose a custom secret or generated word count", domain.ErrInvalid)
	}
	var phrase string
	var err error
	if req.Phrase != nil {
		phrase = *req.Phrase
		if err := pairing.ValidatePhrase(phrase); err != nil {
			return PairingPrepared{}, fmt.Errorf("%w: pairing secret must be nonempty valid UTF-8 and at most 4096 bytes", domain.ErrInvalid)
		}
	} else if req.WordCount != nil {
		if *req.WordCount < 1 || *req.WordCount > 128 {
			return PairingPrepared{}, fmt.Errorf("%w: generated word count must be between 1 and 128", domain.ErrInvalid)
		}
		phrase, err = pairing.GeneratePhraseWords(*req.WordCount)
	} else {
		phrase, err = pairing.GeneratePhrase()
	}
	if err != nil {
		return PairingPrepared{}, err
	}
	key, err := pairing.NewSigningKey()
	if err != nil {
		return PairingPrepared{}, err
	}
	defer crypto.Zero(key)
	expires := store.CanonTime(now.Add(pairingRequestLifetime))
	var out PairingPrepared
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpRemotePairingPrepare, domain.Scope{}, now)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(requesterCapsule{Version: 1, Principal: string(caller.Principal), Epoch: epoch, ExpiresAt: expires, Phrase: phrase, PrivateKey: key})
		if err != nil {
			return err
		}
		defer crypto.Zero(raw)
		sealer := s.Keyring.ForInstance()
		sealed, err := sealer.SealField(pairingAAD("remote_pairing_request", string(caller.Principal), "requester"), raw)
		if err != nil {
			return err
		}
		if err := fenceInstance(ctx, r, p, sealer); err != nil {
			return err
		}
		out = PairingPrepared{Phrase: phrase, RequesterState: base64.RawURLEncoding.EncodeToString(sealed), ExpiresAt: expires}
		return pairingAudit(ctx, az, audit.EventRemotePairingPrepared, caller.Principal, "", "")
	})
	if err != nil {
		return PairingPrepared{}, err
	}
	return out, nil
}
func (s *RemotePairings) CreateInvite(ctx context.Context, actor Actor, req CreatePairingInvite) (PairingInviteCreated, error) {
	now := s.now()
	phrase := req.Phrase
	if err := pairing.ValidatePhrase(phrase); err != nil {
		return PairingInviteCreated{}, fmt.Errorf("%w: invalid pairing phrase", domain.ErrInvalid)
	}
	if err := checkName("invitation label", req.Label); err != nil {
		return PairingInviteCreated{}, err
	}
	if req.InviteLifetime <= 0 || req.InviteLifetime > time.Hour || req.CredentialLifetime <= req.InviteLifetime || req.CredentialLifetime > 365*24*time.Hour {
		return PairingInviteCreated{}, fmt.Errorf("%w: invalid finite pairing lifetimes", domain.ErrInvalid)
	}
	// Do not open an outbound connection for an anonymous or unprivileged
	// caller. Release this transaction before DNS/TLS; reauthorize the write
	// after resolution so a revoked grant cannot survive that wait.
	if err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, _, err := authorize(ctx, az, actor, authz.OpRemotePairingCreate, domain.Scope{}, s.now())
		return err
	}); err != nil {
		return PairingInviteCreated{}, err
	}
	if s.ConfigurationError != "" {
		return PairingInviteCreated{}, ErrPairingOriginConfiguration
	}
	origin, err := remotefetch.CanonicalRemoteURL(s.Origin)
	if err != nil {
		return PairingInviteCreated{}, ErrPairingOriginConfiguration
	}
	if s.ResolveSPKIPin == nil {
		return PairingInviteCreated{}, ErrPairingTLSConfiguration
	}
	spkiPin, err := s.ResolveSPKIPin(ctx)
	if err != nil {
		return PairingInviteCreated{}, err
	}
	pin, err := base64.StdEncoding.Strict().DecodeString(spkiPin)
	if err != nil || len(pin) != 32 {
		return PairingInviteCreated{}, ErrPairingTLSConfiguration
	}
	// Lifetimes start after discovery, not before a possibly slow handshake.
	now = s.now()
	id, err := newID("pin")
	if err != nil {
		return PairingInviteCreated{}, err
	}
	capability, verifier, err := crypto.NewArtifact(crypto.ArtifactHandoffCode)
	if err != nil {
		return PairingInviteCreated{}, err
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return PairingInviteCreated{}, err
	}
	secret, err := json.Marshal(invitationSecret{Phrase: phrase, Salt: salt})
	if err != nil {
		return PairingInviteCreated{}, err
	}
	defer crypto.Zero(secret)
	sealer := s.Keyring.ForInstance()
	sealed, err := sealer.SealField(pairingAAD("remote_pairing_invites", id, "pake"), secret)
	if err != nil {
		return PairingInviteCreated{}, err
	}
	var out PairingInviteCreated
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, _, err := authorize(ctx, az, actor, authz.OpRemotePairingCreate, domain.Scope{}, now)
		if err != nil {
			return err
		}
		if err := az.LockInstanceIdentityRow(ctx); err != nil {
			return err
		}
		if err := az.PruneRemotePairingState(ctx, now); err != nil {
			return err
		}
		n, err := az.CountLiveRemotePairingInvites(ctx, now)
		if err != nil {
			return err
		}
		if n >= 100 {
			return domain.ErrLimitExceeded
		}
		if err := az.LockCredentialPolicy(ctx); err != nil {
			return err
		}
		policy, err := az.CredentialPolicy(ctx)
		if err != nil {
			return err
		}
		_, credentialExpires, _, err := resolveLifetime(MintRequest{Lifetime: req.CredentialLifetime}, policy, now)
		if err != nil {
			return err
		}
		expires := store.CanonTime(now.Add(req.InviteLifetime))
		if !credentialExpires.After(expires) {
			return fmt.Errorf("%w: invitation must expire before its credential", domain.ErrInvalid)
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if err := az.AssertActiveInstanceDEKVersion(ctx, int64(sealer.Version())); err != nil {
			return err
		}
		i := authz.RemotePairingInvite{ID: id, Label: req.Label, Origin: origin, SPKIPin: spkiPin, CapabilityVerifier: verifier, PAKESealed: sealed, CredentialEpoch: epoch, CreatedBy: caller.Principal, CreatedAt: now, ExpiresAt: expires, CredentialExpiresAt: credentialExpires}
		if err := az.CreateRemotePairingInvite(ctx, i); err != nil {
			return err
		}
		bundle, err := json.Marshal(invitationBundle{Version: 1, ID: id, Origin: origin, SPKIPin: spkiPin, Capability: capability, ExpiresAt: expires})
		if err != nil {
			return err
		}
		out = PairingInviteCreated{Invite: pairingInviteView(i), InviteBundle: base64.RawURLEncoding.EncodeToString(bundle)}
		return pairingAudit(ctx, az, audit.EventRemotePairingInviteCreated, caller.Principal, id, "")
	})
	if err != nil {
		return PairingInviteCreated{}, err
	}
	return out, nil
}
func (s *RemotePairings) ListInvites(ctx context.Context, actor Actor) (PairingInviteList, error) {
	out := []PairingInviteView{}
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, _, err := authorize(ctx, az, actor, authz.OpRemotePairingList, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		out = []PairingInviteView{}
		items, err := az.ListRemotePairingInvites(ctx)
		if err != nil {
			return err
		}
		for _, i := range items {
			out = append(out, pairingInviteView(i))
		}
		return pairingAudit(ctx, az, audit.EventRemotePairingInvitesListed, caller.Principal, "", "")
	})
	if err != nil {
		return PairingInviteList{}, err
	}
	configurationError := s.ConfigurationError
	if _, err := remotefetch.CanonicalRemoteURL(s.Origin); err != nil && configurationError == "" {
		configurationError = PairingOriginConfigurationMessage
	}
	return PairingInviteList{Items: out, Origin: s.Origin, ConfigurationError: configurationError}, nil
}
func (s *RemotePairings) RevokeInvite(ctx context.Context, actor Actor, id string) error {
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, _, err := authorize(ctx, az, actor, authz.OpRemotePairingRevoke, domain.Scope{}, s.now())
		if err != nil {
			return err
		}
		ok, err := az.RevokeRemotePairingInvite(ctx, id, s.now())
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrNotFound
		}
		return pairingAudit(ctx, az, audit.EventRemotePairingInviteRevoked, caller.Principal, id, "")
	})
}

func (s *RemotePairings) Start(ctx context.Context, capability, publicJWK string) (PairingStarted, error) {
	if crypto.ParseArtifact(capability, crypto.ArtifactHandoffCode) != nil {
		return PairingStarted{}, ErrPairingInvalid
	}
	key, err := pairing.ParsePublicJWK([]byte(publicJWK))
	if err != nil {
		return PairingStarted{}, ErrPairingInvalid
	}
	jkt, err := pairing.PublicKeyThumbprint(key)
	if err != nil {
		return PairingStarted{}, ErrPairingInvalid
	}
	now := s.now()
	id, _, err := crypto.NewArtifact(crypto.ArtifactHandoffState)
	if err != nil {
		return PairingStarted{}, err
	}
	var out PairingStarted
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		i, err := az.RemotePairingInviteByCapability(ctx, crypto.ArtifactVerifier(capability))
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return pairingFailure(ctx, az, "", "")
			}
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if !i.Live(now, epoch) {
			return pairingFailure(ctx, az, i.ID, "")
		}
		if err := az.LockInstanceIdentityRow(ctx); err != nil {
			return err
		}
		if err := az.PruneRemotePairingState(ctx, now); err != nil {
			return err
		}
		n, err := az.CountLiveRemotePairingExchanges(ctx, i.ID, now)
		if err != nil {
			return err
		}
		if n >= 4 {
			return domain.ErrLimitExceeded
		}
		accepted, err := az.ClaimRemotePairingAttempt(ctx, i.ID, now)
		if err != nil {
			return err
		}
		if !accepted {
			return pairingFailure(ctx, az, i.ID, "")
		}
		sealer := s.Keyring.ForInstance()
		raw, err := sealer.OpenField(pairingAAD("remote_pairing_invites", i.ID, "pake"), i.PAKESealed)
		if err != nil {
			return err
		}
		defer crypto.Zero(raw)
		var secret invitationSecret
		if err := decodePairingJSON(raw, 32768, &secret); err != nil {
			return err
		}
		context := pairing.Context{InviteID: i.ID, Origin: i.Origin, SPKIPin: i.SPKIPin, RequesterThumbprint: jkt, InviteSalt: secret.Salt}
		enrollment, err := pairing.EnrollPhrase(secret.Phrase, context)
		if err != nil {
			return err
		}
		defer crypto.Zero(enrollment.Verifier)
		challenge, err := pairing.StartServer(enrollment)
		if err != nil {
			return err
		}
		defer crypto.Zero(challenge.Secret)
		nonce, _, err := crypto.NewArtifact(crypto.ArtifactHandoffState)
		if err != nil {
			return err
		}
		state, err := json.Marshal(exchangeSecret{Enrollment: enrollment, Secret: challenge.Secret, PublicJWK: publicJWK, Thumbprint: jkt, Nonce: nonce})
		if err != nil {
			return err
		}
		defer crypto.Zero(state)
		sealed, err := sealer.SealField(pairingAAD("remote_pairing_exchanges", id, "state"), state)
		if err != nil {
			return err
		}
		if err := az.AssertActiveInstanceDEKVersion(ctx, int64(sealer.Version())); err != nil {
			return err
		}
		expires := minTime(now.Add(pairingExchangeLifetime), i.ExpiresAt)
		if err := az.CreateRemotePairingExchange(ctx, authz.RemotePairingExchange{ID: id, InviteID: i.ID, StateSealed: sealed, CredentialEpoch: epoch, CreatedAt: now, ExpiresAt: expires}); err != nil {
			return err
		}
		message, err := json.Marshal(pairingChallenge{Context: context, ServerEphemeral: challenge.Ephemeral, Nonce: nonce})
		if err != nil {
			return err
		}
		out = PairingStarted{Exchange: id, ServerMessage: string(message)}
		return pairingAudit(ctx, az, audit.EventRemotePairingStarted, "", i.ID, id)
	})
	if err != nil {
		return PairingStarted{}, err
	}
	return out, nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Finish consumes its exchange even for an invalid PAKE proof. Returning an
// authentication failure from inside the transaction would roll that update
// back and allow unlimited guesses against the same admitted attempt.
func (s *RemotePairings) Finish(ctx context.Context, exchange, message, proof string) (PairingFinished, error) {
	now := s.now()
	var out PairingFinished
	failed := false
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		failed = false
		e, err := az.RemotePairingExchangeByID(ctx, exchange)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return pairingFailure(ctx, az, "", "")
			}
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if e.CredentialEpoch != epoch || !e.ConsumedAt.IsZero() || !now.Before(e.ExpiresAt) {
			return pairingFailure(ctx, az, e.InviteID, e.ID)
		}
		accepted, err := az.ConsumeRemotePairingExchange(ctx, e.ID, now, epoch)
		if err != nil {
			return err
		}
		if !accepted {
			return pairingFailure(ctx, az, e.InviteID, e.ID)
		}
		fail := func() error {
			failed = true
			event, err := newAuditEvent(ctx, audit.EventRemotePairingFailed, "", audit.Object{Type: "remote_pairing_invite", ID: e.InviteID}, audit.OutcomeFailure, "", audit.Payload{"invite_id": e.InviteID, "exchange_id": e.ID})
			if err != nil {
				return err
			}
			return az.RecordAuthEvent(ctx, event)
		}
		i, err := az.RemotePairingInviteByID(ctx, e.InviteID)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return fail()
			}
			return err
		}
		if !i.Live(now, epoch) {
			return fail()
		}
		raw, err := s.Keyring.ForInstance().OpenField(pairingAAD("remote_pairing_exchanges", e.ID, "state"), e.StateSealed)
		if err != nil {
			return err
		}
		defer crypto.Zero(raw)
		var state exchangeSecret
		defer func() { crypto.Zero(state.Secret); crypto.Zero(state.Enrollment.Verifier) }()
		if err := decodePairingJSON(raw, 16384, &state); err != nil {
			return err
		}
		var client pairingClientProof
		if err := decodePairingJSON([]byte(message), 4096, &client); err != nil {
			return fail()
		}
		if _, err := pairing.VerifyDPoP(proof, pairing.DPoPExpectation{Method: "POST", URL: i.Origin + "/api/v1/auth/pairing/finish", AccessToken: e.ID, Nonce: state.Nonce, Thumbprint: state.Thumbprint, Now: now}); err != nil {
			return fail()
		}
		m2, err := pairing.FinishServer(state.Enrollment, state.Secret, client.ClientEphemeral, client.ClientProof)
		if err != nil {
			return fail()
		}
		now = s.now()
		if !i.Live(now, epoch) || !now.Before(e.ExpiresAt) {
			return fail()
		}
		accepted, err = az.ConsumeRemotePairingInvite(ctx, i.ID, now, epoch)
		if err != nil {
			return err
		}
		if !accepted {
			return fail()
		}
		connID, err := newID("icn")
		if err != nil {
			return err
		}
		principalID, err := newID("mch")
		if err != nil {
			return err
		}
		grantID, err := newID("grn")
		if err != nil {
			return err
		}
		value, verifier, err := crypto.NewArtifact(crypto.ArtifactInstanceConn)
		if err != nil {
			return err
		}
		hint, err := prefixHint(value)
		if err != nil {
			return err
		}
		if err := az.LockCredentialPolicy(ctx); err != nil {
			return err
		}
		policy, err := az.CredentialPolicy(ctx)
		if err != nil {
			return err
		}
		_, expiry, _, err := resolveLifetime(MintRequest{Lifetime: i.CredentialExpiresAt.Sub(now)}, policy, now)
		if err != nil {
			return err
		}
		expiry = minTime(expiry, i.CredentialExpiresAt)
		pid := domain.PrincipalID(principalID)
		if err := az.CreateMachinePrincipal(ctx, pid, domain.ClassInstanceConn, now); err != nil {
			return err
		}
		if err := az.CreateGrant(ctx, grantID, pid, domain.Grant{Capability: domain.CapInstanceDirector, Scope: domain.Scope{}}, now); err != nil {
			return err
		}
		if err := az.MintInstanceConnection(ctx, authz.NewInstanceConnection{ID: connID, PrincipalID: pid, Label: i.Label, Kind: domain.CredentialHikyoToken, Verifier: verifier, PrefixHint: hint, Lifetime: domain.LifetimeFinite, ExpiresAt: expiry, CredentialEpoch: epoch, CreatedAt: now, CreatedBy: i.CreatedBy}); err != nil {
			return err
		}
		if err := az.BindRemotePairedConnection(ctx, authz.RemotePairedConnection{ConnectionID: connID, PublicJWK: state.PublicJWK, Thumbprint: state.Thumbprint, Origin: i.Origin}); err != nil {
			return err
		}
		out = PairingFinished{ServerMessage: base64.StdEncoding.EncodeToString(m2), Credential: value, Connection: ConnectionView{ID: connID, Principal: pid, Label: i.Label, Kind: domain.CredentialHikyoToken, PrefixHint: hint, Lifetime: domain.LifetimeFinite, ExpiresAt: expiry, CreatedAt: now, CreatedBy: i.CreatedBy, Live: true}}
		if err := pairingAudit(ctx, az, audit.EventRemotePairingFinished, pid, i.ID, e.ID); err != nil {
			return err
		}
		event, err := domainEvent(ctx, audit.EventRemoteCredentialMinted, i.CreatedBy, audit.Object{Type: "instance_connection", ID: connID}, audit.Payload{"connection_id": connID, "target_principal": principalID, "label": i.Label, "credential_kind": "dpop", "lifetime": string(domain.LifetimeFinite), "clamped": expiry.Before(i.CredentialExpiresAt), "expires_at": expiry.Format(time.RFC3339)})
		if err != nil {
			return err
		}
		return az.RecordAuthEvent(ctx, event)
	})
	if err != nil {
		return PairingFinished{}, err
	}
	if failed {
		return PairingFinished{}, ErrPairingInvalid
	}
	return out, nil
}

func (s *RemotePairings) Pair(ctx context.Context, actor Actor, req PairRemoteRequest) (RemoteView, error) {
	if err := checkName("remote name", req.Name); err != nil {
		return RemoteView{}, err
	}
	if len(req.InviteBundle) > 8192 || len(req.RequesterState) > 65536 {
		return RemoteView{}, ErrPairingInvalid
	}
	bundleBytes, err := base64.RawURLEncoding.Strict().DecodeString(req.InviteBundle)
	if err != nil {
		return RemoteView{}, ErrPairingInvalid
	}
	var bundle invitationBundle
	if err := decodePairingJSON(bundleBytes, 6144, &bundle); err != nil {
		return RemoteView{}, err
	}
	now := s.now()
	if bundle.Version != 1 || bundle.ID == "" || !now.Before(bundle.ExpiresAt) || crypto.ParseArtifact(bundle.Capability, crypto.ArtifactHandoffCode) != nil {
		return RemoteView{}, ErrPairingInvalid
	}
	canonical, err := remotefetch.CanonicalRemoteURL(bundle.Origin)
	if err != nil || canonical != bundle.Origin {
		return RemoteView{}, ErrPairingInvalid
	}
	pin, err := base64.StdEncoding.Strict().DecodeString(bundle.SPKIPin)
	if err != nil || len(pin) != 32 {
		return RemoteView{}, ErrPairingInvalid
	}
	var capsule requesterCapsule
	defer func() { crypto.Zero(capsule.PrivateKey) }()
	err = tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		caller, _, err := authorize(ctx, az, actor, authz.OpRemotePairingComplete, domain.Scope{}, now)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		sealed, err := base64.RawURLEncoding.Strict().DecodeString(req.RequesterState)
		if err != nil {
			return ErrPairingInvalid
		}
		raw, err := s.Keyring.ForInstance().OpenField(pairingAAD("remote_pairing_request", string(caller.Principal), "requester"), sealed)
		if err != nil {
			return ErrPairingInvalid
		}
		defer crypto.Zero(raw)
		if err := decodePairingJSON(raw, 32768, &capsule); err != nil {
			return err
		}
		if capsule.Version != 1 || capsule.Principal != string(caller.Principal) || capsule.Epoch != epoch || !now.Before(capsule.ExpiresAt) || len(capsule.PrivateKey) != ed25519.PrivateKeySize {
			return ErrPairingInvalid
		}
		return nil
	})
	if err != nil {
		return RemoteView{}, err
	}
	if s.Remotes == nil || s.Remotes.Fetch == nil {
		return RemoteView{}, errors.New("service: pairing outbound client is not wired")
	}
	private := ed25519.PrivateKey(capsule.PrivateKey)
	public, err := pairing.PublicJWK(private)
	if err != nil {
		return RemoteView{}, err
	}
	jkt, err := pairing.PublicKeyThumbprint(public)
	if err != nil {
		return RemoteView{}, err
	}
	publicJSON, err := json.Marshal(public)
	if err != nil {
		return RemoteView{}, err
	}
	var started struct {
		Exchange      string `json:"exchange"`
		ServerMessage string `json:"server_message"`
	}
	if err := s.Remotes.Fetch.PairingStart(ctx, bundle.Origin, bundle.SPKIPin, bundle.Capability, string(publicJSON), &started); err != nil {
		return RemoteView{}, err
	}
	var challenge pairingChallenge
	if err := decodePairingJSON([]byte(started.ServerMessage), 8192, &challenge); err != nil {
		return RemoteView{}, err
	}
	expected := challenge.Context
	expected.InviteID = bundle.ID
	expected.Origin = bundle.Origin
	expected.SPKIPin = bundle.SPKIPin
	expected.RequesterThumbprint = jkt
	if challenge.Context.InviteID != bundle.ID || challenge.Context.Origin != bundle.Origin || challenge.Context.SPKIPin != bundle.SPKIPin || challenge.Context.RequesterThumbprint != jkt || challenge.Nonce == "" {
		return RemoteView{}, ErrPairingInvalid
	}
	proofs, err := pairing.ClientProof(capsule.Phrase, expected, challenge.ServerEphemeral)
	if err != nil {
		return RemoteView{}, ErrPairingInvalid
	}
	message, err := json.Marshal(pairingClientProof{ClientEphemeral: proofs.ClientEphemeral, ClientProof: proofs.ClientProof})
	if err != nil {
		return RemoteView{}, err
	}
	dpop, err := pairing.GenerateDPoP(private, pairing.DPoPClaims{Method: "POST", URL: bundle.Origin + "/api/v1/auth/pairing/finish", AccessToken: started.Exchange, Nonce: challenge.Nonce, IssuedAt: s.now()})
	if err != nil {
		return RemoteView{}, err
	}
	var finished struct {
		ServerMessage string          `json:"server_message"`
		Credential    string          `json:"credential"`
		Connection    json.RawMessage `json:"connection"`
	}
	if !s.now().Before(capsule.ExpiresAt) {
		return RemoteView{}, ErrPairingInvalid
	}
	if err := s.Remotes.Fetch.PairingFinish(ctx, bundle.Origin, bundle.SPKIPin, started.Exchange, string(message), dpop, &finished); err != nil {
		return RemoteView{}, err
	}
	m2, err := base64.StdEncoding.Strict().DecodeString(finished.ServerMessage)
	if err != nil || pairing.VerifyServerProof(proofs.ExpectedServerProof, m2) != nil {
		return RemoteView{}, ErrPairingInvalid
	}
	if crypto.ParseArtifact(finished.Credential, crypto.ArtifactInstanceConn) != nil {
		return RemoteView{}, ErrPairingInvalid
	}
	stored, err := remotefetch.EncodePairedCredential(finished.Credential, private)
	if err != nil {
		return RemoteView{}, err
	}
	return s.Remotes.addRemote(ctx, actor, req.Name, bundle.Origin, bundle.SPKIPin, stored, authz.OpRemotePairingComplete, func(ctx context.Context, az *authz.TxAuthorizer) error {
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if epoch != capsule.Epoch || !s.now().Before(capsule.ExpiresAt) {
			return ErrPairingInvalid
		}
		return nil
	})
}
