# Passkey sign-in refused every browser-enrolled passkey

## Symptom

On nightly 52 (`913bde9a`, before #760), and every build before this fix,
"Use a passkey instead" on `/login` failed with the TOTP message: "That code
was not accepted. A code is valid for one time step and is used once…".

## Root cause

Two breaks. Either one alone would cause the failure:

1. `webauthnrp.BeginEnrol` never requested the `credProps` client extension,
   and go-webauthn does not add it by default. A browser only reports
   `credProps.rk` when the RP asks for it.
2. `useEnrolPasskey` (and the e2e fixture's enrolment) never sent
   `credential.getClientExtensionResults()` in the finish body.

Under B13, a missing `credProps` means `discoverable=0` (fail-closed). So every
passkey was stored as non-discoverable, and `attemptPasskeyLogin` refused it
(`!stored.Discoverable` → uniform 401). The login page ran that 401 through
`stepUpFailureText`, whose 401 copy is about authenticator codes.

Why tests missed it: `webauthntest.Device` always emitted `credProps.rk=true`,
whether or not it was asked. No e2e test covered discoverable passkey
sign-in.

## Fix

- The server requests `credProps` at enrolment. The browser and the e2e fixture
  forward the extension results.
- `webauthntest.Device` reports `credProps` only when the options request it,
  like a real browser. Without the server fix, the Go discoverable-login tests
  fail (UVRefused, Clone, SyncedNotFlagged, LoginNoAccountThrottle).
- New `passkeyFailureText` for the passkey legs of Login and StepUpBanner.
  The server's refusal stays uniform (no oracle).
- E2E: new `login › signs in with a passkey alone…`. The account enrol drill
  now asserts that SPA-enrolled passkeys are discoverable.

## Existing passkeys

Rows enrolled before this fix stay `discoverable=0`. B13 forbids inferring
residency. To fix one, remove the passkey and add it again under Account &
security. That is the remedy the new 401 copy names.
