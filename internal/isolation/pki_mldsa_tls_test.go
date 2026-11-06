package isolation

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/pki"
	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
)

// tlsPeer is one end of a PKI-issued identity: a leaf, its intermediates and
// its display-once key, as the operator would have received them.
type tlsPeer struct {
	certPEM, keyPEM string
}

func (p tlsPeer) pair(t testing.TB) tls.Certificate {
	t.Helper()
	pair, err := tls.X509KeyPair([]byte(p.certPEM), []byte(p.keyPEM))
	if err != nil {
		t.Fatalf("crypto/tls rejects the PKI-issued pair: %v", err)
	}
	return pair
}

func poolOf(t testing.TB, pemText string) *x509.CertPool {
	t.Helper()
	certs, _, err := pki.ParseCertificates([]byte(pemText))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	for _, c := range certs {
		pool.AddCert(c)
	}
	return pool
}

// tlsOptions selects what each side enforces. The zero value is compatibility
// on both sides: Go's defaults, no extra authenticity rule.
type tlsOptions struct {
	serverProfile, clientProfile tlspolicy.Profile
	// requirePQChain makes each side refuse, in VerifyConnection, a peer chain
	// that is not pure ML-DSA-65 (profiles.md: Go has no signature-scheme
	// setting, so authenticity is a verified-chain walk).
	serverRequirePQChain, clientRequirePQChain bool
	// clientCurves overrides the client's groups after the profile is applied.
	clientCurves []tls.CurveID
	// clientRoots, when set, replaces the roots the client trusts.
	clientRoots *x509.CertPool
}

type tlsOutcome struct {
	client, server         tls.ConnectionState
	clientErr, serverErr   error
	offeredSignatureScheme []tls.SignatureScheme
	appBytes               string // what the server read after the handshake
}

func requirePQChain(cs tls.ConnectionState) error {
	if len(cs.VerifiedChains) == 0 {
		return errors.New("no verified chain to walk")
	}
	return pki.CheckPQPath(cs.VerifiedChains[0])
}

// runMLDSATLS performs a real mutual TLS exchange over a loopback socket with
// crypto/tls: the client sends "ping", the server answers "pong". The server
// reads the client's bytes only after its handshake succeeds, so a refused
// handshake leaves appBytes empty.
func runMLDSATLS(t testing.TB, server, client tlsPeer, rootsPEM string, o tlsOptions) tlsOutcome {
	t.Helper()
	var out tlsOutcome
	var mu sync.Mutex
	serverPair := server.pair(t)
	serverConfig := &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			mu.Lock()
			out.offeredSignatureScheme = append([]tls.SignatureScheme(nil), hello.SignatureSchemes...)
			mu.Unlock()
			return &serverPair, nil
		},
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  poolOf(t, rootsPEM),
		MinVersion: tls.VersionTLS12,
	}
	if o.serverRequirePQChain {
		serverConfig.VerifyConnection = requirePQChain
	}
	o.serverProfile.Apply(serverConfig)

	clientPair := client.pair(t)
	roots := o.clientRoots
	if roots == nil {
		roots = poolOf(t, rootsPEM)
	}
	clientConfig := &tls.Config{
		Certificates: []tls.Certificate{clientPair},
		RootCAs:      roots,
		ServerName:   "web.svc.example.com",
		MinVersion:   tls.VersionTLS12,
	}
	if o.clientRequirePQChain {
		clientConfig.VerifyConnection = requirePQChain
	}
	o.clientProfile.Apply(clientConfig)
	if o.clientCurves != nil {
		clientConfig.CurvePreferences = o.clientCurves
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		raw, err := listener.Accept()
		if err != nil {
			mu.Lock()
			out.serverErr = err
			mu.Unlock()
			return
		}
		defer raw.Close()
		_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
		conn := tls.Server(raw, serverConfig)
		err = conn.Handshake()
		mu.Lock()
		out.serverErr, out.server = err, conn.ConnectionState()
		mu.Unlock()
		if err != nil {
			return
		}
		buf := make([]byte, 4)
		n, _ := io.ReadFull(conn, buf)
		mu.Lock()
		out.appBytes = string(buf[:n])
		mu.Unlock()
		_, _ = conn.Write([]byte("pong"))
	}()

	raw, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	conn := tls.Client(raw, clientConfig)
	if err := conn.Handshake(); err != nil {
		out.clientErr = err
	} else {
		out.client = conn.ConnectionState()
		if _, err := conn.Write([]byte("ping")); err != nil {
			out.clientErr = err
		} else {
			reply := make([]byte, 4)
			if _, err := io.ReadFull(conn, reply); err != nil || string(reply) != "pong" {
				out.clientErr = fmt.Errorf("application exchange failed: %v %q", err, reply)
			}
		}
	}
	_ = raw.Close()
	<-done
	mu.Lock()
	defer mu.Unlock()
	return out
}

func offeredMLDSA65(schemes []tls.SignatureScheme) bool {
	for _, s := range schemes {
		if s == tls.MLDSA65 {
			return true
		}
	}
	return false
}
