# #651 public fixture preparation, 29 October 2026

**PREPARATION. Public deployment, final-candidate acceptance and release remain
HOLD.** Source assets and executable operator instructions start at
[the fixture README](../../scripts/acceptance/mcp-fixture/README.md).
The owner receives the concrete private host/origin/authorization packet and its
offline HTML companion outside version control. This file contains no private
inventory, credentials or machine-local paths.

## Contract and accepted decisions

Preparation starts from clean canonical main
`ce5e85835383b6428676ff595e2a0c1a72f6a088`, tree
`0a15a814ecb0e17e98a519cc129950dd0e94b02c`, including merged TLS closeout
PR #1090. Live main matched that identity during preparation. It is **not**
the owner-selected integrated final candidate. Required source/trust integration,
including Vault and stable activation, precedes final selection.

The prior [#651 packet](651-mcp-external-acceptance.md) and preparation JSON,
[MCP ADR](../adr/mcp-server.md), [write ADR](../adr/mcp-write.md), accepted
[#631 ruling](631-mcp-human-delegation.md), current #651,
[release acceptance](../release/acceptance-1.0.md) and
[release plan](../release/release-plan-1.0.md) govern this work.
PR #1075 is confirmed merged as `d505c1cbe6de5e4ce3fde3f6e0a932fe085ba990`.
Its old queue messages are historical.

Marc's explicit 28 October approval, "A: Approve the scoped narrower matrix",
is retained. Mandatory 1.0 named-client profiles are:

| Client | Endpoint | Requested/negotiated profile |
| --- | --- | --- |
| Official Go MCP SDK v1.8.0 | `/mcp` | `2026-07-28` |
| Inspector 2.11.0, Modern | `/mcp` | `2026-07-28` |
| Native Codex CLI 0.160.1 | `/mcp/codex` | `2025-06-18` |

Claude Code and OpenAI Responses remain explicitly not verified for 1.0 and
outside the mandatory matrix. The older measured Claude handshake failure stays
historical; no unrun current client is called incompatible. Native Codex is not
T3 desktop acceptance. #631 retains managed credentials without OAuth; it is not
a client waiver. No provider-hosted recipient or model turn is needed.

The endpoint is one reviewable deployment authorization packet, meaningful
disposable local rehearsals, scoped repairs and a source PR. No remote deployment,
production changes, public DNS/ingress/tunnel/certificate changes, registry push,
provider/model call, spending, merge, tag/freeze, Apple action or publication is
authorized. #158, PR #969, controller #1057 and accepted deferrals stay excluded.
Production source and the existing deployment-fixture datastore guard are
unchanged. The store-importer architecture declaration adds only the exact
standalone disposable fixture package; its enforcement remains intact.
The owner subsequently authorized source PR repair and merge. That instruction
does not authorize resource deployment or public acceptance.

## Single ownership and host discovery

#651 remains the single source-fixture owner. Read-only discovery found a viable
existing Kubernetes host pair, existing storage/proxy infrastructure and no
matching fixture namespace. Capacity, exact placement, private names and the
proposed origin are in the local authorization packet. No production alias is
repurposed. [#1055 inventory](1055-public-deployment-acceptance.md) remains separate.

The edge connector refused its read command with
`Command not in whitelist for security reasons`. No alternate credential or
execution path bypassed that policy. A proposed hostname resolved through the
existing wildcard and returned empty HTTP 200; this is not a working fixture
origin or route-ownership proof. The actual edge readback, domain/certificate
authority and network enforcement remain explicit deployment prerequisites.

#1037 source siblings remain under this single source owner. The
[supported audit-seed interface](../../scripts/acceptance/mcp-fixture/audit-source.md)
now provisions the separately guarded PostgreSQL/SQLite scopes through service
writers and emits token-free seed metadata plus a separate protected bearer
curl-config file. Both engines passed local disposable seeding. No audit source
was deployed; actual image/origin/resource bindings and supervised human-MFA
instance export remain HOLD. Destructive sibling-copy removal still needs its
own post-receipt permission and never includes shared MCP database/event rows.

Collector [PR #1093](https://github.com/Hikyo-Org/Hikyo/pull/1093) owns independent
destination preparation and receipts. The owner's later destination approval
replaced its proposed VM with a disposable unprivileged LXC and required cleanup.
That authority does not authorize sources, routes, credentials or notification
delivery here. Production installation custody and stable resources stay excluded.

## Requirements to evidence

| Gate | Prepared or rehearsed evidence | Remaining public/final gate |
| --- | --- | --- |
| Supported synthetic provisioning | Dedicated PostgreSQL 18; supported bootstrap, hierarchy, definitions, values, publication, identities and grant writers; two disjoint canaries | Genuine selected production build/bundle and approved namespace |
| Stateless transport/scope | Actual two-process cursor continuation; nonempty definitions; exact five-tool catalog; absent/invalid uniform 401; scope/ungranted/missing twin; no canary disclosure | Same checks through actual approved public ingress |
| Current authority | Supported committed credential revocation, first following request on each process denied; replacement works; supported grant removal/restoration and recovery on both | Same exact-candidate public observations and commit-completion receipts |
| Existing smoke checker | Runs with its own predecessor; redacted endpoint/polling receipt and replacement recheck | Public candidate execution; polling remains distinct from first-call proof |
| Named clients | Actual Go/Inspector/Codex authenticated configuration reads at frozen pins; measured wire versions; Codex has no model turn/provider recipient | Final published image/public endpoint support table |
| Cancellation | Real admitted query blocked on owned catalogue; cancellation through local TLS proxy; store work and shared claims disappear before lock release on both processes | Actual full public proxy chain, plus separate direct TLS observation |
| Drain/recovery | SIGTERM with admitted work; existing five-second application grace; claim release; peer success; restart under unchanged custody and both recover | Exact public pod/container termination and replacement receipts |
| Isolation | Namespace-only manifests, default deny ingress/egress, database-only server egress, no public operational/datastore service, staged root/TLS modes | Enforcing-CNI positive/negative controls and actual proxy-source review |
| Artifact binding | Null-by-default selection template, immutable digest requirements, bundle inventory and helper-byte binding, genuine runtime admission | Clean selected source/tree, published index/child/config/binary, authenticated provenance/channel/bundle/operator pin |
| Cleanup | Owned processes, labelled local container/network and private scratch removed; redacted receipts retained | Approved exact remote teardown and receipt/credential retention window |

The checked-in [redacted rehearsal receipt](651-public-fixture-rehearsal.json)
binds local source/binary/client identities and actual measurements. It is not
published-image, deployed-production, public-HTTPS or final release evidence.
The frozen [npm lock](../../scripts/acceptance/mcp-fixture/clients/package-lock.json)
records named-client artifact integrity. No globally installed Codex version
substitutes for 0.160.1.

## Prepared operational controls

The namespace renderer accepts a protected owner-selected binding and public
offline bundle. It rejects mutable images, missing identities, changed bundle
inventory and unsuitable public origins. It does no remote work and does not
cryptographically certify inputs. The genuine production datastore gate still
authenticates the selected build/bundle and durable installation/operator custody.

The guarded launcher checks an actual owner decision, expiry, exact rendered
hashes, helper bytes and verifier receipt. It refuses an existing namespace.
Reservation precedes any datastore/server start. The storage owner manually
prepares only the verified new claim directory; two disposable diagnostic pods
then prove cross-node locking and preservation of private custody on remount.
Continuation requires the same namespace UID and a bound storage receipt.
Exclusive namespace creation succeeds before any child mutation; ownership is
recorded from its create response. Failed/ambiguous creation cannot adopt an
existing namespace. Shell dispatch propagates authorization failure explicitly,
including cleanup conditionals. The storage owner must verify and remove this
new claim's archived directory during teardown; namespace deletion alone does
not remove NFS custody under the observed archive policy.
Servers and gateway start at zero replicas, then supported seeding precedes
scaling to two real server processes. Root/TLS staging uses the existing canonical
hidden binary modes. Tooling and both replicas share one new 1 GiB RWX claim on
the existing NFS provisioner; the database has a separate 2 GiB claim. Existing
installation custody is never reused. Shared storage is an explicit deployment
prerequisite, not a locally rehearsed cross-node result. Public projected bundle
bytes are hash-checked and staged into regular files for unchanged no-follow
production readers. Owner-only 0400 canonical root staging is accepted while
protected JSON remains 0600.

Public campaign preflight requires separate reviewed ingress/CNI evidence and
anonymous version/revision controls before a bearer. Runtime process identities
come from authenticated pod inventory and actual gateway backend sockets. A
reported index instead of the selected platform digest is HOLD. No TLS bypass,
redirect bearer forwarding, session affinity or MCP session authority is added.
Wire receipts use closed metadata only, never headers/arguments/body dumps.
The native Codex fixture host bypasses the dead provider proxy. Ambiguous grant
write acknowledgements trigger supported recovery and both-process checks.
Readiness cannot pass after its deadline. Drain checks first observe listener
closure while real store work is still admitted, before releasing the held lock.
Runtime termination evidence remains separate from API pod-removal observations.

Only read tools are enabled. Stage/validate remains an optional separately
approved profile; shipped write tests and capability gates remain intact.
Bounded JSON responses do not establish open-stream cancellation.

## Validation and delivery limits

The local launcher and scoped tests are exercised directly with conservative
workers. Relevant source/owner checks, full-diff fast feedback and ordinary
independent Standards/Spec review are recorded against the source PR's exact
head in its delivery record. Local success, hosted checks and review are separate
evidence classes. No CI/merge/release result is inferred from local rehearsal.
Cross-provider review is not requested; no paid/subscription fallback is used.

The private packet names the exact host/namespace/database, proposed public
origin/TLS path, recipients/custodian, resource ceiling, permitted mutations,
execution/retention window and teardown owner. It also lists source/artifact and
edge prerequisites. Owner approval is the final remote-operation gate. A final
artifact selection is a separate prerequisite, not implicit approval to deploy.

## Hosted repair closeout

Hosted review repairs use the TOML encoder for native client configuration,
verify actual native catalog names, bind wire initialization/tool evidence to a
fresh invocation digest, and read live recorder state rather than trusting a
cached success file. Transport identities cannot be overwritten by response
JSON, null structured content fails, and seed refusal precedes admission writes.
Owned Docker cleanup has bounded contexts and an exact-name-plus-label fallback
for failed starts; child readiness failure has a kill fallback. Log read failures
fail disclosure checks, missing executable hashes fail, canonical parent checks
precede local resource creation, and container custody directories survive
restarts. Public runtime and final artifact gates remain separately HOLD.

Required server/helper/native-client identities are validated before resource
allocation. Final cleanup enumerates/removes private scratch with propagated
errors, and only then sets the outcome; an incomplete campaign cannot receive
a passing receipt. Fault regressions cover missing identities, failed scratch
enumeration and failed removal.

The subsequent raw-driver/SQL invariant failure is repaired through the
canonical reviewed inventory. Only four exact fixture source files may hold
driver handles. Four named protocols bind fresh-store reads, claim/backend
observation, the bounded catalogue lock and local cancellation/drain. Bodies,
callers and reachable guard/probe helpers are pinned. The fresh-store check
validates the separate MCP and audit datastore guards itself; nonempty refusal
also runs after admission and before supported MCP seeding. No domain SQL
writes or generated-query imports are admitted.

Explicit protocol build contexts cover this Unix-only tool. Applicable source
sets still require each owner; excluded sets receive no SQL authority, and
unknown/duplicate contexts fail. Existing protocol/query identities and
authorities are unchanged. Typed mutation regressions exercise query/write-mode
drift, both datastore guards, new callers/aliases, neighboring source files,
generated imports and unexpected SQL in an excluded build context. The hash
updater cannot change the declared source authority.

The fresh-seed sequencer directly invokes the named preflight before and after
admission, so either removal or reordering invalidates its caller pin. The hold
protocol validates its own DSN and opens that exact guarded connection; it
does not accept an ambient store handle. Local rehearsal additionally invokes
the actual `hold` and read-only `observe` commands against the owned datastore,
requiring the observer to finish while the catalogue lock remains held. This
operational-command check is separate from public proxy acceptance.
