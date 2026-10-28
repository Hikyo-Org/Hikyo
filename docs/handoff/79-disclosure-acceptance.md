# #79: O4-O6 disclosure, documentation and support evidence

Snapshot: 2026-10-28. Inspected canonical main and branch base:
`24e198b32a3ffd8d4013207b2f974d76ec49be6f`.
Scope: source and published documentation, security-channel evidence and unsent
quarterly test preparation. Endpoint: signed PR for maintainer review. #79 stays
open. This handoff does not update the release plan, acceptance ledger or roadmap.

## Current disposition

| Evidence | Result | What it establishes |
| --- | --- | --- |
| Canonical public docs | PASS, read-only GETs on 2026-10-28 | Root/docs, CSS/modules, PWA assets, security.txt, policy pages and MX assertions passed. This is endpoint/content evidence, not notification or final-candidate proof. |
| Published source binding | Historical hosted PASS | [Docs run 40535272207](https://github.com/Hikyo-Org/Hikyo/actions/runs/40535272207) succeeded at `5ab189898b6a6ebffed3a676e8df072210da63b6`, an ancestor of inspected main. A current GET does not establish byte equality with this PR or a final release. |
| Source-built docs | Local validation recorded below | The build and policy fixtures validate this branch's source separately from the already-published site. |
| PVR enablement | PASS | GitHub's private-vulnerability-reporting API returned `enabled: true`. Enablement does not prove a self-report surfaced or was acknowledged. |
| Quarterly PVR receipt | HOLD | No qualifying receipt was located in the inspected history, repository evidence or available historical worktrees. A new real self-report and maintainer notification/acknowledgment proof remain required. |
| Fallback receipt metadata | PASS within recorded freshness window | The actual committed metadata below remains unchanged. Original private transport evidence and current mailbox acknowledgment were not reverified. |
| New PVR/fallback notifications | NOTRUN | Concrete unsent synthetic test packets are local chat deliverables. No external report, email, alert or message was sent. |
| Published advisories | PASS, fresh API read | Six published GHSAs; every returned `cve_id` is null. This does not establish the state of any private CVE request. CVE assignment is not a release gate. |
| Stable release/support | No public stable release found | The support policy defines one supported stable version. A development build or nightly does not become production-supported through this check. |

The locked requirements remain [MVP boundary O4-O6 and section 6](../adr/mvp-boundary.md)
and [OSS mechanics, Security disclosure and Releases](../adr/oss-mechanics.md).
Neither this work nor an unavailable reviewer waives an ADR requirement.

## Checker repairs and claim review

The old live checker accepted a fixture with a wrong HTML canonical URL. It
followed redirects without checking the effective endpoint, and some published
policy assertions accepted only one phrase. The new fixtures reproduced the
canonical failure before the repair.

`check-docs-live.sh` now requires the canonical `https://hikyo.app` origin
and verifies the requested final HTTPS URL for pages,
security.txt, manifests, service workers, CSS/modules and icons. HTML pages also
need their exact canonical URL. The implementation-status page is checked,
alongside stronger acknowledgment, support, continuity, pledge and licensing
clauses. GET/MX success explicitly disclaims notification-delivery proof.
`check-oss-policy.sh` now checks the canonical URLs in all corresponding built
pages. Negative fixtures exercise wrong canonical URLs, redirected pages and
assets, stale support/capability/license content and the original degradation
cases. The fallback fixture pins the exact 93-day boundary and rejects one
second beyond it without changing the freshness policy.

Claims reviewed against current source:

- [SECURITY.md](../../SECURITY.md): PVR primary, independently hosted fallback,
  seven-day acknowledgment target, 90 days from the report, severity targets,
  and CVE assignment never blocking release. No policy amendment is made.
- [SUPPORT.md](../../SUPPORT.md): latest patch of latest minor of latest major,
  same-day previous-minor EOL, no backports, prereleases unsupported. Current
  [installation guidance](../site/src/content/docs/docs/installation.mdx)
  correctly says there is no stable release yet.
- [GOVERNANCE.md](../../GOVERNANCE.md): BDFL, continuity, locked amendment
  procedure, fully-open pledge and the live implementation-status reference.
  Existing review obligations remain. No current organization-2FA enforcement
  audit or independent governance review is claimed here.
- [LICENSE](../../LICENSE): MPL 2.0. Source/built-site policy checks retain the
  pinned licensing assertions; this work does not relicense anything.
- [Deployment adapters](../site/src/content/docs/docs/deployment-adapters.mdx):
  the new qualification note distinguishes implemented interfaces from live
  provider qualification. #158, PR #969 and #1057 stay outside 1.0; #161 remains
  deferred to 1.1. No shipped-code test is removed. Real GitHub acceptance,
  named-client/public MCP acceptance and final self-hoster proof belong to
  their respective lanes. Listing source capabilities does not certify them.
- [Implementation status](../status/README.md) remains the current capability
  inventory; historical self-hoster PASS rows remain design/candidate evidence,
  not present-day released-artifact proof. No coordinator-owned record is edited.

## Actual fallback metadata and custody limits

Canonical metadata: `release/repository/fallback-channel-test.json`.

| Field | Recorded value |
| --- | --- |
| Schema/status | `hikyo.dev/fallback-channel-test/v1`, `passed` |
| Address | `security@developwent.io` |
| Sent | `2026-08-13T07:09:44Z` |
| Received | `2026-08-13T07:09:54Z` |
| Message-ID SHA-256 | `b032cf8e5302aa852305cf9aadd8b24cfeb8b6e90811b918692a7c5c8b6a8093` |
| Metadata file SHA-256 | `159adfcc4797325aff6b2fd875e3abd6e10553ce08294bb3f3fe93015f70ce84` |
| 93-day boundary | `2026-11-14T07:09:54Z` |

The checker accepts exactly that boundary; it rejects
`2026-11-14T07:09:55Z`. Refresh before the boundary or before any release check
that needs newer evidence. Never reset timestamps, replace a hash, or infer
acknowledgment from an MX result. The validator checks attested metadata; it
cannot independently prove mailbox delivery.

Discovery included the #78 handoff, the September #79 handoffs and security
notes, repository receipt files, read-only local thread history, and the
available original #78/alpha worktrees. Historical instructions require retaining
the Message-ID and recording send/receipt timestamps plus its hash. The
September docs-security JSON contains a copy of the same August metadata,
not a newer receipt. No documented, available protected raw PVR or fallback
transport receipt location was found in those sources. Mailbox exports and
private notification storage were not available through connected tools. This
is a bounded search result, not a claim that the maintainer has no receipt.
No recovery key or signing-private-key storage was accessed.

## Execute the quarterly packet, human-only

The owner must perform these steps because this assignment prohibits sending
external notifications. The unsent subjects/bodies and correlation IDs are
provided only in the local offline report, not this public PR. Regenerate a
synthetic packet if those chat deliverables are unavailable. Use no real
vulnerability details or production credentials.

1. Select the existing protected security-evidence store and its custodian.
   Retain raw artifacts there, outside Git, under owner-only access and the
   store's encryption/backup policy. Do not place credentials or raw reports in
   a PR, public advisory, issue, CI artifact or transcript. Record the private
   storage reference only in the protected manifest.
2. With an authorized reporter identity, submit the synthetic PVR self-report at
   <https://github.com/Hikyo-Org/Hikyo/security/advisories/new>. From the maintainer
   side, verify that the matching report actually surfaced through the normal
   notification/intake route and send an acknowledgment to the reporter.
   Retain report submission, notification and acknowledgment timestamps in UTC,
   the private report identity and raw notification evidence. A draft created
   directly by a maintainer, a GET, or enabling PVR is not this test. If a
   reporter identity is unavailable, keep PVR HOLD; do not substitute a public
   report or invent evidence.
3. From an independent external mailbox, send the separate synthetic fallback
   test to `security@developwent.io`. Observe it in the monitored destination
   intake, acknowledge it and confirm the reply at the sender. Preserve both
   sides' original messages/headers, the exact Message-ID and correlation ID.
   SMTP acceptance alone is insufficient. Do not send from the destination to
   itself or treat an automatic acknowledgment without observed intake as proof.
4. Both tests must surface and be acknowledged within seven days of submission.
   Do not mark pending/late/missing notification or reply evidence passed. Keep
   failures and partial receipts; reruns get new identities and never overwrite
   them. A failed quarterly test stays HOLD and needs channel repair.
5. Keep a private manifest tying channel, correlation ID, sent/submitted time,
   maintainer-visible time, acknowledgment sent/received time, exact private
   identifiers, custodian, protected-storage reference and SHA-256 of every raw
   artifact. Hash the exact UTF-8 Message-ID value (angle brackets included,
   no trailing newline), recording that convention. Hash raw artifact bytes
   separately. Review redactions against the originals before exporting metadata.
6. Share only a redacted summary: channel, UTC timestamps, outcome, acknowledgment
   interval, artifact/identifier hashes, custodian role and review date. Omit
   bodies, raw headers, addresses other than the published fallback, private
   report URLs/IDs, account identities, credentials and local storage paths.
   Independently review the retained originals and the metadata bindings.
7. Only after actual fallback delivery, update the canonical fallback JSON from
   the observed timestamps and Message-ID hash, preserving historical originals
   in protected storage. Run the command below with the real wall clock. Record
   PVR separately in the scoped handoff; the fallback validator does not validate
   PVR or prove acknowledgment. The coordinator then imports reviewed results
   into #79's acceptance record. No channel test authorizes release or publication.

```sh
scripts/ci/check-fallback-channel-test.sh \
  release/repository/fallback-channel-test.json security@developwent.io
scripts/ci/check-docs-live.sh https://hikyo.app security@developwent.io
```

Expected: recent receipt metadata passes and current public docs assertions
pass. Do not set `HIKYO_FALLBACK_TEST_NOW` for actual acceptance; it exists for
synthetic deterministic fixtures only.

## Fresh advisory lookup

Read-only `GET /repos/Hikyo-Org/Hikyo/security-advisories`, 2026-10-28:

| Advisory | Published | CVE returned |
| --- | --- | --- |
| [GHSA-8h6m-jpwj-v83p](https://github.com/Hikyo-Org/Hikyo/security/advisories/GHSA-8h6m-jpwj-v83p) | 2026-09-08T07:39:08Z | null |
| [GHSA-2qjg-h73x-x9j4](https://github.com/Hikyo-Org/Hikyo/security/advisories/GHSA-2qjg-h73x-x9j4) | 2026-09-08T07:39:09Z | null |
| [GHSA-xv87-mmx6-hw25](https://github.com/Hikyo-Org/Hikyo/security/advisories/GHSA-xv87-mmx6-hw25) | 2026-09-08T07:39:10Z | null |
| [GHSA-hqjh-jg7p-qr2m](https://github.com/Hikyo-Org/Hikyo/security/advisories/GHSA-hqjh-jg7p-qr2m) | 2026-09-08T07:39:11Z | null |
| [GHSA-wxjf-x869-pfh4](https://github.com/Hikyo-Org/Hikyo/security/advisories/GHSA-wxjf-x869-pfh4) | 2026-09-08T07:39:12Z | null |
| [GHSA-p29g-pjgc-jqmv](https://github.com/Hikyo-Org/Hikyo/security/advisories/GHSA-p29g-pjgc-jqmv) | 2026-09-08T07:39:13Z | null |

All returned states were `published`; no new advisory or CVE request was sent.
Re-query at final release qualification rather than carrying this snapshot
forward as current indefinitely.

## Validation and remaining gates

Local validation passed on pinned Node 26.12.1 and pnpm 11.24.0, using the
shared heavy queue (job 2577, exit 0). The production/checker inputs were final
before their checks; this handoff subsequently records their results.

```sh
pnpm --dir docs/site install --frozen-lockfile
pnpm --dir docs/site run check
pnpm --dir docs/site run build
scripts/ci/check-oss-policy.sh . docs/site/dist
scripts/ci/check-oss-policy_test.sh
scripts/ci/check-docs-pwa.sh . docs/site/dist
scripts/ci/check-docs-live_test.sh
scripts/ci/check-fallback-channel-test_test.sh
scripts/ci/check-fallback-channel-test.sh release/repository/fallback-channel-test.json security@developwent.io
scripts/ci/fast-feedback.sh --working-tree
```

Astro checked 46 files with zero errors, warnings or hints; the build produced
79 pages and precached 128 files. Built-site assertions and all focused shell
fixtures passed, including the canonical-page, redirect, stale-policy and exact
93-day cases. The complete-diff feedback plan selected shell and docs only;
ShellCheck 0.11.0, diff hygiene and read-only docs preparation passed. The real
public endpoint check also passed separately. Browser/offline navigation tests
and the full product suite were NOTRUN; no interaction, persistence, application
code or site layout changed. No screenshot evidence is required for this
Markdown/documentation and shell-checker change.

Self-review traced the changed checker through Pages, docs verification and
release/scheduled consumers. No failure propagation, retry or notification
workflow was weakened. GitHub's final PR head and hosted checks are recorded
in the PR and chat, since a file cannot embed its own eventual commit hash.

The public endpoint assertions are current read-only observations of the site,
not a deployment of this branch. Hosted PR CI, final-candidate/full-suite and
both-engine product acceptance, live-provider proof, signed stable artifacts,
publication and runtime acceptance remain separate gates. Persistence is not
changed here; no database fixture is required for shell/docs-only changes.
Independent review is NOTRUN. Cross-provider review is SKIPPED because the
approved Anthropic subscription is unavailable; no paid/provider fallback or
ADR waiver was used. Maintainer review and merge remain held.
