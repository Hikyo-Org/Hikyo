// Package sealedwebhook is the generic HTTPS push adapter. It delivers
// recipient-encrypted, signed envelopes to an instance-admin configured
// receiver (docs/spec/sealed-webhook.md) and accepts only acknowledgements
// signed by the pinned acknowledgement key and bound to the exact envelope.
//
// Its provider interface can express exactly one operation, a POST of an
// envelope to the fixed protocol path. There is no read, no GET, and no
// callback: nothing a receiver returns can move the configured trust boundary.
package sealedwebhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
	"github.com/Hikyo-Org/hikyo/internal/netpolicy"
)

const (
	// MaxTimeout bounds one delivery round trip.
	MaxTimeout = 10 * time.Second
	// MaxResponseBytes bounds an acknowledgement body.
	MaxResponseBytes = sealedhook.MaxAck
)

var (
	// ErrNoAck marks a delivery whose outcome is ambiguous: the receiver may
	// or may not have applied it. The effect stays unknown and is retried
	// under the same idempotency key.
	ErrNoAck = errors.New("sealed-webhook: no valid acknowledgement")
	// ErrOversizedResponse is an ErrNoAck whose body exceeded the bound.
	ErrOversizedResponse = errors.New("sealed-webhook: acknowledgement exceeded the configured size bound")
)

// Delivery is one exact request body and the binding its ack must carry.
type Delivery struct {
	Body           []byte
	Digest         string
	IdempotencyKey string
}

// API is the closed provider surface. The structural test pins it.
type API interface {
	Deliver(context.Context, Delivery) (sealedhook.Ack, error)
}

type operation struct {
	Method string
	Path   string
}

// operationRegistry is the complete linked route table.
var operationRegistry = map[string]operation{
	"deliver": {Method: http.MethodPost, Path: sealedhook.SyncPath},
}

// ResponseError is a non-200 receiver answer. Bodies are never surfaced.
type ResponseError struct{ Status int }

func (e *ResponseError) Error() string {
	return "sealed-webhook: receiver answered status " + strconv.Itoa(e.Status) + " without an acknowledgement"
}

// ClientConfig is the transport half of an Endpoint.
type ClientConfig struct {
	Origin           string
	AckKey           sealedhook.PublicKey
	AllowedCIDRs     []netip.Prefix
	Timeout          time.Duration
	MaxResponseBytes int64
}

// Client is the hand-rolled receiver transport.
type Client struct {
	origin   string
	ackKey   sealedhook.PublicKey
	maxBytes int64
	http     *http.Client
}

var _ API = (*Client)(nil)

// NewClient builds the transport with the shared public-egress dialer.
func NewClient(cfg ClientConfig) (*Client, error) {
	return newClient(cfg, net.DefaultResolver, &net.Dialer{Timeout: cfg.Timeout}, nil)
}

func newClient(cfg ClientConfig, resolver netpolicy.Resolver, dialer netpolicy.Dialer, roots *x509.CertPool) (*Client, error) {
	origin, err := CanonicalOrigin(cfg.Origin)
	if err != nil {
		return nil, err
	}
	if cfg.AckKey.String() == "" {
		return nil, errors.New("sealed-webhook: a pinned acknowledgement key is required")
	}
	if cfg.Timeout <= 0 || cfg.Timeout > MaxTimeout || cfg.Timeout >= adapter.LeaseTime {
		return nil, fmt.Errorf("sealed-webhook: timeout must be in (0, %s]", MaxTimeout)
	}
	if cfg.MaxResponseBytes <= 0 || cfg.MaxResponseBytes > MaxResponseBytes {
		return nil, fmt.Errorf("sealed-webhook: max response bytes must be in (0, %d]", MaxResponseBytes)
	}
	publicDialer, err := netpolicy.NewPublicDialer(cfg.AllowedCIDRs, resolver, dialer)
	if err != nil {
		return nil, fmt.Errorf("sealed-webhook: egress policy: %w", err)
	}
	transport := &http.Transport{
		Proxy:             nil,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots},
		DialContext:       publicDialer.DialContext,
		ForceAttemptHTTP2: true,
	}
	return &Client{
		origin: origin, ackKey: cfg.AckKey, maxBytes: cfg.MaxResponseBytes,
		http: &http.Client{
			Transport: transport,
			Timeout:   cfg.Timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("sealed-webhook: redirects are refused")
			},
		},
	}, nil
}

// Forget drops pooled connections when one outbox attempt ends.
func (c *Client) Forget() { c.http.CloseIdleConnections() }

// CanonicalOrigin accepts only a bare https origin: no userinfo, path, query,
// or fragment. The protocol path is fixed and appended by the client.
func CanonicalOrigin(raw string) (string, error) {
	return adapter.CanonicalOrigin(adapter.SealedWebhookProvider, raw)
}

// Deliver posts one envelope and verifies the answer. Classification:
//   - transport error, non-200, wrong media type, oversized or malformed
//     body: ErrNoAck (ambiguous, retry with the same idempotency key);
//   - well-formed ack that fails signature, key, digest, or idempotency
//     binding: adapter.ErrAckForged (terminal, attention);
//   - verified ack: returned, whatever its status.
func (c *Client) Deliver(ctx context.Context, d Delivery) (sealedhook.Ack, error) {
	op := operationRegistry["deliver"]
	req, err := http.NewRequestWithContext(ctx, op.Method, c.origin+op.Path, bytes.NewReader(d.Body))
	if err != nil {
		return sealedhook.Ack{}, err
	}
	req.Header.Set("Content-Type", sealedhook.ContentType)
	req.Header.Set("Accept", sealedhook.AckContentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return sealedhook.Ack{}, fmt.Errorf("%w: %w", ErrNoAck, sanitizeTransport(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return sealedhook.Ack{}, fmt.Errorf("%w: body could not be read", ErrNoAck)
	}
	if int64(len(raw)) > c.maxBytes {
		return sealedhook.Ack{}, fmt.Errorf("%w: %w", ErrNoAck, ErrOversizedResponse)
	}
	if resp.StatusCode != http.StatusOK {
		return sealedhook.Ack{}, fmt.Errorf("%w: %w", ErrNoAck, &ResponseError{Status: resp.StatusCode})
	}
	if media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err != nil || media != sealedhook.AckContentType {
		return sealedhook.Ack{}, fmt.Errorf("%w: unexpected media type", ErrNoAck)
	}
	ack, err := sealedhook.VerifyAck(raw, c.ackKey, d.Digest, d.IdempotencyKey)
	switch {
	case err == nil:
		return ack, nil
	case errors.Is(err, sealedhook.ErrSignature), errors.Is(err, sealedhook.ErrUnknownKey), errors.Is(err, sealedhook.ErrAckMismatch):
		return sealedhook.Ack{}, fmt.Errorf("%w: %w", adapter.ErrAckForged, err)
	default:
		return sealedhook.Ack{}, fmt.Errorf("%w: malformed acknowledgement", ErrNoAck)
	}
}

// sanitizeTransport preserves typed error classification without formatting
// any receiver-controlled redirect, status-line or header text.
func sanitizeTransport(err error) error {
	return adapter.SafeTransportError(err)
}
