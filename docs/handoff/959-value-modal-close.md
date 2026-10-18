# Issue #959: value modal corner close

## Outcome

The value editor now offers a named, keyboard-operable X beside its title.
The shared Dialog exposes an optional `onClose`; only MatrixRowEditor opts in.
The existing footer Close, Escape and backdrop handlers remain available.
The X delegates to that same handler, including while work is in flight, and
cannot submit the editor form. Long identifiers wrap without shrinking the X.
The existing Button primitive supplies focus, theme and coarse-pointer sizing.

This explicitly amends the earlier no-X design for editors in `DESIGN.md`.
Decision and acknowledgement dialogs retain their existing exits.

## Sensitive-value custody

MatrixRowEditor changes only its Dialog props and an obsolete footer comment.
Reveal, copy, draft, sensitive-state, focus-remasking, cancellation and session
retirement retain their existing owners. Its reviewed inventory pin was updated.
Maintainer security review remains required; local automated review does not
substitute for independent human review.

## Evidence

Before editing, the running browser demo's value modal had a heading and footer
Close but no corner control. After editing, the same flow was inspected at
1280 by 800 and 393 by 851 with the corner X visible and no horizontal overflow.
The desktop/mobile browser regression additionally verifies a 44px target on
the emulated coarse-pointer device, keyboard activation, focus restoration,
discarded unsaved edits, retained Escape dismissal, and revealed secrets absent
on reopening. This uses disposable public demo fixtures, not a live server.

Local validation:

- TypeScript, lint and design checks passed.
- Unit suite: 1,948 tests across 212 files passed.
- Dialog and MatrixRowEditor browser stories: 17 passed in each theme.
- Normal application and demo builds passed, including demo-isolation and CSP.
- New corner-close browser regression passed on desktop and mobile.
- Complete demo browser suite: 82 tests passed across desktop and mobile.
- Standards review: no findings; Spec review: no findings. Both were independent
  read-only source reviews, with runtime checks performed by the implementing agent.
- Cross-provider review was skipped because the configured subscription path is
  unavailable. Jev readiness was skipped because no API key is configured.

Structural graph relationships were refreshed locally. Generated graph files and
offline HTML evidence are not committed.

## Reproduce

From the repository root with the pinned Node and pnpm versions:

```sh
pnpm --dir clients/ts install --frozen-lockfile
pnpm --dir web install --frozen-lockfile
pnpm --dir web run demo
```

Open the LAN `/demo/` URL printed by Vite. Select Demo admin, open LOG_LEVEL in
development, edit without saving, and activate the corner X. The modal closes,
focus returns to the original matrix cell, and the published value stays intact.
Open AUTH_SECRET, reveal the fake value, close with X, and reopen: plaintext is
hidden again. Repeat on a phone.

For the regression against the static artifact:

```sh
pnpm --dir web run build:demo
pnpm --dir web run test:demo --grep 'value editor corner close'
```

## Delivery boundary

Prepared for PR review. Remote CI and independent maintainer security review
are separate gates. No merge, release, provider acceptance or deployment is
claimed by this handoff. Browser evidence covers Chromium demo behavior; no
Safari or Firefox run was performed.
