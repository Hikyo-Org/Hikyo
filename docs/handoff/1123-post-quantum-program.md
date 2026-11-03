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
