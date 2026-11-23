# #1440 Rotation management WebUI: handoff

Split out of #148 (see `148-credential-rotation.md`). The machine-access **Rotations**
tab, status only since #1437, now takes the six rotation acts with the CLI's
authorization. No API, executor or rotation semantics changed.

## As built

- `web/src/api/rotations.ts`: `useCreateRotation` and `useRotationAct` (pause, resume,
  release-hold, run-now, delete). Every act on an existing rotation sends the generation
  the tab read (`expected_generation`), so a concurrent change is a named 409, never an
  overwrite. Run-now always sends `confirm_destructive: true` (the dialog is the
  confirmation) and the chosen `skip_overlap`; delete sends `abandon`. Both hooks
  re-read the environment's rotation list and runs when they settle, success or not.
  `rotationActRefusalText` shows a client sentence per status plus the server's named
  refusal (its safe detail) in parentheses. A fence above the browser's exact integer
  range fails before sending.
- `web/src/routes/Ceremony.tsx`: new purpose `rotation`. It signs `publish` (the window
  the service's `requireRotationCeremony` consumes over the environment and both keys)
  but tells the person what they grant: "hand scheduled publishing to a rotation in ·
  <env>", "standing publish authority over these keys for a scheduled rotation". The
  existing `publish` lede ("publish into a protected environment") would be false: a
  rotation's environment is never protected.
- `web/src/routes/machineAccess/Rotations.tsx`: **Create rotation** dialog; per-row acts
  offered by state (`actVerbs`); one act dialog per verb with its consequences. An open
  dialog whose rotation was re-read at another generation, or no longer offers that act,
  disables its button and says to review the row again. Create,
  resume, run-now and release-hold go through `useProtectedPublishCeremony` with purpose
  `rotation`. Because the purpose is not `publish`, the hook never skips the ceremony for
  an unprotected environment; it skips it only when a live, non-single-decision window
  already covers the act, as the CLI (which tries the act first) and the server do.
  Pause and delete run no ceremony, matching the API formula.
- Release hold shows the restore-hold sentence and the reconcile-then-release instruction,
  and its button is disabled until the person ticks that they reconciled grants and the
  provider credential and read the runs. A held rotation offers release (and pause) only:
  resume, run-now and delete are refused by the server while a hold is on, so they are
  not offered.
- No credential is sent, cached or shown. The mutation variables are ids, seconds, a
  generation and booleans (`sensitiveInventory.json` pins refreshed with that note).
- `api/parity.yaml`: the six rows are `{webui: machine-access}`.
- Docs: `docs/site/src/content/docs/docs/rotation.mdx` "In the browser".

## Evidence

- Unit: `web/src/routes/machineAccess/Rotations.test.tsx` covers acts per state, pause
  without ceremony, resume and release behind the ceremony (stand-in modal asserting
  purpose, environment and both keys; nothing sent before it authorises, nothing after a
  cancel), run-now body, delete query, a 409 shown in the server's words, create
  validation and body.
- Stories: `web/src/routes/Rotations.stories.tsx` adds CreateForm, CreateInvalid,
  CreateNoProvider, CreateRefusedProtected, ResumeAsksForCeremony (real Ceremony modal),
  ResumeRefused, ReleaseHoldInstruction, RunNowSkipOverlap, PauseDone,
  DeleteRefusedInFlight.
- Hook: `useProtectedPublishCeremony.test.tsx` "rotation purpose": prompts in an
  unprotected environment, skips only for a live sliding window, prompts for a
  single-decision window and over a workspace transport.
- e2e: `web/e2e/flows/machine-access.spec.ts` "the Rotations tab takes each act...": the
  rotation routes are stubbed at the network boundary (no provider can mint in the dev
  instance), the publish ceremony is real (reveal-window read and passkey assertion).
  Request bodies are parsed strictly with the API's generated request schemas.

## Remaining

- No real-server e2e of a rotation act: it needs a PostgreSQL dynamic provider in the dev
  instance. The Go service tests own the server behaviour, including that a live window
  opened by one act satisfies `requireRotationCeremony` for the next; the e2e only shows
  the client skips the second prompt.
- A 403 is shown as "you may lack access"; the dialog does not re-prompt the ceremony
  on its own. Retrying the act runs the ceremony again if no window is live.
