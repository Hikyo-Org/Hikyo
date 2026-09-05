# Private federation transport handoff

Private working document. Do not publish the advisory, worktree, patch, tests, or this handoff without the parent's coordinated disclosure decision.

Base: `33a213ca80bfff4ffb4a808cf916f209fd90138d`. Advisory: `GHSA-p29g-pjgc-jqmv`.

## Operator configuration

Default human OIDC egress permits canonical HTTPS endpoints that resolve entirely to public addresses. It rejects ambient HTTP/HTTPS/ALL_PROXY forwarding and every redirect, including same-origin redirects. Discovery, token and JWKS requests each resolve immediately before a new connection and dial only approved IPs. TLS authenticates the original endpoint hostname; certificate verification and TLS1.2 minimum remain enabled. Standard system trust applies; for a private CA, install the CA in the server's trust store (or use Go's supported system-root file/directory environment configuration). There is no insecure TLS switch.

Set `HIKYO_OIDC_EGRESS_POLICY_FILE` to an operator-owned JSON file before starting `server` or `admin`. Changes require restart; provider administrators and discovery documents cannot grant network exceptions. Example with documentation-only addresses:

```json
{
  "https://login.internal.example": ["10.42.10.0/24"],
  "https://token.internal.example:8443": ["10.42.20.15/32"],
  "https://keys.internal.example": ["fd00:42:30::/64"]
}
```

Each key is the exact HTTPS origin: scheme plus lowercase host and optional explicit port, no userinfo, path, query, or fragment. Every private discovery, token and JWKS origin needs its own grant. A grant for discovery does not authorize a different token/JWKS origin, host, or port. Different public origins need no private grant. All resolved addresses must satisfy public policy or that origin's explicit CIDRs, including mixed DNS answers. Keep grants narrow; these are network authorities, not application credentials. Do not put client secrets in this file. A malformed file fails configuration closed; credential-like userinfo is not echoed in the error.

An explicit existing `--dev` flag permits HTTP only to literal loopback IPs (`127.0.0.1` or `::1`), and permits those IPs at the address boundary. It does not permit HTTP DNS aliases, private LAN hosts, or arbitrary public HTTP. Tests set this policy explicitly on isolated fixtures. Production defaults never infer development from an issuer or listener address.

## Fixed availability limits

| Leg | Strict decoded-body ceiling | Total operation/client deadline |
| --- | --- | --- |
| Discovery | 1,048,576 bytes | 15 seconds |
| JWKS | 1,048,576 bytes | 15 seconds |
| Token response | 262,144 bytes | 15 seconds |

Connection and TLS handshake limits are5seconds; response headers are limited to64KiB and5seconds. Body measurement happens after HTTP content decoding, includes trailing bytes after valid JSON, and reads cap+1 before returning anything to the OIDC library. Both known-length and chunked/compressed overflows fail. Bodies close on every success/refusal, and each request's transport closes idle connections. Operation errors expose only closed refusal classes, never upstream bodies, tokens or URL credentials.

`go-oidc` intentionally detaches JWKS cancellation from its caller. The injected HTTP client's independent total timeout still terminates detached work; the real-wire test proves its upstream socket releases. Discovery, Exchange and VerifierContext all override ambient context clients with the owned client. OIDC signature algorithms, issuer/audience/azp/expiry/iat checks, nonce handling, PKCE and application authorization remain unchanged.

## Decisions

1. Reuse `netpolicy.PublicDialer`, already shared by SAML and authenticated adapters. Keep SAML's current proxy-compatible metadata behavior unchanged.
2. Add a separate OIDC policy file using the existing exact-origin/CIDR parser model. SAML has no private-IdP allowlist; inheriting adapter grants would silently widen unrelated authority.
3. Disable all proxies and redirects. This is the recommended first secure policy: an opaque proxy cannot substitute its remote DNS for the locally approved IP. Organizations requiring forced outbound proxies must explicitly review a pinned proxy extension; this patch offers no fallback.
4. Keep config as a leaf package. A full boundary test caught an initial transport import; configuration now uses only standard-library validation, and transport independently validates/copies policy at use. Empty policy entries remain present for validation.
5. Keep all patch and evidence private. No commit, push, public PR, advisory update, or release signature was performed.

## Verification

Baseline `TestDiscoveryRefusesImplicitLoopbackHTTP` failed on the original source: implicit HTTP loopback discovery returned nil error. Same test passes after implementation.

Transport tests exercise explicit private-origin grants; DNS pinning, mixed public/private answers and rebinding; disabled proxies and redirect pivots; exact-cap acceptance and known/chunked/gzip overflow; slow headers/body with real upstream request release. OIDC wire tests exercise all three network legs, ambient-client override, discovered unsafe endpoint refusal, closed upstream-error bodies, oversized discovery/token/JWKS responses, and detached JWKS timeout release.

Focused both-engine `^TestOIDC` and the same suite under race each passed58 events with0skips (19tests,38engine subtests, package). Service SAML metadata and app candidate tests passed. Final transport/OIDC/config/boundary race verification is recorded in the private report. The full repository suite and public CI remain parent's integration responsibility.

Private fixture credentials remain in `../federation-pg-private.json`, never in reports. The shared owned PostgreSQL container has a separate federation base/sibling database; it does not use SCIM's fixture database. Retain it until the parent completes any independent rerun; cleanup only those owned federation databases.
