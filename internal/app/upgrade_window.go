package app

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/webui"
)

// UpgradeWindow owns temporary listeners while the unattended coordinator holds
// tenant serving closed. Start accepts authenticated effective configuration,
// never unvalidated bootstrap transport settings.
type UpgradeWindow struct {
	mu                          sync.Mutex
	status                      service.RuntimeStatus
	public, operational         *managedHTTPServer
	publicAddr, operationalAddr string
	done                        chan error
}

func NewUpgradeWindow() *UpgradeWindow {
	return &UpgradeWindow{status: service.RuntimeStatus{State: "maintenance", Phase: "preparing"}}
}

func (u *UpgradeWindow) RuntimeStatus(context.Context) (service.RuntimeStatus, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.status, nil
}

func (u *UpgradeWindow) Progress(phase string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if phase == "recovery-required" {
		u.status = service.RuntimeStatus{State: phase}
		return
	}
	switch phase {
	case "preparing", "backup", "restore-check", "migration", "health-check":
		u.status = service.RuntimeStatus{State: "maintenance", Phase: phase}
	}
}

func (u *UpgradeWindow) Start(cfg *config.Config) error {
	if u.public != nil {
		return errors.New("upgrade maintenance listener already started")
	}
	certificate, err := newManagedCertificate(cfg.TLSCertPEM, cfg.TLSKeyPEM)
	if err != nil {
		return err
	}
	public, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	operational, err := net.Listen("tcp", cfg.OperationalListen)
	if err != nil {
		return errors.Join(err, public.Close())
	}
	if certificate != nil {
		public = tls.NewListener(public, certificate.tlsConfig())
	}
	u.publicAddr, u.operationalAddr = public.Addr().String(), operational.Addr().String()
	u.public = newHTTPServer(server.NewMaintenance(u, webui.Assets(), server.PublicOptions{HSTS: config.EmitHSTS(cfg.ExternalOrigin), ExternalOrigin: cfg.ExternalOrigin}))
	u.operational = newHTTPServer(server.NewOperational(nil, nil, nil))
	u.done = make(chan error, 2)
	go func() { u.done <- u.public.Serve(public) }()
	go func() { u.done <- u.operational.Serve(operational) }()
	return nil
}

func (u *UpgradeWindow) Started() bool { return u.public != nil }

func (u *UpgradeWindow) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case err := <-u.done:
		// Keep Close's two completion receives balanced.
		u.done <- err
		return err
	}
}

func (u *UpgradeWindow) Close() error {
	if u.public == nil {
		return nil
	}
	err := shutdownHTTPServers(5*time.Second, u.public, u.operational)
	<-u.done
	<-u.done
	u.public, u.operational = nil, nil
	return err
}
