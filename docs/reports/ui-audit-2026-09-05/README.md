# Web UI audit against the locked prototypes and DESIGN.md (2026-09-05)

Scope: every surface in `web/src/app/navigation.ts`, compared against the six locked prototype
families under `docs/site/public/prototypes/`, `DESIGN.md`, and the ADR-delegated UI deltas in
`docs/spec/ui-spec.md`. Evidence came from two fixtures: the Vite prototype mock
(`pnpm --dir web prototype`) for the chrome families, and the real embedded Go server booted through
`web/e2e/fixtures/instance.ts` with its seeded tenant for everything the mock does not serve
(history, machine access, adapters, audit, remotes, approvals, SCIM). Captures ran at 1280x800,
820x1000 and 393x851, dark and light.

Method: five read-only source auditors (one per family plus one for the un-prototyped surfaces)
produced `file:line` findings; every finding was verified against source or the API contract before
a fix was made. `raw-findings.md` is the unedited ledger. The sections below record what was fixed,
what was decided, and what is left for the API.

## Decisions recorded in DESIGN.md

- Unknown reads render the word `unknown`; a column the caller may not read renders `· unreadable`.
  Dashes are never placeholders.
- The unpublished-draft dot stays (locked prototype) and carries the word "draft" in accessible text.
- Warnings are neutral with a slate tint; unknown diagnostics are dashed. Red is for errors only.
- Status dots may use the pill radius; labelled chips and revision labels use the badge radius.
- Group collapse is instant (rows are virtualised); the ease token is ease-out-quart.
- Key rows are one line; linked-key membership is a glyph with the group in the accessible text.
- Revision history on a phone is a drill-in with a back action.
- No em-dash in user-visible text anywhere, including the server-emitted Git refusal sentence and the
  spec line that mandated it (`docs/spec/ui-spec.md`, punctuation only).

## Fixed in this change

Server
- `GET /auth/whoami` now carries the account's `display_name` (login already did), so a reloaded
  SPA names its holder in the header instead of printing a principal id.
- The Git-managed refusal sentence lost its em-dash on the server, in the client constant, the
  spec and every test that pinned it.

Environment matrix (prototype env-matrix/31)
- Rows measure the row token again (36px desktop, 44px touch): the linked-key membership that
  #665 added as a second line is now an inline glyph with the group in the accessible text.
- The key column is pinned at 236px so a 20-character key fits; `production PROTECTED` has a gap;
  the group count no longer inherits the heading's uppercase and weight.
- The table fills the content well instead of a fixed 70vh box (no double scrollbar on a phone).
- Degraded columns render `· unreadable` with an accessible label instead of a hidden dash; the
  problem message names the environment, not its id; the lock reaches assistive tech.
- Draft dot carries the word "draft"; the problems chip takes the badge radius; the empty state
  names the browser Import button instead of claiming imports are CLI-only.
- Publish sheet has a Close button, returns focus, and groups linked keys.

Cell editor and ceremonies (prototype reveal-edit/6)
- Reveal fails closed (`can_reveal === true`), copy of a secret is gated on the same answer and
  labelled as an audited disclosure, and a role without reveal reads why.
- The plaintext is no longer hidden from screen readers by an `aria-label`; the re-mask countdown
  is announced once.
- Dialogs are named; a click in the dialog padding no longer discards edits; "Edit all
  environments" toggles back; per-row clear lives only in the single-environment view.
- Schema is a human summary line (`string · pattern · required in production`) with the raw
  declaration behind a nested disclosure and an "Edit declaration" link.
- Secret entry is a masked textarea (pasted newlines survive) with a "show while typing" opt-out;
  value fields grow with content.
- Key creation: near-miss warning against existing names, visible trim notice, normalised-name
  preview instead of rewriting keystrokes, the authoring statement, the Git banner.
- `any_of` failures list what each alternative accepts; the scan-warn dialog lost its third
  button and shows the locator; the ceremony names the act (publish vs disclosure), the reveal
  window, secret keys, and tracks pending per factor.
- Clipboard auto-clear compares before clearing, so a value the operator copied later survives.

App chrome (prototypes app-chrome/15, 16, 18)
- Breadcrumbs use the surface label from the navigation table everywhere (no more `members` /
  `settings` / `account` lowercase drift) and project surfaces name the surface as a fourth crumb.
- Chrome surfaces use `--bg-panel` and `--bg-hover` (buttons, avatars, toasts, menus, chips,
  popovers, editors); the light theme no longer floats controls above the paper.
- Radius roles restored (pill only on identity circles, count badges and status dots; badges on
  chips and revision labels; containers on cards); the on-accent ink and the QR paper are tokens;
  the ease token is ease-out-quart.
- Diagnostics banners are `role="status"` with `data-severity`; warnings are neutral, errors red,
  unmeasured dashed.
- Header identity on a phone: the chrome auditor asked to keep the name visible at every width.
  Rejected. Below 960px the label hides and the avatar stays; below 700px the whole identity
  control hides because the fixed account avatar already names the holder there. Do not
  re-litigate.
- Dead controls removed: the permanently disabled identity swatches, glyphs and upload; the
  read-only "Description is not available in the API" field; the fake prototype-only slug rename;
  the "not exposed by this API" sentence. Prototype-mode delete hints now tell the truth (never
  cascades).
- Members hides grant, invite and reset controls when the grants list is refused and says so;
  retention inputs refuse out loud and reset; the definitions-source detail follows the mode;
  the machine-credential ceiling reads "90 days" instead of `7776000s`.
- Overview, Projects and Not found use the page anatomy; Overview links and lists projects.
- The sidebar group pill carries a `!` glyph when it counts problems; the fabricated
  "Organisation member" role line is gone; "not available yet" became "local-instance only".

Revision history (prototype revision-history/6)
- Sole-keeper pins are badged and explained; the release confirmation states the consequence and
  the button says "allow collection"; schema drift is slate (Δ), collected payloads are muted.
- The settings pointer is a link; the impact preview groups per environment with the PROTECTED
  marker; the publish label names every environment; absence uses the shared vocabulary.
- Deep links with `?rev=` land on the detail on a phone; glyphs are hidden from assistive tech.

Machine access (prototype machine-access/3)
- Tab counts say "unknown" while a read is pending or failed and the Kubernetes tab carries no
  count; expiry badges have tiers beside the words; grant scopes show origin chips.
- The grant dialog offers `read` or `reveal` (reveal only once the project opt-in is on) with a
  blast statement that distinguishes configuration delivery from plaintext.
- The journey rail carries its actions (open the opt-in, open the grant dialog); tabs use a roving
  tabindex; the mint title is sentence case; unknown leases are one badge each plus one status line.

Surfaces without a prototype
- Remotes: badge shows human state text, one state sentence joins unreachability and staleness,
  credential rejection has its own recovery line, duplicate instance identities are marked and
  not served, self-connection is refused at add, cards became hairline rows.
- Adapters: `selected` visibility with a repository-id list, a visible reason for locked routing,
  "GitHub organization", prefix preview instead of silent uppercase, failure names per line.
- Audit and Change approvals: page anatomy with jump index and panels, loading and empty states
  that name the next action, no nested `<main>`, outcome glyphs, hairline rows.
- SCIM: origin chip per capability line, deprovision flag with glyph only when manual grants
  survive, correct plurals.
- Key declaration: `any_of` alternatives are editable, deprecation warns about live values,
  tightening carries the "cannot un-disclose, rotate" advisory.
- Import wizard: Git banner as its own alert with the recovery named, "Step n of 5".
- Workspace: refused OIDC reauthentication stays on screen with a Back link; "Session expired on
  {origin}" reconnect page; polling stated; approval failure named; CLI reauth loading uses the
  card shell.
- Login: SAML providers listed, per-provider busy label, loading line, fields disabled while a
  ceremony runs (the busy label says why), quiet secondary links.
- Copy: every user-visible em-dash in `web/src` replaced (visible strings by hand; comments by a
  mechanical pass).

Test infrastructure
- The prototype mock serves `/api/v1/meta` and a contract-shaped retention health; a unit test
  pins both to the generated Zod schemas so the mock cannot drift again.
- `expectNoStrayPills` allows status dots up to 12px, matching DESIGN.md.

## API follow-up in #680

[#680 implementation and verification](../../handoff/680-ui-api-gaps.md) closes
publisher names, mint expiry, GHES origin and environment-create consent,
adapter findings, secret-safe per-key revision comparison, owner-only draft
validity, persisted instance identity, per-capability SCIM origins, and
project policy/deletion capability signals.

The remaining rows have separate owners:

| Gap | Follow-up |
| --- | --- |
| Kubernetes CR conditions | [#683](https://github.com/Hikyo-Org/Hikyo/issues/683): design controller-to-server reporting before adding a live-status UI |
| Social sign-in family | #615, explicitly excluded from #680 |

The shared-secret-default row from #680 was based on superseded inheritance
semantics. [The flat-model ADR's normative ripple register](../../adr/flat-model.md#ripple-register-normative)
explicitly removes every defaulting mechanism and retains the prohibition on a
schema `default`. No default field or advisory is added; the stale UI-spec
requirement is corrected.

## Verification

- `node --run typecheck` and `node --run test`: 101 files, 883 tests green.
- `go build ./...`, `go test ./internal/server/ ./internal/service/ ./internal/isolation/` green.
- Real embedded server, seeded tenant: every route captured at 1280, 820 and 393 wide; no
  horizontal overflow; no console errors except the pre-existing cross-origin `/meta` probe from
  the Remotes page against the sibling instance.
- Playwright, real embedded instances, one uninterrupted `pnpm run e2e` on the final tree (build,
  desktop project, then mobile project): desktop 225 passed, 4 skipped; mobile 227 passed,
  2 skipped; exit 0. The skips are the project-scoped cases: mobile-only (the short-key
  touch floor, the history mobile matrix, the two mobile drawer flows) and desktop-only (the
  locked chrome composition, the instance context stacking). Earlier full runs during the work each surfaced pins that had to move to the
  new truth; the e2e pins changed in this work: `matrix.spec` editor row radius role (control to
  container, the row is a card), `shell.spec` well border token (panel line, the surfaces now use
  the settings anatomy), `history.spec` heading name (glyph is decorative) and retention link,
  `login.spec` unchanged (fields stay disabled during a ceremony, the busy label is the reason),
  `machine-access.spec` read-grant sentence and unique journey label, `reveal.spec` audited copy
  label.
- `go test ./internal/isolation/` green (its definitions e2e test carries the Git-mode refusal
  copy that lost its em-dash).
- Cross-model review (Codex, high effort, three rounds): round one raised ten findings, all
  addressed in the third commit (reveal gating fails closed in Values, Members treats 403 as the
  second-factor refusal and blanks cached rows on any refusal, reveal blast lists secrets only,
  stale grant choices fold back when the opt-in is withdrawn, the sensitivity inventory now
  detects `useSensitiveMutation`, one-line key rows by construction, remote badge state name,
  provenance styling, DESIGN.md wording); round two verified all ten closed with no new critical
  findings; round three returned CLEAN with one residual minor (the reveal grant's empty state on a
  config-only catalogue), fixed in the fourth commit. A later self-review caught a specificity
  regression from the one-line key row (`min-width: 0` beat the phone touch floor on short keys)
  and restored the 44px floor after the shrink rule, fifth commit, with a mobile-only
  `matrix.spec` case that mints `PORT` and measures the link (31px before the fix, 44px after).

## Handoff

Branch `t3code/align-web-ui-prototypes`, five signed commits on top of `3700a0ef`. Nothing is pushed. To pick
this up: `pnpm --dir web install`, `node --run typecheck && node --run test` in `web/`, and
`pnpm --dir web e2e` for the flow suite (boots two instances from source, needs Go). The prototype
mock (`pnpm --dir web prototype`) now serves clean chrome without a 501 banner. The API follow-up section above records #680 delivery and the separately owned
Kubernetes and social sign-in work.
