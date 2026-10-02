package forgejo

import (
	"bytes"
	"crypto/x509"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestTransportErrorsDoNotLogReceiverSecrets(t *testing.T) {
	const marker = "github_pat_CANARY_PRIVATE_TOKEN"

	for _, scenario := range []string{"redirect", "malformed_location", "malformed_status", "malformed_header"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				received := r.Header.Get("Authorization")
				if !strings.Contains(received, marker) {
					t.Error("receiver did not receive test credential")
				}
				switch scenario {
				case "redirect":
					w.Header().Set("Location", "https://receiver.example/"+marker)
					w.WriteHeader(http.StatusTemporaryRedirect)
				case "malformed_location":
					w.Header().Set("Location", "https://receiver.example/"+marker+"%zz")
					w.WriteHeader(http.StatusTemporaryRedirect)
				default:
					conn, buffer, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					if scenario == "malformed_status" {
						_, err = fmt.Fprintf(buffer, "HTTP/1.1 %s\r\nContent-Length: 0\r\n\r\n", marker)
					} else {
						_, err = fmt.Fprintf(buffer, "HTTP/1.1 200 OK\r\n%s\r\nContent-Length: 0\r\n\r\n", marker)
					}
					if err != nil {
						t.Error(err)
					}
					if err := buffer.Flush(); err != nil {
						t.Error(err)
					}
				}
				_ = body
			}))
			defer server.Close()
			client, err := NewClient(ClientConfig{Origin: server.URL, Credential: marker, AllowedCIDRs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, Deadline: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Forget()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			client.http.Transport.(*http.Transport).TLSClientConfig.RootCAs = roots
			_, err = client.Version(t.Context())
			if err == nil {
				t.Fatal("malicious transport response unexpectedly succeeded")
			}
			var logs bytes.Buffer
			slog.New(slog.NewTextHandler(&logs, nil)).Error("adapter operation failed", "error", err)
			for _, text := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), logs.String()} {
				if strings.Contains(text, marker) {
					t.Fatalf("receiver credential escaped into error/log: %q", text)
				}
			}
		})
	}
}
