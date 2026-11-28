package server_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// lifecycleAuth records what the real handler hands the service.
type lifecycleAuth struct {
	stubAuth
	err    error
	calls  int
	slug   string
	header http.Header
	body   []byte
}

func (a *lifecycleAuth) OIDCLifecycle(_ context.Context, slug string, header http.Header, body []byte) error {
	a.calls++
	a.slug, a.header, a.body = slug, header, body
	return a.err
}

func postLifecycle(t *testing.T, auth *lifecycleAuth, slug string, body []byte, headers map[string]string) (*http.Response, string) {
	t.Helper()
	srv := httptest.NewServer(server.New(&server.API{Auth: auth}, nil))
	defer srv.Close()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+api.PathPrefix+"/auth/oidc/"+slug+"/lifecycle", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, _ := io.ReadAll(resp.Body)
	return resp, string(out)
}

func TestOIDCLifecycleHTTP(t *testing.T) {
	t.Run("headers and raw body reach the service", func(t *testing.T) {
		auth := &lifecycleAuth{}
		raw := []byte(`{"type":"user.deleted",  "data":{"id":"u"}}`)
		resp, _ := postLifecycle(t, auth, "clerk", raw, map[string]string{
			"Svix-Id": "msg_1", "Webhook-Signature": "v1,abc", "Content-Type": "application/json",
		})
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("status %d, want 204", resp.StatusCode)
		}
		if auth.slug != "clerk" || !bytes.Equal(auth.body, raw) {
			t.Fatalf("service saw slug %q body %q", auth.slug, auth.body)
		}
		if auth.header.Get("Svix-Id") != "msg_1" || auth.header.Get("Webhook-Signature") != "v1,abc" {
			t.Fatalf("headers did not reach the service: %v", auth.header)
		}
	})
	t.Run("error mapping", func(t *testing.T) {
		for name, tc := range map[string]struct {
			err    error
			status int
		}{
			"unauthenticated": {domain.ErrUnauthenticated, http.StatusUnauthorized},
			"overloaded":      {admission.ErrOverloaded, http.StatusTooManyRequests},
			"retry":           {service.ErrLifecycleRetry, http.StatusServiceUnavailable},
			"fault":           {errors.New("disk on fire"), http.StatusInternalServerError},
		} {
			resp, body := postLifecycle(t, &lifecycleAuth{err: tc.err}, "clerk", []byte("{}"), nil)
			if resp.StatusCode != tc.status {
				t.Fatalf("%s: status %d, want %d (%s)", name, resp.StatusCode, tc.status, body)
			}
			if strings.Contains(body, "disk on fire") {
				t.Fatalf("%s leaked the cause: %s", name, body)
			}
			if tc.status != http.StatusInternalServerError && resp.Header.Get("Retry-After") == "" && tc.status != http.StatusUnauthorized {
				t.Fatalf("%s: no Retry-After", name)
			}
		}
	})
	t.Run("64 KiB bound", func(t *testing.T) {
		auth := &lifecycleAuth{}
		resp, _ := postLifecycle(t, auth, "clerk", bytes.Repeat([]byte("a"), service.MaxLifecycleBodyBytes), nil)
		if resp.StatusCode != http.StatusNoContent || len(auth.body) != service.MaxLifecycleBodyBytes {
			t.Fatalf("a body at the bound: status %d, %d bytes", resp.StatusCode, len(auth.body))
		}
		auth = &lifecycleAuth{}
		resp, _ = postLifecycle(t, auth, "clerk", bytes.Repeat([]byte("a"), service.MaxLifecycleBodyBytes+1), nil)
		if resp.StatusCode != http.StatusBadRequest || auth.calls != 0 {
			t.Fatalf("a body over the bound: status %d, service calls %d, want 400 and 0", resp.StatusCode, auth.calls)
		}
	})
	t.Run("malformed slug", func(t *testing.T) {
		auth := &lifecycleAuth{}
		resp, _ := postLifecycle(t, auth, strings.Repeat("a", 65), []byte("{}"), nil)
		if resp.StatusCode != http.StatusBadRequest || auth.calls != 0 {
			t.Fatalf("an over-long slug: status %d, service calls %d, want 400 and 0", resp.StatusCode, auth.calls)
		}
	})
}
