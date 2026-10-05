# Members: one access-card list

## Scope

Replace the Members grants table with the member-access rule-card layout and
implement the prototype's per-key human permissions. Delivery includes a green
pull request and merge; production deployment is outside the requested endpoint.

## Behavior

- Members lists each principal once, with scope-wide access and narrowed rules
  together. Search covers both, including rule-only members and machines.
- Existing access groups all permissions at the same scope into one card.
  Capabilities outside the narrowed-rule vocabulary, including instance
  administration, remain visible. Permission origins retain their subjects in
  a disclosure and in the Edit access dialog.
- Edit access revokes an individual permission through the existing grant API,
  or opens the existing scope-wide composer with the member and scope selected.
  Cards report an in-flight revoke. Dialogs resolve from the current listing,
  rather than retaining a stale snapshot after a refetch.
- Narrowed rules support Edit, Publish, Reveal (including history), Define
  keys and Manage access for selected keys/folders. See remains environment-wide,
  even when included in a card with a key selector. Environment/project
  administration remains broad. Machines retain scope-wide access.
- Rule-card changes use one atomic replacement request. All additions and
  removals are authorized against the original state before any write. Refused,
  stale or invalid requests leave the original card intact; uncertain responses
  trigger an authoritative refresh.
- Delegated managers receive only the rules they may manage. Grant-list refusal
  does not block this view. Broad access, invitations and credential reset are
  available only through their existing authority. Without See, the editor uses
  disclosed selector IDs, preserves existing selections and exposes no key names
  or environment protection settings.

## Authorization

Publish proves every selected key and every coupled group member before named
draft conflicts or materialization. Approval ceremonies, votes, current quorum
and merges check the pinned key set. Restore and history disclosure authorize
the addressed key. Key-definition changes authorize their automatic publication
in every affected environment before mutation.

Manage access contains the target rule's complete selector, including future
environments, folder descendants and exceptions. Delegated managers can assign
only permissions they hold. Assigning See requires management over all keys of
the selected environments because See itself is environment-wide. Ordinary grant
APIs cannot turn narrowed management into broader grant authority.

Rules and grants remain additive. A narrowed rule does not reduce an existing
broad grant. Existing scope-wide cards retain their complete permissions and
origins; no legacy grant storage is removed. All/future-project grants and machine
access keep their existing scope-wide path.

## Sensitivity review

The new dialog stores only a grouping key for public permission metadata.
Credential-reset authority stays in `useSensitiveState` and its display-once
dialog. No reset results enter a query/mutation cache. Sensitive inventory hashes
are refreshed only after reviewing the changed Members and rule API sources.

## Entry points

- `web/src/routes/Members.tsx`: unified list and existing-access dialog.
- `web/src/routes/accessRules/RulesPanel.tsx`: shared member-card presentation.
- `web/src/routes/accessRules/ExistingAccess.tsx`: complete scope-wide cards.
- Storybook: `Routes/Members/WithAccessRules` and `ProjectWithAccessRules`.

## Validation

Typecheck, lint, design checks and SPA build pass. The full web unit suite passes
1,484 tests; the affected Storybook browser suites pass 15 tests. Desktop and
390px browser inspection confirms the cards and single-key editor without
horizontal overflow. Real-server desktop and mobile coverage creates a single-key Publisher
card, checks sibling exclusion, atomically switches the selected key, then removes
the card. Relevant Go suites and isolation regressions pass on SQLite and
PostgreSQL, including atomic replacement, management containment, single-key
publication, group closure, history/restore and key-definition fan-out.
