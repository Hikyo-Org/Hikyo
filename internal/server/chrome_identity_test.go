package server_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestOrgIdentityImageIsServedWithItsExactType(t *testing.T) {
	image := []byte("RIFF\x00\x00\x00\x00WEBPVP8 ")
	srv := newTestServer(t, stubAuth{identity: liveIdentityFn}, stubOrgs{
		image: func(context.Context, service.Actor, domain.OrgID) (service.ChromeImage, error) {
			return service.ChromeImage{Bytes: image, ContentType: "image/webp", Version: "abababababababababababababababababababababababababababababababab"}, nil
		},
	})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+api.PathPrefix+"/orgs/"+string(testOrgID)+"/identity/image?v=abababababababababababababababababababababababababababababababab", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer hik_1_cli_x")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !bytes.Equal(body, image) {
		t.Fatalf("status %d, %d bytes", resp.StatusCode, len(body))
	}
	if got := resp.Header.Get("Content-Type"); got != "image/webp" {
		t.Errorf("Content-Type = %q, want exactly image/webp", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestOrgIdentityImageRefusalsAreUniform(t *testing.T) {
	missing := stubOrgs{}
	srv := newTestServer(t, stubAuth{identity: liveIdentityFn}, missing)
	path := api.PathPrefix + "/orgs/" + string(testOrgID) + "/identity/image"
	resp, payload := call(t, srv, http.MethodGet, path, "hik_1_cli_x", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("no image or not listed = %d %s, want 404", resp.StatusCode, payload)
	}
	anonymous := newTestServer(t, stubAuth{}, stubOrgs{
		image: func(context.Context, service.Actor, domain.OrgID) (service.ChromeImage, error) {
			return service.ChromeImage{}, domain.ErrUnauthenticated
		},
	})
	if resp, payload := call(t, anonymous, http.MethodGet, path, "hik_1_cli_x", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d %s, want 401", resp.StatusCode, payload)
	}
}
