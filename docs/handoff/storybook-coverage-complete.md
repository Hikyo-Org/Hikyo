# Storybook coverage — remaining atoms and every route screen

Follow-on to `storybook-coverage-sub-components-and-pages.md`. Before this pass
59 story files covered the ui/ atoms and the route sub-components; 31 route,
panel and app components had no story. After it every component with a visual
surface has one: 108 story files, 508 stories, all passing the a11y gate
(`test: 'error'`) in both palettes.

## What was added

| Group | Story files |
|---|---|
| Atoms | `ui/Field`, `ui/ThemeIcon` (Field's docblock previously said "no story by design"; updated) |
| Identity panels | OidcProvidersPanel, SamlProvidersPanel, SamlSpKeysPanel, FederationIssuersPanel, ScimProvisioning |
| Account / ceremony | AccountProfile, AccountSecurity, CLIReauth, Ceremony, WorkspaceStepUp, EstablishCredential, EnrolmentGate |
| Workspace | WorkspaceApprove, WorkspaceScope, Reconnect, WorkspaceCallback, OIDCDone, `app/RuntimeMaintenanceBoundary` |
| Instance / org | InstanceAdmin, InstanceConfig, SystemProjectNotice, OrgSettings, CompactOrgRetention |
| Values core | Values, Matrix, HistoryDrawer (desktop + phone drill-in), PinReleaseOutcome, RevisionDiff |
| Key declarations | KeyDeclarationDetail, ImportWizard (every step), CatalogueManageDialog, DefinitionsBundlePanel, LastApplyProvenance |
| Machine access | MachineAccess page, MintDialog, GrantDialog, BindingCard, MachineRevealDialog, and the nine mutation dialogs (BindingDialog, CreateAccountDialog, DeleteAccountDialog, CreateProviderDialog, SetCredentialDialog, MachineRevokeCredentialDialog, DeleteProviderDialog, LeaseMintDialog, LeaseActionDialog) |

Pages wrapped by `gateSystemScope` export and story their inner page component;
the gate's own answers are already in `SystemScopeRefusal.stories.tsx`.
File-private components that were storied gained an `export` keyword and
nothing else (`MachineAccess.tsx`: 12, `ScimProvisioning.tsx`: 1). The
sensitivity inventory pin was refreshed for those two files with a review note.

## Deliberately not storied
- `app/App.tsx`, `app/AuthProvider.tsx`: router root and provider, no visual surface.
- Success states that navigate the frame away (OIDCDone login/link/reauth success, WorkspaceCallback close).
- Real WebAuthn prompts and popups (Ceremony "waiting for your passkey", WorkspaceStepUp authorising); EnrolmentGate's codes/choose/totp steps need the verified-remint whoami handshake and are storied in `SecondFactorSetup.stories.tsx`.
- `RuntimeMaintenanceBoundary.Ready`: the component seeds `ready` before its first poll, so a play could not tell fixture from seed.
- One-sentence 404 "not disclosed" states on the instance panels, and refusal variants that render the same `Alert` as an existing story.

## Component fixes made on the way (campsite rule)
- `FederationIssuersPanel.tsx`: two `event.target.value as …` casts replaced by an option lookup (`optionById`).
- `KeyDeclarationDetail.tsx`: `as PresenceMode` cast replaced by a narrowing helper like the existing `ruleType`.
- `vite.config.ts`: throws with the fix in the message when `clients/ts/node_modules/zod` is missing. Before, the bundler only warned `UNRESOLVED_IMPORT` and treated zod as an external the SPA could never load. A `resolve.alias`/`dedupe` route was tried and dropped: it resolved the import but rolldown's native pre-resolve still printed the warning.
- No a11y fixes were needed: axe passed on every new story in both palettes.

## Gotchas learned (add to the pattern doc when next touched)
- Wire bodies in `responses[].body` must be `z.input` shapes: int64 fields as JSON numbers, because `withApp` runs `JSON.stringify` and a bigint throws; `parsed()` coerces them back. Type fixtures `satisfies z.input<typeof zX>` from `@hikyo/zod`.
- POST-create rows need the contract's status (`status: 201`); a default-200 row on a `[201]`-only operation is treated as a refusal.
- Never name a story after a global (`Error`, `Set`): use `Failed`, `FirstCredential`.
- A click on a control that exists before whoami settles is lost when `AuthProvider` swaps the tree; wait for signed-in content first.
- Pages that read the URL in a `useState` initialiser (CLIReauth, WorkspaceApprove) get their query string from a per-story `beforeEach` that rewrites the frame URL and cleans it up.
- Pages with an advisory `/events` stream (Matrix) hold it `pending` so the story sits in "connecting" instead of a reconnect loop.
- `getByRole(..., { hidden: true })` is needed to assert a `display:none` pane is not visible (HistoryDrawer phone stories).
- Bare `$FILES` in zsh does not word-split for `vitest run`; quote-less expansion silently finds no tests.

## zod/mini: measured, not adopted
Measured with Vite on a representative schema (object, string, array of int, enum, url, boolean, record): classic 20.5 KB gzip, mini 6.4 KB gzip, so about 14 KB gzip saved. zod ships in the lazy `account` chunk, not the initial bundle, so first paint is unchanged. Cost: `@hey-api/openapi-ts` emits mini via `compatibilityVersion: 'mini'`, but 42 app files use the classic chained API and mini is functional (`z.optional(x)`, `z.parse(s, v)`, `.check(...)`), so every call site rewrites plus a regen and client-skew churn. Verdict: not worth it now; revisit if the account chunk becomes a measured problem.

## Open
- Design links (`parameters.design`) stay Button-only per `openpencil-storybook.md`; link stories as they are touched.
- The four CI `clients/ts install` steps are still required (the vite guard now enforces them); `check-build-artifact-reuse_test.sh` asserts one of them, so removing them is its own change.
