// Package oidcrp is the OIDC relying-party wrapper: discovery, authorization
// URL construction, code exchange and complete ID-token validation, all behind
// go-oidc and golang.org/x/oauth2. It exists so the protocol library is used in
// exactly one place under one policy - the human-auth ADR's "library selection
// is not policy selection" - and the boundary test pins who may import it
// (internal/service and its tests only).
//
// It owns wire mechanics, never product policy: purpose walls, byte-exact
// (issuer, subject) linking, mix-up defence, the browser binding, assurance
// evaluation and the nonce/state single-use bookkeeping all live in
// internal/service, because they are decisions a library does not make.
package oidcrp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/federationhttp"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// allowedAlgs is the signature-algorithm allowlist. `none` is never in it, and
// go-oidc refuses an unsigned token: algorithm confusion via an unvalidated
// `alg` is closed by pinning the set rather than trusting the header.
var allowedAlgs = []string{
	oidc.RS256, oidc.RS384, oidc.RS512,
	oidc.ES256, oidc.ES384, oidc.ES512,
	oidc.PS256, oidc.PS384, oidc.PS512,
}

// allowedSkew is the clock tolerance for the iat freshness check: an IdP whose
// clock runs slightly ahead is tolerated up to this bound, further is refused.
const allowedSkew = 2 * time.Minute

// Sentinel refusals, mapped by the service to closed audit causes. Every one is
// a refusal, never a downgrade.
var (
	// ErrDiscovery is a discovery/JWKS fetch or issuer-mismatch failure.
	ErrDiscovery = errors.New("oidcrp: discovery failed")
	// ErrEmptySubject is a token with no subject (A15).
	ErrEmptySubject = errors.New("oidcrp: token carries no subject")
	// ErrAudience is a token whose azp, when present, is not this client.
	ErrAudience = errors.New("oidcrp: token azp is not this client")
	// ErrTokenInvalid is any other validation failure (signature, aud, expiry).
	ErrTokenInvalid = errors.New("oidcrp: token validation failed")
	// ErrNoIDToken is a token response carrying no id_token.
	ErrNoIDToken = errors.New("oidcrp: token response carried no id_token")
	// ErrExchange is a code-exchange transport or grant failure.
	ErrExchange = errors.New("oidcrp: code exchange failed")
)

// IssuerMismatchError is a discovery document whose `issuer` differs from
// the configured string. It carries the document's issuer because that value
// is the remedy for the one deployment mistake it exists for (#588 d1): an
// Entra `common`, `organizations` or domain-name authority discovers to the
// tenant's GUID issuer (or the `{tenantid}` placeholder), and the row must be
// configured with the GUID form. It unwraps to ErrDiscovery.
type IssuerMismatchError struct {
	Discovered string
}

func (e *IssuerMismatchError) Error() string {
	return fmt.Sprintf("oidcrp: the discovery document names issuer %q", e.Discovered)
}

func (e *IssuerMismatchError) Unwrap() error { return ErrDiscovery }

// Provider is a discovered OpenID Provider pinned to a byte-exact issuer.
type Provider struct {
	issuer       string
	op           *oidc.Provider
	client       *http.Client
	tokenClient  *http.Client
	subjectTypes []string
}

// Discover reconstructs a provider via go-oidc NewProvider, which re-asserts
// the byte-exact issuer against the discovery document (A20): every refresh
// rebuilds from the document, never patches a cached endpoint set. The issuer
// passed here is the byte-exact string; NewProvider returns IssuerMismatchError
// when the document disagrees.
func Discover(ctx context.Context, issuer string) (*Provider, error) {
	return DiscoverWithPolicy(ctx, issuer, federationhttp.Policy{})
}

// DiscoverWithPolicy discovers an issuer, binding every network leg to the
// immutable operator egress policy, and rejects invalid issuer or endpoint
// URLs. A document naming a different issuer returns IssuerMismatchError,
// which unwraps to ErrDiscovery; other discovery and transport failures also
// return ErrDiscovery.
func DiscoverWithPolicy(ctx context.Context, issuer string, policy federationhttp.Policy) (*Provider, error) {
	target, err := federationhttp.ValidateURL(issuer, policy.Development)
	if err != nil || target.RawQuery != "" || target.ForceQuery {
		return nil, ErrDiscovery
	}
	client, err := federationhttp.NewClient(policy, federationhttp.DocumentBytes)
	if err != nil {
		return nil, ErrDiscovery
	}
	tokenClient, err := federationhttp.NewClient(policy, federationhttp.TokenBytes)
	if err != nil {
		return nil, ErrDiscovery
	}
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, client), federationhttp.Deadline)
	defer cancel()
	op, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		var mismatch *oidc.IssuerMismatchError
		if errors.As(err, &mismatch) {
			return nil, &IssuerMismatchError{Discovered: mismatch.Discovered}
		}
		// The operator log gets the class of the cause; callers answer on
		// errors.Is(ErrDiscovery) alone, so the wire is unchanged. go-oidc's
		// own error is not wrapped: it carries the provider's response body,
		// which must never reach a log (TestAllOIDCLegsUseBoundedClient).
		return nil, fmt.Errorf("%w: %s", ErrDiscovery, discoveryCause(err))
	}
	if op.Endpoint().AuthURL == "" || op.Endpoint().TokenURL == "" {
		return nil, fmt.Errorf("%w: discovery document is missing an authorization or token endpoint", ErrDiscovery)
	}
	var metadata struct {
		JWKS         string   `json:"jwks_uri"`
		SubjectTypes []string `json:"subject_types_supported"`
	}
	if err := op.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("%w: the discovery document's metadata does not decode", ErrDiscovery)
	}
	for _, endpoint := range []string{op.Endpoint().AuthURL, op.Endpoint().TokenURL, metadata.JWKS} {
		if _, err := federationhttp.ValidateURL(endpoint, policy.Development); err != nil {
			return nil, ErrDiscovery
		}
	}
	return &Provider{issuer: issuer, op: op, client: client, tokenClient: tokenClient, subjectTypes: metadata.SubjectTypes}, nil
}

// discoveryCause classifies a discovery failure for the operator log without
// quoting it: a transport failure (unreachable, refused by egress policy, a
// deadline) or a provider answer that is not a usable document.
func discoveryCause(err error) string {
	var transport *url.Error
	if errors.As(err, &transport) || errors.Is(err, context.DeadlineExceeded) {
		return "the discovery request did not complete"
	}
	return "the provider answered without a usable discovery document"
}

// Issuer returns the byte-exact issuer this provider is pinned to.
func (p *Provider) Issuer() string { return p.issuer }

// PairwiseSubjectsOnly reports whether the discovery document advertises
// `subject_types_supported` as a nonempty list containing only `pairwise`
// (#588 d2): its `sub` values are bound to the client registration. Missing
// subject types return false. Provider-asserted, never keyed on the issuer host.
func (p *Provider) PairwiseSubjectsOnly() bool {
	return len(p.subjectTypes) > 0 && !slices.ContainsFunc(p.subjectTypes, func(t string) bool { return t != "pairwise" })
}

func (p *Provider) config(clientID, clientSecret, redirectURI, scopes string) oauth2.Config {
	return oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     p.op.Endpoint(),
		RedirectURL:  redirectURI,
		Scopes:       splitScopes(scopes),
	}
}

// AuthCodeURL builds the authorization request URL with PKCE S256 always, the
// state and nonce, and - for reauth - prompt=login and max_age=0, which the
// caller passes via extra. It never derives the redirect from a request header.
// The client secret is not part of an authorization request, so none is taken.
func (p *Provider) AuthCodeURL(clientID, redirectURI, scopes, state, nonce, pkceVerifier string, extra map[string]string) string {
	cfg := p.config(clientID, "", redirectURI, scopes)
	opts := []oauth2.AuthCodeOption{
		oauth2.S256ChallengeOption(pkceVerifier),
		oidc.Nonce(nonce),
	}
	for k, v := range extra {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}
	return cfg.AuthCodeURL(state, opts...)
}

// Exchange trades the authorization code for tokens at the RECORDED provider's
// token endpoint only, presenting the PKCE verifier, and returns the raw ID
// token. The client secret is used here and nowhere else, and arrives as bytes
// so the plaintext window is the exchange call alone.
//
// ponytail: oauth2.Config.ClientSecret is an immutable string, so the
// conversion below leaves one plaintext copy the GC owns and we cannot zero —
// the residual ceiling. It lives only for this exchange and is not retained by
// the caller. Removing it entirely needs an oauth2 client that accepts a
// []byte secret (none exists); revisit if x/oauth2 ever grows one.
func (p *Provider) Exchange(ctx context.Context, clientID string, clientSecret []byte, redirectURI, scopes, code, pkceVerifier string) (string, error) {
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, p.tokenClient), federationhttp.Deadline)
	defer cancel()
	cfg := p.config(clientID, string(clientSecret), redirectURI, scopes)
	tok, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		return "", ErrExchange
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", ErrNoIDToken
	}
	return raw, nil
}

// Claims is the validated ID-token content the service policy consults. Nonce
// is returned raw for the caller to compare against the hashed transaction
// value (A19); the AMR/ACR/auth_time are what the provider asserted, recorded
// verbatim in the assurance record (A12). No unverified claims leave this boundary.
//
// Issuer is the PINNED issuer, never the token's `iss` (#588 d3): go-oidc
// tolerates Google's bare `accounts.google.com`, and no caller may see it.
// Raw is every claim of the signed token, undecoded, for the policy reads that
// consult the token itself (the verified-email assertion and the allowlist
// claim, #598 d5): parse, don't cast.
type Claims struct {
	Issuer          string
	Raw             map[string]json.RawMessage
	Subject         string
	Nonce           string
	ACR             string
	AMR             []string
	AuthorizedParty string
	AuthTime        time.Time
	HasAuthTime     bool
}

// Verify validates an ID token completely: the issuer by go-oidc's own
// unconditional check (SkipIssuerCheck is never set; its only relaxation is
// Google's documented bare `accounts.google.com`, #588 d3, so Hikyo keeps no
// duplicate belt), signature with an algorithm from the allowlist (never none),
// audience containing this client, and azp when present equaling this client.
// It rejects an expired token, an absent iat, or an iat more than two minutes
// in the future or after exp. Empty subject is refused (A15). Nonce equality
// is the caller's, because the transaction stores it hashed. Validation
// failures return ErrTokenInvalid, except for ErrEmptySubject and ErrAudience.
func (p *Provider) Verify(ctx context.Context, clientID, rawIDToken string, now func() time.Time) (Claims, error) {
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, p.client), federationhttp.Deadline)
	defer cancel()
	verifier := p.op.VerifierContext(ctx, &oidc.Config{
		ClientID:             clientID,
		SupportedSigningAlgs: allowedAlgs,
		Now:                  now,
	})
	tok, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return Claims{}, ErrTokenInvalid
	}
	if tok.Subject == "" {
		return Claims{}, ErrEmptySubject
	}
	// iat must be present, not from the future beyond the skew, and not after
	// exp: go-oidc validates exp but leaves iat sanity to the caller.
	if tok.IssuedAt.IsZero() {
		return Claims{}, fmt.Errorf("%w: token carries no iat", ErrTokenInvalid)
	}
	if tok.IssuedAt.After(now().Add(allowedSkew)) {
		return Claims{}, fmt.Errorf("%w: token iat is in the future", ErrTokenInvalid)
	}
	if tok.IssuedAt.After(tok.Expiry) {
		return Claims{}, fmt.Errorf("%w: token iat is after exp", ErrTokenInvalid)
	}
	var extra struct {
		ACR      string   `json:"acr"`
		AMR      []string `json:"amr"`
		AZP      string   `json:"azp"`
		AuthTime *int64   `json:"auth_time"`
	}
	var raw map[string]json.RawMessage
	if err := tok.Claims(&extra); err != nil {
		return Claims{}, ErrTokenInvalid
	}
	if err := tok.Claims(&raw); err != nil {
		return Claims{}, ErrTokenInvalid
	}
	if extra.AZP != "" && extra.AZP != clientID {
		return Claims{}, ErrAudience
	}
	c := Claims{
		Issuer: p.issuer, Raw: raw, Subject: tok.Subject, Nonce: tok.Nonce,
		ACR: extra.ACR, AMR: extra.AMR, AuthorizedParty: extra.AZP,
	}
	if extra.AuthTime != nil {
		c.AuthTime = time.Unix(*extra.AuthTime, 0).UTC()
		c.HasAuthTime = true
	}
	return c, nil
}

func splitScopes(scopes string) []string {
	fields := strings.Fields(scopes)
	hasOpenID := false
	for _, f := range fields {
		if f == oidc.ScopeOpenID {
			hasOpenID = true
		}
	}
	if !hasOpenID {
		fields = append([]string{oidc.ScopeOpenID}, fields...)
	}
	return fields
}
