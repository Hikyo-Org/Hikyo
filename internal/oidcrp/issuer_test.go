package oidcrp

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/federationhttp"
	"github.com/Hikyo-Org/hikyo/internal/oidctest"
	"github.com/coreos/go-oidc/v3/oidc"
)

const googleIssuer = "https://accounts.google.com"

// pinnedAt builds a provider pinned to `issuer` whose documents are served by
// the fixture IdP. Only a test may take the insecure-issuer context: it stands
// in for "the real Google discovery document", which a unit test cannot fetch.
func pinnedAt(t *testing.T, idp *oidctest.IdP, issuer string) *Provider {
	t.Helper()
	op, err := oidc.NewProvider(oidc.InsecureIssuerURLContext(context.Background(), issuer), idp.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &Provider{issuer: issuer, op: op, client: http.DefaultClient, tokenClient: http.DefaultClient}
}

func tokenClaims(iss string) map[string]any {
	now := time.Now()
	return map[string]any{
		"iss": iss, "aud": "client", "sub": "1234", "nonce": "n",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	}
}

// #588 d3: go-oidc owns the ID-token issuer check. Google documents both
// `https://accounts.google.com` and the bare `accounts.google.com`; the library
// tolerates the bare form for that pinned issuer only, Hikyo keeps no belt that
// would refuse it, and Claims.Issuer is always the pinned value.
func TestGoogleBareIssuerIsTheLibraryCheck(t *testing.T) {
	idp, err := oidctest.New()
	if err != nil {
		t.Fatal(err)
	}
	defer idp.Close()
	idp.IssuerOverride = googleIssuer
	p := pinnedAt(t, idp, googleIssuer)
	for _, iss := range []string{googleIssuer, "accounts.google.com"} {
		raw, err := idp.MintIDToken(tokenClaims(iss))
		if err != nil {
			t.Fatal(err)
		}
		claims, err := p.Verify(context.Background(), "client", raw, time.Now)
		if err != nil {
			t.Fatalf("iss %q refused: %v", iss, err)
		}
		if claims.Issuer != googleIssuer {
			t.Fatalf("iss %q surfaced as %q, want the pinned issuer", iss, claims.Issuer)
		}
		if string(claims.Raw["sub"]) != `"1234"` {
			t.Fatalf("raw claims not carried: %v", claims.Raw)
		}
	}
	for _, iss := range []string{"https://accounts.google.com/", "http://accounts.google.com", "https://evil.example"} {
		raw, err := idp.MintIDToken(tokenClaims(iss))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Verify(context.Background(), "client", raw, time.Now); !errors.Is(err, ErrTokenInvalid) {
			t.Fatalf("iss %q: %v, want ErrTokenInvalid", iss, err)
		}
	}

	// The tolerance is Google's only: the bare form of any other pinned
	// issuer is refused by the same library check.
	other, err := oidctest.New()
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	other.IssuerOverride = "https://idp.example"
	q := pinnedAt(t, other, "https://idp.example")
	raw, err := other.MintIDToken(tokenClaims("idp.example"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Verify(context.Background(), "client", raw, time.Now); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("bare non-Google issuer: %v, want ErrTokenInvalid", err)
	}
}

// #588 d1: an Entra `common`, `organizations` or domain-name authority
// discovers to a document whose issuer is the tenant GUID form (or the
// `{tenantid}` placeholder); discovery refuses and names that issuer.
func TestDiscoveryIssuerMismatchNamesTheDocumentIssuer(t *testing.T) {
	const guid = "https://login.microsoftonline.com/72f988bf-86f1-41af-91ab-2d7cd011db47/v2.0"
	idp, err := oidctest.New()
	if err != nil {
		t.Fatal(err)
	}
	defer idp.Close()
	idp.IssuerOverride = guid
	_, err = DiscoverWithPolicy(context.Background(), idp.Server.URL, federationhttp.Policy{Development: true})
	var mismatch *IssuerMismatchError
	if !errors.As(err, &mismatch) || mismatch.Discovered != guid || !errors.Is(err, ErrDiscovery) {
		t.Fatalf("discovery = %v, want an IssuerMismatchError naming %q", err, guid)
	}
}

func TestPairwiseSubjectsOnly(t *testing.T) {
	for _, c := range []struct {
		types []string
		want  bool
	}{
		{nil, false},
		{[]string{"public"}, false},
		{[]string{"pairwise"}, true},
		{[]string{"public", "pairwise"}, false},
	} {
		idp, err := oidctest.New()
		if err != nil {
			t.Fatal(err)
		}
		idp.SubjectTypes = c.types
		p, err := DiscoverWithPolicy(context.Background(), idp.Server.URL, federationhttp.Policy{Development: true})
		idp.Close()
		if err != nil {
			t.Fatal(err)
		}
		if got := p.PairwiseSubjectsOnly(); got != c.want {
			t.Fatalf("subject types %v: pairwise-only = %v, want %v", c.types, got, c.want)
		}
	}
}
