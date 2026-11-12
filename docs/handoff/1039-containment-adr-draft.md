# #1039 scoped containment: ADR and runbook draft

Issue: [#1039](https://github.com/Hikyo-Org/Hikyo/issues/1039), finding F5 of
[#1032](https://github.com/Hikyo-Org/Hikyo/issues/1032). Base: `dev` at
`426bf59ceb8787234d425f1d5bd2055569fd92d3` (2026-11-12). This is design work. It
changes no product code, schema, command or generated artifact, and it does not close
the issue.

## What this delivers

- [Verified inventory and authority map](../design/incident-containment.md): the six
  claims in the issue re-checked at the current SHA with `path:line` evidence, and what
  changed since the pinned `f14fbec07` baseline.
- [Proposed ADR](../adr/incident-containment.md): scopes, what each revokes at once and
  leaves pending, enumeration, credential replacement, audit trail and the privacy and
  last-manager interactions, with eight decisions as lettered options and a
  recommendation for each.
- [Draft runbook](../operations/incident-containment.md): an ordered procedure that
  uses only commands present on `dev` today.

## What the owner must decide

The ADR is Proposed. Nothing is accepted. Decisions O1 to O8 are listed in its
"Still open" section: mechanism, capability and assurance, last-manager behavior,
release semantics, ledger durability, how far Hikyo goes upstream, instance scope, and
personal values during a long containment.

## Next steps once the owner decides

1. Record the accepted options in the ADR (status line and a decision record), and add
   the declared-amendment banners it lists to the amended ADRs.
2. Open an implementation issue per accepted piece: the account state (if O1-B), the
   ledger, the missing listings, the replace-before-clear rule, and the API, CLI and web
   surface. Each needs both datastores, real-path tests (including adversarial remint,
   restart and concurrency) and a desktop, mobile and CLI exercise.
3. Replace the "What the ADR would change" section of the runbook as each piece ships,
   and exercise the runbook once against a disposable instance with synthetic
   credentials.
4. Open the three small follow-ups listed in the ADR as separate PRs.

## Remaining boundaries

This work establishes none of: running tests, a runbook rehearsal, provider cleanup,
notification delivery, deployed behavior, or maintainer security review. Those are
separate claims. Detection and evidence (#1034 to #1038) are outside this change and
must reconcile with it at parent acceptance.
