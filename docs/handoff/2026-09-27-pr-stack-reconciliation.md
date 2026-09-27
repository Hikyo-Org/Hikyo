# PR stack reconciliation, 2026-09-27

## Scope and delivery order

Repair, adversarial review, validate and merge the owner's PRs #822, #823,
#825, #826, #827, #828, #829, #830 and #831. Exclude Valentin's #812.
No release or deployment is part of this work.

The schema-bearing PRs are integrated in that order, using migrations 62
through 69. Scanner #831 is independent. GitHub's required squash merge queue
validates the actual merge candidates; never bypass that queue or replace
its CI verdict with local test results.

## Resolved findings

- Cloudflare/Vault moves reserve config names on their actual secret surface.
  Pending Cloudflare targets reject repository IDs. AWS move reservations
  reject case-insensitive collisions across object and per-key destinations.
- Vault create-finalization failures retain indeterminate ownership for safe
  replay. Replay refuses intervening external versions. Pruning deletes only
  the inspected version. AppRole renewal preserves usable short leases.
  Canonical origins normalize hostname case and numeric ports. Loop detection
  unifies namespace/mount aliases and applies to both flags and the live wizard.
  Requests derive paths from the operation registry and retain constant
  request origins, public-egress checks and redirect refusal.
- PKI validates CA signatures, CRL-required subject key identifiers and leaf key
  usage. Renewal respects non-minute TTL limits. Parent deletion explicitly
  refuses while certificate history exists, including cross-project issuance;
  credentials can be revoked without deleting retained revocation evidence.
  Authenticated recovery before schema 64 omits unavailable PKI queries;
  ordinary runtime deletion still fails closed if PKI storage is missing.
  CRL publication records a
  captured revocation sequence so concurrent revocations remain due regardless
  of timestamp ordering. Renewal refreshes metadata even after a lost response.
  Retirement retains revocation coverage until leaves and child CA certificates
  expire; child revocation schedules its serial in the parent CRL. Human CLI
  generated-key issuance refuses an unsupported mint handoff before any request;
  browser mint and locally generated keys with CSR issuance remain supported.
  Certificate lists and profile selection expose loading and failure states.
- Transit serializes admission and trimming, fences purges, retains accurate
  deletion counts and rejects overflowing or negative delays before conversion.
  Unconfigured services fail safely; AAD flags are limited to supported verbs.
- Temporary access preserves the permanent-grant bound, locks policy/quorum
  decisions, bounds repeated queue reads and confirms policy deletion. CLI
  updates preserve omitted settings; queue responses expose effective approval
  eligibility, including permitted self-approval. Historical recovery dispatch
  uses the pre-access resolver for schemas below 66. Operational metrics use
  read transactions; missing policy IDs return the CLI not-found status.
- AWS descriptors reject trailing JSON. Workload identity rejects unrecognized
  AWS endpoints before credential discovery. Uncertain adoption tagging retains
  prior ownership state and supports replay when the tag already landed.
  Node identity remains opt-in. Resuming
  an AWS origin move waits for its provider to load and uses the AWS descriptor
  form. Live Vault imports apply the
  loop guard in both flag and wizard flows. Malformed source or active Vault
  origins refuse import because overlap cannot be established safely. Vault
  mappings cannot fall through to another provider's workflow syntax.
- GitLab updates preserve omitted boolean flags and scope, require ceremonies
  when weakening hidden protection, reject alias collisions, and retain explicit
  input presence in generated clients. Pending move claims distinguish GitLab
  environment scopes, preserve scope on resume and expose it in API responses.
  Origin moves close PostgreSQL result cursors before writing pending keys.
  AWS and GitLab clamp retry seconds before
  duration conversion. CLI help and bootstrap validation cover GitLab options.
- File sync uses atomic no-replace installation and validates destination stamps
  instead of trusting a replaceable cursor. Scanner refuses unsupported Git
  versions, does not run clean filters or lazy-fetch transports, and handles
  merge-range secret attribution without reporting inherited parent content.
  Git attributes preserve checksum-pinned vendored bytes on Windows checkout;
  the checksum test and its pinned upstream values remain intact.

Duplicate ADR dispositions were consolidated without changing the recorded
feature decisions. Amendment references and generated status evidence ordering
were corrected. Conflict resolutions preserve all provider enums, existing SSH
and sealed-webhook behavior, and the combined UI. Schema compatibility data is
regenerated from real SQLite and PostgreSQL catalogs after migration changes.

## Verification and remaining delivery evidence

Local validation includes relevant Go/race tests, both database backends,
backup/restore upgrade drills, authorization/query inventories, generated API
and TypeScript checks, CLI golden help, web tests/typecheck/lint, documentation
checks and cryptographic-signature/DCO verification. Regression tests cover the
security and concurrency findings above. Native browser automation was
unavailable; browser evidence comes from remote desktop/mobile CI, not manual
inspection.

Direct adversarial review and repository CodeRabbit findings were executed.
The separate native cross-provider pass was **skipped**, not CLEAN: current
Codex session quota could not be established under the shared quota policy.
The selected OpenAI reviewer is GPT-6-Astra with medium reasoning effort.

At this document's commit, final remote checks and merges remain in progress.
Read each PR's current head, review threads, checks and merge-queue entry before
continuing. A new head invalidates any earlier-head verdict. Verify final merged
state and merge SHA for all nine PRs; no post-merge release/deployment is implied.
