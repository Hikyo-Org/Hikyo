# Self-hosted privacy controls and compliance documentation

## Scope

User requested implemented controls and documentation discoverable in hikyo.app's
docs, for self-hosting now and possible hosting later. This change supports an
operator's compliance program; it makes no SOC 2 attestation or GDPR certification
claim. The acceptance source is `docs/spec/self-hosted-compliance.md`.

## Implementation

- Migration46 and audit retention: access90/security365 defaults, validated bounded
  host configuration, audited policy adoption, atomic correlation-unit pruning,
  failure rollback and commit-safe export snapshots on SQLite and PostgreSQL.
- Migration47 and host-local identity lifecycle: explicit subject export,
  correction, restriction/release, credential/identity erasure, pseudonymous
  historical tombstones and instance/account-bound receipt replay after restore.
  No new public API or tenant-admin privacy authority.
- Doctor `--evidence -o json`: timestamped server/client metadata and existing
  diagnostic findings, preserving exit status and naming unassessed controls.
- Existing environment deletion verified as coarse content erasure, including
  active pins/history. Neighboring environments/tenants and shared accounts survive;
  dependency refusal rolls the entire deletion back.

## Public documentation

`/docs/compliance/`, `/docs/privacy/`, `/docs/compliance-operations/` are linked from
sidebar, docs index, self-hosting, configuration, upgrades and backup guidance.
The inventory, operator responsibilities, rights requests, incident response,
backup reconciliation, retention consequences and residual limitations are explicit.

## Review findings resolved

Independent review identified missing upgrade warnings, stale assessment wording,
a precommit-timestamp race in audit export and retry state retained by privacy
export. Documentation now describes implemented behavior. Snapshot capture holds
the audit writer gate. Export constructs a fresh result on every transaction retry.
Regression evidence covers the relevant database engines. Historical source44
recovery initially reached the new privacy-state query before migration. The
authenticated source manifest now selects a narrowly confined pre47 recovery
grant projection; runtime authorization retains privacy-state filtering. The
source44 drill passes on both engines, and final independent review is CLEAN.

## Validation and delivery

All Go packages passed across the combined run and the repository's three
isolation CI groups on PostgreSQL 18 and SQLite. The unsharded isolation run
reached Go's default 10-minute aggregate deadline while a new SAML test was
hashing a password, without an assertion failure. The canonical CI planner
assigned all 359 isolation tests exactly once; groups passed in 196.688s,
241.183s and 249.258s with the existing timeout. No test or timeout was weakened.
The latest main fix was integrated and its config/service checks passed.

`go vet ./...`, formatting, SQLC regeneration and the documentation status check
passed. Docs Astro check/build, OSS/PWA gates and offline browser test passed.
Local desktop/mobile inspection confirmed sidebar links, all three guides in
search, and no horizontal overflow. Search remains precached; its 2.1 MB index
uses an explicit 3 MiB Workbox ceiling with fatal warnings retained.

Independent security/spec review is CLEAN. Delivery proceeds through a signed,
DCO-signed-off PR and exact-head CI. Public routes are not live until the PR
merges and the Pages deployment succeeds. The standing human merge gate applies;
verify `/docs/compliance/`, `/docs/privacy/`, `/docs/compliance-operations/` and
search on hikyo.app after that deployment.

## Operator boundaries

Audit expiry activates on upgrade; operators must choose justified periods and
preserve independent evidence before startup. Correlated units outlive individual
cutoffs until all members expire. No native external audit forwarder is added.
Identity exports omit secret values, audit payloads and unrelated personal data;
requests require operator review. Pseudonymization is not anonymization. Receipts
must be kept outside backups and reapplied before reopening a restored service.
Customer content, project metadata, storage/WAL, downstream systems and legal
retention decisions remain explicit operator responsibilities. Environment deletion
is destructive and coarse; there is no selective historical-key erasure claim.

## PR 679 CI fixture repair

The first exact-head CI run found an unrelated mobile setup collision: the
second fake OIDC provider required fixed port 45795. A regression reproduced
that exact refusal with the port occupied. Both fake providers now use
OS-assigned ports by default; explicit occupied-port overrides still refuse.
The second provider's validated issuer is stored in a file for browser workers,
permissions are tightened to 0600 even on rewrite, and teardown removes it.
No production authentication behavior or test timeout changed.

Independent review is CLEAN. All 804 web tests, TypeScript checking and the web
build passed. A real mobile provider configuration/login-advertisement/disable/
delete flow passed with separate local application ports (1 test, 2.4 minutes
including setup). The original default application ports were occupied by
another local test service, which was left running. The new regression also
checks real provider discovery with the old IdP port occupied, explicit-port
refusal, persisted issuer reads, permissions and cleanup.
