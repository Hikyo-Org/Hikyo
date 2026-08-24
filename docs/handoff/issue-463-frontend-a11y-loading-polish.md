# Handoff: #463 frontend accessibility and loading-state polish

Issue: https://github.com/Hikyo-Org/Hikyo/issues/463. Base:
`5543b70eae3f3851247ce34f842e976c60ad02cf`.

## Contract

- The members list announces `Loading members…` only while its grants query is
  pending; its existing loaded-empty status and table remain unchanged.
- The organisation retention panel announces `Loading the retention policy…`
  only while its retention query is pending; the error and editor branches stay
  mutually exclusive with that state.
- The organisation Name input references its explanatory hint through
  `aria-describedby` and a `useId`-derived hint id.
- A pending revoke marks and disables only the button whose grant id matches the
  TanStack mutation variables. Other revoke buttons remain available.
- The acting revoke button exposes `Revoking…`, a matching accessible name, and
  `aria-busy="true"`; no API or server contract changes.

## Coverage

- `web/src/routes/AccessibilityPolish.test.tsx` exercises all four behaviors at
  the rendered accessible-DOM seam.
- The tests cover pending-to-loaded rendering for both reads, resolved hint
  association, and two sibling revoke actions with one in flight.
- Generated outputs: none.
