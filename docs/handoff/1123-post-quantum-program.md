# #1123: post-quantum migration parent draft

## Result and scope

The [program acceptance draft](../design/post-quantum-program.md) is the single
entry point for #1123. It supplies a source checkpoint, child dependency and
deliverable register, fifteen stable parent requirement IDs, a reusable evidence
record and staged acceptance/abort/closure gates. It is coordination documentation;
it implements no cryptographic primitive, profile default, key or wire format.

The draft was written against canonical main on 2026-11-01. The parent, #1124,
all ten other child specifications and their comments were checked, together
with current source, governing ADRs, #26/#79/#527 ownership and open PRs. It was
refreshed on 2026-11-02 onto `b22eb0adb6480b3b2c61b8c86dee6df1a5a552a8`, with
child states, open PRs and the source entry points rechecked.
No accepted profile/prototype was linked in the retrieved PQ issue discussions.
Candidate and review identities belong in the draft PR's exact-head evidence;
the commit containing this handoff identifies its source bytes without a
self-referential commit hash.

The parent draft does not duplicate #1124's exhaustive inventory/decisions,
the implementation children's source changes or #1135's integrated executable
harness/runbooks. On 2026-11-02, the adjacent open maintenance, deployment and
release-trust PRs kept their existing owners. Published release history and prior delivered features
are not rewritten or promoted into PQ proof.

## Evidence and remaining gates

| Evidence class | Status for this draft |
| --- | --- |
| Issue/dependency/decision/source inspection | Performed at the starting base; bounded entry-point comparison, not exhaustive cryptographic inventory |
| Local documentation checks | Record complete-diff feedback plan/execution and link/ledger inspection in the draft PR for its final candidate |
| Independent review | Required: exact final range, separate Standards and Spec coverage; record the model that ran and the actual result in the PR |
| Finding repairs | Required when applicable, at the root cause; any changed candidate needs fresh independent review |
| Hosted CI and signatures | Record actual exact-head jobs and GitHub Verified signatures after push; local checks alone do not establish them |
| PQ profile and maintainer security approval | #1124 owner decided; see the delivery state below for each PR |
| Child implementation and integrated qualification | HOLD pending prerequisites and the child's actual evidence; this parent document satisfies none of those runtime criteria |
| Crypto/parser/lifecycle tests, both-engine drills and platform measurements | NOTRUN by this documentation-only draft; required of the affected children and #1135 |
| Provider/production checks, new custody, publication and deployment | NOTRUN; no environment action authorized |
| UI screenshots | SKIPPED: no rendered UI or visual behavior changes |

## Delivery state on 2026-11-04

No child ticket is closed and this program has delivered nothing to production.
#1123 cannot close: gate 5 of the [program draft](../design/post-quantum-program.md)
needs #1135's real qualification evidence or an owner-approved narrowed scope with linked
residual work.

On 2026-11-04 the owner said "review them once more, and merge when green", then "as
recommended" to the choices below. That instruction is the maintainer security approval for
each PR that merges under it. #1150's earlier approval covers its text at `27a16c276`. The
final merged state is recorded by the last PR in this stack.

New capabilities are opt-in, but these changes reach every upgraded instance:

- #1156: runtimeconfig migration 6 writes a desired revision older binaries cannot read, so a
  binary rollback means restoring the upgrade's backup. The owner accepted this for the next
  nightly.
- #1158: the reader rule applies to every restore, and upgrades read the stored backup policy
  before any journal.
- #1186: a malformed `trust.json` or a persisted strict floor refuses the controller.
- #1141: importing, rotating or installing a classical issuer under an ML-DSA root is refused.
- #1172, held: migration 00083 would move the rollback floor of every instance.

The stack is rooted on `dev`: #1148, then #1150, then #1155. #1156 and #1158 stack on #1155,
as do #1172 and #1186. #1171, #1188, #1190 and #1141 stack on #1156. #1192 stacks on #1158.
PR #1195 merges all nine child branches as the evidence candidate and is not for merging.
#1197 stacks on #1195 and lands last, moved onto `dev` with its packet documents only, so it
cannot carry #1172.

| Ticket | PR | Head | What exists | Limits that keep it open | Independent review |
| --- | --- | --- | --- | --- | --- |
| #1124 contract | #1150 | `771d15047` | Inventory, profiles, decisions, owner decided | Register premises need owner edits (packet C-01 to C-14) | 2026-11-04, at this head |
| #1126 backups | #1158 | `9c01e77e7` | Hybrid backups through CLI, receipts, drill, restore | Automatic upgrades refuse under a hybrid policy; every export path emitting a receipt is unmet (E-19); no hybrid vault | 2026-11-04, at this head |
| #1127 transport | #1156 | `d3d662967` | Strict TLS 1.3 and `X25519MLKEM768` on the listener and CLI trust entries; client selection is partial | Per process only, no stale-node fence (DEC-10 amended by the owner on 2026-11-04, E-22); no external hop | 2026-11-04, at this head |
| #1128 webhooks | #1188 | `d70889f82` | Protocol 2 sender, reference receiver, vectors | R-1128-A, R-1128-B; recipient ownership PARTIAL; revision route HOLD | 2026-11-04, at this head |
| #1129 Transit | #1172 | `afda0f96b` | `mldsa65` below the API | Held from merge by the owner until the v1 freeze is decided; public surface HOLD; restore-hold and takeover NOTRUN | 2026-11-04, at this head |
| #1130 pairing | #1190 | `35e057451` | Threat analysis, feasibility survey, HOLD record | No acceptable PQ PAKE; DEC-13 HOLD; E-01 not yet effective | 2026-11-04, at this head |
| #1131 rollout | #1186 | `5e7bbef32` | Opt-in dual-signed strict profile, generation floor | G-1 to G-4; real cluster NOTRUN | 2026-11-04, at this head |
| #1132 release | #1192 | `07eb2a721` | Readers and verifiers, called by no shipping path | No signer; E-11 bridge cannot close; custody unnamed; floor persistence not built | 2026-11-04, at this head |
| #1133 PKI | #1141 | `c904100cd` | ML-DSA lifecycle below the API, gated off in production | Public surface HOLD on the v1 freeze; G1 to G6 | 2026-11-04, at this head |
| #1134 external | #1171 | `912b4f5c8` | 24-route register, local qualification | No route verified; strict-route refusal HOLD (#1183) | 2026-11-04, at this head |
| #1135 qualification | #1197 | `500a1827f` | Packet, runbook marked not executable, integrated run | HOLD by construction: no real environment was authorized | 2026-11-04, at this head |

The candidate `68b2673de` passed one integrated run on SQLite and PostgreSQL (11 top-level
tests, 55 PASS lines, none skipped or failed), the focused packages, and hosted
`repository invariants`, including lint. That is local and hosted evidence only. Real
provider, cluster, receiver, deployment and amd64 evidence is NOTRUN.

### Owner decisions

Decided on 2026-11-04, as recommended:

1. Merge to `dev` when green and accept publication in the next nightly, with rollback floors,
   except #1172, which is held until the `/api/v1` freeze is decided. This settles packet C-10's
   ordering and rollback policy for this round.
2. Stale-node fencing: strict is per process, recorded in DEC-10 and E-22. The fence stays #1127 work.
3. `backup-receipt/v2`: the banner is extended, and the E-19 receipt requirement stays registered as unmet.
4. The #1155 banners that were not the owner's exact text are labeled as not owner-locked.
5. Merge order is bottom-up.
6. The non-ADR specs (#1188, #1186) use the declared-amendment banner.
7. #1141, #1192 and #1190 merge as built, with their choices below still open.

Still open. None blocks merging what is listed above, and each blocks use or closure:

1. The `/api/v1` freeze: ML-DSA stays unreachable for Transit and PKI, and #1172 stays out, until it is decided.
2. Transit signed bytes: cheap to change only until the first key signs.
3. ML-DSA recovery key custody: no release signer until it is named (DEC-14).
4. Per-route strict selection (#1183), and the F-nn decisions #1175, #1178 and #1179.
5. DEC-17 budget thresholds, from the baselines each child recorded.
6. #1126 profile selector and vault generation path, #1127 proxy-leg confirmation.
7. #1133's five unconfirmed choices, one of them unconditional ML-DSA leaves against the locked condition, and whether #1133 keeps the `VerifyConnection` walk and platform verifiers.
8. #1132's key-role shape, sidecar floor, shared banner wording, signing granularity and pin delivery. No signer can exist until these and custody are settled.
9. #1130's options A to D, E-01 fields, DEC-13 wording and the narrowing text #1135 needs.

### Tickets and issues touched

Reopened for ADR amendments under DEC-18: #14, #8, #638, #25, #156, #154, #71, #22 and #33.
Close each when its amendments are operative. #638 carries two banners on
`signed-upgrade-compatibility` with different triggers, so close it after both. The
`stable-workflow-signing` banner has no reopened ticket and cites PR #714. Filed: #1154, #1175 to #1185 and #1191.

## Continuation procedure

1. Read the program and full owning issue; recheck `dev`, open PRs,
   comment decisions and ownership before taking work. Record exact base/head,
   complete changed paths and actual consuming code. Do not overwrite another
   active owner's implementation or downgrade a ready PR.
2. #1124's contract is owner decided. Marc owns unresolved profile
   and scope decisions; obtain immutable owner and maintainer security receipts,
   and complete each ADR amendment under the
   [governance procedure](../../GOVERNANCE.md#amendment-procedure), confirming that
   a ticket reopened for an amendment covers only that amendment and recording
   cross-provider review as SKIPPED by explicit owner instruction, before dependent
   algorithm/default changes are marked ready. A closed issue or merged draft is
   not that receipt.
3. Take only the child-owned delta after its prerequisites pass. Preserve
   closed algorithms and existing custody/authorization/audit/restore semantics;
   stack dependent PRs when necessary. If pairing protocol feasibility remains
   unresolved, keep #1130 and that claimed authenticity scope HOLD.
4. Select checks from the complete candidate diff using
   `scripts/ci/fast-feedback.sh --plan --working-tree`, then
   `scripts/ci/fast-feedback.sh --working-tree`, with explicit
   `FAST_FEEDBACK_BASE` and `FAST_FEEDBACK_HEAD` for the checked-out candidate.
   Follow [CONTRIBUTING](../../CONTRIBUTING.md#local-verification) and the
   [CI guide](../development/ci.md); those commands do not replace affected
   owner/consumer, both-engine, security or real-environment qualification.
5. Populate the evidence records per surface and evidence class. #1135 owns
   the executable operator campaign, exact tested commands, protected custody,
   reader-first transitions, rollback floor and final integrated packet.
6. Sign each commit and add its DCO sign-off. Record final review coverage, local
   checks, hosted status, findings/repairs and precise remaining gates in the PR
   description. Merge, release, deployment, production trust and provider changes
   are separate actions needing their own authority.

No runtime, provider or private signing-custody resource is needed to review
these Markdown changes. Offline HTML companions stay outside Git.
