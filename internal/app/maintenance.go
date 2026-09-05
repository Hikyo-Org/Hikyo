package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/server"
)

// bootMaintenance is reached only after the authenticated gate reports a
// durable maintenance refusal. It opens no runtime datastore or tenant listener
// and constructs no keyring, scheduler, provider, or authentication service.
func bootMaintenance(cfg *config.Config, log *slog.Logger, resources bootResources) (*Server, error) {
	ln, err := resources.listen("tcp", cfg.OperationalListen)
	if err != nil {
		return nil, fmt.Errorf("boot: maintenance operational listener: %w", err)
	}
	return &Server{
		Maintenance: true, OperationalAddr: ln.Addr().String(),
		operationalLn: ln, operationalHandler: server.NewOperational(nil, nil, nil), log: log,
	}, nil
}

func (s *Server) serveMaintenance(ctx context.Context, ready func()) error {
	httpServer := newHTTPServer(s.operationalHandler)
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(s.operationalLn) }()
	operationalAddr := s.OperationalAddr
	s.log.Warn("maintenance active; tenant serving unavailable; complete local upgrade or recovery and restart", "operational_addr", operationalAddr)
	if ready != nil {
		ready()
	}
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		err := shutdownHTTPServers(5*time.Second, httpServer)
		serveErr := <-done
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		return errors.Join(err, serveErr)
	}
}
