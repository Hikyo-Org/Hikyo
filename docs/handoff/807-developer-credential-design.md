# Issue #807: developer credential decision handoff

## Entry point and phase

Read the complete [developer credential ADR draft](../adr/developer-credentials.md).
This is the design phase of [#807](https://github.com/Hikyo-Org/Hikyo/issues/807),
not a completed credential implementation. The ADR is **unlocked and
inoperative**. Keep #807 open. Documentation ask A already merged through #811.

The local baseline for this task is
`36c323c417d1c29b20431a8c4bdb539882b9136c` on
`dunky13/fix-issue-807`. The authorized endpoint is implementation, relevant
checks, review, and a signed commit on this branch, after the required design
gate. Push, PR creation, merge, release, and deployment are not requested.

## Accepted direction

- Separate bounded developer credential for one unprotected environment.
- Eight-hour default, instance lifetime ceiling, current human read/reveal
  checked on every fetch, and recorded human audit authority.
- No renewal or offline delivery; ordinary human-session ceremony unchanged.
- Explicit consent to current and future published keys in that environment.
- Self-delegation from current read/reveal and fresh scope-bound human
  reauthentication, without project manage-identities.
- For login coupling, the owner's instruction is security without breaking
  development UX. The draft proposes surviving ordinary logout/idle expiry
  while security invalidation is terminal, including issuing-session security
  revocation, factor changes, provider/identity invalidation, and scoped
  offboarding. Global sign-out must cover developer credentials if provided.

The eight-hour hard maximum, four-live-credential cap, and exact lifecycle
resolution are proposals for the final owner lock. The draft names all owning
ADR amendments and both-engine implementation acceptance requirements.

## Review evidence

Two independent OpenAI subagents ran the code-review skill's Standards and
Spec axes against the entire new ADR draft. Both found an expiry-revival flaw
in a recomputed policy ceiling. The revision durably clamps existing expiry
inside the serialized ceiling-change transaction and explicitly tests lowering
past expiry followed by raising. Both review follow-ups confirmed the fix and
reported no new critical findings. They do not establish cross-provider review.

On 2026-10-09 the owner confirmed keeping the cached review choices
`gpt-6.1-sol/high` and `claude-opus-5-5/high`, and stated that there is no longer
a Claude subscription. The shared review cache marks Anthropic unavailable;
the owner's working-style guidance records that no Claude quota prompts or
review launches should occur until access is explicitly restored. No vendor
skill was edited and no Claude model call was launched.

Cross-provider review: **SKIPPED by explicit owner instruction**. On
2026-10-09 the owner answered the review-path question with "skip cross provider
review". This waives the provider-review gate for this #807 design, not all
future work. Do not request a replacement provider or quota for this decision.
The ordinary reviews remain ordinary reviews; no skipped pass is CLEAN.
The final owner lock of the concrete lifecycle and limits remains pending.

## Validation and remaining work

Relative ADR links, repository punctuation rules, whitespace, the complete
offline HTML companion, and desktop/mobile document rendering are checked.
No application code changed, so no Go, web TypeScript, or runtime suite is
claimed. Remote CI, publication, deployment, and runtime proof are absent.

Next steps: obtain explicit owner lock including pending limits; record required owning
ADR amendments through the governance procedure; implement with regression
coverage at the credential, delivery, custody, and revocation seams; run
focused checks during implementation and the relevant full suite at its end;
review and commit. Do not weaken existing human authentication or snapshot
rules to make the new delivery path work.
