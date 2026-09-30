package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
)

func (o *ownerRuntime) serve(ctx context.Context, ready func()) error {
	defer o.server.db.Close()
	o.changeMu.Lock()
	o.mu.Lock()
	o.workerContext = ctx
	o.current.start(ctx)
	o.serving = true
	o.startEndpoint(o.publicEndpoint)
	o.startEndpoint(o.operationalEndpoint)
	if o.cliServer != nil {
		o.endpointWG.Add(1)
		go func() {
			defer o.endpointWG.Done()
			select {
			case o.endpointErrors <- o.cliServer.Serve(o.server.cliLn):
			default:
			}
		}()
	}
	address, operationalAddress := o.publicEndpoint.listener.Addr().String(), o.operationalEndpoint.listener.Addr().String()
	o.mu.Unlock()
	o.changeMu.Unlock()
	configCtx, stopConfig := context.WithCancel(ctx)
	configDone := make(chan struct{})
	go func() { defer close(configDone); o.selfConfig.Run(configCtx) }()
	o.server.log.Info("server ready", "version", Version, "addr", address, "operational_addr", operationalAddress)
	diagnostics.Printf(ctx, 1, "Server is accepting requests")
	if ready != nil {
		ready()
	}
	var serveErr error
	select {
	case serveErr = <-o.endpointErrors:
	case <-ctx.Done():
	}
	// No installer may transfer endpoint ownership after shutdown snapshots
	// the active sockets. The coordinator owns and disposes uncommitted ones.
	stopConfig()
	<-configDone
	o.changeMu.Lock()
	o.mu.Lock()
	o.transitioning = true
	o.serving = false
	public, operational := o.publicEndpoint, o.operationalEndpoint
	public.retired.Store(true)
	operational.retired.Store(true)
	o.mu.Unlock()
	servers := []*managedHTTPServer{public.server, operational.server}
	if o.cliServer != nil {
		servers = append(servers, o.cliServer)
	}
	shutdownErr := shutdownHTTPServers(5*time.Second, servers...)
	o.endpointWG.Wait()
	o.changeMu.Unlock()
	o.stop()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return errors.Join(serveErr, shutdownErr)
	}
	return shutdownErr
}
