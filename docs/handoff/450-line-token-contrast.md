# Issue 450: control boundary contrast

## Contract

Unfocused text inputs must expose a rendered boundary with at least 3:1 contrast against their fill in both the dark and light themes, satisfying WCAG 1.4.11.

## Implementation

- `--line` is `oklch(0.5 0.016 220)` in the dark theme and `oklch(0.64 0.012 210)` in both light-theme declarations.
- The values remain shared by control and decorative hairlines, preserving the single asserted token named by the issue.
- The login Playwright flow samples the browser-rendered input border and fill pixels and requires a ratio of at least 3:1 in both themes.

## Verification

- Run `pnpm --dir web typecheck`.
- Run `pnpm --dir web exec playwright test e2e/flows/login.spec.ts`.
- Run the repository full suite before merge when host memory pressure permits.
