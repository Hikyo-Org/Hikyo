package oauth2rp

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGitHubProfile(t *testing.T) {
	p, err := New("github", "https://github.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(p.AuthCodeURL("client", "https://hikyo.test/callback", "state", "verifier"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "github.com" || q.Get("scope") != "user:email" || q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != "https://hikyo.test/callback" || q.Get("code_challenge") == "verifier" {
		t.Fatalf("unsafe authorization URL: %s", u)
	}
	for _, origin := range []string{"https://github.com/", "http://github.com", "https://github.com?x=1", "https://enterprise.example", "https://github.com:443", "https://github.com#x"} {
		if _, err := New("github", origin, nil); err == nil {
			t.Errorf("accepted origin %q", origin)
		}
	}
	if _, err := New("oidc", "https://github.com", nil); err == nil {
		t.Fatal("accepted unknown profile")
	}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestGitHubEmailPagination(t *testing.T) {
	for _, mode := range []string{"later-primary", "later-error", "exhausted", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			p, _ := New("github", "https://github.com", &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "api.github.com" || r.URL.Path != "/user/emails" || r.URL.Query().Get("per_page") != "100" {
					t.Fatalf("unpinned request: %s", r.URL)
				}
				if mode == "later-error" && calls == 2 {
					return nil, errors.New("fixture")
				}
				rows := make([]map[string]any, 100)
				for i := range rows {
					rows[i] = map[string]any{"email": "other@example.com", "primary": false, "verified": true}
				}
				if calls == 2 && mode != "exhausted" {
					rows = []map[string]any{{"email": "primary@example.com", "primary": true, "verified": true}}
				}
				if mode == "duplicate" && calls == 1 {
					rows[0] = map[string]any{"email": "first@example.com", "primary": true, "verified": true}
				}
				b, _ := json.Marshal(rows)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b))), Header: http.Header{}}, nil
			})})
			got, err := p.VerifiedEmail(t.Context(), "fixture")
			switch mode {
			case "later-primary":
				if err != nil || got != "primary@example.com" || calls != 2 {
					t.Fatalf("got %q %v calls %d", got, err, calls)
				}
			case "duplicate":
				if err != nil || got != "" {
					t.Fatalf("duplicate admitted: %q %v", got, err)
				}
			default:
				if !errors.Is(err, ErrUserInfo) || got != "" || calls > 10 {
					t.Fatalf("not refused: %q %v calls %d", got, err, calls)
				}
			}
		})
	}
}
func TestGitHubProfileCannotSupplyAdmissionClaims(t *testing.T) {
	p, _ := New("github", "https://github.com", &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"id":42,"name":"Display","company":"allowed","email_verified":true,"email":"profile@example.com"}`)), Header: http.Header{}}, nil
	})})
	user, err := p.User(t.Context(), "fixture")
	if err != nil || user.Subject != "42" || len(user.Claims) != 1 || string(user.Claims["name"]) != `"Display"` {
		t.Fatalf("profile claims: %+v %v", user, err)
	}
}
