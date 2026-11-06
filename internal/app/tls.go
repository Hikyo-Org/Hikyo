package app

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
)

// The applied certificate belongs to an immutable application generation.
// Bootstrap paths are imported once by configuration and never watched here.
type managedCertificate struct {
	pair     *tls.Certificate
	notAfter int64
	// profile is the listener's key-exchange policy for this generation. It
	// travels with the certificate so both are prepared, activated and replaced
	// together at the graph boundary.
	profile tlspolicy.Profile
	// observe receives each handshake that reaches verification. Nil discards.
	observe func(tlspolicy.Negotiation)
}

func newManagedCertificate(certPEM, keyPEM string, profile tlspolicy.Profile) (*managedCertificate, error) {
	if certPEM == "" && keyPEM == "" {
		// Fail closed under every path that builds a generation, not only config
		// parsing: strict on a plaintext listener would claim protection that no
		// connection has.
		// Anything other than compat fails closed, as Profile.Apply does.
		if profile != tlspolicy.ProfileCompat && profile != "" {
			return nil, errors.New("TLS profile other than compat requires a TLS certificate pair; the listener would serve plaintext")
		}
		return nil, nil
	}
	pair, leaf, err := tlspolicy.ParseCertificatePEM([]byte(certPEM), []byte(keyPEM), time.Now())
	if err != nil {
		return nil, err
	}
	return &managedCertificate{pair: pair, notAfter: leaf.NotAfter.Unix(), profile: profile}, nil
}

// tlsConfig is the one place the listener policy is applied. Compat keeps the
// TLS 1.2 floor and Go's group defaults exactly as before. Strict handshakes
// refuse a classical-only peer before any request byte is read. Certificate
// authentication is unchanged and classical; it is not reported as PQ.
func (c *managedCertificate) tlsConfig() *tls.Config {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return c.pair, nil }}
	c.profile.Apply(cfg)
	if c.observe != nil {
		tlspolicy.Observe(cfg, tlspolicy.HopNativeListener, c.profile, c.observe)
	}
	return cfg
}

// observeTo logs every observed negotiation at debug level. Together with
// logConfigured it keeps two evidence classes apart (#1127): "configured" says
// what the active listener is set to enforce; "observed" is a handshake that
// reached verification in this process. VerifyConnection runs before the
// handshake finishes, so it may still fail afterwards. Values come from closed
// vocabularies. Authentication is not claimed here.
func (c *managedCertificate) observeTo(log *slog.Logger) {
	if c == nil || log == nil {
		return
	}
	c.observe = func(n tlspolicy.Negotiation) {
		log.Debug("native listener TLS negotiation", "hop", n.Hop, "profile", n.Profile, "version", n.Version, "group", n.Group, "resumed", n.Resumed, "evidence", "observed")
	}
}

// logConfigured is called when a generation becomes the active one, never while
// it is only prepared: a candidate that later fails must not leave a record of a
// profile that never activated.
func (c *managedCertificate) logConfigured(log *slog.Logger) {
	if c == nil || log == nil {
		return
	}
	log.Info("native listener TLS policy", "hop", tlspolicy.HopNativeListener, "profile", c.profile, "evidence", "configured",
		"authentication", "classical certificate chain; reported separately")
}

func (c *managedCertificate) TLSMetrics() (int64, uint64) {
	// Failed candidates are reported by the Apply job; they never replace this
	// certificate or become a second, file-watcher configuration authority.
	return c.notAfter, 0
}

type operationalHealth struct {
	retention interface {
		OperationalHealth(context.Context) (service.PruneHealth, error)
	}
	tls *managedCertificate
}

func (h operationalHealth) OperationalHealth(ctx context.Context) (service.PruneHealth, error) {
	return h.retention.OperationalHealth(ctx)
}

func (h operationalHealth) TLSMetrics() (int64, uint64) {
	if h.tls == nil {
		return 0, 0
	}
	return h.tls.TLSMetrics()
}
