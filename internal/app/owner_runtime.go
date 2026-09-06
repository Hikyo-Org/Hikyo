package app

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// ownerRuntime lives outside every replaceable graph. In particular, its
// installer never stops or joins the configuration worker calling it.
type ownerRuntime struct {
	server       *Server
	base         *config.Config
	resources    bootResources
	selfConfig   *service.SelfConfig
	budget       *service.Budget
	fakeProvider *devFakeProvider
	advisory     *service.Advisory
	haCoord      *store.Coordination
	haTick       func(context.Context)
	haStatus     *haStatus

	changeMu                            sync.Mutex
	mu                                  sync.Mutex
	current                             *runningGeneration
	values                              map[string]string
	nodeValues                          map[string]string
	seedNodeValues                      map[string]string
	publicEndpoint, operationalEndpoint *runtimeEndpoint
	endpointErrors                      chan error
	endpointWG                          sync.WaitGroup
	serving                             bool
	workerContext                       context.Context
	transitioning                       bool
	closed                              bool
}

type runningGeneration struct {
	graph          *applicationGeneration
	requests       sync.WaitGroup
	requestContext context.Context
	cancelRequests context.CancelFunc
	cancelWorkers  context.CancelFunc
	workersDone    chan struct{}
}

func newRunningGeneration(graph *applicationGeneration) *runningGeneration {
	ctx, cancel := context.WithCancel(context.Background())
	return &runningGeneration{graph: graph, requestContext: ctx, cancelRequests: cancel}
}

func (g *runningGeneration) start(ctx context.Context) {
	ctx, g.cancelWorkers = context.WithCancel(ctx)
	g.workersDone = make(chan struct{})
	var workers sync.WaitGroup
	for _, run := range []func(context.Context){g.graph.scheduler.Run, g.graph.adapterWorker.Run, g.graph.dynamicWorker.Run, g.graph.updateReconciler.Run} {
		workers.Add(1)
		go func() { defer workers.Done(); run(ctx) }()
	}
	go func() { workers.Wait(); close(g.workersDone) }()
}

func (o *ownerRuntime) handler(operational bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		g := o.current
		if o.closed || o.transitioning || g == nil {
			o.mu.Unlock()
			server.WriteRuntimeUnavailable(w, r)
			return
		}
		g.requests.Add(1)
		o.mu.Unlock()
		defer g.requests.Done()
		ctx, cancel := context.WithCancel(r.Context())
		stop := context.AfterFunc(g.requestContext, cancel)
		defer func() { stop(); cancel() }()
		h := g.graph.publicHandler
		if operational {
			h = g.graph.operationalHandler
		}
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (o *ownerRuntime) stop() {
	o.changeMu.Lock()
	defer o.changeMu.Unlock()
	o.mu.Lock()
	o.closed = true
	g := o.current
	o.mu.Unlock()
	if g == nil {
		return
	}
	g.cancelRequests()
	if g.cancelWorkers != nil {
		g.cancelWorkers()
		<-g.workersDone
	}
	g.requests.Wait()
	g.graph.closeIdleConnections()
}

func (o *ownerRuntime) Prepare(ctx context.Context, bundle *runtimeconfig.Bundle) (runtimeconfig.PreparedActivation, error) {
	if bundle.BootstrapSources() != (config.ManagedBootstrapSources{}) && o.selfConfig.Deployment == nil {
		return nil, errors.New("bootstrap source changes require an installed controlled rollout coordinator")
	}
	o.changeMu.Lock()
	defer o.changeMu.Unlock()
	node := maps.Clone(o.seedNodeValues)
	if bundle.HasNodeValues() {
		var err error
		nodeID := o.selfConfig.NodeID
		if target := bundle.BootstrapSources().Topology; target.NodeID != "" {
			nodeID = target.NodeID
		}
		node, err = bundle.NodeValues(nodeID)
		if err != nil {
			return nil, err
		}
	}
	values := bundle.OwnerValues()
	o.mu.Lock()
	unchanged, closed := maps.Equal(o.values, values) && maps.Equal(o.nodeValues, node), o.closed
	previous := o.current
	o.mu.Unlock()
	if closed {
		return nil, errors.New("application runtime is closed")
	}
	if unchanged {
		return noOwnerActivation{}, nil
	}
	cfg, err := config.ApplyManagedOwnerAndNodeValues(o.base, values, node)
	if err != nil {
		return nil, err
	}
	if cfg.DevAdapterFakeProvider != previous.graph.cfg.DevAdapterFakeProvider {
		if err := o.checkDevelopmentProviderSwitch(ctx, cfg); err != nil {
			return nil, err
		}
	}
	graph, err := o.prepareGeneration(ctx, cfg)
	if err != nil {
		return nil, err
	}
	endpoints, err := o.prepareEndpoints(cfg, graph.certificate)
	if err != nil {
		graph.closeIdleConnections()
		return nil, err
	}
	prepared := &preparedOwnerActivation{owner: o, graph: graph, values: values, nodeValues: node, endpoints: endpoints, previous: previous}
	if cfg.Store.PostgresPoolMax != previous.graph.cfg.Store.PostgresPoolMax {
		prepared.pool, err = o.server.db.PreparePostgresPool(ctx, cfg.Store.PostgresPoolMax)
		if err != nil {
			_ = prepared.Close()
			return nil, err
		}
	}
	return prepared, nil
}

type noOwnerActivation struct{}

func (noOwnerActivation) Activate(context.Context) error { return nil }
func (noOwnerActivation) Close() error                   { return nil }

type preparedOwnerActivation struct {
	mu         sync.Mutex
	owner      *ownerRuntime
	graph      *applicationGeneration
	values     map[string]string
	nodeValues map[string]string
	endpoints  *preparedEndpoints
	pool       *store.PreparedPostgresPool
	previous   *runningGeneration
	used       bool
}

func (p *preparedOwnerActivation) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.used && p.graph != nil {
		p.graph.closeIdleConnections()
	}
	p.graph = nil
	var err error
	if p.endpoints != nil {
		err = p.endpoints.close(p.owner)
	}
	if p.pool != nil {
		err = errors.Join(err, p.pool.Close())
	}
	return err
}

func (p *preparedOwnerActivation) Activate(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.used {
		return nil
	}
	if p.graph == nil {
		return errors.New("prepared application runtime is closed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	o := p.owner
	o.changeMu.Lock()
	defer o.changeMu.Unlock()
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return errors.New("application runtime is closed")
	}
	if maps.Equal(o.values, p.values) && maps.Equal(o.nodeValues, p.nodeValues) {
		o.mu.Unlock()
		p.graph.closeIdleConnections()
		if p.endpoints != nil {
			_ = p.endpoints.close(o)
		}
		if p.pool != nil {
			_ = p.pool.Close()
		}
		p.used = true
		return nil
	}
	old := o.current
	if old != p.previous {
		o.mu.Unlock()
		return errors.New("prepared application graph was superseded")
	}
	o.transitioning = true
	responses := p.endpoints.drainingResponses(o)
	o.mu.Unlock()
	// Stop adding requests before waiting. Existing requests have a bounded
	// graceful interval, then inherit cancellation from their graph. Worker
	// cancellation happens independently of the configuration coordinator.
	requestsDone := make(chan struct{})
	go func() { old.requests.Wait(); close(requestsDone) }()
	drained := make(chan struct{})
	go func() {
		<-requestsDone
		for _, response := range responses {
			<-response.done
		}
		close(drained)
	}()
	timer := time.NewTimer(5 * time.Second)
	select {
	case <-drained:
	case <-timer.C:
		for _, response := range responses {
			_ = response.conn.Close()
		}
	case <-ctx.Done():
		timer.Stop()
		o.mu.Lock()
		o.transitioning = false
		o.mu.Unlock()
		return ctx.Err()
	}
	timer.Stop()
	old.cancelRequests()
	if old.cancelWorkers != nil {
		old.cancelWorkers()
	}
	// Never install a new graph while an old handler/provider can still use
	// the former policy. Cancellation is cooperative, so an unjoined graph
	// remains fenced and the durable target stays unacknowledged.
	<-requestsDone
	if old.workersDone != nil {
		<-old.workersDone
	}
	if err := ctx.Err(); err != nil {
		o.resume(old.graph)
		return err
	}
	if p.graph.cfg.DevAdapterFakeProvider != old.graph.cfg.DevAdapterFakeProvider {
		if err := o.checkDevelopmentProviderSwitch(ctx, p.graph.cfg); err != nil {
			o.resume(old.graph)
			return err
		}
	}
	if err := p.graph.limiter.InheritCounters(old.graph.limiter); err != nil {
		o.resume(old.graph)
		return err
	}
	if p.pool != nil {
		if err := p.pool.Activate(ctx); err != nil {
			o.resume(old.graph)
			return err
		}
	}
	// All old requests/workers are drained and every fallible check completed.
	// Update the stable budget used by both services and the coordinator.
	o.budget.SetDevelopmentDisabled(p.graph.cfg.Dev && p.graph.cfg.DevServiceBudgetsDisabled)
	next := newRunningGeneration(p.graph)
	o.mu.Lock()
	p.endpoints.activate(o)
	o.current = next
	o.values = maps.Clone(p.values)
	o.nodeValues = maps.Clone(p.nodeValues)
	if o.workerContext != nil {
		next.start(o.workerContext)
	}
	o.transitioning = false
	o.mu.Unlock()
	p.used = true
	old.graph.closeIdleConnections()
	return nil
}

// A failed installation restores the last usable administrative graph. The
// server's generation fence and background admission still refuse new work
// against the uninstalled durable target.
func (o *ownerRuntime) resume(graph *applicationGeneration) {
	next := newRunningGeneration(graph)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.current = next
	if o.workerContext != nil {
		next.start(o.workerContext)
	}
	o.transitioning = false
}

// Each graph reports the shared HA observations with its own scheduler state;
// preparing a candidate does not mutate the active graph's leader callback.
type generationHAStatus struct {
	status    *haStatus
	scheduler *Scheduler
}

func (g *generationHAStatus) HASnapshot() server.HAStats {
	stats := g.status.HASnapshot()
	stats.IsLeader = g.scheduler.IsLeader()
	return stats
}

// A real/fake provider switch is admitted only for a pristine adapter domain.
// Even idle configurations can enqueue work later with their existing remote
// identities and credentials. Checking again after the drain catches writes
// that raced preparation; HA nodes cannot make this node-local switch.
func (o *ownerRuntime) checkDevelopmentProviderSwitch(ctx context.Context, cfg *config.Config) error {
	if !cfg.Dev || cfg.HA {
		return errors.New("development provider changes require a standalone development node")
	}
	if o.fakeProvider.hasState() {
		return errors.New("development provider changes require an empty simulated provider")
	}
	return store.NewAdapterRuntime(o.server.db, nil).CheckProviderSwitch(ctx)
}
