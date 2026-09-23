# 785 - SPA sign-in challenge (passkey) and enrolment gate

Follow-up to #760 (server enforcement, `docs/handoff/760-second-factor-at-signin.md`).
#760 shipped the password-then-authenticator hop; this is the rest of the SPA
slice: the passkey as a second factor, and the enrolment gate `whoami` drives.

## What landed

1. **Passkey as second factor.** `api/stepup.ts` `useLoginChallengePasskey`
   runs `loginChallengeWebauthnStartOp` -> `navigator.credentials.get` ->
   `loginChallengeWebauthnFinishOp` (same shape as `usePasskeyLogin`, epoch
   guarded, body-token check, `acceptSession`). `routes/Login.tsx` shows the
   button when the 202 challenge lists `webauthn` and the platform can assert;
   both challenge legs share one refusal slot. Parity rows
   `loginChallengeWebauthn{Start,Finish}` are `webui: login`.
2. **Enrolment gate.** `app/App.tsx` has a third router branch: when
   `whoami.enrolment_required` is true, `/login` renders `routes/EnrolmentGate`
   and every other path redirects there. The gate ends by itself: enrolling a
   factor reissues the session without the flag and the branch falls away.
   Sign-out is offered beside the card (not a skip: the next sign-in lands here
   again).
3. **e2e under the product default.** The fixture no longer sets
   `HIKYO_SECOND_FACTOR=optional`; flows that sign in unenrolled accounts walk
   the gate with `passEnrolmentGate` (fixtures/instance.ts).

## Decisions (Marc, 2026-09-23)

- **D1 - the gate re-asks the password (option A).** `enrolTotpStart` and
  `enrolPasskeyStart` need a password proof and the server does not waive it for
  a gated session. The sign-in form's copy is gone by then (the root transition
  unmounts the router), and a reload lands on the gate too. So the gate opens on
  a password step. Rejected: server waiver (posture change), carrying the
  password across components.
- **D2 - recovery codes first (option 1).** `regenerateRecoveryCodes` proves
  with a TOTP code once one stands, and the confirm code has spent its step. So
  the order is password -> recovery codes (password proof) -> factor. The gate
  is then driven purely by `enrolment_required`; nothing holds it open after the
  flag clears. `ui/auth/SecondFactorSetup` steps became
  `password | codes | choose | totp` (Step 2..4 of 4).

## Non-obvious mechanics

- **Password across the codes remint.** `regenerateRecoveryCodes` reissues the
  session, and `acceptAccountSession` retires every sensitive value. The gate's
  factor enrolment still needs the password, so `useEnrolmentGateCodes`
  (`api/account.ts`) delivers `{ codes, password }` through the one sanctioned
  transfer (`prepareTransfer`) instead of plain state. Both die with the gate.
- **No shell flash after sign-in.** A `LoginResult` has no
  `enrolment_required`. `AuthProvider.acceptSession`, for a session established
  from anonymous without capabilities, binds the identity but keeps the root on
  `transitioning` until a blocking whoami answers (`awaitWhoami`). Otherwise the
  shell would paint and fire every surface's reads into the gate's 404s (each
  one an audited admission refusal). A step-up of a live session is unchanged
  (keeps painting, quiet refresh).
- **The flag survives an account remint.** `acceptAccountSession` used to
  rebuild the identity as `{session, principal, capabilities}`, dropping
  `enrolment_required` after the gate's recovery-code step, so the shell painted
  over the gate. It now spreads the whoami-verified identity and overrides only
  session/principal. Found by the e2e run, pinned by an AuthProvider unit test.
- `identityVersion` includes the flag, so a focus revalidation notices it.
- **e2e TOTP on the serving instance.** `completeSecondFactor` for B used to
  sleep into a fresh 30s step unconditionally, which could outlast a 30s test
  (`workspace.spec` popup login flaked on it). It now presents the current
  step, then the next step (inside the skew window) on refusal.
- `whoami` carries no username; the gate names the display name (or "your
  account").

## Verification

- Unit: `routes/Login.oidc.test.tsx` (passkey leg), `routes/EnrolmentGate.test.tsx`,
  `app/App.gate.test.tsx`, `app/AuthProvider.test.tsx` (awaitWhoami hold, proven
  red without it). Stories: `ui/auth/SecondFactorSetup`, `ui/auth/LoginFlow`.
- `go test ./api` (parity) green.
- e2e: see the PR for the desktop run result.

## Open / not done

- None known. If a future caller needs the gate to survive the flag clearing
  (e.g. codes after the factor), revisit D2: the password stops being a valid
  recovery proof once TOTP stands.
