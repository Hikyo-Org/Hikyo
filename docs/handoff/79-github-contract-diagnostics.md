# #79: Contract-only organization-variable diagnostics

## Purpose and limits

The direct-source GitHub contract can now emit `provider_operation` evidence for
organization-variable creates and selected-recipient full replacements. The
observer is compiled only into tests. Production adapter behavior,
`ResponseError`, the acceptance runner and its all-six/18 verifier are unchanged.

This is diagnostic preparation, not a behavioral repair or acceptance result.
The failed qualification on source
`45f1608de1fede18fa4f00ca5ba3ec2423e8ca8a`, tree
`881270448dcef9f60ad5c07ad1ab42163b05325d`, remains HOLD. Its immutable failed
receipt SHA-256 is
`6cffa6132fe2ed6007a38eba6402030b995b4b239e60ddcf72eec94ba0c493dd`.
That attempt consumed 13 of its 18 dispatch slots. The unused five do not
authorize a retry. The 404 cause, including possible outage involvement, remains
unconfirmed. No historical receipt is amended by this observer.

## Evidence boundary

Only names already registered for cleanup are observed. The destination must
match the configured organization ID and owner, `selected` visibility, and the
single configured fixture repository ID. Names must match the bounded synthetic
`HIKYO_CONTRACT_*` grammar. Requests must use the exact GitHub HTTPS origin and
expected POST-create or PUT-recipient route, with no query, fragment or userinfo.
Unrelated operations and artifact URLs are passed through without observation.

Each event contains:

- Operation and HTTP method, synthetic name, organization identity, surface,
  selected visibility and exact selected repository ID.
- `metadata_source: owned_call_arguments`. These fields describe the arguments
  passed to the unchanged client, not an inspection or attestation of wire bodies.
- UTC `started_at` and `finished_at`, HTTP status when present, and one closed
  error category: `success`, `http_error`, `http_404_ambiguous`, or
  `transport_error`. A 404 never asserts a permission or provider root cause.
- Optional `request_id` from `X-GitHub-Request-Id` (at most 128 characters,
  bounded hexadecimal groups) and `accepted_permissions` from
  `X-Accepted-GitHub-Permissions` (at most 256 characters, only
  `organization_variables=read|write` and `metadata=read`, with comma/semicolon
  separators). Duplicate, malformed, oversized or reflected secret/value headers
  are omitted with a fixed `*_redacted` flag. Missing headers stay absent.

The permissions header describes endpoint requirements, not the token's actual
grants. GitHub documents its comma/semicolon syntax in
[REST troubleshooting](https://docs.github.com/en/rest/using-the-rest-api/troubleshooting-the-rest-api#resource-not-accessible).
The [replacement endpoint](https://docs.github.com/en/rest/actions/variables#set-selected-repositories-for-an-organization-variable)
documents organization Variables write and a successful 204 response.

The transport observer never reads Authorization, request/response bodies,
variable values, raw transport errors, arbitrary headers or signed URLs. The
test client wrapper supplies only internal length/SHA-256 fingerprints of its
credential and every observed owned create value, retaining earlier values
across names and failed/duplicate attempts, to reject reflected header content.
These fingerprints are never emitted. It
returns the original response/error, forwards each request once, preserves idle
connection cleanup, and adds no retry or workflow dispatch.

## Local regression checks and future execution

With the repository's pinned Go toolchain, run only the owning package:

```sh
HIKYO_GITHUB_CONTRACT=0 GOTOOLCHAIN=local GOMAXPROCS=2 \
  go test -p 2 -parallel 2 -count=1 ./internal/adapter/githubactions
```

Synthetic tests check variable-route 204/404, create-201 followed by
recipient-PUT-404, status-only errors, unknown/dispatched custody after a landed
create, no automatic retry/dispatch, malicious and reflected metadata redaction,
body/error pass-through, owned-scope filtering, and transport cleanup.

No live provider retest is part of this change. A future attempt still needs
deliberate candidate selection, a valid source-bound authorization, seven fresh
reviewed distinct positive/negative/harness PATs, protected input custody, its own
dispatch budget and fresh evidence directory. Cleanup and independent external
revocation of all seven PATs remain mandatory after success or failure.
The [acceptance packet](79-github-provider-acceptance.md) owns the execution
contract; its dated preparation observations are historical, not a substitute
for the owner's current fixture and candidate authorization.
