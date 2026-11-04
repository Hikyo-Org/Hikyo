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
| PQ profile and maintainer security approval | HOLD under #1124 and the affected owning ADRs |
| Child implementation and integrated qualification | HOLD pending prerequisites and the child's actual evidence; this parent document satisfies none of those runtime criteria |
| Crypto/parser/lifecycle tests, both-engine drills and platform measurements | NOTRUN by this documentation-only draft; required of the affected children and #1135 |
| Provider/production checks, new custody, publication and deployment | NOTRUN; no environment action authorized |
| UI screenshots | SKIPPED: no rendered UI or visual behavior changes |

## Delivery state on 2026-11-04

Nothing has merged, no child ticket is closed and nothing is post-quantum in production.
#1123 cannot close: gate 5 of the [program draft](../design/post-quantum-program.md)
needs #1135's real qualification evidence or an owner-approved narrowed scope with linked
residual work. Every implementation is opt-in, and each PR needs maintainer security
approval, which is not given.

The stack is rooted on `dev`: #1148, then #1150, then #1155. #1156 and #1158 stack on #1155,
as do #1172 and #1186. #1171, #1188, #1190 and #1141 stack on #1156. #1192 stacks on #1158.
PR #1195 merges all nine child branches as the evidence candidate and is not for merging.
#1197 stacks on #1195 and should move to its parent's merged state before it lands.

| Ticket | PR | Head | What exists | Limits that keep it open | Last repair reviewed |
| --- | --- | --- | --- | --- | --- |
| #1124 contract | #1150 | `771d15047` | Inventory, profiles, decisions, owner decided | Register premises need owner edits (packet C-01 to C-14) | Yes, before the base merges |
| #1126 backups | #1158 | `9c01e77e7` | Hybrid backups through CLI, receipts, drill, restore | Automatic upgrades refuse under a hybrid policy; every export path emitting a receipt is unmet (E-19); no hybrid vault | No |
| #1127 transport | #1156 | `d3d662967` | Strict TLS 1.3 and `X25519MLKEM768` on the listener and owned clients | Per process only, no stale-node fence; no external hop | No |
| #1128 webhooks | #1188 | `d70889f82` | Protocol 2 sender, reference receiver, vectors | R-1128-A, R-1128-B; recipient ownership PARTIAL; revision route HOLD | No |
| #1129 Transit | #1172 | `afda0f96b` | `mldsa65` below the API | Public surface HOLD on the v1 freeze; restore-hold and takeover NOTRUN | No |
| #1130 pairing | #1190 | `35e057451` | Threat analysis, feasibility survey, HOLD record | No acceptable PQ PAKE; DEC-13 HOLD; E-01 not yet effective | No |
| #1131 rollout | #1186 | `5e7bbef32` | Opt-in dual-signed strict profile, generation floor | G-1 to G-4; real cluster NOTRUN | No |
| #1132 release | #1192 | `07eb2a721` | Readers and verifiers, called by no shipping path | No signer; E-11 bridge cannot close; custody unnamed; floor persistence not built | No |
| #1133 PKI | #1141 | `c904100cd` | ML-DSA lifecycle below the API, gated off in production | Public surface HOLD on the v1 freeze; G1 to G6 | No |
| #1134 external | #1171 | `912b4f5c8` | 24-route register, local qualification | No route verified; strict-route refusal HOLD (#1183) | No |
| #1135 qualification | #1197 | `500a1827f` | Packet, runbook marked not executable, integrated run | HOLD by construction: no real environment was authorized | No |

The candidate `68b2673de` passed one integrated run on SQLite and PostgreSQL (11 top-level
tests, 55 PASS lines, none skipped or failed), the focused packages, and hosted
`repository invariants`, including lint. That is local and hosted evidence only. Real
provider, cluster, receiver, deployment and amd64 evidence is NOTRUN.

### Owner decisions outstanding

These block merging the named PR:

1. Stale-node fencing against DEC-10 (#1156).
2. `backup-receipt/v2` and the E-19 receipt requirement (#1158).
3. Accept the #1155 banners, including the `api-cli-surface` banner, which has no review.
4. Maintainer security approval per PR, and merge order.
5. Revision route for the non-ADR specs (#1128, #1186).
6. #1133's five unconfirmed choices, one of them unconditional ML-DSA leaves against the locked condition, and whether #1133 keeps the `VerifyConnection` walk and platform verifiers (#1141).
7. #1132's key-role shape, sidecar floor and the shared banner wording on `system-architecture` and `oss-mechanics` (#1192).
8. #1130 banner timing, E-01 row fields, DEC-13 wording, and the narrowing text #1135 needs (#1190).

These do not block merging, but block use or closure:

9. The `/api/v1` freeze: ML-DSA stays unreachable for Transit and PKI until it is decided.
10. Transit signed bytes: cheap to change only until the first key signs.
11. ML-DSA recovery key custody: no release signer until it is named (DEC-14).
12. Per-route strict selection (#1183), and the F-nn decisions #1175, #1178 and #1179.
13. DEC-17 budget thresholds, from the baselines each child recorded.
14. #1126 profile selector and vault generation path, #1132 signing granularity, #1127 proxy-leg confirmation.

### Tickets and issues touched

Reopened for one ADR amendment each, under DEC-18: #14, #8, #638, #25, #156, #154, #71, #22
and #33. Close each when its amendment is operative. Filed: #1154, #1175 to #1185 and #1191.

## Continuation procedure

1. Read the program and full owning issue; recheck `dev`, open PRs,
   comment decisions and ownership before taking work. Record exact base/head,
   complete changed paths and actual consuming code. Do not overwrite another
   active owner's implementation or downgrade a ready PR.
2. Continue #1124 inventory/design work first. Marc owns unresolved profile
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
