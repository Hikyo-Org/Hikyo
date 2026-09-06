package app

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
)

// The applied certificate belongs to an immutable application generation.
// Bootstrap paths are imported once by configuration and never watched here.
type managedCertificate struct {
	pair     *tls.Certificate
	notAfter int64
}

func newManagedCertificate(certPEM, keyPEM string) (*managedCertificate, error) {
	if certPEM == "" && keyPEM == "" {
		return nil, nil
	}
	pair, leaf, err := tlspolicy.ParseCertificatePEM([]byte(certPEM), []byte(keyPEM), time.Now())
	if err != nil {
		return nil, err
	}
	return &managedCertificate{pair: pair, notAfter: leaf.NotAfter.Unix()}, nil
}

func (c *managedCertificate) tlsConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return c.pair, nil }}
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
