# Public deployment acceptance

This procedure implements the evidence boundary for [#1055](https://github.com/Hikyo-Org/Hikyo/issues/1055).
It verifies a named installation through its real ingress. It does not certify a
release, authorize live changes, or extend the supported client/provider matrix.
Use the [dated handoff](../handoff/1055-public-deployment-acceptance.md) for the
current inventory and outstanding decisions.

## Authority and evidence

Before any request, record the instance owner, authorized origin, permitted
methods, rate budget and observation window in a private operator record. A
read-only inventory authorization permits bounded anonymous GETs and missing
credential refusals on registered read routes. It does not permit login attempts,
fabricated tokens, credential issuance/revocation, fixture writes, policy changes,
upgrades, containment, restore, or deletion. Each requires explicit operational
authority and owner-approved disposable fixtures. Do not probe guessed object
identifiers or scan management networks. Preserve existing access controls when
a tool refuses a command.

Keep these evidence classes separate for every result:

| Class | Establishes | Does not establish |
| --- | --- | --- |
| Source | Tests and inspected behavior at a source commit | The bytes or configuration running behind ingress |
| Published artifact | Verified manifest, signature, provenance and immutable digest | That this installation runs those bytes |
| Deployment | Canonical owner revision, desired image, runtime image/process identity and policy | End-to-end ingress behavior or enforcing network isolation |
| Runtime | A dated observation through a named path, with an explicit oracle | Every credential, route, client or later deployment |

Use `PASS`, `FAIL`, `NOTRUN`, `SKIPPED` and `HOLD` explicitly. `HOLD` means the
acceptance condition remains open; it is not a pass. Missing/expired receipts,
unknown provenance and partial checks never inherit another candidate's result.

## Inventory, provenance and installation disposition

For every installation, privately record the public origin, ingress/reverse-proxy
owner and upstream chain, deployment owner, datastore engine, operational access,
parallel-instance purpose and responsible operator. Assign a non-identifying alias
before exporting evidence. A chart label or an image tag is not runtime identity.

Read `GET /api/v1/meta` through ordinary verified public HTTPS, without a cookie
jar or Authorization header. Record `server_version` and `api_revision` only.
`GetMeta` can also emit `instance_identity` when Discovery is configured; do not
publish that identifier. The handler's [source](../../internal/server/api.go) and
the [OpenAPI schema](../../api/openapi.yaml) govern the shape, not a minimal
three-field fixture or an old screenshot. Record public runtime availability
separately; a successful metadata request is not a readiness/restore proof.

Obtain provenance through the canonical deployment owner:

1. Kubernetes: record GitOps/Helm revision, rendered chart identity, desired
   digest and each running container's `imageID`. Resolve OCI index to the
   selected platform manifest/config and verify that relationship. A runtime
   that reports the index digest alone leaves the selected child unproved.
2. Native/Compose: record the service/project owner and running executable/image
   digest, then compare the exact bytes to authenticated published provenance.
   A version string alone is self-reported, not a binary measurement.
3. Verify the release manifest and image signature against the reviewed trust
   policy. Record source commit, manifest digest, image index/child digests,
   verifier version and result. Downloading a JSON manifest is not authentication.
4. Give each installation a deliberate disposition: retain with an approved
   purpose and upgrade target, retire its public route while preserving data,
   or contain access pending qualification. Record recommendation versus approval
   versus execution separately. No action follows from this runbook alone.

The 23 October 2026 builds in #1055 are historical observations. For upgrades,
follow [manual verified upgrades](manual-upgrades.md),
[signed nightlies](signed-nightlies.md), and the
[signed compatibility ADR](../adr/signed-upgrade-compatibility.md). Select a
target only after authenticating its exact supported route, backup, independent
restore, writer fencing and recovery custody. A newer version or image does not
prove a supported upgrade edge. Do not create trust, sign with recovery keys,
switch to development mode, or reset installation custody to make a target boot.

## Deliberate public surface

Review the deployed version's registered routes and the current
[OpenAPI contract](../../api/openapi.yaml). `ClassUnauthenticated` is an admission
classification, not an anonymous allowlist. The following is the reviewed source
surface; old deployments can differ and must be recorded separately.

| Surface | Deliberately session-free behavior |
| --- | --- |
| `GET /api/v1/meta` | Version, revision and protocol discovery; optional instance identity |
| `GET /api/v1/runtime/status` | Closed availability/upgrade phase, no tenant/configuration details |
| `GET /api/v1/auth/methods` | Configured sign-in method discovery |
| `GET /api/v1/auth/saml/{provider}/metadata` | Registered provider metadata; inspect/probe only an owner-approved provider |
| `POST /api/v1/auth/local/login`, `/auth/webauthn/login/{start,finish}` | Native authentication ceremonies, bounded by pre-auth admission |
| `POST /api/v1/auth/login/challenge/{challenge}/{totp,webauthn/start,webauthn/finish}` | Bound second-factor login challenge |
| `POST /api/v1/auth/credential/establish`, `/auth/recovery/begin`, `/auth/signup`, `/auth/signup/verify` | Enrollment, recovery or registration authorities; session-free does not mean authority-free |
| `POST /api/v1/auth/cli-login/start`, `/auth/cli-login/device/start`, `/auth/cli-login/redeem`, `/auth/cli-login/device/token`, `/auth/cli-reauth/redeem` | Transaction/device authority, approval and PKCE where required by that route |
| `POST /api/v1/auth/saml/{provider}/acs` | Validated SAML response and transaction binding |
| `POST /api/v1/auth/pairing/{start,finish}`, `/auth/workspace/{start,redeem}` | One-time pairing/handoff authority and bound transaction/PKCE |

All abbreviated REST paths after `/api/v1/auth/` above retain the `/api/v1`
prefix. Check method, artifact allowlist and ceremony semantics per operation;
do not exercise POSTs under read-only inventory authority. Provider/browser
redirect routes also belong in the owner-reviewed route inventory, even when
outside OpenAPI. The SPA and static assets are intentionally public where the
binary embeds a UI; root 404 can simply mean an API-only binary.

When enabled, `/mcp` and `/mcp/codex` have their separate
[MCP contract](../adr/mcp-server.md): tenant-free discovery/catalog and lifecycle
methods do not establish authority for tool calls. #651 owns exact candidate,
public HTTPS and named-client acceptance. Reuse its one authorized isolated
fixture; do not enable MCP, deploy another server or infer supported clients
from this inventory.

`/healthz`, `/readyz` and `/metrics` belong to the operational listener, never the
public hostname. A public refusal on these registered operational paths is
useful route-partition evidence. It is not a substitute for authentication tests
on registered protected application operations.

## Bounded read-only ingress checks

Run from an ordinary client path through the public hostname, with normal CA and
hostname verification, no redirects, no cookie jar and no credentials. Record the
observation vantage; a LAN request to public DNS is not independent external
vantage proof. Use an approved external observer separately when required.

The following POSIX-shell check is safe only after origin/read-only authorization.
Set `PUBLIC_ORIGIN` to the approved HTTPS origin without a trailing slash. It
checks two registered protected operations that require no object identifiers:

```sh
set -eu
: "${PUBLIC_ORIGIN:?Set the approved HTTPS origin}"
for route in /api/v1/auth/whoami /api/v1/orgs; do
  status=$(curl --disable --silent --show-error --proto '=https' \
    --connect-timeout 5 --max-time 15 --max-redirs 0 \
    --output /dev/null --write-out '%{http_code}' "$PUBLIC_ORIGIN$route")
  printf '%s %s\n' "$route" "$status"
  test "$status" = 401
done
```

Expected: each registered route returns `401`; inspect its closed
`unauthenticated` error privately without retaining sensitive bodies. A redirect,
HTML login page, unknown-route `404`, timeout or `429` is not this oracle passing.
Do not retry into a rate limit. Compare declared anonymous endpoints with their
expected `200` closed shapes. Verify operational routes refuse through the same
ingress. Inspect every failed result before repeating requests.

## Credential and object boundary, separately authorized

Use only isolated owner-approved synthetic accounts, organizations, projects and
credentials. Never use production users or copy their data into the fixture.
Follow the [native artifact harness oracles](../../scripts/security/artifact-harness/)
without extending its loopback-only, owned-process execution boundary:

1. Prove a credential is live with a successful registered read on an existing
   synthetic object before testing its denial. Keep a sibling positive control.
2. Test absent, malformed, fabricated, naturally expired short-lived and explicitly
   revoked credentials against the uniform invalid-credential `401`, including
   relevant headers/error shape and bounded anti-enumeration comparisons. Do not
   alter server time or datastore rows to manufacture expiry or grants.
3. Use two synthetic actors/scopes. Compare cross-actor/cross-object denial on
   existing objects with an authorized missing-object twin on the same operation.
   An unknown route `404` proves neither authentication nor authorization.
4. With separate mutation authority, attempt a denied synthetic write, compare
   before/after state, and prove the legitimate sibling still works. Keep secrets
   and raw snapshots private. Check async completion before calling state unchanged.
5. Use artifact-specific revocation semantics. Parent logout does not by itself
   terminate bounded developer delegation; explicit session revocation has its
   own contract. Follow the [developer credential ADR](../adr/developer-credentials.md).

## Network and proxy acceptance

[#1041](https://github.com/Hikyo-Org/Hikyo/issues/1041) and
[PR #1043](https://github.com/Hikyo-Org/Hikyo/pull/1043) own the restrictive chart.
Consume the reviewed merged policy at its exact revision, including its explicit
opt-in/default behavior. An unmerged proposal or successful Helm render is not
an approved or applied production boundary. Change canonical chart values and
regenerate manifests through the installation's GitOps/Helm owner; for native or
Compose use the declared host/firewall owner. Never hand-edit a rendered live
object as a shortcut.

Prepare one positive/negative runtime matrix in the existing isolated fixture
before requesting a production change window:

| Check ID | Allowed control | Denied control / required observation |
| --- | --- | --- |
| `NET-INGRESS` | Approved proxy and each declared direct client reaches 8080 | Unapproved peer cannot reach 8080; test same/cross namespace and label authority |
| `NET-OPS` | Supported same-pod loopback metrics collector and health checks work | Other pods/proxies cannot reach 8081 or expose operational paths publicly |
| `NET-DNS` | Configured resolver over UDP and TCP 53 | Unapproved resolver is refused |
| `NET-DB` | Real application transaction over approved datastore TLS/socket | Unapproved datastore destination is refused |
| `NET-IDP` | Configured synthetic IdP discovery, keys and login paths | Unapproved destination is refused; trust verification stays enabled |
| `NET-PROVIDER` | Authorized synthetic provider/remote operation succeeds | An approved isolated deny target is unreachable |
| `NET-CLEANUP` | Async revoke/delete/retry cleanup reaches the synthetic provider after client exit | Queues drain and provider absence is independently read back; denied egress must not strand cleanup |
| `NET-DEPENDENCIES` | Declared SMTP, release lookup, collector, storage monitor or rollout APIs work where enabled | Undeclared destinations are refused; disabled features create no extra allowances |
| `NET-MANAGEMENT` | Operator retains its approved independent recovery path | Workload cannot reach an isolated management-network sentinel; no live management scan |
| `PROXY-IP` | Approved chain attributes a synthetic request to the known client | Client-supplied spoofed forwarding headers do not choose the audited source/admission identity |

For each network row, retain source/destination *aliases*, policy/render digest,
CNI/version, enforcing mode, protocol/port, successful allowed control, denied
control and teardown result privately. First prove each deny-target listener is
live using the same request without the restriction in the disposable fixture;
then apply the approved restriction and repeat while sibling allowed controls
still succeed. A dead listener, wrong destination, DNS failure or unrelated
outage is not a passing isolation result. Verify the CNI actually enforces both
directions. Kubernetes NetworkPolicies are additive: inventory every policy
selecting the workload and prove there is no surviving broad allowance.
Selectors in a peer intersect, separate peers are alternatives, and permission
to relabel/create selected pods/namespaces is part of the boundary. Standard
NetworkPolicy has no FQDN allowlist. Dynamic provider addresses require an
explicit supported CNI/FQDN or reviewed destination policy, never invented CIDRs
or universal HTTPS egress. Cover IPv6 when enabled. Negative probes must target
owned sentinels, not real management services.

Review the complete ingress chain and effective `HIKYO_TRUSTED_PROXY_CIDRS`.
Trust only the necessary peers, with no broad cluster/client catch-all. Proxies
must strip untrusted forwarding input or append a trustworthy peer chain, so the
right-to-left resolution cannot be controlled by a client. Normalize that chain
into one `X-Forwarded-For` field: the current handler reads the first field value,
so appending a separate field is insufficient. Confirm actual audit
attribution, not just a response header. The source tests in
[sourceip_internal_test.go](../../internal/server/sourceip_internal_test.go) cover
untrusted peers, multi-hop/all-trusted chains, malformed entries and IPv4/IPv6;
they do not prove live proxy configuration. A loopback reverse proxy can qualify
as a loopback metrics scraper, so explicitly prevent operational forwarding.

Correlate authorized refusal probes with private instance audit and bounded
[RED metrics](../../internal/server/metrics.go). Traffic counts alone do not
prove attributable refusal events or delivered alerts. #1034/#1035 own refusal
and security-audit behavior, and #1039 owns containment/recovery. Require reviewed
alert thresholds, recipients and separately authorized delivery evidence; this
procedure sends no security notification.

## Receipts, cleanup and handoff

Use a closed per-check public receipt with only these fields:

| Field | Allowed content |
| --- | --- |
| `instance` | Non-identifying alias |
| `evidence_class` | `source`, `published-artifact`, `deployment`, `runtime` |
| `check_id` | `INV-BUILD`, `INV-PROVENANCE`, `INV-OWNER`, `PUBLIC-DISCOVERY`, `AUTH-MISSING`, `AUTH-LIFECYCLE`, `AUTH-OBJECT`, `AUTH-WRITE`, `OPS-REFUSAL`, the network/proxy IDs above, `REFUSAL-SIGNAL`, `UPGRADE-RESTORE`, `CLEANUP` |
| `observed_at` | UTC timestamp |
| `identity` | API revision, version, source commit and public artifact digests; unknown members stay unavailable |
| `expected_status`, `observed_status` | HTTP status when applicable, otherwise omitted |
| `outcome`, `reason` | Closed outcome above and concise sanitized explanation |
| `authorization_ref` | Non-secret reference to the private owner approval |
| `cleanup` | `not-created`, `pass`, `hold` or `fail`, with a sanitized reason |

Keep the alias map, hosts, private addresses/CIDRs, DSNs, account/object/credential
IDs, instance identities, tokens, cookies, one-time authorities, raw response
bodies, logs, private resource names, fixture directories and user data out of
public receipts, issues and PR attachments. Review free-text reasons too. Private
operator records retain the exact route/owner/policy/resource binding needed to
reproduce the observation; an alias without that private record is insufficient.

Track exact owned fixture resources and their teardown at creation. Stop owned
workers and remove only approved disposable state after all asynchronous cleanup
and provider readback finish. Preserve pre-existing installations, recovery
backups and evidence needed for pending acceptance. Record any failed teardown
as `HOLD`; an incomplete cleanup never qualifies a passing fixture.

Write the dated scoped handoff under `docs/handoff/`, link the public source and
artifact identities, and record the outstanding owner decisions. Keep its full
offline HTML companion ignored and local. Do not update release acceptance or
support claims from partial deployment observations. Production upgrade,
containment and retirement need a separate approved plan with exact resources,
rollback/access impact and retained-data decision before execution.
