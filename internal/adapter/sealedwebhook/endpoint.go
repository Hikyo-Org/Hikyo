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
	Signer               *sealedhook.Signer
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
	sealer      *sealedhook.Sealer
}

// ErrFingerprintUnconfirmed refuses activation without an exact match.
var ErrFingerprintUnconfirmed = errors.New("sealed-webhook: endpoint fingerprint was not confirmed; verify it out of band and set the confirmed fingerprint")

// NewEndpoint validates and activates one receiver.
func NewEndpoint(cfg EndpointConfig) (*Endpoint, error) {
	if !sealedhook.ValidID(cfg.ID) || !sealedhook.ValidID(cfg.InstanceID) {
		return nil, errors.New("sealed-webhook: endpoint and instance ids must match [a-z0-9][a-z0-9._-]{0,62}")
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
	fingerprint, err := sealedhook.Fingerprint(origin, cfg.RecipientKey, ackKey.String(), cfg.Generation)
	if err != nil {
		return nil, fmt.Errorf("endpoint %s: %w", cfg.ID, err)
	}
	if strings.TrimSpace(cfg.ConfirmedFingerprint) != fingerprint {
		return nil, fmt.Errorf("%w (endpoint %s)", ErrFingerprintUnconfirmed, cfg.ID)
	}
	if cfg.Signer == nil {
		return nil, errors.New("sealed-webhook: an instance signing key is required")
	}
	sealer, err := sealedhook.NewSealer(cfg.RecipientKey, cfg.Signer)
	if err != nil {
		return nil, err
	}
	return &Endpoint{
		id: cfg.ID, instanceID: cfg.InstanceID, origin: origin, ackKey: ackKey, generation: cfg.Generation,
		timeout: cfg.Timeout, maxResponse: cfg.MaxResponseBytes, fingerprint: fingerprint, sealer: sealer,
	}, nil
}

// Origin is the canonical configured origin.
func (e *Endpoint) Origin() string { return e.origin }

// ID is the instance-admin endpoint identity.
func (e *Endpoint) ID() string { return e.id }

// Fingerprint is the confirmed trust-boundary fingerprint.
func (e *Endpoint) Fingerprint() string { return e.fingerprint }

// ClientConfig derives the transport configuration.
func (e *Endpoint) ClientConfig() ClientConfig {
	return ClientConfig{Origin: e.origin, AckKey: e.ackKey, Timeout: e.timeout, MaxResponseBytes: e.maxResponse}
}
