# Issue #807: developer credential administration

## Delivered scope

The locked developer credential core and loader guide already shipped before
this change. The September 28 issue comment predates that implementation. This
change closes the four remaining administration rows in `api/parity.yaml`:

| Operation | Web UI | Authority |
| --- | --- | --- |
| `listProjectDeveloperCredentials` | Project settings, Developer credentials | `manage-identities` at the project |
| `revokeProjectDeveloperCredential` | The same list, followed by exact-credential confirmation | `manage-identities` at the project |
| `getDeveloperCredentialPolicy` | Instance settings, Developer credentials | `instance-config`, with second-factor assurance |
| `setDeveloperCredentialPolicy` | Bounded seconds editor, then consequence confirmation | The same instance authority |

Personal mint/list/revoke remains expressly CLI-only. No browser bearer mint,
renewal, offline cache, loader plugin, or core authorization change is introduced.
The existing server bounds the ceiling to 1 through 28,800 whole seconds and
atomically reduces stored expiry when the ceiling decreases. Increasing it does
not restore prior expiry. The UI explains that reduction and that already
delivered process values cannot be erased by revocation.

## Implementation ownership

`web/src/api/developerCredentials.ts` reaches generated operation descriptors
through the normal `parsed`/`ok` client. Queries retain metadata only; mutations
carry a credential ID or bounded seconds, never a bearer. Errors use fixed
messages rather than raw server detail. Transport, contract, session-change and
server failures remain distinct; an uncertain mutation response asks for a
metadata refresh before retry. Instance policy 403 is documented in OpenAPI
and generated types, and the UI names both permission and assurance causes. Permission refusals refetch authorized
metadata so a newly denied scope loses its old rows and controls. Session-epoch
errors do not start a new fetch. `AuthProvider` owns cache retirement on session
changes, and the project panel is keyed by both scope IDs.

`ProjectDeveloperCredentials.tsx` and `DeveloperCredentialPolicy.tsx` own their
forms and confirmations. Existing buttons, side panels, dialogs, table overflow,
and section tabs retain the application's design and accessibility behavior.
The local-development guide documents the new administrative paths and the
unchanged personal CLI lifecycle.

## Acceptance and evidence

- The issue's loader documentation is in
  `docs/site/src/content/docs/docs/local-development.mdx`, including local
  execution choices, development-only values, process-level disclosure limits,
  TTY-only human sessions, varlock composition and dotenvx positioning.
- The accepted credential design is
  `docs/adr/developer-credentials.md`. Existing lifecycle and isolation coverage
  remains in `internal/service/developer_credentials_test.go`,
  `internal/isolation/developer_credentials_e2e_test.go`,
  `internal/isolation/developer_security_test.go`, and
  `internal/cli/developer_credentials_test.go`; those files are unchanged here.
- `web/src/routes/DeveloperCredentials.test.tsx` exercises actual generated
  fetch contracts, exact-scope revocation, confirmation/cancellation, lifetime
  bounds, redacted errors, retry recovery, denial after permission narrowing,
  and late-response session retirement.
- The two new Storybook modules cover metadata, phone layout, empty/loading or
  denied state, and consequence confirmation.
- `web/e2e/flows/settings.spec.ts` owns the real-server desktop/mobile journey:
  a human without administrative authority is denied; a fixture mints a real
  bounded credential with fresh consent; the UI lowers and raises the instance
  ceiling without reviving its old expiry, cancels then confirms revocation,
  persists it across reload, and verifies the next delivery is refused.

## Validation status

Local Node 26.7.0 validation:

- Web typecheck, lint and design checks pass.
- Full web unit suite: 203 files, 1,801 tests pass, including all 38 focused
  developer credential tests. The owner independently inspected their actual
  request, response, redaction and session-retirement assertions.
- Production SPA and embedded Go UI binary builds pass.
- TypeScript generated-client verification: 37 tests pass; `go test ./api` passes.
- New Storybook modules: 10 tests pass in each of dark and light themes.
  Full Storybook build and Docs index check pass: 138 modules, 797 stories.
- Documentation `verify` passes, including built-page CSP and real offline PWA
  checks. Documentation status verifies all 35 existing ledger entries.
- Real desktop and Pixel 5 mobile browser: both administration journeys pass
  against the embedded server, two tests per viewport. The mobile run also
  verifies 44px action targets and no document overflow. Both viewports pass
  axe checks; the owner inspected screenshots of metadata and confirmation.

The first desktop run caught misleading 403 text: the instance authorization
owner returns the same status for missing grants and inadequate assurance.
The UI and missing OpenAPI 403 declarations were corrected; the rerun passes.
The first Empty story assertion matched transient loading status; it now waits
for the actual empty-state text. The suite rejects combined viewport runs, so
the browser projects run separately against fresh fixture instances.

Independent Sol 6.1 high Standards review found one generic-error classification
issue, independently reproduced and fixed using the canonical client classifier.
The second combined Standards, Spec and Security source review reported no
supported findings. Cross-provider review was explicitly waived for this change
and is SKIPPED, never described as clean. Runtime proof above belongs to the
issue owner; reviewers did not run the browser.

To reproduce the focused real-server journey after the production SPA build:

```sh
pnpm --dir web exec playwright test e2e/flows/settings.spec.ts --project=desktop -g 'developer credential administration'
pnpm --dir web exec playwright test e2e/flows/settings.spec.ts --project=mobile -g 'developer credential administration'
```

The repository fixture builds the embedded binary and owns isolated test state.
The tests mint only fixture credentials through the existing fresh-consent API;
the product UI exposes no minting. Both journeys restore policy and revoke their
fixture credential on cleanup. Screenshots and local logs are ephemeral chat
evidence, not committed checkout paths.

Remote CI and verified merge are separate from these local results. This change
does not authorize a release or production deployment.
