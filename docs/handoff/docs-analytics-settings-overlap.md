# Docs analytics settings overlap

The floating Analytics settings button occupied the bottom-left viewport corner
with `position: fixed` and a z-index of 999. It covered documentation navigation
and its footer controls on narrow screens.

The shared consent component now puts the settings button in a centered,
ordinary page footer after the document content. It inherits the page theme and
retains a minimum 44px touch target. The same component serves the landing page.
Consent storage, opt-in behavior, and analytics initialization are unchanged.

## Regression coverage

`docs/site/scripts/check-analytics-layout.mjs` runs within the existing PWA browser
gate. It checks the built docs and landing pages at 320, 390, 768, and 1280px in
dark and light themes. It verifies that settings follow page content, remain
clickable, reopen consent, and can save a refusal. On mobile it also checks that
settings do not overlap the open navigation drawer and that theme switching and
closing navigation work.

The original build fails the page-content placement assertion at 320px. The
fixed build passes all layout cases and the existing offline-navigation/search
gate. The browser layout check explicitly skips analytics-disabled builds;
required-analytics CI and Pages builds exercise the enabled component.

## Validation and delivery

Run `pnpm --dir docs/site run verify` with the deployment's public analytics
configuration to exercise all documentation gates, then run the shared feedback
driver with the complete PR base and head. No additional ESLint check applies to
this package because it does not configure ESLint.

Delivery is authorized through a signed, DCO-signed-off commit, push, pull
request, and merge after required checks pass. Hosted feedback, merge-group
proof, and Pages publication must be reported separately from local validation.
