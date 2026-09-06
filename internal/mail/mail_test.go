package mail_test

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/mail"
	"github.com/Hikyo-Org/hikyo/internal/mailtest"
)

func TestAbsentConfigurationDisablesMail(t *testing.T) {
	client, err := mail.New(mail.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if client.Configured() {
		t.Fatal("absent mail configuration enabled mail")
	}
}

func TestSendRefusesUntrustedTLSAndPrivateRelayWithoutExplicitException(t *testing.T) {
	for _, mode := range []string{"implicit", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			sink := mailtest.New(t, mode)
			for name, cfg := range map[string]mail.Config{
				"untrusted certificate": {Addr: sink.Addr, TLS: mode, From: "hikyo@example.com", AllowedCIDRs: "127.0.0.1/32"},
				"private relay":         {Addr: sink.Addr, TLS: mode, From: "hikyo@example.com", CAPEM: sink.CAPEM},
			} {
				t.Run(name, func(t *testing.T) {
					client, err := mail.New(cfg)
					if err != nil {
						t.Fatal(err)
					}
					if err := client.Send(context.Background(), "recipient@example.com", "Test", "confidential body"); !errors.Is(err, mail.ErrDelivery) {
						t.Fatalf("got %v, want redacted delivery failure", err)
					}
				})
			}
			if len(sink.Messages()) != 0 {
				t.Fatal("unsafe delivery reached recipient")
			}
		})
	}
}

func TestSendRequiresTLS12AndMatchingHostname(t *testing.T) {
	for _, mode := range []string{"implicit", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			for name, options := range map[string]mailtest.Options{
				"obsolete TLS":   {Mode: mode, MinTLSVersion: tls.VersionTLS10, MaxTLSVersion: tls.VersionTLS11},
				"wrong hostname": {Mode: mode, WrongHostname: true},
			} {
				t.Run(name, func(t *testing.T) {
					sink := mailtest.NewWithOptions(t, options)
					client, err := mail.New(mail.Config{Addr: sink.Addr, TLS: mode, From: "hikyo@example.com", CAPEM: sink.CAPEM, AllowedCIDRs: "127.0.0.1/32"})
					if err != nil {
						t.Fatal(err)
					}
					if err := client.Send(context.Background(), "recipient@example.com", "Test", "Test body"); !errors.Is(err, mail.ErrDelivery) {
						t.Fatalf("got %v, want TLS refusal", err)
					}
					if len(sink.Messages()) != 0 {
						t.Fatal("unsafe TLS delivered a message")
					}
				})
			}
		})
	}
}

func TestSendCancellationInterruptsSMTPGreeting(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	client, err := mail.New(mail.Config{Addr: listener.Addr().String(), TLS: "starttls", From: "hikyo@example.com", AllowedCIDRs: "127.0.0.1/32"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- client.Send(ctx, "recipient@example.com", "Test", "Test body") }()
	select {
	case conn := <-accepted:
		t.Cleanup(func() { _ = conn.Close() })
	case <-time.After(3 * time.Second):
		t.Fatal("send did not connect to local stalled relay")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("send ignored cancellation after connecting")
	}
}

func TestSendUsesVerifiedTLSAndPreservesPasswordBytes(t *testing.T) {
	for _, mode := range []string{"implicit", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			sink := mailtest.New(t, mode)
			client, err := mail.New(mail.Config{Addr: sink.Addr, TLS: mode, From: "Hikyo <hikyo@example.com>",
				User: "operator", Password: " password\n", CAPEM: sink.CAPEM, AllowedCIDRs: "127.0.0.1/32"})
			if err != nil {
				t.Fatal(err)
			}
			if err := client.Send(context.Background(), "recipient@example.com", "Configuration test", "Test body"); err != nil {
				t.Fatal(err)
			}
			messages := sink.Messages()
			if len(messages) != 1 {
				t.Fatalf("received %d messages", len(messages))
			}
			message := messages[0]
			if message.TLSVersion < tls.VersionTLS12 || message.Username != "operator" || message.Password != " password\n" {
				t.Fatal("TLS posture or exact credential bytes were not preserved")
			}
			if message.From != "hikyo@example.com" || len(message.Recipients) != 1 || message.Recipients[0] != "recipient@example.com" || !strings.Contains(message.Data, "Test body") {
				t.Fatal("incorrect SMTP message")
			}
		})
	}
}

func TestPreparationRejectsPartialAndUnsafeConfigurationWithoutValuesInErrors(t *testing.T) {
	for name, cfg := range map[string]mail.Config{
		"missing address":       {TLS: "implicit"},
		"missing TLS":           {Addr: "relay.example:465", From: "Hikyo <hikyo@example.com>"},
		"cleartext TLS":         {Addr: "relay.example:465", TLS: "none", From: "hikyo@example.com"},
		"port range":            {Addr: "relay.example:65536", TLS: "implicit", From: "hikyo@example.com"},
		"sender injection":      {Addr: "relay.example:465", TLS: "implicit", From: "hikyo@example.com\r\nBcc: victim@example.com"},
		"password without user": {Addr: "relay.example:465", TLS: "implicit", From: "hikyo@example.com", Password: "sensitive-value"},
		"user without password": {Addr: "relay.example:465", TLS: "implicit", From: "hikyo@example.com", User: "sensitive-value"},
		"invalid EHLO":          {Addr: "relay.example:465", TLS: "implicit", From: "hikyo@example.com", EHLO: "bad host"},
		"invalid CIDR":          {Addr: "relay.example:465", TLS: "implicit", From: "hikyo@example.com", AllowedCIDRs: "sensitive-value"},
		"invalid CA":            {Addr: "relay.example:465", TLS: "implicit", From: "hikyo@example.com", CAPEM: "sensitive-value"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := mail.New(cfg)
			if err == nil {
				t.Fatal("unsafe configuration accepted")
			}
			if strings.Contains(err.Error(), "sensitive-value") || strings.Contains(err.Error(), "victim@example.com") {
				t.Fatal("validation disclosed a configured value")
			}
		})
	}
}

func TestPreparationAcceptsValidConfigurationWithoutContactingRelay(t *testing.T) {
	client, err := mail.New(mail.Config{Addr: "no-such-relay.invalid:465", TLS: "implicit", From: "Hikyo <hikyo@example.com>", User: "operator", Password: " password\n"})
	if err != nil {
		t.Fatal(err)
	}
	if !client.Configured() {
		t.Fatal("valid mail configuration disabled")
	}
}
