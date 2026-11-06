package tlstest

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"testing"
)

// PeerOptions selects the server side of a real, in-process TLS peer. The zero
// value is a TLS 1.2 to 1.3 server with Go's default groups, which is a
// compatible hybrid peer on a toolchain that ships X25519MLKEM768.
type PeerOptions struct {
	MinVersion, MaxVersion uint16
	// Curves is the server's group preference list. Nil keeps Go's defaults.
	Curves []tls.CurveID
	// Hosts are the certificate names; the default is localhost and 127.0.0.1.
	Hosts []string
	// Tickets leaves server session tickets enabled so resumption can be tested.
	Tickets bool
}

// Peer is an HTTPS server that records what actually reached it. It is the
// server-side witness for "no credential left the process on a refused
// handshake": Hits and Authorizations only grow when a request was delivered.
type Peer struct {
	Addr string
	URL  string
	// Pool trusts the peer's test CA.
	Pool *x509.CertPool
	// CertPEM is the leaf-plus-CA chain the peer presents.
	CertPEM []byte

	mu             sync.Mutex
	hits           int
	authorizations []string
	states         []tls.ConnectionState
	raw            bytes.Buffer
}

// Hits is the number of requests the handler received.
func (p *Peer) Hits() int { p.mu.Lock(); defer p.mu.Unlock(); return p.hits }

// Authorizations are the Authorization header values the handler received.
func (p *Peer) Authorizations() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.authorizations...)
}

// States are the server-side connection states of delivered requests.
func (p *Peer) States() []tls.ConnectionState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]tls.ConnectionState(nil), p.states...)
}

// RawContains reports whether the canary appeared in any byte the server read
// from the socket before decryption.
func (p *Peer) RawContains(canary string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return bytes.Contains(p.raw.Bytes(), []byte(canary))
}

type spyConn struct {
	net.Conn
	peer *Peer
}

func (c spyConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.peer.mu.Lock()
	c.peer.raw.Write(b[:n])
	c.peer.mu.Unlock()
	return n, err
}

type spyListener struct {
	net.Listener
	peer *Peer
}

func (l spyListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return spyConn{Conn: conn, peer: l.peer}, nil
}

// StartPeer serves HTTPS on a loopback port until the test ends.
func StartPeer(t testing.TB, options PeerOptions) *Peer {
	t.Helper()
	hosts := options.Hosts
	if len(hosts) == 0 {
		hosts = []string{"localhost", "127.0.0.1"}
	}
	certPEM, keyPEM, _ := MintServerCert(t, hosts...)
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	rest := certPEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if cert, err := x509.ParseCertificate(block.Bytes); err == nil && cert.IsCA {
			pool.AddCert(cert)
		}
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	peer := &Peer{Addr: raw.Addr().String(), URL: "https://" + raw.Addr().String(), Pool: pool, CertPEM: certPEM}
	config := &tls.Config{
		Certificates:           []tls.Certificate{pair},
		MinVersion:             options.MinVersion,
		MaxVersion:             options.MaxVersion,
		CurvePreferences:       options.Curves,
		SessionTicketsDisabled: !options.Tickets,
		NextProtos:             []string{"http/1.1"},
	}
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer.mu.Lock()
			peer.hits++
			peer.authorizations = append(peer.authorizations, r.Header.Get("Authorization"))
			if r.TLS != nil {
				peer.states = append(peer.states, *r.TLS)
			}
			peer.mu.Unlock()
			_, _ = io.WriteString(w, "{}")
		}),
		ErrorLog: log.New(io.Discard, "", 0),
	}
	go func() { _ = server.Serve(tls.NewListener(spyListener{Listener: raw, peer: peer}, config)) }()
	t.Cleanup(func() { _ = server.Close() })
	return peer
}
