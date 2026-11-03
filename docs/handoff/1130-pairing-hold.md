# #1130: pairing post-quantum feasibility HOLD, handoff

Status: **documents only. HOLD. Not merge-ready as a feature, not operative.**
Refs [#1130](https://github.com/Hikyo-Org/Hikyo/issues/1130); the PR does not close
it and no PR may, because the ticket says a feasibility note alone does not
establish PQ pairing. No Go code, schema, configuration or behavior changed.
Security approval: not given (no cryptographic change is made, but the recorded
exception wording needs the maintainer's approval like the other PQ amendments).
Independent adversarial review is required; cross-provider review is SKIPPED by
explicit owner instruction (2026-11-02) and is not CLEAN.

Stacked on PR #1156 (`dunky13/pqc-1127-transport-policy`, head `f5d549182`). Draft PR
base: that branch.

## What this delivers

| Item | Where |
| --- | --- |
| Threat analysis, offline-guessing statement, feasibility survey, separable blockers, owner options, strict-transport statement | [pairing feasibility](../security/pqc/pairing-feasibility.md) |
| Dated banner for the "Remote-instance pairing" proposal, verbatim, plus a link to the feasibility note; the proposal block in the migration ADR is now a pointer | [remote-instance-pairing](../adr/remote-instance-pairing.md), [post-quantum-migration](../adr/post-quantum-migration.md#remote-instance-pairing) |
| One README row indexing the feasibility note (beyond the ticket's list, flagged below) | [contract README](../security/pqc/README.md) |

## Result in four sentences

No standardized, maintained, reviewed post-quantum PAKE exists, so the PAKE half
of #1130 stays on HOLD under DEC-13 (a) and E-01. **One DEC-13 premise has
changed:** the register says `go-jose` lacks ML-DSA, but v4.1.8 (2026-11-02) adds
it for Go 1.27 and RFC 9964 (May 2026) defines the JOSE identifiers. The proof
signature is separable and no longer library-blocked, but ML-DSA-65 does not fit
the code's closed bounds (JWK 2048, header 2048, proof 8192) or its empty-context
rule, and a PQ proof alone leaves enrollment classical. The register was not
amended; this change is an input to the owner's recheck.

## Acceptance checklist

| # | Criterion (short) | Status | Why |
| --- | --- | --- | --- |
| 1 | Focused threat analysis; TLS, PAKE and proof-signature guarantees stated | DONE | In the feasibility note, from pinned source. The offline-guessing statement is [derived] from the SRP structure, not run; independent review of the note is NOTRUN |
| 2 | Approve standards-backed implementation and composition with independent security review; else HOLD and name the blocker | HOLD | The ticket's own allowed outcome. Blocking decision: DEC-13 (a), no acceptable PQ PAKE. Blocking dependency: a standard or reviewed profile plus a maintained Go implementation with vectors (below) |
| 3 | Versioned protocol, encodings, domain separation, algorithms, binding, replay, expiry, reviewed bounds | HOLD | Needs criterion 2. The size arithmetic for a proof signature is recorded as input |
| 4 | Full lifecycle through server, CLI, API, client and durable state | HOLD | Needs criterion 2. The existing classical lifecycle is unchanged |
| 5 | PQ-required pairing cannot negotiate classical; compatibility stays explicitly classical with a retirement plan | PARTIAL | The classical limits are now recorded as E-01 in the pairing ADR banner. No PQ-required mode exists and no retirement plan is written |
| 6 | Preserve approval, single use, tenant and scope limits, expiry, reauthentication, pin trust, audit; refuse before side effects | PARTIAL | Nothing was changed, so nothing was weakened. Only `internal/pairing` and `internal/remotefetch` tests were run (pass). Not re-verified: service, authz, server |
| 7 | Tests: substitution, wrong origin, relay, reuse, ordering, malformed input, downgrade, DoS bounds, crash and restart | HOLD | Applies to a new protocol. Existing tests were not reassessed |
| 8 | Simultaneous pairing, multiple nodes, failover replay, revocation, restore holds, both engines | HOLD | Needs criterion 4. No engine was run |
| 9 | Migration, re-pairing, key rollover, revocation, rollback floor, observable demonstration | HOLD | Needs criterion 4. The ADR's existing re-pairing recovery stands |
| 10 | Close only when implemented, or the owner narrows scope with a tracked residual gap | HOLD | Neither is true. #1130 must stay open as the tracked residual gap |

## Evidence by class

| Class | State |
| --- | --- |
| Source capability | Read at `f5d549182`; the named files have no diff against the issue baseline `2ac7bb366`. `go-jose` v4.1.8 source read from the module (sum `h1:k38WifTutpGuqt6Imq9xW1OgCvnWFri0Nz7FnfiACM8=`) |
| Local tests | `go test -p 2 -count=1 ./internal/pairing/ ./internal/remotefetch/` PASS on 2026-11-03 (go1.27.3 darwin/arm64). The three `rg` commands in the note run as described |
| External documents | Registry data (RFC 9964 level and date, draft revision, date, stream, expiry) fetched directly. Document contents (draft text, RFC 9807, RFC 9449, NIST page, nginx page, CFRG list) read through a page-summarizing tool: wording unverified. Library searches: none found, not "none exist". Not read: RFC 9964 after its first 100,000 characters, the last third of the draft |
| Hosted CI, real-provider, runtime, deployment | NOTRUN. No pairing was run between two instances and no handshake group was observed |
| Review | Independent adversarial review: pending. Cross-provider: SKIPPED by explicit owner instruction, not CLEAN |
| Heavy checks | Not run: no code changed |

## Exact blocking decisions and dependencies

1. **Blocking decision (PAKE).** DEC-13 (a) stands unless the owner chooses
   otherwise. A decision to proceed needs a named PQ PAKE profile; the only
   candidate found is an individual draft with no IETF or IRTF stream, which the
   brief and the issue do not allow adopting.
2. **Blocking dependency (PAKE).** A standardized or CFRG-reviewed PQ PAKE, a
   maintained Go implementation with published vectors, and an independent
   cryptographic review of Hikyo's composition.
3. **Owner decisions for a proof-only ticket (option B in the note).** A reviewed
   DPoP bound against the 2048/2048/8192 constants (DEC-08); widening the closed key
   and algorithm sets; the empty-context rule against DEC-02 and DEC-07; the DEC-03
   form (all-required or PQ-only) for credentials; approval of the `go-jose` pin
   change; qualified header limits for supported edges.
4. **Enrollment stays classical** in any option short of a PQ PAKE and PQ-authenticated
   pin. The ticket's "PQ-required pairing cannot negotiate a classical exchange"
   cannot be met by a proof-only change.

## Recheck triggers

As listed at the end of the feasibility note: a PQ PAKE reaching an IETF or IRTF
stream or an RFC, or a reviewed analysis of OQUAKE; a maintained Go implementation
with vectors; a `go-jose` or Go minor release touching ML-DSA or DPoP; an IETF
draft or registry entry for ML-DSA in DPoP; owner decisions on the DPoP bound, the
context rule and the DEC-03 form; header qualification; NIST action on PAKEs; a
practical attack on SRP-2048 or ML-DSA. A trigger starts a recheck, not a build.

## Scope-narrowing text for #1135 (a proposal; the owner must accept it)

DEC-13 records that #1135 is blocked by #1130 and that the owner decided to narrow
scope with #1130 as linked residual work. The text below is what would make that
usable. It is a proposal and has not been posted or applied; editing issues and
their dependency links is outside this task.

> #1135 qualifies instance pairing (I20) at its current classical profile only.
> The pairing and revocation drill runs against the existing SRP-2048 and Ed25519
> DPoP flow on SQLite and PostgreSQL and states no post-quantum claim for it.
> Pairing is reported in the capability statement as HOLD with exception E-01
> (classical authenticity; a recorded classical transcript exposes the phrase and
> the bearer credential to a future quantum adversary; the pin is read over a
> classical chain; the hop is at compatibility, E-08). #1130 remains open as the
> tracked residual gap with its recheck triggers and no longer blocks #1135. #1123
> closure uses the "owner-approved scope reduction with linked residual issues"
> route for pairing and proof authority, recorded in the closure note.

Actions only the owner can take: accept or edit the text; remove #1130 from
#1135's "Blocked by" (or confirm that "narrowed with linked residual work" is
the intended reading); note the same narrowing on #1123's pairing acceptance line.

## Beyond the ticket, and for the reviewer

- **Banner timing.** The brief says a banner becomes operative when the
  implementation merges. #1130 has no implementation, so the banner says it is
  operative when this docs PR merges, which does not close #1130. The text is a
  statement of current limits, not a behavior change. Confirm that reading.
- **README row.** One row added to `docs/security/pqc/README.md`. It may conflict
  trivially with sibling branches that also add rows.
- **Banner wording.** Verbatim from the proposal, plus the review sentence and one
  link. Proposal sections "Cryptographic profile" and "Limits of the guarantee" were
  not edited in body; the banner carries the amendment, as for the other ADRs.
- **Dependency drift, observation only.** Hikyo pins `go-jose` v4.1.6; v4.1.7
  (2026-10-30) fixed five security issues (release summary read through the
  summarizing tool, wording unverified), and v4.1.8 followed. Updating it is a
  dependency change outside this task.
- **DEC-13 text.** The register's "`go-jose` lacks ML-DSA" is now stale. Not
  edited: the register is the owner's.

## Rollback and key retention

Revert the documentation commit. No key, certificate, credential, schema or
configuration is created or changed. No peer or provider was contacted except for
reading public documents and downloading a module into a scratch directory.

## Next authorized step

Independent Standards and Spec review of these documents, then the owner's decision
on options A to D in the feasibility note and on the #1135 narrowing text. Merge,
issue edits, release and deployment are not authorized.
