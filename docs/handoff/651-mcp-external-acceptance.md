# #651 exact-candidate MCP acceptance preparation

28 October 2026. **Preparation complete; external acceptance HOLD.**
Owning issue: [#651](https://github.com/Hikyo-Org/Hikyo/issues/651).
This is the entry point for this lane, including the
[redacted preparation record](651-mcp-preparation.json). The release coordinator
owns final candidate selection and the canonical acceptance ledger.

## Scope and authority

Inspected canonical main `24e198b32a3ffd8d4013207b2f974d76ec49be6f` and live
#651 on 28 October. This PR prepares the checker and acceptance packet. It
does not select a final image, certify the inspected baseline, close #651,
change an accepted ADR, authorize deployment, or authorize a provider recipient.
Stabilization, release-scope, governance and trust work precedes final selection.

The operative [MCP ADR](../adr/mcp-server.md),
[write amendment](../adr/mcp-write.md) and
[accepted #631 decision](631-mcp-human-delegation.md) remain in force. The
five-tool read profile and optional seven-tool stage/validate profile remain
implemented and tested. #631 is a no-OAuth decision, not a named-client waiver.
#158, #969, #1057 and the accepted post-1.0 deferrals stay outside this task.

## Availability and fixture custody audit

| Input | Observation | Acceptance implication |
| --- | --- | --- |
| Go | Installed Go 1.27.3; official MCP SDK v1.8.0 in `go.mod` | Buildable client source; version inventory is not deployed tool proof. |
| Inspector | Frozen package pin is **2.11.0**, not the older report's 2.10.1 | Prepare the frozen package and execute this exact profile. Tenant-free conformance is separate from authenticated candidate proof. |
| Codex | Globally installed 0.164.0; isolated 0.160.1 acquired and its executable version confirmed | Do not replace the required profile with the globally installed version. Existing 0.160.1 local success on both engines remains valid historical evidence. Acquisition is not a new authenticated execution. |
| Claude Code | Installed 2.1.292; historical profile 2.1.261 | No Anthropic subscription or authorized paid substitute. Current authenticated execution is NOTRUN. The measured historical incompatible handshake remains historical. |
| Responses | No authorized API credential or provider-hosted recipient | NOTRUN. A locally installed SDK or an unrelated subscription is not authorization. |
| Historical fixture | The documented old runtime/custody directories and three named containers are absent | No existing release fixture was reused, restarted or modified. The older handoff's cleanup-pending statement is historical. |
| Current environment | MCP endpoint, live/rotating credentials and PostgreSQL fixture DSN are unset | No credential or target is inferred from an unrelated installation or another lane's database. |
| Protected storage | Checked documented custody names, application storage locations and available tool interfaces without reading secret material | No reusable authorized fixture locator was established. Do not inspect recovery keys or make a replacement public recipient. |

Exact preparation execution and local validation outcomes are recorded in the
JSON companion. No client version/help output counts as an authenticated call.
No new public endpoint, tunnel, production mutation, provider call, model turn,
external message or release publication was performed.

| Pinned client/profile | Final-candidate support classification | Existing evidence boundary |
| --- | --- | --- |
| Go SDK v1.8.0, modern `/mcp`, `2026-07-28` | Not yet verified | Current SDK interoperability regressions; older deployed v1.7.0 success is not this profile's candidate pass. |
| Inspector 2.11.0, Modern `/mcp`, `2026-07-28` | Not yet verified | Current tenant-free conformance preparation; older authenticated 2.5.0 success is historical. |
| Codex CLI 0.160.1, `/mcp/codex`, `2025-06-18` | Not yet verified | Actual local native read and stage/validate coverage on both engines; no final public candidate. |
| Claude Code 2.1.261, historical modern `/mcp` attempt | Unsupported for the measured handshake; current candidate not verified | The old legacy initialization was refused. No claim about installed 2.1.292 or another endpoint without execution. |
| OpenAI Responses remote MCP | Not yet verified | No authorized provider call or authenticated tool execution. |

This table makes no supported final-candidate claim. A measured incompatibility
is distinct from missing runtime evidence; the approved option A below records
the release disposition for the two unexecutable profiles.

**Single-fixture owner:** #651 owns the isolated release fixture and its image
binding. #1055 inventories existing public installations separately and should
reference this packet for fixture evidence. Production aliases are not fixture
origins. No deployment exists for this assignment yet. Create or reuse only
one specifically authorized fixture; never duplicate it across lanes.

Before fixture execution, the coordinator/operator must supply this concrete
custody and authorization packet through protected channels:

1. The selected clean source SHA/tree and published immutable image RepoDigest,
   including the selected platform manifest, image configuration and binary
   identities. Distinguish a local image ID, a mutable tag and a published digest.
2. A matching verified admission bundle, manifest hash, provenance, trust
   channel and public installation-operator pin, with verification receipts.
   Nightly, synthetic, signed-draft and stable identities remain separate.
   Do not mint synthetic release authority to unblock the fixture or reset
   durable verification/operator custody after a refusal.
3. The isolated nonproduction deployment resource/revision and exact verified
   public HTTPS origin, certificate verification policy, ingress configuration,
   PostgreSQL identity/TLS posture and two independently observed server
   process/replica identities. No affinity or MCP session IDs.
4. An explicit resource-specific authorization naming that fixture, synthetic
   organization/project, credential recipients, custodian, allowed mutations
   and cleanup owner. Retain mode-0700 runtime/mode-0600 secret custody. Transfer
   missing secrets through `request_secret`, never chat or process arguments.
5. The approved client support disposition below and any separately authorized
   provider recipient. A fixture deployment authorization alone does not
   authorize Responses or Claude credential transfer or paid execution.

The launch index was not present in the parent workspace locations inspected.
Dependencies here are the accepted source/trust/scope lane outcomes, not an
inferred final candidate from launch order. Recheck the coordinator's current
index before selection or execution.

## Scoped support decision approved by the owner

**Approved option A, 28 October 2026:** limit the 1.0 named-client acceptance
claim to official Go SDK v1.8.0, Inspector 2.11.0 Modern `/mcp` and Codex CLI 0.160.1
`/mcp/codex`. Require actual authenticated final-candidate tool execution for
each, with measured negotiated protocol and canary exclusion. Leave Claude
Code and OpenAI Responses explicitly **not verified for 1.0**. Do not describe
an unavailable current client as incompatible or erase the older measured
failure. Do not imply T3 desktop acceptance from native Codex acceptance.

| Owner choice | Consequence |
| --- | --- |
| A, approved: this precise narrower client claim | This owner decision is the scoped #651 release/ADR disposition amending the mandatory client matrix for 1.0. Keep transport, scope, canary, current-authority, two-process, cancellation, drain and public-candidate gates unchanged. Existing governance/review obligations still apply. |
| B, not selected: retain the full mandatory client matrix | Would retain Claude and Responses as mandatory until authorized execution and credential/recipient custody exist. No paid/provider fallback would follow from choosing B. |

**Decision status: APPROVED A.** Marc Went explicitly replied
"A: Approve the scoped narrower matrix" in this task conversation on
28 October 2026. This approval changes only #651's mandatory named-client
acceptance matrix for 1.0. Claude and Responses are not verified for 1.0 and
are outside that mandatory matrix; neither acquires a passing result. The
three required profiles still need actual final-candidate proof.

The locked protocol/authority ADRs and all other acceptance gates remain in
force. No deployment, provider recipient/call, paid fallback, merge or review
waiver is authorized. Maintainer review remains required. Unavailable
cross-provider review is SKIPPED, never CLEAN; this decision does not extend
an earlier issue-specific waiver.

## Checker repair and bounded receipt

The checker previously returned success as soon as the predecessor was
denied. Its replacement had only been tested before revocation, so revoking
both credentials could falsely pass. A TLS regression reproduces that exact
sequence. The repaired checker requires a fresh, successful, stateless
replacement read after the observed uniform revoked-token denial. A failed
replacement or failed receipt write fails the command.

The optional `--json` output records UTC phase observations, the pinned
protocol, poll interval and success without credentials, endpoint, tenant IDs,
response bodies or tool material. It is explicitly `endpoint_smoke_only` with
`polling_only_no_commit_receipt`. It binds no image/deployment and proves
neither zero-delay revocation nor the first request after committed revocation.
No passing receipt is emitted for a failed rotation. Store it with separately
verified deployment/candidate and committed-revocation evidence.

From the repository root, after an authorized launcher loads the protected
fixture environment, these are the scoped entry points:

```sh
go run -p 2 ./scripts/mcp-public-smoke \
  --url "$HIKYO_MCP_URL" --org "$HIKYO_MCP_ORG_ID" \
  --project "$HIKYO_MCP_PROJECT_ID" --json
go run -p 2 ./scripts/mcp-production-client
```

`HIKYO_MCP_URL` must be the exact verified HTTPS `/mcp` URL.
`HIKYO_MCP_TOKEN` holds the replacement; `HIKYO_MCP_ROTATING_TOKEN` holds
the still-live predecessor. Organization/project variables hold only synthetic
fixture IDs. Supply trust roots through the documented runtime trust channel;
never disable verification. The Go probe uses the canonical fixture IDs and
requires that fixture; it is not a general tenant administrator. Observe the
smoke prompt on stderr, revoke the exact predecessor through the supported
credential service/CLI, retain the commit-completion receipt, and separately
prove its first subsequent denial on each actual process. JSON stdout appears
only after the replacement recheck passes.

The existing `TestMCPDeploymentFixture` helper requires
`HIKYO_MCP_PROOF_DIR`, matching production linker stamps,
`HIKYO_MCP_PROOF_BUNDLE` and `HIKYO_MCP_PROOF_OPERATOR_KEY`. Its datastore
guard intentionally accepts only the dedicated loopback scratch database.
Do not point it at public/production data or weaken the guard to fit a remote
target. Provision identity/grants with the existing automation fixture and
supported service writer. Do not recreate the superseded project-wide
workload grant with raw SQL. Historical scratch replay launchers are absent;
they are not executable release automation supplied by this PR.

## Exact-candidate evidence checklist

All rows below are **HOLD/NOTRUN for the final candidate**, regardless of local
regression or conformance results. Retain a redacted receipt/hash per row,
UTC execution time, exact client/tool versions and source/image/deployment
bindings. Do not commit secret/response dumps or private tenant identifiers.

| Gate | Required observation |
| --- | --- |
| Admission and HTTPS | Verify published bytes/provenance/admission against selected source, actual deployed revision and TLS origin before sending a bearer. Fail closed on mismatch, redirect or unverified ingress. |
| Public checker | Tenant-free discovery and exact closed catalog; a nonempty scoped production definitions result; invalid/missing uniform denial; proven-live predecessor later revoked; fresh replacement success. The checker permits empty definitions, so separately assert the seeded fixture is nonempty. |
| Committed revocation | Supported writer commits; record completion; the very next known request to each process denies. Compare denial bytes/challenge without publishing bodies. Two-second polling alone is insufficient. |
| Named clients | Actual Go v1.8.0, Inspector 2.11.0 and Codex 0.160.1 authenticated calls on the final candidate. Record requested/negotiated wire versions and tool outcome separately from discovery. Under approved option A, Claude/Responses remain not verified for 1.0 and outside the mandatory matrix; do not advertise either as supported. |
| Cursor/current authority | Two actual PostgreSQL-backed server processes; alternate successive calls and cursor continuation without stickiness/session ID. Commit grant removal, prove the same bearer denied on both, restore via the supported writer and prove both recover. Preserve scoped/wrong-scope/ungranted refusal and canary exclusion. |
| Cancellation and drain | Observe an admitted operation on each process, cancel through the actual public proxy and verify context/store work ends and shared claim releases. Retain direct TLS versus proxy behavior separately. If streaming is claimed, exercise actual streaming without proxy buffering. SIGTERM must complete/cancel within the unchanged drain budget, release claims and preserve the peer. |
| Support and cleanup | Redacted support table for every exact profile, including failures/unavailable execution; fixture teardown verified against exact owned resources. Retain recovery evidence and protected custody only for the agreed review window. |

The server currently returns bounded JSON tool responses. Do not label direct
request-context cancellation as cancellation of an open stream. In-process
replicas and local private-CA HTTPS remain useful regression/rehearsal evidence,
not deployed public proof. A later source/image change requires affected proof
again. Only the coordinator/qualifier writes final ledger status.

## Validation and handoff

See the redacted JSON for this PR's local validation and exact code-patch
identity. Hosted PR checks and review are separate from local success; the PR
head and checks are reported in the PR/chat handoff. No merge is authorized.
Cross-provider review is SKIPPED because Anthropic access is unavailable and
no substitute was authorized.

The client-matrix decision is recorded as approved option A. The coordinator
must still select the integrated candidate and obtain the resource-specific
fixture authorization/custody packet before external execution. External
acceptance remains HOLD; this approval supplies neither final proof nor broad
deployment/provider permission.
