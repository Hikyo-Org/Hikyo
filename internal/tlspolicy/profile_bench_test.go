package tlspolicy_test

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"sync/atomic"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/tlspolicy"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

type countingConn struct {
	net.Conn
	n *atomic.Int64
}

func (c countingConn) Write(b []byte) (int, error) { c.n.Add(int64(len(b))); return c.Conn.Write(b) }

// BenchmarkHandshake records LOCAL numbers for a full in-memory handshake under
// each profile (DEC-17, #1127 baseline). They come from the machine that ran
// them, over net.Pipe with a P-256 test chain. They are not amd64 or arm64
// target measurements, set no budget, and prove no pass or fail.
func BenchmarkHandshake(b *testing.B) {
	certPEM, keyPEM, _ := tlstest.MintServerCert(b, "localhost")
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		b.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	for _, profile := range []tlspolicy.Profile{tlspolicy.ProfileCompat, tlspolicy.ProfileStrict} {
		b.Run(string(profile), func(b *testing.B) {
			var wire atomic.Int64
			b.ReportAllocs()
			for b.Loop() {
				server := &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
				profile.Apply(server)
				client := &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12}
				profile.Apply(client)
				cc, sc := net.Pipe()
				done := make(chan error, 1)
				go func() { done <- tls.Server(countingConn{sc, &wire}, server).Handshake() }()
				if err := tls.Client(countingConn{cc, &wire}, client).Handshake(); err != nil {
					b.Fatal(err)
				}
				if err := <-done; err != nil {
					b.Fatal(err)
				}
				cc.Close()
				sc.Close()
			}
			b.ReportMetric(float64(wire.Load())/float64(b.N), "handshake-bytes/op")
		})
	}
}
