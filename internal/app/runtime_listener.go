package app

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/Hikyo-Org/hikyo/internal/config"
)

// A socket survives a change between HTTP and HTTPS. Transport is selected for
// each newly accepted connection; existing connections are closed at the graph
// boundary so an old plaintext keep-alive cannot bypass newly required TLS.
type runtimeListener struct {
	net.Listener
	mu          sync.Mutex
	tls         *tls.Config
	connections map[*runtimeConnection]struct{}
}

type runtimeConnection struct {
	net.Conn
	listener *runtimeListener
}

func (c *runtimeConnection) Close() error {
	err := c.Conn.Close()
	c.listener.mu.Lock()
	delete(c.listener.connections, c)
	c.listener.mu.Unlock()
	return err
}

func (l *runtimeListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	tracked := &runtimeConnection{Conn: conn, listener: l}
	l.connections[tracked] = struct{}{}
	transport := l.tls
	l.mu.Unlock()
	if transport != nil {
		return tls.Server(tracked, transport), nil
	}
	return tracked, nil
}

func (l *runtimeListener) replaceTransport(transport *tls.Config) {
	l.mu.Lock()
	l.tls = transport
	previous := make([]*runtimeConnection, 0, len(l.connections))
	for conn := range l.connections {
		previous = append(previous, conn)
	}
	l.mu.Unlock()
	for _, conn := range previous {
		_ = conn.Close()
	}
}

type runtimeEndpoint struct {
	listener    *runtimeListener
	address     string
	operational atomic.Bool
	retired     atomic.Bool
	server      *managedHTTPServer
	started     bool // owner changeMu
	responseMu  sync.Mutex
	responses   map[net.Conn]chan struct{}
}

func (o *ownerRuntime) newEndpoint(address string, operational bool, certificate *managedCertificate) (*runtimeEndpoint, error) {
	raw, err := o.resources.listen("tcp", address)
	if err != nil {
		return nil, err
	}
	endpoint := &runtimeEndpoint{address: address, listener: &runtimeListener{Listener: raw, connections: make(map[*runtimeConnection]struct{})}, responses: make(map[net.Conn]chan struct{})}
	endpoint.operational.Store(operational)
	if certificate != nil {
		endpoint.listener.tls = certificate.tlsConfig()
	}
	endpoint.server = newHTTPServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		o.handler(endpoint.operational.Load()).ServeHTTP(w, r)
	}))
	endpoint.server.ConnState = endpoint.trackResponse
	return endpoint, nil
}

// Handler return precedes net/http's final response flush. A transport change
// waits for these captured responses too, so closing an old TLS/plaintext
// connection does not truncate an otherwise gracefully completed response.
func (e *runtimeEndpoint) trackResponse(conn net.Conn, state http.ConnState) {
	e.responseMu.Lock()
	defer e.responseMu.Unlock()
	if state == http.StateActive {
		if e.responses[conn] == nil {
			e.responses[conn] = make(chan struct{})
		}
	} else if state == http.StateIdle || state == http.StateClosed || state == http.StateHijacked {
		if done := e.responses[conn]; done != nil {
			delete(e.responses, conn)
			close(done)
		}
	}
}

type drainingResponse struct {
	conn net.Conn
	done <-chan struct{}
}

func (p *preparedEndpoints) drainingResponses(o *ownerRuntime) []drainingResponse {
	if p.public == o.publicEndpoint && p.operational == o.operationalEndpoint && !p.resetPublic {
		return nil
	}
	var responses []drainingResponse
	for _, endpoint := range []*runtimeEndpoint{o.publicEndpoint, o.operationalEndpoint} {
		endpoint.responseMu.Lock()
		for conn, done := range endpoint.responses {
			responses = append(responses, drainingResponse{conn: conn, done: done})
		}
		endpoint.responseMu.Unlock()
	}
	return responses
}

func (o *ownerRuntime) startEndpoint(endpoint *runtimeEndpoint) {
	if endpoint.started {
		return
	}
	endpoint.started = true
	o.endpointWG.Add(1)
	go func() {
		defer o.endpointWG.Done()
		err := endpoint.server.Serve(endpoint.listener)
		if endpoint.retired.Load() {
			return
		}
		select {
		case o.endpointErrors <- err:
		default:
		}
	}()
}

func (o *ownerRuntime) retireEndpoint(endpoint *runtimeEndpoint) error {
	endpoint.retired.Store(true)
	if endpoint.started {
		return endpoint.server.Close()
	}
	return o.resources.closeListener(endpoint.listener)
}

type preparedEndpoints struct {
	public, operational *runtimeEndpoint
	created             []*runtimeEndpoint
	certificate         *managedCertificate
	resetPublic         bool
}

// Address changes reserve their sockets before touching the active graph.
// An exact role swap reuses both existing sockets without a bind gap.
func (o *ownerRuntime) prepareEndpoints(cfg *config.Config, certificate *managedCertificate) (*preparedEndpoints, error) {
	p := &preparedEndpoints{certificate: certificate}
	choose := func(address string, operational bool) (*runtimeEndpoint, error) {
		for _, endpoint := range []*runtimeEndpoint{o.publicEndpoint, o.operationalEndpoint} {
			if endpoint != nil && endpoint != p.public && !endpoint.retired.Load() && (endpoint.address == address || endpoint.listener.Addr().String() == address) {
				return endpoint, nil
			}
		}
		var cert *managedCertificate
		if !operational {
			cert = certificate
		}
		endpoint, err := o.newEndpoint(address, operational, cert)
		if err == nil {
			p.created = append(p.created, endpoint)
		}
		return endpoint, err
	}
	var err error
	p.public, err = choose(cfg.Listen, false)
	if err != nil {
		return nil, err
	}
	p.operational, err = choose(cfg.OperationalListen, true)
	if err != nil {
		_ = p.close(o)
		return nil, err
	}
	if o.current != nil {
		old := o.current.graph.cfg
		p.resetPublic = old.TLSCertPEM != cfg.TLSCertPEM || old.TLSKeyPEM != cfg.TLSKeyPEM
	}
	return p, nil
}

func (p *preparedEndpoints) close(o *ownerRuntime) error {
	var err error
	for _, endpoint := range p.created {
		err = errors.Join(err, o.retireEndpoint(endpoint))
	}
	p.created = nil
	return err
}

// activate runs after all graph requests/workers have drained. Listener binds
// and certificate parsing already succeeded; only ownership transfer remains.
func (p *preparedEndpoints) activate(o *ownerRuntime) {
	oldPublic, oldOperational := o.publicEndpoint, o.operationalEndpoint
	if p.public.operational.Load() || p.resetPublic {
		var transport *tls.Config
		if p.certificate != nil {
			transport = p.certificate.tlsConfig()
		}
		p.public.listener.replaceTransport(transport)
	}
	if !p.operational.operational.Load() {
		p.operational.listener.replaceTransport(nil)
	}
	p.public.operational.Store(false)
	p.operational.operational.Store(true)
	o.publicEndpoint, o.operationalEndpoint = p.public, p.operational
	o.server.publicLn, o.server.operationalLn = p.public.listener, p.operational.listener
	o.server.Addr, o.server.OperationalAddr = p.public.listener.Addr().String(), p.operational.listener.Addr().String()
	if o.serving {
		o.startEndpoint(p.public)
		o.startEndpoint(p.operational)
	}
	for _, old := range []*runtimeEndpoint{oldPublic, oldOperational} {
		if old != nil && old != p.public && old != p.operational {
			_ = o.retireEndpoint(old)
		}
	}
	p.created = nil
}
