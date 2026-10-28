# #1055 public deployment acceptance, 2026-10-28

## Disposition

**HOLD: production acceptance remains incomplete.** Read-only inventory and the
missing-credential ingress checks below are complete. No installation, ingress,
policy, credential, datastore or provider mutation command was issued. This packet does
not close [#1055](https://github.com/Hikyo-Org/Hikyo/issues/1055).

The executable entry point is the
[public deployment acceptance procedure](../operations/public-deployment-acceptance.md).
It separates observation from operational authority, lists deliberate anonymous
routes, and supplies credential/object, network, provider-cleanup and recovery
oracles for a later approved window. Private owner records map aliases to actual
origins and retain command evidence. No private address, hostname, account or
instance identity is published here.

Source baseline and live main rechecked at start:
[`24e198b32a3ffd8d4013207b2f974d76ec49be6f`](https://github.com/Hikyo-Org/Hikyo/commit/24e198b32a3ffd8d4013207b2f974d76ec49be6f).
The user's assignment authorizes bounded read-only inventory and missing-credential
GET refusals. It does not authorize credential exercises or production operations.

## Observed installations

Runtime observations used ordinary verified HTTPS to the configured public
hostnames, without cookies, Authorization headers, redirects or TLS exceptions.
Vantage was the operator workstation using public DNS and real ingress, not an
independent external observer. Requests ran from **09:54:59 to 09:55:02 UTC on
2026-10-28**. Root-route behavior was not used as an authentication test.

| Runtime observation | `native-a` | `cluster-b` |
| --- | --- | --- |
| `GET /api/v1/meta` | 200, revision 12 | 200, revision 9 |
| Self-reported `server_version` | `0.0.1-nightly.20261023.91.gcd2352d2` | `0.0.1-nightly.20261009.72.g36c323c4` |
| `GET /api/v1/runtime/status` | 200, `application/json` | 200, `application/json` |
| `GET /api/v1/auth/methods` | 200, `application/json` | 200, `application/json` |
| `GET /api/v1/auth/whoami`, no credential | 401, `unauthenticated` | 401, `unauthenticated` |
| `GET /api/v1/orgs`, no credential | 401, `unauthenticated` | 401, `unauthenticated` |
| `GET /metrics`, `/healthz`, `/readyz` through public ingress | Each 404 JSON | Each 404 JSON |

`PUBLIC-DISCOVERY`, `AUTH-MISSING` and `OPS-REFUSAL` pass only the recorded status,
content type, metadata fields and parsed 401 error code. Other complete response
schemas and readiness were not validated. No provider metadata, session-free POST, protected
object ID, malformed/fabricated/expired/revoked credential or denied mutation was
exercised. The two protected routes are registered in the
[inspected OpenAPI](https://github.com/Hikyo-Org/Hikyo/blob/24e198b32a3ffd8d4013207b2f974d76ec49be6f/api/openapi.yaml).
Unknown-route 404s are not authentication evidence. Both metadata responses also
contained `instance_identity`; its value was discarded. See the conditional
[GetMeta implementation](https://github.com/Hikyo-Org/Hikyo/blob/24e198b32a3ffd8d4013207b2f974d76ec49be6f/internal/server/api.go).

### `native-a`: deployment and provenance

- Proxmox's read-only inventory reports the historical native owner container
  running. Binding that container to the currently served origin remains
  **HOLD** until the live route/process owner can be read back.
- The metadata suffix resolves to source
  [`cd2352d2cb5967955b02d730f00b2cd4811da5a6`](https://github.com/Hikyo-Org/Hikyo/commit/cd2352d2cb5967955b02d730f00b2cd4811da5a6).
  The corresponding [nightly 91 release](https://github.com/Hikyo-Org/Hikyo/releases/tag/v0.0.1-nightly.20261023.91.gcd2352d2)
  exists. This is a locator match, not measurement of the running executable.
- Downloaded public `release-manifest.json` SHA-256:
  `a89cf58bc578646d1520718e05edf0e98bd71348d268c2af80dcca6aa49f2ea7`.
  Its detached signature/full release bundle was not verified in this lane.
- `INV-PROVENANCE` is **HOLD**: read-only service inspection was refused with
  `CMD_POLICY_NOT_ALLOWLISTED`. The installed Proxmox command policy allows only
  narrow network diagnostics, not Hikyo service/process/hash reads. No policy
  edits or alternative execution path were used to bypass that refusal.
- `INV-OWNER` is **HOLD**: the historical Caddy route is a locator only. The
  OPNsense CLI read returned `Endpoint not found: /firewall/filter/reconfigure`;
  its dedicated SSH read returned `Transport closed`. No live route configuration
  or effective trusted-proxy values were obtained. No reload was requested.

### `cluster-b`: deployment and published image

Read-only Kubernetes API observations found one Ready Hikyo server replica and a
Ready PostgreSQL pod. The server and TLS staging sidecar both use:

```text
ghcr.io/hikyo-org/hikyo@sha256:afa73647dc6d42f99cf074f7f85c86cacc24c06f0d32ac0510ab697396bb8097
```

The Deployment image and both runtime `imageID` fields report this index digest.
The cluster reports arm64, Kubernetes `v1.36.2+k3s1` and
`containerd://2.3.2-k3s2`. Registry index inspection lists the arm64 child:

```text
sha256:b1e3b6711b96125055c001808a4f09489b4c23c39af64d28a1a1a830ce870419
```

Cosign **3.1.4** successfully verified the index signature with issuer
`https://token.actions.githubusercontent.com` and certificate identity
`https://github.com/Hikyo-Org/Hikyo/.github/workflows/nightly.yml@refs/heads/main`.
Claims, certificate chain and transparency-log inclusion verified. This is
**published-image signature evidence**, not a full release/provenance or runtime
behavior attestation. The child image config labels bind source
`36c323c417d1c29b20431a8c4bdb539882b9136c` and version
`0.0.1-nightly.20261009.72.g36c323c4` within that signed index's digest tree.
Independent runtime selection of the arm64 child remains
**HOLD** because the API reports the index rather than the selected child.

The private GitOps owner is `apps/hikyo-dbugit`: edit `values.hikyo.yaml` and
regenerate `hikyo.yaml`, with owner patches preserved. Flux reports Ready and
applied revision `main@sha1:1ad97b1c54c96314e077ddead0bd1430d7ad8fd3`.
Historical owner source at `649812552d773924ab649779d6bb2c20bfcf2c49` records
chart source `36c323c417d1c29b20431a8c4bdb539882b9136c` and the same index pin.
This local historical checkout is not proof of the complete current applied Git
tree. Its documented intended LAN-only edge conflicts with observed public
reachability and needs an owner decision. No Kubernetes Ingress resource for
Hikyo was found; the public route's upstream chain remains unverified outside
the cluster. The same OPNsense read failures above apply.

The [nightly 72 release](https://github.com/Hikyo-Org/Hikyo/releases/tag/v0.0.1-nightly.20261009.72.g36c323c4)
manifest names source
[`36c323c417d1c29b20431a8c4bdb539882b9136c`](https://github.com/Hikyo-Org/Hikyo/commit/36c323c417d1c29b20431a8c4bdb539882b9136c).
Downloaded manifest SHA-256:
`ab9329e20140604b29d0fbb540ccf79f00d993d5c024e4f358b3d2e0c28b32a6`.
Its detached signature/full release bundle was not verified here, and that
nightly manifest contains binary/package entries, not an OCI subject binding.
Do not promote this locator match into exact release-candidate proof.

Deployment policy remains the original ingress-only allowance for TCP 8080 with
no source selectors and no egress rules. The operational listener is bound to
8081 separately; no public operational response was observed. CNI enforcement,
cluster-wide CNI policies, direct peer isolation, effective
proxy trust and provider/cleanup functionality remain **NOTRUN/HOLD**. A policy
specification alone is not a runtime isolation result.

## Required installation decisions

These are concrete recommendations for owner review, **not approved or executed
operations**. Preserve both installations' data and recovery material.

| Installation | Recommended disposition | Alternative and consequence | Required next authority |
| --- | --- | --- | --- |
| `native-a` | Contain to approved operator access pending role/provenance confirmation; retire its public route if it has no distinct ongoing purpose | Retain public native canary with a named owner and explicit purpose; requires complete ingress, artifact, auth and recovery acceptance | Owner confirms ongoing users/dependencies, exact route and retained-data custody, then approves a scoped containment/retirement change packet |
| `cluster-b` | Retain the service/data, restore the documented private-access intent after owner confirmation, then prepare a verified upgrade | Keep public access deliberately; owner must accept that exposure and qualify the full public boundary | Owner confirms access intent, independent recovery path and downstream consumers; approve canonical GitOps/ingress change separately |

Neither installation has a selected supported 1.0 upgrade target yet. Select one
only after stabilization, trust and signed route artifacts exist. The
[manual upgrade procedure](../operations/manual-upgrades.md) and
[signed compatibility decision](../adr/signed-upgrade-compatibility.md) require
exact route verification, quiesced writers, retained encrypted backup and
independent restore. Do not upgrade either production instance to manufacture
the isolated release fixture or infer compatibility from nightly sequence.

One focused human decision remains: confirm the recommended containment/private
access intent for these two aliases, or explicitly choose the public-use
alternative with a named purpose. That intent decision still does not authorize
execution; the route/resource-specific change and fixture packet must be reviewed
before a production window.

## Dependencies and unrun acceptance

- **#1041 / PR #1043:** owns the restrictive chart and disposable enforcing-CNI
  exercise. At inspection, PR #1043 is open/draft at
  `0516ab6dfbceb7d7647e0e04b7abe920e260741b`; no production application is implied.
  Consume its reviewed final revision through the canonical installation owner.
  Apply only under separate authority after the positive/negative matrix in the
  runbook, including functional provider cleanup and operator recovery access.
- **#651:** the MCP lane owns the single isolated public fixture and exact
  candidate/client evidence. Owner coordination reports the historical fixture
  absent and no replacement deployed. Reuse is **HOLD** until a selected published
  RepoDigest, matching verified admission bundle, explicitly authorized synthetic
  fixture, HTTPS origin and credential-custody owner exist. The owner's packet is
  `docs/handoff/651-mcp-external-acceptance.md`, to be consumed with its reviewed
  PR. This lane created no fixture. These production aliases are not that fixture,
  and neither build proves the final 1.0 candidate or a supported client matrix.
- **Release exclusions:** #158 AWS acceptance, PR #969 and extracted controller
  #1057 remain outside 1.0. This packet adds no provider or deployment support
  claim and changes no accepted deferral or canonical release record.
- **#1034/#1035/#1039:** refusal attribution, security audit and scoped containment
  are their owners' work. No alert delivery, private notification, compromised
  credential exercise, restore or provider call occurred here.
- **Runtime HOLD:** credential lifecycle/object/write oracles, spoofed-header audit
  attribution, restrictive network positive/negative controls, provider cleanup,
  independent external vantage, upgrade/restore and exact candidate proof.
- **Review HOLD:** maintainer security/adversarial review and human merge gate.
  Cross-provider review is **SKIPPED** because the approved Anthropic subscription
  path is unavailable; no provider/model or paid fallback was substituted.

## Source validation and cleanup

Added direct `API.sourceIP` / `API.trusted` tests for explicit trust, spoofed
untrusted peers, right-to-left/all-trusted chains, malformed and empty hops,
mixed IPv4/IPv6 and CIDR matching. Production attribution code is unchanged.
Source behavior follows the [proxy-trust decision](../adr/system-architecture.md#transport--tls-posture-and-proxy-trust).
These tests do not certify deployed forwarding headers or audit attribution.

Validation is recorded with the PR head in its description. The selected shared
feedback plan covers the changed Go owner, and docs preparation is checked
explicitly because this Markdown-only operation guide does not select that lane
automatically. Persistence is unchanged; no new datastore fixture is required.

No deployment, container, namespace, volume, process server or provider fixture
was created. Local downloaded public manifests, signature/index evidence,
sanitized GET receipts and the private alias map are retained in ignored storage
for this pending acceptance/review. The full offline HTML companion is local
only. Remove those local records after the owner records the disposition and
durable acceptance evidence; never prune pre-existing production/recovery state.
