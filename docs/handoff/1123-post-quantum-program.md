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
each PR's final head that is independently reviewed and green at merge, as PQ-13 requires. It
covers no earlier or later head. #1150's earlier approval covers its text at `27a16c276`. The
last PR in this stack records each merged head.

New capabilities are opt-in, but these changes reach every upgraded instance:

- #1156: runtimeconfig migration 6 writes a new desired revision. From reading the source,
  older binaries refuse it, so a binary rollback means restoring the upgrade's backup; no old
  binary was run. The owner accepted this for the next nightly.
- #1156: the CLI matches origins across equivalent spellings. A store holding `https://h` and
  `https://h:443` at different pins is now refused, and deleting developer credentials also
  removes those stored under equivalent spellings.
- #1158: the reader rule applies to every restore, and upgrades read the stored backup policy
  before any journal.
- #1186: a malformed `trust.json` or a persisted strict floor refuses the controller.
- #1141: importing, rotating or installing a classical issuer under an ML-DSA root is refused.
- #1172, held: migration 00083 would move the rollback floor of every instance.

### Merged on 2026-11-04

Ten child and contract PRs and this ledger landed on `dev` with merge commits. #1172 is held
until the `/api/v1` freeze is decided, PR #1195 was evidence only and closed unmerged, and #1197
lands last with its documents only. Each head below is the head that merged.

| PR | Merged head | Merge commit |
| --- | --- | --- |
| #1148 | `5c7830c5c` | `6428249ee` |
| #1150 | `0918418c5` | `af7929bf0` |
| #1155 | `5d0d24299` | `9a82575a3` |
| #1156 | `4e69c7b52` | `b1274dc96` |
| #1158 | `46e7279e9` | `c70498b20` |
| #1141 | `b3bfe5112` | `c7f10d263` |
| #1186 | `c06edd893` | `c20ab5ae2` |
| #1192 | `5fcc53268` | `6a8ec7af6` |
| #1171 | `57f1c327e` | `7042dbe4d` |
| #1188 | `d61ae5e2d` | `8cbd32446` |
| #1190 | `a91256980` | `455b94655` |

Review coverage is not identical to these heads. The independent review, re-review and final
check each read earlier heads. After the final check came merges of `dev`, including two
conflict resolutions on the register text (#1171, #1192), wording fixes on #1158, #1186, #1192
and #1141, and CodeRabbit's three wording fixes on #1190. CodeRabbit reviewed only some of
these PRs before its free limit. Hosted `ci-dev-required` and, for code, `repository
invariants` and `ops-floor` passed on each merged head. The candidate's integrated run is not
evidence for this tree.

| Ticket | PR | What exists | Limits that keep it open | Independent review |
| --- | --- | --- | --- | --- |
| #1124 contract | #1150 | Inventory, profiles, decisions, owner decided | Register premises need owner edits (packet C-01 to C-14) | 2026-11-04, repairs re-reviewed |
| #1126 backups | #1158 | Hybrid backups through CLI, receipts, drill, restore | Automatic upgrades refuse under a hybrid policy; every export path emitting a receipt is unmet (E-19); no hybrid vault | 2026-11-04, repairs re-reviewed |
| #1127 transport | #1156 | Strict TLS 1.3 and `X25519MLKEM768` on the listener and CLI trust entries; client selection is partial | Per process only, no stale-node fence (DEC-10 amended by the owner on 2026-11-04, E-22); no external hop | 2026-11-04, repairs re-reviewed |
| #1128 webhooks | #1188 | Protocol 2 sender, reference receiver, vectors | R-1128-A, R-1128-B; recipient ownership PARTIAL; declared-amendment route accepted | 2026-11-04, repairs re-reviewed |
| #1129 Transit | #1172 | `mldsa65` below the API | Held from merge by the owner until the v1 freeze is decided; public surface HOLD; restore-hold and takeover NOTRUN | 2026-11-04, repairs re-reviewed |
| #1130 pairing | #1190 | Threat analysis, feasibility survey, HOLD record | No acceptable PQ PAKE; DEC-13 HOLD; E-01 effective since 2026-11-04 (expiry 2027-05-03) | 2026-11-04, repairs re-reviewed |
| #1131 rollout | #1186 | Opt-in dual-signed strict profile, generation floor | G-1 to G-4; real cluster NOTRUN | 2026-11-04, repairs re-reviewed |
| #1132 release | #1192 | Readers and verifiers, called by no shipping path | No signer; E-11 bridge cannot close; custody unnamed; floor persistence not built | 2026-11-04, repairs re-reviewed |
| #1133 PKI | #1141 | ML-DSA lifecycle below the API, gated off in production | Public surface HOLD on the v1 freeze; G1 to G7 | 2026-11-04, repairs re-reviewed |
| #1134 external | #1171 | 24-route register, local qualification | No route verified; strict-route refusal narrowed under DEC-06 (#1183), no strict claim on external routes | 2026-11-04, repairs re-reviewed |
| #1135 qualification | #1197 | Packet, runbook marked not executable, integrated run | HOLD by construction: no real environment was authorized | 2026-11-04, repairs re-reviewed |

The candidate `68b2673de` passed one integrated run on SQLite and PostgreSQL (11 top-level
tests, 55 PASS lines, none skipped or failed), the focused packages, and hosted
`repository invariants`, including lint. That is local and hosted evidence only. Real
provider, cluster, receiver, deployment and amd64 evidence is NOTRUN.

### Owner decisions

Decided on 2026-11-04, as recommended:

1. Merge to `dev` when green and accept publication in the next nightly, with rollback floors,
   except #1172, which is held until the `/api/v1` freeze is decided. This settles packet C-10's
   rollback policy for this round.
2. Stale-node fencing: strict is per process, recorded in DEC-10 and E-22. The fence stays #1127 work.
3. `backup-receipt/v2`: the banner is extended, and the E-19 receipt requirement stays registered as unmet.
4. The #1155 banners that were not the owner's exact text are labeled as not owner-locked.
5. Merge order is bottom-up.
6. The non-ADR specs (#1188, #1186) use the declared-amendment banner.
7. #1141, #1192 and #1190 merge as built, with their choices below still open.

Decided on 2026-11-04, when the owner said "All open points as recommended":

8. #1130 pairing: option A, HOLD under E-01. The E-01 fields are accepted (expiry 2027-05-03, every
   release to date, recorder case included) and the DEC-13 wording is applied. #1135 qualifies I20 at its
   classical profile only, and #1130 no longer blocks it. #1130 stays open as the tracked residual gap.
9. #1183 (F-09): narrowed under DEC-06. Strict stays on the listener and owned clients, with no strict
   claim on any external route and no per-route selector.
10. #1175 (F-01): strict does not disable the GitHub Actions secret route, because the provider has no PQ
    alternative and strict does not govern it. It stays exception E-06, now effective (expiry 2027-05-03).
11. #1178 (F-04): `ssh-mldsa44-ed25519` is not adopted, because DEC-02 excludes ML-DSA-44 and DEC-03
    selected no composite.
12. #1179 (F-05): the relying party offers no ML-DSA credential parameters and stays on classical
    credentials under E-05, with no enrollment or recovery work.
13. The register's dispositions for F-01 to F-11 are accepted; their issues stay open as tracked work,
    except the four above.

Still open. None blocks merging what is listed above, and each blocks use or closure:

1. The `/api/v1` freeze: ML-DSA stays unreachable for Transit and PKI, and #1172 stays out, until it is decided.
2. Transit signed bytes: cheap to change only until the first key signs.
3. ML-DSA recovery key custody: no release signer until it is named (DEC-14). Deferred by the owner
   until a signer exists.
4. DEC-17 budget thresholds, from the baselines each child recorded. Deferred by the owner until an
   amd64 baseline exists.
5. #1126 profile selector and vault generation path, #1127 proxy-leg confirmation, and packet C-10's step 2 abort wording.
6. #1133's five unconfirmed choices, one of them unconditional ML-DSA leaves against the locked condition.
   Deferred by the owner until ML-DSA is enabled for PKI. Whether #1133 keeps the `VerifyConnection` walk
   and platform verifiers is not deferred and stays open (packet C-05).
7. #1132's key roles, sidecar floor, shared banner wording and pin delivery. Deferred by the owner until a
   signer exists, as is recovery custody (item 3). Signing granularity is not deferred and stays open. No
   signer can exist until these and custody are settled.

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
