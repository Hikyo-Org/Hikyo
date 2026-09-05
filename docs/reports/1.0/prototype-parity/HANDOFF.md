# Locked prototype validation and machine readiness corrections

Worktree: `/tmp/hikyo-web-parity`; source baseline `1b5c04cf1241343c556a2c662e988aeca846a20e`.
Parent owns signed commit, push, PR644 and final combined-head replay. No worker commit or push.

## Reviewable result

Open `report.html` alongside `evidence/`, `observations-app.json`,
`observations-ref.json` and `interactions.json`. The report has theme/viewport selectors
and paired frozen-reference/real-app images for all five assigned families.
32 reference images and 35 actual baseline/detail images were captured. All actual
views passed serious/critical axe and page-width checks. Four corrected journey
screenshots supplement the retained before-fix evidence.

The fixture was the real embedded Go server, genuine authenticated tenant, SQLite,
and alternate ports47789-47795. The app was not a mocked prototype route. Frozen
references came directly from docs/site/public/prototypes and were never edited.
Different record counts and identifiers are intentional; current DESIGN.md controls
visual tokens while the prototype controls anatomy/interaction and ADR controls semantics.

## Findings fixed in the worktree

- Machine setup called the supported but prerequisite-blocked reveal grant “not in this build.”
  The state is now blocked; withdrawing project opt-in also blocks existing held grants.
- A read grant was labeled as successful first delivery without any fetch evidence.
  It now states configuration-delivery permission and explicitly says fetch success is unverified.
- Kubernetes empty states incorrectly called the operator absent from the build.
  They now report no status in this view and direct operators to HikyoSecret conditions with kubectl.
- Desktop claim `/kubernetes.io/serviceaccount/uid` overlapped its value in the fixed140px label column.
  `.kv dt` now wraps; browser regression measures label scrollWidth against clientWidth.

Five changed files: web/src/api/identities.ts, its test, routes/MachineAccess.tsx,
styles/app.css, e2e/flows/machine-access.spec.ts. Tests preserve the supported
opt-in/grant semantics and distinguish readiness from observed delivery.

## Validation

- Typecheck and production build pass.
- All83 web files/677 unit tests pass; focused identity suite39 passes.
- Six real embedded desktop/mobile machine tests passed in2.8minutes: all five
  tabs, expansion with truthful journey, display-once mint and gated dismissal.
- Final desktop/mobile expansion replay after claim-label wrapping passed in2.8minutes.
- Actual supplemental interactions passed: matrix group collapse, explicit all-env
  edit disclosure, db/git source options, history list-to-detail, sidebar30px
  section padding and1px rule; mobile Escape restores Menu focus.
- Report selector controls rendered without browser script errors.

## Limits and follow-up

The five assigned families are covered. The sixth locked social-signin/2 family
(#587) is explicitly post-1.0 in its owning scope. Existing real login was captured:
password, passkey, configured IdP, setup authority and recovery only, no false
registration/JIT door. Cosmetic icon/image/description API work is deferred with
explicit disabled controls; options and reasons are recorded in the HTML report.
Single-environment desktop editor has spare grid space; classified as optional
density polish, not a canonical structural or behavior failure. No broad layout
redesign was made. No production deployment claim is made.
