# #1127: post-quantum key-exchange policy on controlled hops, handoff

Status: **draft implementation, core path only. Not merge-ready, not operative.**
It needs maintainer security approval before merge (DEC-06 and the
[register](../adr/post-quantum-migration.md#decision-register) require one per
implementation PR). Owner and maintainer are one person, so adversarial review
compensates; cross-provider review is SKIPPED by explicit owner instruction and
is not CLEAN. Operator-facing text is in
[transport policy](../operations/transport-policy.md).

Stacked on the ADR amendment branch (base `26f112bde5b92acb1157be969683b8ca00ae15ae`).
Refs [#1127](https://github.com/Hikyo-Org/Hikyo/issues/1127); the PR does not close it.

## Scope of the claim

Strict means **handshake confidentiality only**: TLS 1.3, exactly `X25519MLKEM768`,
tickets off. Certificate authentication is reported separately and is **classical**
(a classical chain, or an SPKI pin that replaces chain and hostname checks).
Nothing here verifies an ML-DSA chain; that is #1133 and `VerifyConnection`
authentication is deliberately absent. Pin-based clients get confidentiality with
no authenticity claim (E-20). A strict ML-DSA listener certificate and its client
exclusion (E-18) are out of scope. Host-authority recovery selecting a lower
revision (E-21) is registered, not redesigned.

## What was built

| Area | Change |
| --- | --- |
| Policy seam | `internal/tlspolicy/profile.go`: `Profile` (`compat`, `strict`), exact `ParseProfile`, `Profile.Apply(*tls.Config)` (touches only version bounds, `CurvePreferences`, `SessionTicketsDisabled`, `ClientSessionCache`; never certificates, roots, names or verify callbacks), closed `VersionName`/`GroupName`, `Observed`, `Observe`. Unknown profile values fail closed to strict |
| Configuration | `HIKYO_TLS_PROFILE`, owner-scoped, `app-reload`: parsed in `config.Load` (default `compat`, exact match), exported in `ManagedOwnerValues`, copied by `applyManagedOwnerValues`, catalogue enum, runtimeconfig migration 6 recording `compat`, inventory report regenerated, one managed-configuration guide row |
| Precedence | managed > bootstrap > default `compat`. Bootstrap env only seeds `ManagedInputs`; an adopted value is complete and never reads stale environment |
| No-certificate refusal | `requireStrictTLS`: startup (`Load`), managed Apply (`ApplyManagedOwnerValues`, which also sees a node document's certificate through the combined `ApplyManagedOwnerAndNodeValues` path and uses the owner profile being applied, not the stale bootstrap one), and `newManagedCertificate` at generation preparation. Parser-only validation does not refuse (it has no node context) |
| Native listener (I09) | `managedCertificate` carries the profile; `tlsConfig()` applies it; a profile-only change replaces the transport and closes existing connections (`listenerTransportChanged`); the upgrade maintenance listener uses the same certificate and profile |
| CLI (I11) | `TrustEntry.TLSProfile` / `TrustBundle.TLSProfile` (`tls_profile`), validated, applied on top of the trust mode, sticky against silent relaxation, strict refused for local-socket entries |
| Operator client (I11) | `client.WithTLSProfile` option; no CRD field |
| Evidence | server log `evidence=configured` and debug `evidence=observed`; CLI `-vv` TLS line via `diagnostics.Transport` (`Hop`/`Profile`); sanitized closed vocabularies |
| Tests | `tlstest.StartPeer` real HTTPS peer recording handler hits, credentials, server-side states and raw bytes |
| Benchmark | `BenchmarkHandshake` (local numbers only) |

## Acceptance criteria

| Criterion | Status | Notes |
| --- | --- | --- |
| Map hops and terminators; unexamined stays UNKNOWN | PARTIAL | Matrix below. Go-owned configs verified against current source (`MinVersion` search, unchanged since the draft map). SDK paths, browsers, TS client UNKNOWN |
| Stale-node fencing on the strict surface (DEC-10) | HOLD | Not built, by decision of this round; the owner decides. Strict is a per-process property: a stale node keeps admitting credentials at its old profile and no fleet-wide claim can be made (see operations doc) |
| Policy through the approved configuration path, validated default, upgrade compatibility, managed semantics, restart/reload | PASS for the listener | Default `compat`, migration 6 explicit, Apply closes old connections. Client selection is PARTIAL (see remaining work) |
| Strict requires approved version and group; incompatible peers fail before credentials; certificate/hostname/trust intact; tickets and resumption reviewed | PASS for listener, CLI, operator client | Real handshakes; server-side non-delivery checked |
| Sanitized negotiation evidence, three classes separate, no proxy inference | PARTIAL | Logs and CLI line. No metric (ops-spec registry is pinned and locked; needs its own amendment). Operator client and CLI-less clients have no evidence line |
| Prove against hybrid, TLS 1.3 classical, TLS 1.2, GODEBUG, overridden groups, untrusted cert, hostname mismatch, unknown groups, mixed-version rollout | PARTIAL | All but mixed-version are tested in-process. Mixed-version covers what source permits (below) |
| Direct ingress, proxy-to-backend, CLI and operator consumers, database transport, representative adapter/federation paths, redirect/proxy, reconnect/resumption | PARTIAL | Direct listener, CLI, operator client, reconnect and resumption PASS. Proxy-to-backend, database, adapter, federation, redirect and forward proxy paths NOTRUN: those hops stay `compat` by decision and were not changed |
| Certificate-authentication limits recorded separately | PASS | Every doc, log and catalogue string says confidentiality only, authentication classical |
| Retain deadlines, cancellation, pooling, SSRF/egress, proxy trust, CA/SPKI | PASS for touched code | `Apply` never touches trust fields (tested); CLI redirect refusal, timeouts, `ProxyFromEnvironment` and the operator's no-proxy transport are unchanged. Sibling uses checked below |
| Measure handshake size/latency, concurrency/memory, resumption on amd64/arm64 with approved budgets | NOTRUN | Targets unavailable this round; DEC-17 budgets are the owner's. Only a local benchmark exists |
| Operator rollout/rollback and a capability matrix; unsupported hops explicit | PARTIAL | Operations doc and matrix. Real ingress and providers NOTRUN, none contacted |

## Tests and results

Commands are run from the repository root; `go test` uses `-p 2`.

| Command | Result |
| --- | --- |
| `go test -p 2 -count=1 ./internal/tlspolicy/` | PASS (matrix, trust fields, resumption, GODEBUG subprocess, unknown groups, observation) |
| `go test -p 2 -count=1 ./internal/cli/ ./internal/operator/client/ ./internal/diagnostics/` | PASS |
| `go test -p 2 -count=1 ./internal/app/ -run 'TestNativeListenerProfiles\|TestListenerReports\|TestBootWithStrict\|TestStrictProfileRefuses\|TestProfileChange\|TestConfigAndPolicy'` | PASS (incl. real `Boot` with strict) |
| `go test -p 2 -count=1 ./internal/config/ ./internal/runtimeconfig/ ./internal/operator/...` | PASS |
| `go test -p 2 -count=1 ./internal/app/ ./internal/server/ ./internal/conformance/` | PASS (app 276 s) |
| `go test -p 2 -count=1 ./internal/store/upgrade/ ./internal/store/migrate/` with a throwaway PostgreSQL 18 container (`HIKYO_TEST_POSTGRES_DSN`), then the same on SQLite only | PASS on both engines, including the new `old-tls-profile-catalogue` and `existing-strict-profile` migration scenarios; the container was removed |
| `go test -p 2 -count=1 ./internal/service/ -run 'SelfConfig\|Seed\|Managed\|Config'` | PASS (subset, not the full package) |
| `go test -p 2 -run '^$' -bench Handshake -benchtime 200x ./internal/tlspolicy/` | LOCAL numbers, Apple M2 Pro, arm64 darwin, in-memory pipe, P-256 test chain: compat 452 us, 873 allocs, 3799 handshake bytes; strict 446 us, 865 allocs, 3723 bytes. Go's default already offers the hybrid group, so strict's byte cost equals compat's on this toolchain. Not a target measurement, no budget |
| `FAST_FEEDBACK_BASE=<stack base> scripts/ci/fast-feedback.sh --working-tree` (pinned Node 26.12.1) | PASS: diff check, Go formatting, test compile of nine packages, docs content check. Hosted CI NOT RUN |
| `scripts/ci/check-go-imports.sh` | PASS |

Queue note: `agent-heavy run` lost its daemon socket (`i/o timeout`) four times
while waiting behind other jobs, so the broad `service`/`isolation` suites were
not run; the packages above ran directly with `-p 2` as single-package runs and
the orphaned queue jobs were cancelled. The full `internal/service` and
`internal/isolation` suites are NOTRUN.

Mutation check: removing the `Apply` call in `cli.NewClient` fails the CLI strict
tests, so they do exercise the policy.

GODEBUG finding on the pinned toolchain (Go 1.27.3): with `GODEBUG=tlsmlkem=0` a
default (compat) client negotiates `x25519`, while the strict client still
negotiates `x25519mlkem768` against a server that offers it. `tlssecpmlkem=0` and
`tls13=0` do not change the strict result. The invariant asserted is "hybrid or
refused", never a classical group.

### Mixed-version rollout: what source permits

- A CLI since the system-CA trust change (nightlies from 2026-10-19, `v1.0.0`)
  decodes with unknown fields disallowed and refuses a store holding `tls_profile`.
  **Earlier CLI builds use `json.Unmarshal`, which drops the field: a strict entry
  silently behaves as compat there (a downgrade).** The test
  `TestCurrentTrustDecoderRefusesAFieldOutsideItsEntryShape` pins only the current
  decoder and says nothing about older binaries.
- A server binary that does not know `HIKYO_TLS_PROFILE` refuses the managed
  owner configuration as containing an unsupported key (source reading; not run
  against an old binary, NOTRUN). Migration 6 reaches nodes as a new desired
  revision written by the offline upgrade gate, so an older binary cannot read
  the post-upgrade configuration; binary rollback is a backup restore.
- An unknown profile value is refused by `Load`, managed validation and the CLI.
- Not covered: old and new nodes serving together, stale-node admission and
  restored settings. See remaining work.

### Sibling uses of shared transport config

`Apply` is called only on configs freshly built for the listener, `cli.NewClient`
and `opclient.NewClient`. The CLI's TLS identity probe (`inspectTLSIdentity`) and
pairing-peer clients (`remotes.go`) build their own configs and stay `compat`
(no profile in their entry). No shared `tls.Config` is mutated. Remote fetch,
update check, adapters, mail, federation, JWKS, SAML, PostgreSQL and the
Kubernetes SDK paths were not touched.

## Hop capability matrix

Legend: **Profile** is the starting profile from the hop table. **Group** is the
negotiated key-exchange group observed in production; it is UNKNOWN everywhere
because no production hop was observed (NOTRUN). "Go-owned" TLS floor is from
source. A proxy or database terminator outside the process is outside any claim.

| Hop | IDs | Owner and terminator | Profile now | Strict selectable | Enforced and tested here | Group in production |
| --- | --- | --- | --- | --- | --- | --- |
| Browser, CLI or client to external ingress | I10 | Operator edge, outside process | compat, no claim | No | No | UNKNOWN |
| Client to native Hikyo listener | I09 | Go, `managedCertificate` (TLS 1.2 floor in compat) | compat, strict opt-in | Yes | Yes (listener, real `Boot`) | UNKNOWN |
| Edge to native TLS backend | I09, I10 | Same listener, edge owns the other leg | compat, strict opt-in | Yes (backend leg only) | Backend leg only; frontend leg cannot be inferred | UNKNOWN |
| Edge to plaintext backend (trusted proxy) | I10 | No TLS in process | compat, no claim | No (strict refused) | Refusal tested | n/a, not TLS |
| Operational listener, local CLI socket | I09 | Plaintext local boundary | compat, no claim | No | CLI strict refused for socket entries | n/a, not TLS |
| CLI to Hikyo HTTPS origin | I11 | Go, `cli.NewClient` | compat, strict opt-in | Yes (bundle) | Yes | UNKNOWN |
| Operator to Hikyo origin | I11 | Go, `opclient.NewClient` | compat, strict opt-in | Client option only; no CRD field | Yes (option) | UNKNOWN |
| MCP clients calling Hikyo | I11 | No Go-owned outbound MCP client exists (`rg -il mcp --glob '*.go'` over `internal` and `cmd` finds the server in `internal/mcpserver` and unrelated mentions); MCP is served by the listener | Covered by listener | Via listener | Listener tests | UNKNOWN |
| Generated TS client, browser fetch | I11 | `clients/ts`, `web`, runtime and browser stacks | not examined | Unknown | No | UNKNOWN |
| Pairing fetch to a remote Hikyo | I11, I20 | Go, `remotefetch` (leaf pin) | compat | No | No | UNKNOWN |
| Pairing forward-proxy hop | I11, I20 | Go, `dialThroughProxy`; proxy outside claim | compat | No | No | UNKNOWN |
| Serving-identity probe | I20 | Go, `pairing_identity.go` | compat | No | No | UNKNOWN |
| Update metadata and artifact download | I11 | Go, `updatecheck`; external peer | compat | No | No | UNKNOWN |
| Application PostgreSQL | I12 | `pgx`, server terminator outside process | compat | No | No | UNKNOWN |
| Upgrade, recovery, candidate datastore connections | I12 | `store` and `store/upgrade` connection creators | not traced | Unknown | No | UNKNOWN |
| Dynamic PostgreSQL provider | I13 | `pgx`, verify-full enforced | compat | No | No | UNKNOWN |
| Forgejo, GitHub Actions, Cloudflare, GitLab, Vault KV, sealed webhook adapters | I14 | Go `http.Transport`, external peers | compat | No | No | UNKNOWN |
| AWS Secrets Manager and explicit STS | I14 | Go, SDK with injected clients | compat | No | No | UNKNOWN |
| AWS ambient credential sources | I14 | SDK `LoadDefaultConfig` | not traced | Unknown | No | UNKNOWN |
| SMTP relay | I16 | Go `mail`, go-mail | compat | No | No | UNKNOWN |
| OIDC discovery, JWKS, OAuth2 exchange | I17 | Go, `federationhttp` | compat | No | No | UNKNOWN |
| SAML metadata | I17 | Go, `service/saml_providers.go` | compat | No | No | UNKNOWN |
| Kubernetes API (operator, config rollout), kubelet | I30 | SDK-owned | compat | No | No | UNKNOWN |
| OpenBao import and other ambient SDK sources | I30 | SDK-owned | compat | No | No | UNKNOWN |
| Helm chart ingress and secrets | I31 | Operator-owned edge | not examined | Unknown | No | UNKNOWN |

## Deviations and decisions to confirm

1. **Migration 6.** Added with an explicit `compat` default so an upgraded
   instance records its profile. It will collide with any other child's
   migration number on rebase (#1126 may also add one); renumber then.
2. **No metric.** The metrics registry is pinned by the locked ops-spec ADR with
   exact series counts and a closed label whitelist. A negotiations series is an
   ops-spec amendment, not a feature detail; the log and CLI line stand in.
3. **CLI selection is bundle-only.** The interactive `login`/`context create`
   path has no `--tls-profile` flag (`establishWithTLSMode` takes positional
   parameters across three verbs). Provisioned bundles and trust files work.
4. **Operator selection is a library option.** A CRD `tlsProfile` field needs
   regenerated CRDs (`scripts/gen-crds.sh`), the drift test, the chart copy and
   plumbing at `reporter.go` and `reconciler.go`.
5. **Strict on the proxy leg.** A CLI with strict and `HTTPS_PROXY` naming an
   `https://` proxy applies the policy to the proxy connection too, because
   `http.Transport` shares the config. Fail-closed and documented, but a
   behavior to confirm with the maintainer.
6. **Maintenance listener** honors the profile with the certificate, so a strict
   node never serves classical TLS during an upgrade window (covered by
   `TestUpgradeWindowHonorsTheStrictProfile`).
7. **Loopback development plaintext** with strict is refused too (the hop table
   names trusted-proxy mode; a plaintext loopback strict claim would be as false).

## Remaining work

- **Stale-node fencing: not built.** A node below the desired generation keeps
  serving its old profile and is only reported as `pending`. Managed
  certificates behave the same way today. A node-side loop that makes the strict
  surface fail closed when its active generation is below the desired one is new
  machinery. Until it exists, do not claim fleet-wide strict.
- Metric series for negotiations (ops-spec amendment) and an operator-client
  evidence line.
- Interactive CLI `--tls-profile`; CRD `tlsProfile`.
- `VerifyConnection` ML-DSA authentication on strict-marked clients: #1133.
- Real amd64/arm64 measurement and the owner's DEC-17 budgets: NOTRUN.
- Other hops: PostgreSQL, adapters, federation and the rest stay compat; each
  needs peer evidence under #1134 before moving.
- Proxy-to-backend, redirect, forward-proxy and database tests with their real
  owners; mixed-version old-binary run; platform verifier on macOS and Windows
  (`SSL_CERT_FILE` and `x509sslcertoverrideplatform`): NOTRUN.
- Managed-generation reload under load and connection drain with a real
  multi-node Apply: NOTRUN.

## Repair-round findings (review of c02a6b2a3)

- **Recovery baseline weakened strict (fixed).** A node missing from the node
  document retried a refused owner configuration with the bootstrap seed, which
  carries `compat`. `bootNodeConfiguration` now carries the adopted
  `HIKYO_TLS_PROFILE` into that baseline, so the node keeps strict or refuses to
  boot. Real-boot regressions: `TestRealBootKeepsAdoptedStrictWhenTheNodeIsMissingFromTheNodeDocument`
  (no certificate refuses; a certificate-bearing node stays hybrid-only);
  helper-level `TestBootNodeConfigurationCarriesAdoptedStrictIntoTheRecoveryBaseline`
  covers the IP-origin trigger, which a full boot already refuses at candidate
  health. Other `Prepare(seed)` sites are seed-only (unmanaged) paths and do not
  override an adopted value.
- **Repair path, as found.** `ResolveRepairRuntimeBundle` runs the previous
  completed snapshot, so a repair boot uses that snapshot's profile (possibly
  `compat`). Not changed: it is the same downgrade class as E-21, and carrying the
  desired profile into an unverified repair graph would be new policy.
- **Trust entries bind a profile to a name (fixed).** `Put` refuses a second name
  for an origin with a different profile; `lookupByOrigin` refuses ambiguous
  matches whose verification policies differ instead of returning a random one.
  Interactive establishment cannot create a strict entry (no flag), so its prompt
  needs no profile line yet.
- **ADR check (api-cli-surface, not edited).** Its owner-locked #966 amendment
  closes the bundle field set (clause 5: unknown trust-store and bundle fields
  are refused) and lists what a change to an established entry covers (clause 4:
  origin, mode, pin, socket). `tls_profile` extends both. A declared amendment is
  needed through DEC-18 (reopen that ADR's originating ticket); the migration ADR's
  amendment table does not list api-cli-surface.
- **Migration 6 was not exercised end to end.** It ran only through
  `MigrateConfiguration` scenarios on SQLite and PostgreSQL (the `store/upgrade`
  tests). The upgrade gate never ran it with two real release identities: both dev
  binaries share one identity, and a new binary refusing a base database with
  "catalogue has changed" is a dev-build artifact observed by the reviewer, not
  something these tests cover. NOTRUN.

## Rollback and key retention

Rollback is publishing and applying `compat`; no key, certificate or trust anchor
is created, rotated or destroyed by this feature. An older binary must not be
used as a workaround: it refuses the unknown key. No external peer or provider
was contacted.

## Next authorized step

Independent Standards and Spec review of this draft, then maintainer security
approval of the policy behavior. No merge, release or deployment is authorized.
