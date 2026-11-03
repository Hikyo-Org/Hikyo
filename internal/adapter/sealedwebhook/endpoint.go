package sealedwebhook

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
)

// EndpointConfig is one instance-admin configured receiver. It is never
// tenant-supplied: tenants may only bind an adapter to an existing origin.
type EndpointConfig struct {
	ID                   string
	InstanceID           string
	Origin               string
	RecipientKey         string
	AckKey               string
	Generation           int64
	Timeout              time.Duration
	MaxResponseBytes     int64
	ConfirmedFingerprint string
	// Signer is the Ed25519 envelope signing key. Protocol 2 also requires
	// SignerPQ (ML-DSA-65) and AckKeyMLDSA65; both signatures are always
	// required and there is no single-algorithm version 2 target.
	Signer        *sealedhook.Signer
	SignerPQ      *sealedhook.MLDSASigner
	AckKeyMLDSA65 string
	// Protocol is 0 or 1 for the version 1 protocol, or 2.
	Protocol int
}

// sealer is the version-specific half of an endpoint.
type sealer interface {
	Seal(sealedhook.Draft) (sealedhook.Sealed, error)
}

// Endpoint is an activated receiver: its origin, keys, and generation were
// confirmed by an instance admin typing the independently verified
// fingerprint. Trust-on-first-use is refused.
type Endpoint struct {
	id          string
	instanceID  string
	origin      string
	ackKey      sealedhook.PublicKey
	generation  int64
	timeout     time.Duration
	maxResponse int64
	fingerprint string
	sealer      sealer
	protocol    int
	ackPQ       sealedhook.MLDSAPublicKey
}

// ErrFingerprintUnconfirmed refuses activation without an exact match.
var ErrFingerprintUnconfirmed = errors.New("sealed-webhook: endpoint fingerprint was not confirmed; verify it out of band and set the confirmed fingerprint")

// NewEndpoint validates and activates one receiver.
func NewEndpoint(cfg EndpointConfig) (*Endpoint, error) {
	if !sealedhook.ValidID(cfg.ID) || !sealedhook.ValidID(cfg.InstanceID) {
		return nil, errors.New("sealed-webhook: endpoint and instance ids must match [a-z0-9][a-z0-9._-]{0,62}")
	}
	protocol := cfg.Protocol
	if protocol == 0 {
		protocol = 1
	}
	if protocol != 1 && protocol != 2 {
		return nil, fmt.Errorf("sealed-webhook: endpoint %s: unsupported protocol %d", cfg.ID, protocol)
	}
	origin, err := CanonicalOrigin(cfg.Origin)
	if err != nil {
		return nil, fmt.Errorf("endpoint %s: %w", cfg.ID, err)
	}
	if origin != cfg.Origin {
		return nil, fmt.Errorf("sealed-webhook: endpoint %s: origin must be written canonically as %s", cfg.ID, origin)
	}
	ackKey, err := sealedhook.ParsePublicKey(cfg.AckKey)
	if err != nil {
		return nil, fmt.Errorf("endpoint %s: ack key: %w", cfg.ID, err)
	}
	if cfg.Timeout <= 0 || cfg.Timeout > MaxTimeout {
		return nil, fmt.Errorf("sealed-webhook: endpoint %s: timeout must be in (0, %s]", cfg.ID, MaxTimeout)
	}
	if cfg.MaxResponseBytes <= 0 || cfg.MaxResponseBytes > MaxResponseBytes {
		return nil, fmt.Errorf("sealed-webhook: endpoint %s: max response bytes must be in (0, %d]", cfg.ID, MaxResponseBytes)
	}
	if cfg.Signer == nil {
		return nil, errors.New("sealed-webhook: an instance signing key is required")
	}
	e := &Endpoint{
		id: cfg.ID, instanceID: cfg.InstanceID, origin: origin, ackKey: ackKey, generation: cfg.Generation,
		timeout: cfg.Timeout, maxResponse: cfg.MaxResponseBytes, protocol: protocol,
	}
	switch protocol {
	case 1:
		// Version 2 material on a version 1 target would be silently ignored:
		// refuse instead, so a typo cannot leave a target classical.
		if cfg.SignerPQ != nil || cfg.AckKeyMLDSA65 != "" {
			return nil, fmt.Errorf("sealed-webhook: endpoint %s: ML-DSA material needs protocol 2", cfg.ID)
		}
		if e.fingerprint, err = sealedhook.Fingerprint(origin, cfg.RecipientKey, ackKey.String(), cfg.Generation); err != nil {
			return nil, fmt.Errorf("endpoint %s: %w", cfg.ID, err)
		}
	case 2:
		if e.ackPQ, err = sealedhook.ParseMLDSAPublicKey(cfg.AckKeyMLDSA65); err != nil {
			return nil, fmt.Errorf("endpoint %s: ack key (ML-DSA-65): %w", cfg.ID, err)
		}
		if e.fingerprint, err = sealedhook.FingerprintV2(origin, cfg.RecipientKey, ackKey, e.ackPQ, cfg.Generation); err != nil {
			return nil, fmt.Errorf("endpoint %s: %w", cfg.ID, err)
		}
	}
	if strings.TrimSpace(cfg.ConfirmedFingerprint) != e.fingerprint {
		return nil, fmt.Errorf("%w (endpoint %s)", ErrFingerprintUnconfirmed, cfg.ID)
	}
	if protocol == 1 {
		e.sealer, err = sealedhook.NewSealer(cfg.RecipientKey, cfg.Signer)
	} else {
		var signer *sealedhook.SignerV2
		if signer, err = sealedhook.NewSignerV2(cfg.Signer, cfg.SignerPQ); err == nil {
			e.sealer, err = sealedhook.NewSealerV2(cfg.RecipientKey, signer)
		}
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// Protocol is the wire protocol version this endpoint speaks, 1 or 2.
func (e *Endpoint) Protocol() int { return e.protocol }

// ConnectionVersion is reported by TestConnection for this endpoint.
func (e *Endpoint) ConnectionVersion() string {
	if e.protocol == 2 {
		return "sealed-webhook/v2"
	}
	return ConnectionVersion
}

// DestinationID is the ledger destination identity of a namespace under this
// endpoint's fingerprint and protocol.
func (e *Endpoint) DestinationID(namespace string) int64 {
	if e.protocol == 2 {
		return sealedhook.DestinationIDV2(e.fingerprint, namespace)
	}
	return sealedhook.DestinationID(e.fingerprint, namespace)
}

func (e *Endpoint) idempotencyKey(p sealedhook.IdempotencyParts) string {
	if e.protocol == 2 {
		return sealedhook.IdempotencyKeyV2(p)
	}
	return sealedhook.IdempotencyKey(p)
}

// Origin is the canonical configured origin.
func (e *Endpoint) Origin() string { return e.origin }

// ID is the instance-admin endpoint identity.
func (e *Endpoint) ID() string { return e.id }

// Fingerprint is the confirmed trust-boundary fingerprint.
func (e *Endpoint) Fingerprint() string { return e.fingerprint }

// ClientConfig derives the transport configuration.
func (e *Endpoint) ClientConfig() ClientConfig {
	return ClientConfig{Origin: e.origin, AckKey: e.ackKey, AckKeyMLDSA65: e.ackPQ, Protocol: e.protocol, Timeout: e.timeout, MaxResponseBytes: e.maxResponse}
}
