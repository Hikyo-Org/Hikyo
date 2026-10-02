// Package oauth2rp implements profile-pinned OAuth2 wire mechanics over x/oauth2.
// It never treats account-level MFA enrollment or a mutable login as identity proof.
package oauth2rp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Hikyo-Org/hikyo/internal/federationhttp"
	"golang.org/x/oauth2"
)

var (
	ErrProfile  = errors.New("oauth2: unsupported profile or origin; GitHub Enterprise requires verified PKCE support")
	ErrExchange = errors.New("oauth2: token exchange failed")
	ErrUserInfo = errors.New("oauth2: userinfo failed")
)

// Provider fixes every endpoint and scope. The client is bounded and refuses redirects.
type Provider struct{ client *http.Client }

func New(profile, origin string, client *http.Client) (*Provider, error) {
	if profile != "github" || origin != "https://github.com" {
		return nil, ErrProfile
	}
	if client == nil {
		var err error
		client, err = federationhttp.NewClient(federationhttp.Policy{}, federationhttp.TokenBytes)
		if err != nil {
			return nil, err
		}
	}
	return &Provider{client: client}, nil
}

func (p *Provider) config(clientID, secret, redirectURI string) *oauth2.Config {
	return &oauth2.Config{ClientID: clientID, ClientSecret: secret, RedirectURL: redirectURI,
		Scopes: []string{"user:email"}, Endpoint: oauth2.Endpoint{
			AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams,
		}}
}

func (p *Provider) AuthCodeURL(clientID, redirectURI, state, verifier string) string {
	return p.config(clientID, "", redirectURI).AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

// JSONAccept preserves the bounded transport while requiring GitHub's JSON token response.
type jsonAccept struct{ base http.RoundTripper }

func (t jsonAccept) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	copy.Header.Set("Accept", "application/json")
	return t.base.RoundTrip(copy)
}

// Exchange retains only the access token for this callback. Refresh tokens are discarded.
func (p *Provider) Exchange(ctx context.Context, clientID, secret, redirectURI, code, verifier string) (string, error) {
	if code == "" || verifier == "" || redirectURI == "" {
		return "", ErrExchange
	}
	client := *p.client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = jsonAccept{base: base}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &client)
	token, err := p.config(clientID, secret, redirectURI).Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil || token.AccessToken == "" {
		return "", ErrExchange
	}
	return token.AccessToken, nil
}

// User is a profile identity. No login, node_id or two_factor_authentication field participates.
type User struct {
	Subject string
	Claims  map[string]json.RawMessage
}

func (p *Provider) get(ctx context.Context, token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com"+path, nil)
	if err != nil {
		return ErrUserInfo
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := p.client.Do(req)
	if err != nil {
		return ErrUserInfo
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrUserInfo
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, federationhttp.TokenBytes+1))
	if err != nil || len(b) > federationhttp.TokenBytes || json.Unmarshal(b, out) != nil {
		return ErrUserInfo
	}
	return nil
}

func (p *Provider) User(ctx context.Context, token string) (User, error) {
	var raw map[string]json.RawMessage
	if err := p.get(ctx, token, "/user", &raw); err != nil {
		return User{}, err
	}
	var id int64
	if json.Unmarshal(raw["id"], &id) != nil || id <= 0 {
		return User{}, ErrUserInfo
	}
	return User{Subject: strconv.FormatInt(id, 10), Claims: raw}, nil
}

// VerifiedEmail is called only on the admitted unknown sign-up branch.
func (p *Provider) VerifiedEmail(ctx context.Context, token string) (string, error) {
	var rows []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := p.get(ctx, token, "/user/emails", &rows); err != nil {
		return "", err
	}
	address := ""
	for _, row := range rows {
		if row.Primary && row.Verified && row.Email != "" {
			if address != "" {
				return "", nil
			}
			address = row.Email
		}
	}
	return address, nil
}
