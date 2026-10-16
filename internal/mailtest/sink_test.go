package mailtest

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/textproto"
	"testing"
	"time"
)

func TestStartRejectsInvalidMode(t *testing.T) {
	if sink, err := Start(Options{Mode: "cleartext"}); err == nil || sink != nil {
		t.Fatal("invalid transport mode was accepted")
	}
}

func TestSinkAcceptsFragmentedSMTPAndUnstuffsDots(t *testing.T) {
	sink := New(t, "implicit")
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(sink.CAPEM)) {
		t.Fatal("sink did not supply a valid trust anchor")
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", sink.Addr, &tls.Config{
		RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := textproto.NewReader(bufio.NewReader(conn))
	if _, _, err := reader.ReadResponse(220); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		"EHLO fixture.test\r\nMAIL FROM:<sender@hikyo.test>\r\nRCPT TO:<person@hikyo.test>\r\nDA",
		"TA\r\nSubject: fragment test\r\n\r\n..dot escaped\r\n.\r\nQUIT\r\n",
	} {
		if _, err := io.WriteString(conn, fragment); err != nil {
			t.Fatal(err)
		}
	}
	for _, code := range []int{250, 250, 250, 354, 250, 221} {
		if _, _, err := reader.ReadResponse(code); err != nil {
			t.Fatal(err)
		}
	}
	messages := sink.Messages()
	if len(messages) != 1 {
		t.Fatalf("accepted messages = %d, want 1", len(messages))
	}
	m := messages[0]
	if m.From != "sender@hikyo.test" || len(m.Recipients) != 1 || m.Recipients[0] != "person@hikyo.test" {
		t.Fatalf("SMTP envelope not preserved: %+v", m)
	}
	if m.Data != "Subject: fragment test\r\n\r\n.dot escaped\r\n" {
		t.Fatalf("SMTP content not preserved: %q", m.Data)
	}
	if m.TLSVersion != tls.VersionTLS12 && m.TLSVersion != tls.VersionTLS13 {
		t.Fatalf("unprotected SMTP version %d", m.TLSVersion)
	}
}

func TestCloseImmediatelyReleasesListener(t *testing.T) {
	for _, mode := range []string{"implicit", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			for range 10 {
				sink, err := Start(Options{Mode: mode})
				if err != nil {
					t.Fatal(err)
				}
				// A command can lose its stdin before Serve has started. Both its
				// explicit shutdown and deferred cleanup must finish successfully.
				if err := sink.Close(); err != nil {
					t.Fatal(err)
				}
				if err := sink.Close(); err != nil {
					t.Fatal(err)
				}
				listener, err := net.Listen("tcp", sink.Addr)
				if err != nil {
					t.Fatalf("closed sink still owns listener: %v", err)
				}
				if err := listener.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
