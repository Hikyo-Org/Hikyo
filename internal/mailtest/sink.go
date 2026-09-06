// Package mailtest supplies an in-process TLS SMTP sink for conformance tests.
package mailtest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

type Message struct {
	From       string
	Recipients []string
	Data       string
	TLSVersion uint16
	Username   string
	Password   string
}

type Sink struct {
	Addr      string
	CAPEM     string
	server    *smtp.Server
	mu        sync.Mutex
	messages  []Message
	onMessage func(Message) error
}

// New starts a TLS-only sink. mode selects implicit or mandatory STARTTLS.
func New(t testing.TB, mode string) *Sink {
	return NewWithOptions(t, Options{Mode: mode})
}

type Options struct {
	Mode          string
	MinTLSVersion uint16
	MaxTLSVersion uint16
	WrongHostname bool
	OnMessage     func(Message) error
}

func NewWithOptions(t testing.TB, options Options) *Sink {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Hikyo mail test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	if options.WrongHostname {
		template.DNSNames = []string{"wrong-host.invalid"}
		template.IPAddresses = nil
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sink := &Sink{Addr: listener.Addr().String(), CAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
	sink.onMessage = options.OnMessage
	minVersion := options.MinTLSVersion
	if minVersion == 0 {
		minVersion = tls.VersionTLS12
	}
	tlsConfig := &tls.Config{MinVersion: minVersion, MaxVersion: options.MaxTLSVersion, Certificates: []tls.Certificate{cert}}
	sink.server = smtp.NewServer(smtp.BackendFunc(func(conn *smtp.Conn) (smtp.Session, error) {
		state, _ := conn.TLSConnectionState()
		return &session{sink: sink, version: state.Version}, nil
	}))
	sink.server.Domain = "localhost"
	sink.server.TLSConfig = tlsConfig
	sink.server.ReadTimeout = 5 * time.Second
	sink.server.WriteTimeout = 5 * time.Second
	sink.server.ErrorLog = log.New(io.Discard, "", 0)
	if options.Mode == "implicit" {
		listener = tls.NewListener(listener, tlsConfig)
	} else if options.Mode != "starttls" {
		t.Fatalf("invalid mail sink mode %q", options.Mode)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = sink.server.Serve(listener) }()
	t.Cleanup(func() { _ = sink.server.Close(); _ = listener.Close(); <-done })
	return sink
}

func (s *Sink) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]Message, len(s.messages))
	copy(result, s.messages)
	for i := range result {
		result[i].Recipients = append([]string(nil), result[i].Recipients...)
	}
	return result
}

type session struct {
	sink                     *Sink
	version                  uint16
	username, password, from string
	to                       []string
}

func (s *session) AuthMechanisms() []string { return []string{sasl.Plain} }
func (s *session) Auth(mechanism string) (sasl.Server, error) {
	if mechanism != sasl.Plain {
		return nil, smtp.ErrAuthUnknownMechanism
	}
	return sasl.NewPlainServer(func(identity, username, password string) error {
		s.username, s.password = username, password
		return nil
	}), nil
}
func (s *session) Reset()        { s.from = ""; s.to = nil }
func (s *session) Logout() error { return nil }
func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	if s.version < tls.VersionTLS12 {
		return smtp.ErrAuthRequired
	}
	s.from = from
	return nil
}
func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error { s.to = append(s.to, to); return nil }
func (s *session) Data(r io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return err
	}
	s.sink.mu.Lock()
	message := Message{From: s.from, Recipients: append([]string(nil), s.to...), Data: string(data), TLSVersion: s.version, Username: s.username, Password: s.password}
	s.sink.messages = append(s.sink.messages, message)
	s.sink.mu.Unlock()
	if s.sink.onMessage != nil {
		return s.sink.onMessage(message)
	}
	return nil
}
