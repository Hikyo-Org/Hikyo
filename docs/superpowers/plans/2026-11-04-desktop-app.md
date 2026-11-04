# Desktop app plan

Implements the [desktop-app ADR](../../adr/desktop-app.md). Evidence is in the
[decoupling report](../../reports/server-webapp-decoupling-2026-11-04.md) and the
[spike report](../../reports/wails-shell-spike-2026-11-04.md). Sizes are rough and
low-confidence. The spike code is
[on its own branch](https://github.com/Hikyo-Org/Hikyo/tree/8d95bf25dbce670541719a253bd5c6e09925d35a/spike/wails-shell)
and never merges.

## Gate 0: before any implementation

Done on 2026-11-04, except where noted. The ADR's status section has the record.

1. The owner settled the open decisions.
2. The owner waived cross-provider review for this decision. It is SKIPPED, not CLEAN.
3. The owning tickets are reopened, and [#1215](https://github.com/Hikyo-Org/Hikyo/issues/1215) tracks the work.
4. The amendment text is applied as banners, each with [its own
   trigger](../../adr/desktop-app.md#amendments).

Still needed: [#1214](https://github.com/Hikyo-Org/Hikyo/pull/1214) merges, and the owner
authorizes each implementation PR. When a PR makes a banner operative, close that banner's
reopened ticket.

## PR 1: server and SPA app mode

Size: 1 to 2 weeks. Server and SPA ship together, because the capability token promises
the SPA's behaviour.

**Server:**

- Add `desktop-app/1` to the `ProtocolCapability` `x-extensible-enum` in `api/openapi.yaml`,
  update the meta description, and regenerate.
- Advertise it from `GetMeta` in `internal/server/api.go` only when `webui.Assets()` is
  non-nil, because an instance must not advertise what it doesn't serve. Test both builds.
- There is no session, migration or audit change, because the app holds a `cli` session.

**SPA:** many of these files are hash-pinned in `web/src/api/sensitiveInventory.json`, so
each edit needs a re-review and a new hash. Rewrite the spike's `values.ts` change rather
than carrying it over.

Add one module, `web/src/app/shell.ts`, that owns detection and every `/__shell/*` call
and parses each response with Zod. Detection is `location.protocol`, which no browser can
match.

| Flow | Site | App behaviour | Run in spike? |
|---|---|---|---|
| `reveal`, including the `clipboard`, `restore` and `pin` decisions it signs | `runPasskeyCeremony` in `web/src/api/values.ts`, from `routes/Ceremony.tsx` | Browser hop | Reveal only |
| `copy`, `publish`, `approve`, `reject`, `bypass` | Same | Browser hop | No |
| `adapter` | `runAdapterPasskeyCeremony`, `routes/Adapters.tsx` | One browser hop over the environment set, replacing the per-environment loop. The far end still asks for a touch per environment | No |
| `self-config` | `web/src/api/selfConfig.ts` | Browser hop, carrying its intent | No |
| OIDC reauth for disclosure | `runOIDCCeremony`, `routes/Ceremony.tsx` | Browser hop. Its `BroadcastChannel` can't cross into the window | No |
| TOTP for any decision but `developer-credential` | Existing TOTP paths | Stays in the window. Test that the proxy removes the token on `reauth/totp` | Reveal and step-up |
| Passkey `mint` | `routes/Members.tsx`, `CertificatesTab.tsx`, `SSHCertificates.tsx`, `machineAccess/*` | Continue in browser | No |
| Passkey `access` | Callers of `runPasskeyCeremony` with this operation | Continue in browser | No |
| Passkey step-up | `useStepUpPasskey` in `web/src/api/stepup.ts`, `routes/StepUpBanner.tsx` | Continue in browser | No |
| Passkey enrolment | `web/src/api/account.ts` (`credentials.create`), `routes/EnrolmentGate.tsx` | Continue in browser | No |
| Identity link and establish | `routes/AccountSecurity.tsx` (`window.location.assign`) | Continue in browser | No |
| Sign-in and dead sessions | `app/AuthProvider.tsx`, `routes/Login.tsx`, `web/src/api/session.ts` | Signal the shell | No |
| Session epoch | `web/src/api/sessionEpoch.ts` | Goes inert without the cookie. The shell owns rotation, so replace its check in app mode | Skipped |
| Links built from `location.origin` | `routes/Members.tsx`, `routes/OpenRegistration.tsx` | Use `GET /__shell/origin` | No |
| Workspace launch | `routes/Remotes.tsx`, `web/src/api/workspace.ts` | Hidden | No |
| Downloads | `routes/Audit.tsx`, definitions exports, the KRL link in `routes/SSHCertificates.tsx` | Go through the shell's save panel | Partly |
| Clipboard | `web/src/app/clipboard.ts` | Shell clears | No |
| Toast links | `web/src/app/notifications.tsx` (`_blank`, possibly relative) | Resolve against the instance's origin | No |
| Self-origin check | `routes/Remotes.tsx` | Compare against `GET /__shell/origin` | No |
| Session list | `routes/AccountSecurity.tsx` | Show `user_agent` in the real list. Today only the prototype does | No |
| Camera and file inputs | `routes/PairingQrImport.tsx`, `DefinitionsBundlePanel`, `ChromeIdentityControls`, `ImportWizard` | Probe under `wails:`. Continue in browser if refused | No |

**Far end of the hop.** `routes/CLIReauth.tsx` and `routes/WorkspaceApprove.tsx` run in the
browser on the real origin, so they are not rerouted. `CLIReauth.tsx` and the CLI login
page need these changes, with no server change:

- Neutral wording in place of "Authorize CLI" and "terminal", including the server detail
  strings that mention the CLI.
- Device approval says plainly that a separate device is being approved.
- The signed-in identity is shown. A browser signed in as a different person gets only
  "invalid or expired" from the server, and that can't change under the freeze.
- Environment and key names from existing reads, where the signed-in user can read them,
  in place of raw IDs.

**Check while there:**

- `web/src/api/client.ts` and `app/AuthProvider.tsx` branch on `artifact !== 'browser'`.
  The spike ran through them with no visible problem, but confirm each branch.
- `web/src/api/account.ts`: recovery codes shown once must survive the proxy removing a
  `login` token from the same response. This is plausible, but not traced.
- `key_ids` allows 500 on the handoff, against 10000 elsewhere. This affects every
  key-bound purpose. Decide what a larger unit does before calling the API impact complete.
- A `cli` session confined until enrolment: trace whether browser enrolment releases it or
  the app must sign in again.

**Tests:**

- Vitest for each row, with `shell.ts` stubbed.
- Browser builds are unchanged: every branch sits behind detection.
- The sensitive inventory test passes with reviewed hashes.

## PR 2: move login and trust code

Size: days. No behaviour change. Importing `internal/cli` pulls in most of its 88.8 MB,
so move the login and trust code into small packages the shell can import.

## PR 3: the shell

Size: 2 to 3 weeks. New directory `desktop/`, module
`github.com/Hikyo-Org/hikyo/desktop`.

Port from the spike, and fix these as you go:

| Item | Spike state | Product |
|---|---|---|
| Connection origin | `conn-N` | Random, stored, never reused |
| Sessions | `sessions.json` from `hikyo login` | Own handoff, keychain restricted to the signed app |
| Rotation | Any non-GET JSON response, in memory | The ADR's named routes, token grammar and session id match, keychain first, one at a time |
| Page requests to the D4 refused routes | Allowed | Refused. Build the login part of the list from the contract, so a new route fails closed |
| Session id and principal | Not stored | Stored, with confirmation when a sign-in changes the principal |
| `/__shell/reauth` | Needs `key_ids`, no `self_config`, no `adapter`, ceremony tied to the page's request | All D7 purposes, refusing `developer-credential`, with its own lifetime and a rate limit |
| `openExternal` | Any http or https URL | D9 and D10 limits |
| Download binding | Any known connection | The owning window only |
| Downloads | Silent, `~/Downloads`, no filename | Save panel, filename, quarantine |
| Handlers | Installed, unchecked | Startup check refuses to open a window |
| Wails bridge | `window._wails` and `external` present | Disabled, or listed with an abuse test |
| Sign-in | None | Loopback handoff, with the device flow as fallback |
| Trust | System CA only | System CA or SPKI pin, with a native first-contact confirmation |
| Connections | One, by flag | List, add, remove, revoking and wiping on removal |
| User-Agent | Default | `Hikyo-Desktop/<version>` |

**Tests:** the spike's `main_test.go` as a start (redirects, hostile filenames, rotation,
window binding), plus connection removal, capability refusal, refused routes and hostile
calls to each `/__shell/*` route. The `probes/` scripts give a driven check.

## PR 4: CI

Size: days. Not yet investigated.

- How do the existing lanes treat a second `go.mod`? Check `scripts/ci/ci-job-registry.json`
  and the conservation baseline in `docs/development/ci.md`.
- Add a macOS runner job for the shell. Current builds are Linux with `CGO_ENABLED=0`.
- Playwright can't drive `WKWebView`, so the shell needs its own driven lane.
- Add a check that `desktop/` dependencies never reach the root `go.mod`.

## PR 5: release lane

Size: 1 to 2 weeks. The owner creates `Hikyo-Org/hikyo-app` and its signing identity.

- `.app` nesting, entitlements and notarization, which
  `scripts/release/sign-apple-artifacts.sh` doesn't cover.
- Tags and releases live only in that repository. No updater and no update check.
- `scripts/ci/check-oss-policy.sh` pins "Metadata-only ecosystem repositories are the sole
  exception." When the second exception becomes operative, change that sentence and the
  check together.

## PR 6: public docs

Revise the statements listed in the report's section 11, including `README.md`, the
installation and architecture pages, and the "not an application to drag into
Applications" sentence. Add "you don't need a server on your Mac", which is already true
with the CLI and a hosted instance.

## Acceptance

Run the ADR's [acceptance list](../../adr/desktop-app.md#acceptance-before-release) by hand
on a signed build. Tests don't replace it: the spike was never run by a person.

## Out of scope

Windows, Linux, an updater, passkey `mint` and `access` in the app, a new session class, a local
server in the app, and a client-only CLI.
