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
Shipped production source, tests and the existing deployment-fixture datastore
guard are unchanged.

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

#1037 coordination proposes audit source siblings under this source owner,
with separate collector/destination ownership. Their SQLite/PostgreSQL seeding
and destructive custody-copy removal are **not implemented or authorized by
these #651 assets**. They must not use/delete the shared MCP database/event rows.
Production installation custody and stable recovery/signing resources are excluded.

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
hash, helper bytes and verifier receipt. It refuses an existing namespace.
Servers and gateway start at zero replicas, then supported seeding precedes
scaling to two real server processes. Root/TLS staging uses the existing canonical
hidden binary modes; installation custody persists in dedicated PVCs. No NFS
or production storage is reused.

Public campaign preflight requires separate reviewed ingress/CNI evidence and
anonymous version/revision controls before a bearer. Runtime process identities
come from authenticated pod inventory and actual gateway backend sockets. A
reported index instead of the selected platform digest is HOLD. No TLS bypass,
redirect bearer forwarding, session affinity or MCP session authority is added.
Wire receipts use closed metadata only, never headers/arguments/body dumps.

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
