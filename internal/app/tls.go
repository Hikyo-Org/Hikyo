package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
)

type certificateFileState struct {
	certModTime time.Time
	certSize    int64
	keyModTime  time.Time
	keySize     int64
}

type certReloader struct {
	certPath string
	keyPath  string
	log      *slog.Logger
	interval time.Duration
	holder   atomic.Pointer[tls.Certificate]
	notAfter atomic.Int64
	failures atomic.Uint64
	stateMu  sync.Mutex
	state    certificateFileState
}

func newCertReloader(certPath, keyPath string, log *slog.Logger, interval time.Duration) (*certReloader, error) {
	pair, leaf, err := tlspolicy.LoadCertificate(certPath, keyPath, time.Now())
	if err != nil {
		return nil, err
	}
	state, err := certificateState(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	r := &certReloader{certPath: certPath, keyPath: keyPath, log: log, interval: interval, state: state}
	r.holder.Store(pair)
	r.notAfter.Store(leaf.NotAfter.Unix())
	return r, nil
}

func certificateState(certPath, keyPath string) (certificateFileState, error) {
	certInfo, err := os.Stat(certPath)
	if err != nil {
		return certificateFileState{}, fmt.Errorf("stat TLS certificate: %w", err)
	}
	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		return certificateFileState{}, fmt.Errorf("stat TLS key: %w", err)
	}
	return certificateFileState{
		certModTime: certInfo.ModTime(), certSize: certInfo.Size(),
		keyModTime: keyInfo.ModTime(), keySize: keyInfo.Size(),
	}, nil
}

func (r *certReloader) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	pair := r.holder.Load()
	if pair == nil {
		return nil, fmt.Errorf("TLS certificate is unavailable")
	}
	return pair, nil
}

func (r *certReloader) tlsConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: r.getCertificate}
}

func (r *certReloader) reload() error {
	state, err := certificateState(r.certPath, r.keyPath)
	if err != nil {
		r.failures.Add(1)
		r.log.Error("TLS certificate reload failed; retaining last known-good certificate", "err", err)
		return err
	}
	pair, leaf, err := tlspolicy.LoadCertificate(r.certPath, r.keyPath, time.Now())
	if err != nil {
		r.failures.Add(1)
		r.log.Error("TLS certificate reload failed; retaining last known-good certificate", "err", err)
		return err
	}
	r.holder.Store(pair)
	r.notAfter.Store(leaf.NotAfter.Unix())
	r.stateMu.Lock()
	r.state = state
	r.stateMu.Unlock()
	r.log.Info("tls certificate reloaded", "spki_fingerprint", tlspolicy.SPKIFingerprint(leaf), "not_after", leaf.NotAfter.UTC())
	return nil
}

func (r *certReloader) reloadIfChanged() {
	state, err := certificateState(r.certPath, r.keyPath)
	if err != nil {
		r.failures.Add(1)
		r.log.Error("TLS certificate watch failed; retaining last known-good certificate", "err", err)
		return
	}
	r.stateMu.Lock()
	changed := state != r.state
	if changed {
		r.state = state
	}
	r.stateMu.Unlock()
	if changed {
		_ = r.reload()
	}
}

func (r *certReloader) run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reloadIfChanged()
		}
	}
}

func (r *certReloader) TLSMetrics() (int64, uint64) {
	return r.notAfter.Load(), r.failures.Load()
}

type operationalHealth struct {
	retention interface {
		OperationalHealth(context.Context) (service.PruneHealth, error)
	}
	tls *certReloader
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
