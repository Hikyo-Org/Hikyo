# Handoff: #257 guarded SAML metadata transport

Issue: https://github.com/Hikyo-Org/Hikyo/issues/257 (parent #207; programme
#203; audit ID `BE21-B`). Base: `f55b5038`.

## Contract

- `SAMLProviders` no longer exposes an `HTTPClient` field. Production app
  wiring uses `NewSAMLProviders`, which constructs the guarded metadata
  transport with the production resolver, dialer, TLS minimum, response-header
  timeout, and whole-request timeout.
- Deterministic tests inject only `netpolicy.Resolver`, `netpolicy.Dialer`, test
  trust roots, and a shorter timeout below the policy boundary. They cannot
  replace the HTTP client or round tripper.
- Metadata URLs remain HTTPS-only and reject userinfo, fragments, missing
  hosts, and literal non-public targets before DNS or dialing.
- Every request, including each same-origin redirect, revalidates URL shape,
  resolves again, and rejects any non-public answer. Direct requests use the
  shared `netpolicy.PublicDialer`, which dials a validated pinned IP while the
  HTTP transport preserves the original host and TLS server name.
- Proxy requests retain the proxy-aware pinned-request path: the metadata
  target is validated before CONNECT and the proxy receives its approved IP.
  This avoids pretending that a `DialContext` seeing only an opaque proxy hop
  validated the ultimate metadata target.
- Redirects remain same-origin and capped at five. TLS 1.2, the 10-second
  response-header timeout, the 15-second whole-request timeout, and the
  `samlsp.MaxDocumentBytes` response limit remain enforced.

## Regression evidence

- A real TLS metadata fixture proves deterministic injected responses traverse
  the guarded URL, TLS, DNS, and dial path.
- Direct fallback coverage exercises the shared public dialer; existing proxy
  coverage pins target IP, HTTP host, TLS identity, and first-hop behavior.
- Redirect tests reject cross-origin pivots, userinfo/fragments, and a
  same-origin hostname that rebinds to loopback before the second request.
- Slow and oversized responses fail within their configured bounds.
- Constructor coverage pins the production resolver/dialer, TLS 1.2 minimum,
  response-header timeout, and whole-request timeout.
- App structural coverage pins production wiring to `NewSAMLProviders` and
  rejects restoring a `SAMLProviders` struct literal.
- Existing proxy, public-IP pinning, multi-address fallback, literal private
  target, SAML service, app, boundary, and isolation coverage remains green.

Generated outputs: none.

## Validation

```text
go test -count=1 ./internal/service/... -run SAML        passed (27 tests)
go test -count=1 ./internal/app/... ./internal/boundary/... passed (67 tests)
go test -count=1 ./internal/isolation/ -run SAML         passed (6 tests)
go test -race -count=1 ./internal/service/... -run 'TestSAMLMetadata|TestSAMLProvidersExposeNoHTTPClientOverride|TestNewSAMLProvidersBuildsProductionMetadataPolicy'
                                                          passed (17 tests)
go test -race -count=1 ./internal/netpolicy/... ./internal/adapter/...
                                                          passed (128 tests)
go build ./...                                            passed
go vet ./...                                              passed
go test -count=1 ./...                                    passed (3398 tests)
./scripts/ci/verify-docs.sh                               passed
```

Two-axis review round 1 found incomplete redirect URL-shape validation and a
missing app-wiring regression test; both were fixed. Round 2 returned Standards
`CLEAN` and Spec `SOUND`. Final round 3 returned Spec `SOUND`; Standards found
that the HTTPS-proxy TLS hop bypassed the injected dialer. The proxy hop now
uses that guarded primitive, with an executable TLS regression test.
