# 607 (UI slice): the staged sign-in entry and social provider rows

The `[UI]` acceptance line of #607 ("staged entry, door only while open,
confirmation copy, one Microsoft row per Entra tenant row, brand rules per
`docs/research/social-providers.md`"), built ahead of the federated sign-up
backend, against the locked prototype
`docs/site/public/prototypes/social-signin/2/` (#587 iteration 2). Stacked on
#798 (`feat/606-registration-policy`), which owns `signup_open`,
`signup_paused` and `signup_methods` on `GET /auth/methods`. No Go, OpenAPI or
SQL changed here.

## What landed

1. **`ui/auth/ProviderButton`** (new). One identity-provider row. `brand`
   (`google | microsoft | github`, optional) selects the vendor's published
   fill, hairline, ink and mark; a plain row is the house `.btn`. Wording per
   the research: Google and GitHub "Continue with" on sign-in and "Sign up
   with" on the door; Microsoft is always "Sign in with Microsoft · <tenant>"
   (#587 Q3), with one "Microsoft: work or school account" hint per surface.
   Brand colours are the only hex in a component; both themes carry the
   vendor's own pair and pass AA. The tenant span is `inline-block` so the
   accessible name reads "Sign in with Microsoft · Contoso", not
   "Microsoft· Contoso" (the name algorithm trims text at inline edges).
2. **`ui/auth/LoginForm`** rebuilt as the staged entry. Step one: one row per
   way in (Password, Passkey when the platform can assert, one row per
   provider). Password opens the credential form with "Other ways to sign
   in" back; passkey and provider rows start their ceremony from step one.
   New props: `signup: SignupDoor | null` (admitted provider slugs plus the
   worded landing line) and `paused: boolean`. With a door, "New here? Create
   an account" opens the sign-up surface listing only admitted providers;
   choosing one shows the confirmation ("This creates a new account. Already
   have one? Sign in with it first, then add X under Settings › Security.")
   and "Continue to X", which calls `onProvider(slug, 'sign-up')`. The
   callback signature gained the intent. The paused line moved into the card.
3. **`routes/Login`** feeds the card honestly from today's wire: no brand
   (nothing carries one), `signup={null}` (the start request has no `intent`,
   so a door would only end in an unknown-identity refusal), `paused` from
   `signup_paused`. Headings: step one "Sign in to Hikyo", step two "Sign in
   with a password".
4. **Storybook**: `ProviderButton.stories` (every brand, both intents, busy,
   disabled), `LoginForm.stories` (rows, password step, busy per leg, refusal,
   paused, door open, sign-up door, confirmation, back links, intent on the
   start), `LoginFlow.stories` (new `sign-up` scenario walking door to
   "Signed in" with intent `sign-up`), `Login.stories` (`PasswordStep`,
   `Paused`). Fixtures in `ui/auth/fixtures.ts` (`socialProviders`). Both
   theme runs pass the a11y gate.
5. **e2e**: every password sign-in site now clicks the Password row first
   (`login`, `shell`, `instance-admin`, `workspace`, `members` flows); the
   pinned login surface asserts step one (rows, no field, no door, axe) and
   then the credential form; the OIDC-pending assertion counts three rows.
   Every `{ name: 'Sign in' }` button query became `exact: true`: Playwright
   matches names by substring, and "Other ways to sign in" now shares the
   page. The two `shell` sign-out tests answer the challenge with the shared
   passkey instead of a TOTP code: back to back they each spent a 30 s step,
   and the second waited most of its 30 s budget for the next one (they ran
   at 28 to 30 s before, 1.5 to 2.3 s now).
6. **"Last used" badge** (Marc, 2026-09-23). `api/lastSignIn.ts` keeps the
   way in this browser used last (`password`, `passkey`, `provider:<slug>`)
   in localStorage beside the theme choice, best-effort and Zod-parsed on
   read; never a credential, identifier or token. The route reads it once per
   mount and hands `lastUsed` to the card, which badges that one row with a
   filled-accent `ui/Badge` riding the row's top edge (Marc: on top, popping);
   the only place the accent fills a badge. Written when the password is accepted, the passkey
   asserts, or a provider round-trip starts (the redirect leaves no later
   moment).
7. **Sensitivity inventory** re-pinned for `LoginForm.tsx` and `Login.tsx`:
   the password stays component-owned `useSensitiveState`, cleared on submit;
   only presentation moved.

## Decisions (Marc, 2026-09-23)

D1 option (a), D2 option (a), D3 and D4 as recommended. D1's server-side
brand derivation is #607 work; dated amendment lines posted on #607 and #589
(2026-09-23).

- **D1. Brand is not on the wire.** Spec section 3.1 pins only
  `profile: [github]` on `AuthMethodProvider`; nothing tells the client Google
  from Entra. The route therefore renders every provider plain. Options: (a)
  `AuthMethodProvider.brand` derived server-side from the issuer host
  (`accounts.google.com`, `login.microsoftonline.com`) plus `profile` for
  GitHub; (b) an explicit admin-set field. Recommendation: (a), decided on
  #607 with #589 reopened for the spec line. Until then the brand rows exist
  only in Storybook.
- **D2. The picker always shows**, even on a local-only instance with no
  passkey support (one row). The prototype shows it unconditionally; the
  common path costs one extra click. Collapsing to the form when Password is
  the only row is a two-line change if wanted.
- **D3. Local sign-up ("Email + password" row, "Send me a sign-up link") is
  not in the atom.** #608 owns the endpoint; the door type takes providers
  only. Adding `local` to `SignupDoor` when #608 lands is additive.
- **D4. Landing copy is a prop**, worded by the route. The wire does not
  expose the policy's landing to the public door; #607 decides whether it
  should, or whether the door says less.

## How to pick this up (#607 backend)

Picked up by #804 (2026-09-24): brand, door, landing and intent now ride the
wire; see `607-federated-signup.md`. The items below record what was asked.

- Pass `signup` from `signup_open && signup_methods` (slugs of `{kind, slug}`
  entries present in `providers`) and word `landing` from whatever #607 puts
  on the wire. Send `intent` on the OIDC start when `onProvider` gives
  `'sign-up'`.
- Populate `brand` per D1. `mock-api.ts` (`vite --mode prototype`) can then
  carry the `socialProviders` shapes for preview verification.
- The CLI login handoff (#612) must pass `signup={null}` and send `sign-in`
  (spec section 9, "Handoff page").

## Verification

- `pnpm run typecheck`, `lint`, `test` (1131), `test-storybook` and
  `test-storybook:light` (272 each), `design:check` (tokens, adherence at
  budget, markup): green.
- e2e desktop, local fresh instance: `login`, `shell`, `instance-admin`,
  `workspace`, `members` all green after the fixes above (118 passed, then the
  two reworked sign-out tests and the contrast test twice each). Mobile
  project and the other flows: CI.
