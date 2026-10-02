package cloudflare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

type safeErrorTransport func(*http.Request) (*http.Response, error)

func (f safeErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type safeErrorTimeout struct{}

func (safeErrorTimeout) Error() string   { return "PRIVATE_RECEIVER_CANARY" }
func (safeErrorTimeout) Timeout() bool   { return true }
func (safeErrorTimeout) Temporary() bool { return true }

func TestTransportErrorsHideCauseAndPreserveClassification(t *testing.T) {
	const marker = "PRIVATE_RECEIVER_CANARY"
	for _, cause := range []error{errors.New(marker), fmt.Errorf("%s: %w", marker, context.Canceled), fmt.Errorf("%s: %w", marker, context.DeadlineExceeded), safeErrorTimeout{}} {
		t.Run(fmt.Sprintf("%T", cause), func(t *testing.T) {
			client, err := NewClient(ClientConfig{Credential: testToken, Deadline: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Forget()
			client.http.Transport = safeErrorTransport(func(r *http.Request) (*http.Response, error) {
				return nil, cause
			})
			_, err = client.VerifyToken(t.Context(), strings.Repeat("a", 32))
			if err == nil {
				t.Fatal("transport failure unexpectedly succeeded")
			}
			if !errors.Is(err, cause) {
				t.Fatal("transport cause identity lost")
			}
			for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
				if errors.Is(cause, sentinel) != errors.Is(err, sentinel) {
					t.Fatal("context classification lost")
				}
			}
			var returnedTimeout net.Error
			if originalTimeout, ok := cause.(net.Error); ok && (!errors.As(err, &returnedTimeout) || returnedTimeout.Timeout() != originalTimeout.Timeout()) {
				t.Fatal("timeout classification lost")
			}
			var logs bytes.Buffer
			slog.New(slog.NewTextHandler(&logs, nil)).Error("provider failed", "error", err)
			for _, text := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), logs.String()} {
				if strings.Contains(text, marker) {
					t.Fatalf("transport cause escaped into error/log: %q", text)
				}
			}
		})
	}
}
