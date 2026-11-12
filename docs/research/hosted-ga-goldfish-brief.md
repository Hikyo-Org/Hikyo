# Hikyo hosted service: gaps before general availability

* **Author:** Marc Went
* **Date:** 2026-11-12

## Overview

Hikyo's locked hosted-service ADR (`docs/adr/hosted-service.md`) puts many customer orgs on one shared PostgreSQL
installation, with Sealed orgs whose secret values the server opens only with a key the customer presents. Nothing
of it is built. This brief collects what is still missing, undecided or wrong before that service can reach general
availability (GA), judged first on trust and security. The owner's hard requirements: no single key whose loss or
leak affects all orgs; org owners control their own keys and who may help them; the hoster's super admin supports a
customer only after the owner approves and never reads their secrets; leaked data is unusable; assume a paranoid
hoster facing supply-chain compromise, full database theft and insider access.

Seven fresh-context Goldfish reviewers (design critic, codebase details, zoom-out, comprehension, implementation
readiness, structural, inventive catalyst) read the ADR, related ADRs and code on `origin/dev` 426bf59ce. All seven
completed. Their findings are merged below by theme; the reviewer count shows how many raised each one independently.

## Analysis

### A. One key reaches every org (7 of 7 reviewers)

- `sealedSlotKey` wraps every slot, every unattended environment and every adapter credential on an installation.
  Losing it destroys every Sealed org; leaking it with a dump opens every unattended environment and every customer's
  outbound GitHub, AWS and Cloudflare credentials. This breaks requirement 1.
- `holderTransitKey` derives every hourly transit key, with no forward secrecy: the root key obtained later opens every
  archived capture of every org's recovery keys.
- A per-org outer key alone is not enough: all tier-3 keys are wrapped by one master key under one root key held as a
  file or environment variable (`internal/crypto/keyring.go:42-47`, `rootkey.go:12-51`). The encryption-model ADR
  reserves a KMS seam at the root key (`encryption-model.md:161`); per-scope KMS routing is a new format version (`:50`).
- **Timing is the trap.** Every backup taken under the shared key keeps that exposure for its whole retention. The slot
  format must be settled before the first customer ciphertext and backup exist. Old outer-key versions must also
  outlive every backup that uses them, or a restore loses every Sealed org.
- Contradiction to resolve: does requirement 1 mean confidentiality only (a Sealed environment without unattended
  access already needs a holder secret), or also loss and availability, and does it reach the root and master key?

### B. Kill switch, crypto-shredding and backups (6 of 7)

- Deleting an org or its slots does not reach backups. A backup plus the root key still opens unattended environments,
  adapter credentials and audit personal data; a backup plus any old holder key opens the rest.
- A per-org key held outside the database and outside backups (hoster KMS by default, customer-held as an option)
  makes revocation a kill switch and destruction a shred. Costs: a KMS call per request clashes with "nothing cached"
  (ADR decision 4), so a cache TTL bounds how fast revocation bites; the customer's KMS becomes an availability
  dependency; a lost KMS key is lost data.
- Simpler counter-position (zoom-out): a Sealed org can already shred itself by destroying all holder keys; document
  that plus backup-retention expiry as erasure, and add no new feature at GA.
- Recovery keys are tied to their installation and are not an offline copy. Losing the root key loses every Sealed
  org. Open: must owner control include surviving loss of the installation (a customer-held offline export)?

### C. Operator trust and support (5 of 7)

- **Requirement 3 contradicts the ADR.** Decision 2 and the honest statement accept that anyone with production
  access can capture holder secrets or tamper; the owner accepted this on 2026-11-06. The super admin is usually that
  person. Either separate the application super-admin role from production infrastructure access (two-person rule,
  just-in-time access, external audit), or reword requirement 3 to match the honest statement.
- The support grant (decision 6) has no defined scope, maximum duration, approver count or revocation, and the
  customer cannot tell what approving it reveals. One taken-over admin account can approve a fake support request.

### D. Breach detection, response and notification (6 of 7)

- The ADR has no detection, incident response or notification section. Its only canary is the operator's restore-drill
  org. A direct datastore edit leaves no record, and the audit trail has no hash chain or external sink
  (`audit-model.md:168`), so an attacker can rewrite the evidence.
- Decision 6 hides a Sealed org's members from the operator, so the operator cannot reach the org's admins through the
  application to meet GDPR's 72-hour notice (Art. 33) or tell affected people (Art. 34), and cannot force the rotation
  a hostile release requires.
- Candidates: canary orgs, slots and honeytoken values per installation; a content-free per-org security-signal stream;
  an operator-held contact channel per org outside the core; customer-visible alerts on holder and slot changes; a
  guided "rotate everything" flow.

### E. Supply chain (4 of 7)

- A modified server, and equally a hostile npm dependency in the web bundle, captures holder secrets. The passkey-PRF
  member key is unlocked in operator-served JavaScript. The ADR accepts the server case and does not name the browser.
- Cheap candidates: publish the signed release digest each installation runs (deployed-binary transparency); isolate
  browser key handling in a dependency-free worker with Subresource Integrity. Stronger, research only: a minimal
  reproducibly built unsealer process, confidential VMs, client-side decryption.

### F. Member keys across devices, tools and orgs (6 of 7)

- No defined place for a member's private key on the CLI, CI or desktop app; a `cli` session carries no slot. Without
  a blessed store, developers will put X25519 keys in env vars and dotfiles. Multi-device enrolment and users with no
  passkey (OIDC, social or SAML only) are undecided.
- One member key per person means one stolen device opens that person's environments in every org on the
  installation. A key per (person, org), for example a PRF with an org-specific salt, contains that.
- Membership is grants only; there is no memberships table.

### G. Identity and SSO (5 of 7)

- Identity providers are instance-scoped with global slugs on purpose (open item 3). Per-org SSO needs a design that
  keeps an org's provider from minting identities elsewhere, for example identities namespaced to the org, at the
  cost of one person holding several identities.
- A SCIM removal always proceeds; if it drops an environment below two holders, publishes block. HR offboarding can
  halt unrelated teams' deploys with no defined alert channel.
- If per-org SSO ships after GA, the identity-link schema should be shaped now to avoid a risky migration later.

### H. Installations, routing and residency (5 of 7)

- Undecided: one front door or an origin per installation; how the CLI finds an org's installation without revealing
  which emails have accounts; how clients verify an installation's identity and pin its transit key.
- An org cannot move between installations without a holder-driven export and import plus re-minting every token. The
  federation audience embeds the installation origin, so a domain change breaks every binding.
- EU residency is not mapped to installations. A third-party CDN terminating TLS sees plaintext values and can swap
  the transit key, and becomes a sub-processor.

### I. Org lifecycle and GDPR erasure (5 of 7)

- No suspension (`orgs.active` is never updated); deletion refuses while projects exist (`hierarchy.go:426`); no
  per-org backup or restore (`backup.go:101`).
- Needed: one persisted org state machine (active, suspended, cut off, shredded, deleted) every path checks; an
  erasure promise tied to backup retention (the ops spec ceiling is 180 days); whether per-org restore is needed at
  GA at all (zoom-out: customer export plus installation restore may suffice).

### J. Limits, metering and the billing boundary (5 of 7)

- No limit storage: `registration_policies` has only `fresh_org_cap`; orgs have no plan or limit field; rate budgets
  are compile-time, in-memory, single-node constants (`budget.go:79,162-206`).
- Undecided: counter rows or count queries (counter rows add SERIALIZABLE conflicts inside an org); where shared
  counters live across nodes; what a missing limit means for existing orgs after upgrade; the one documented usage
  interface where billing begins outside the core; audit volume per org (one estimate: ~70 million rows a day at
  1,000 orgs × 50 workloads fetching once a minute) and its retention per plan.

### K. Locks, contention and throughput (6 of 7)

- Writes run SERIALIZABLE with 4 attempts (`tx.go:38-42,200,347`). The cause of cross-org serialization failures is
  unproven.
- Every `commit_seq` assignment takes exclusive advisory lock 84; every insert into either audit trail takes shared
  lock 85; audit export takes 85 exclusive, so one export stalls every org's audited writes and fetches.
- Session advisory locks on pinned connections (`admission_serialized.go:17-33`, `upgrade/lock.go:172-175`) break
  under the transaction-mode pooler decision 10 requires.
- Every transaction takes `FOR SHARE` on the one `upgrade_control` row; MultiXact growth is unmeasured at real core
  counts. Measure with the gate 7 load test before redesigning.
- Migrating `commit_seq` to per-org order: continue above the global maximum without rewriting append-only tables;
  lock order when one transaction writes both trails; exports in flight across the upgrade.

### L. Rolling upgrades and mixed versions (3 of 7)

- Admission accepts one exact schema and the chart uses `Recreate`; rolling upgrades need a new admission model and a
  rollback boundary.
- **An N-1 node does not know Sealed exists** and would write server-readable data into a Sealed org during a rolling
  upgrade, irreversibly once backed up. Needs one rule for every security feature: enforce invariants in the database,
  or gate activation until no N-1 node remains.
- Old CLIs and operators do not speak the decision 12 envelope; minimum client versions and the error contract are
  undefined.

### M. Sealed coverage and day-to-day use (6 of 7)

- Personal values are sealed under the project sealer (`service/personal.go:132-136`), which Sealed refuses; the ADR
  never mentions them. Refuse, wrap to the member alone (conflicts with the two-holder rule), or wrap to member plus
  recovery key.
- No inventory of `ForProject` callers; a feature added later can open an unlisted plaintext path. Candidate: every
  feature declares a Sealed disposition, checked by an analyzer.
- Schema editing needs keys for every environment, so schema editors must be able to open production.
- Refusals have no closed error vocabulary; "locked" is undefined; the per-environment protection state (never
  unattended, unattended, exposed, rotated) has no single enum shown everywhere.
- Rotation is heavy enough that customers will skip it; no guided, resumable flow is specified.
- GA nudges toward long-lived raw bearer tokens (bootstrap Secret until phase 2, MCP unprotected by decision 12).
- Clock skew against the 30-second window will fail CI with no obvious cause.

### N. Observability that respects Sealed (2 of 7)

- Ops logs may carry org, project and environment names (`audit-model.md:150`), contradicting the promise that the
  operator sees no Sealed metadata in the application.
- The ops spec bans per-org metric labels, so on-call cannot find the noisy tenant without reading the database.
- No first-party tracing, no span-attribute policy, no production log line with the request ID, no alert delivery
  contract for key-loss alerts, no `build_info` or retry counter for judging gate 8.

### O. Migration into the hosted service (1 of 7)

- What a self-hosted-to-hosted import carries (revisions, snapshots, pins, audit trail, tokens) is undefined; losing
  the audit trail weakens incident evidence.

### P. Governance and public claims (3 of 7)

- Cross-provider review was waived by the owner, who has the disclosed conflict of interest; GOVERNANCE.md has no
  waiver clause. Customers will read this record.
- Who checks marketing copy against the honest statement, and is that a release gate?
- "Holder", "sealed", "instance" and "installation" are overloaded; a glossary is needed before UI copy.

### Inventive contradictions and patterns

| Contradiction | Pattern | Ideal result |
|---|---|---|
| Server rotates the outer layer without the customer vs orgs isolated | Segmentation: outer key per org | One org's key loss or leak touches only that org |
| Restorable backups vs erasable data | Extraction: org key outside the backup domain | Revoking one key makes every copy unreadable |
| Operator cannot see inside an org vs must scope a breach | Content-free per-org signals, tripwires | Notify exactly the affected orgs |
| Stateless nodes vs forward secrecy | Pre-generated, deleted epoch keys or a KMS key | Captured traffic stays closed after a later root-key leak |
| Fetch writes audit vs read replicas | Per-org audit partitions, buffered audit | Fetch scales out without losing the audit guarantee |

## Goals vs. Non-Goals

* **In-Scope Goals:** a GA spec covering themes A to P; amendment proposals for locked ADRs where a gap requires one;
  research on client-side decryption and confidential VMs.
* **Out-of-Scope Non-Goals:** price points and business model; building the desktop app; the post-quantum migration
  except where new hosted primitives choose classical or hybrid; implementation.

## Alternatives Weighed

* Per-org kill switch as a new feature at GA: challenged by the zoom-out reviewer, who proposes customer-side shredding
  (destroy all holder keys) plus backup expiry instead. Not yet decided.
* Rewording requirement 3 to match the honest statement, instead of separating super-admin from production access.
  Not yet decided.
* Deferring rolling upgrades and shipping GA with maintenance windows. Not yet decided.
* GA as phase 1 only (no unattended access). Not yet decided.

## Key Decisions & Staging for Next Steps

None decided yet. The themes above become decision tickets and fog on the wayfinder map.

## Appendix: code facts checked on origin/dev 426bf59ce

- 1 SERIALIZABLE writes: CONFIRMED (tx.go:200,347; 4 attempts 10/50/250ms, 15s deadline)
- 2 Audit lock 85: PARTLY. Shared 85 on BOTH tenant+instance audit inserts (00011:63,70-76); export exclusive (recovery_runtime.sql:7, store.go:1140,1191). Two sequences, table-wide UNIQUE each (00011:20-21,42,44). Plus exclusive lock 84 on every commit_seq assignment (00011:92,111).
- 3 Personal values under project sealer: CONFIRMED (service/personal.go:132-136,297)
- 4 No limits: CONFIRMED (registration_policies only fresh_org_cap, 00057:50-65; orgs no plan/limit; budget.go:162-206 constants, :79 in-memory single node)
- 5 Org create WriteSerialized session advisory lock on pinned conn: CONFIRMED (tx.go:58,84-94; admission_serialized.go:17-33; 4 callers)
- 6 upgrade_control FOR SHARE (upgrade/admission.go:173); chart Recreate (deployment.yaml:19-20): CONFIRMED
- 7 cli session no key material; no Sealed code exists: CONFIRMED
- 8 Org delete exists (refuses with projects, hierarchy.go:426); per-org backup/restore none (backup.go:101); suspension none (orgs.active never updated)
- 9 OIDC/SAML/OAuth2 providers instance-scoped, global slug: CONFIRMED
- 10 Multi-org membership via grants, no memberships table: CONFIRMED
