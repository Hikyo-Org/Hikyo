package oauth2rp

import (
	"net/url"
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
