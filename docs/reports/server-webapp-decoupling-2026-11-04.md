# Decoupling the Hikyo server and web app

Investigation report, 2026-11-04, against commit `7a5fa5f7a`. Read-only: no product
code changed. Paths are repository-relative. Section 13 separates what was run
from what was only read. Follow-up: the [desktop-app ADR](../adr/desktop-app.md)
and [plan](../superpowers/plans/2026-11-04-desktop-app.md).

## 1. Summary

**A UI-only desktop app is feasible, and it probably needs little server work, as
long as the app does not use the existing cross-origin "workspace" tier.** The
hard part is which credential the app holds, not how the UI is packaged.

1. **Your premise is half right.** A Mac install contains the server code, but
   runs none of it. The DMG, the cask and `install.sh` all place one `hikyo`
   executable. There is no app bundle, no launchd job and nothing starts
   ([section 2](#2-what-exists-today)). The bigger gap is that **no desktop app
   exists at all**. The only way to get one UI across several instances today is
   to run a local `hikyo server` and use Remotes.
2. **Two of your three shapes already exist in practice.** "Server without app" is
   the Linux package, container and chart. T3 Code's own `t3 serve` also just means
   no window opens. "App only" is the real missing piece.
3. **The workspace tier looks like the obvious base, but it is the wrong one.** Its
   bearer lasts 15 minutes idle and 4 hours at most, may only live in memory by ADR
   decision, is refused by `whoami`, and works on 6 of 33 routes. Each server's
   admin must also allowlist the app's origin, and `tauri://`-style schemes are
   rejected outright.
4. **The CLI session is the better base.** `hikyo login` already performs a
   system-browser handoff that handles passkeys, OIDC and SAML on the instance's
   real origin, and it returns a session accepted by the `human-session` operations
   the UI uses. It needs no per-server consent.
5. **Keep the SPA inside the server, and do not ship a server without UI as your
   default.** With no document on the API origin, the sign-in, passkey, OIDC, SAML
   and CLI approval pages all return JSON 404, so no human can be onboarded.
6. **Keep the app out of the signed `hikyo` release manifest.** Shipped v1.0.0
   verifiers reject unknown artifact kinds, so adding one to that manifest risks
   breaking `hikyo upgrade` in the field. A separately tagged release avoids this.
7. **The largest UX cost is secret reveals.** By default every reveal takes its own
   ceremony that only a passkey can satisfy. Passkeys cannot run on an app origin,
   so each reveal from the app would hop out to the system browser, as the CLI does
   today.

**Recommendation:** option C1 below, a thin native shell with a CLI-class session
held natively, loading each instance's own SPA through a per-connection origin.
Run a short spike against its failure conditions before committing. This is a
recommendation, not a decision. [Section 12](#12-decisions-needed-from-you) lists
what is yours to decide.

## 2. What exists today

| Your shape | Hikyo today | T3 Code, for comparison |
|---|---|---|
| App + server | `hikyo server --dev` plus a browser. Evaluation only: macOS has no supported server host (`internal/selfupdate/selfupdate.go:172-176`) and dev mode is not for real secrets (`docs/site/src/content/docs/docs/configuration.mdx:26`) | Electron desktop app with a local server. Closing its last window stops that server |
| UI only, connecting out | **Does not exist.** Dedicated clients are recorded as "future work" (`docs/adr/remote-instance-pairing.md:92-94`) | Desktop, mobile and app.t3.codes connect to paired environments |
| Server only | Linux packages, container image, Helm chart. Each embeds the SPA | `t3 serve`: runs without opening a browser, and can run as a background service |

**What a Mac install does:**

| Path | Result | Evidence |
|---|---|---|
| DMG | `hikyo` and `LICENSE`, copied by hand | `docs/release/apple-signing.md:257-260` |
| Homebrew cask | `binary "hikyo"` only. Not live yet: the tap holds no cask | `scripts/release/render-homebrew-cask.sh:77-91` |
| `install.sh` | Verifies the bundle, then installs `hikyo` | `install/install.sh.in:59,161-213` |

The release topology is frozen as "two standalone signed CLI executables, signed
tarballs, and two HFS+ UDZO DMGs. No app bundle, package installer or universal
binary." (`docs/handoffs/unattended-apple-releases.md:16-17`). A repository scan
for launchd and app-bundle terms found nothing relevant.

**The server code you carry on a Mac:** v1.0.0 Darwin arm64 is 42.9 MB as tar.gz
and 48.8 MB as DMG. Measured sizes are in [section 9](#9-client-only-cli-and-server-without-ui).

**How T3 Code differs, and why its design does not transfer directly.** Its
[remote access guide](https://raw.githubusercontent.com/pingdotgg/t3code/main/docs/user/remote-access.md)
describes a one-time pairing link that "authorizes that device for future
connections", with no per-origin consent step described. Its desktop app registers
a privileged scheme with `corsEnabled` and `stream`
(`apps/desktop/src/electron/ElectronProtocol.ts:112-136` in a local T3 Code
checkout at `ce9e016406`). T3 Code's paired device is effectively the machine's
owner. Hikyo has many humans, fine-grained permissions, mandatory second factors,
per-operation step-up, a WebAuthn relying party and an audit model that all assume
a real human ceremony. So the shape transfers, but the pairing model should not.

## 3. Why this is not "point the SPA at another URL"

The SPA assumes it is served by the instance it talks to:

- `web/src/api/client.ts:100-101` sets `baseUrl: ''` and `credentials: 'same-origin'`.
- Sessions are `__Host-hikyo` cookies with a CSRF companion (`internal/server/browser.go:49-74`).
- CORS never sends credentials and does not allow the CSRF header (`internal/server/cors.go:27-30,45`).
- WebAuthn admits exactly one expected origin (`internal/webauthnrp/webauthnrp.go:126-133`).
- Roughly 231 of 360 SDK call sites use only the same-origin singleton. 129 are
  transport-aware. These counts come from a heuristic scanner, so treat them as
  approximate.

**The deciding constraint is the credential class:**

| | Browser cookie | Workspace bearer | CLI session |
|---|---|---|---|
| Lifetime, idle / absolute | 7 d / 30 d | 15 min / 4 h | 30 d / 90 d |
| Evidence | `internal/service/auth.go:36-42` | `internal/remotefetch/bounds.go:65-66` | `internal/service/auth.go:36-37` |
| Can persist | HttpOnly cookie | **No**: persistence rejected (`docs/adr/multi-instance.md:127`) | Yes: 0600 file (`internal/cli/state.go:137`) |
| Origin bound | By browser | Yes, and a missing `Origin` counts as mismatch (`internal/authz/session.go:448-460`) | No |
| `whoami` and account surface | Yes | **Refused** (`internal/authz/session.go:168-177`, `internal/service/auth.go:949-952`) | Yes (`session.go:177`) |
| Needs a server admin's consent | No | **Yes**, per instance (`internal/service/workspace.go:441-620`) | No |
| CSRF contract | Yes | No | No: decided by transport (`internal/server/browser.go:30-36`) |
| Passkey, OIDC, SAML login | On the instance page | Popup on the instance | System browser on the instance |

303 of 406 operations are `[human-session]` and another 56 also accept machine
credentials (`api/openapi.yaml`, counted by script). `workspace` is never named
directly. It is admitted to ordinary data, then narrowed.

**Origin grammar.** `CanonicalOrigin` accepts only `https://…` and loopback
`http://…` (`internal/service/workspace.go:626-648`). `tauri://`, `app://` and
`file://` are refused. A fixed `http://localhost:PORT` is allowlistable, but a
random port is not.

## 4. Options for the UI-only app

| | A. Browser, no code | B. Shell on real origin | C. Shell with native session | D. Shell on workspace bearer | E. Hosted static UI |
|---|---|---|---|---|---|
| What it is | Hosted instance in a browser, plus optional Add to Dock | Webview navigates to `https://instance` | Native layer holds a CLI-class session and owns transport | Same SPA, cross-origin bearer per server | `app.hikyo.…` serves one UI that connects anywhere |
| Multi-server switcher | No, unless using Remotes | Yes | Yes | Yes | Yes |
| Server changes | None | None | None for login; small SPA changes | Large: `whoami`, origin grammar, ADR | Same as D |
| Per-server admin consent | No | No | No | **Yes** | **Yes** |
| Passkeys | Yes | **No on WKWebView; partial on Electron** | Yes, in system browser | Popup path unverified | Yes |
| Remembers login | Yes | Yes | Yes | **No, by ADR** | **No, by ADR** |
| Features available | All | All | All, subject to spike | 6 of 33 routes | 6 of 33 routes |
| Effort | None to small | Small | Medium | Very large | Very large |
| Verdict | Do now | Fallback only | **Recommended** | Reject | Reject |

**A. Browser only.** Works today for the "connect to my hosted instance" need. You
keep `hikyo` for the CLI, and never run `hikyo server`. There is no web manifest
(`web/public` holds only `favicon.svg`), so install-as-app relies on the browser's
generic behaviour. Safari "Add to Dock" with Hikyo passkeys was not tested.

**B. Shell on the real origin.** Lowest code, and the whole SPA keeps working
unchanged. Passkeys are the blocker. Apple states WKWebView needs an
[Associated Domains](https://developer.apple.com/forums/thread/747334) entitlement
for the relying party, which cannot exist for arbitrary self-hosted domains.
Electron is [still partial](https://github.com/electron/electron/issues/24573):
user comments report Touch ID via `app.configureWebAuthn` on macOS, but incomplete
Linux support. Some identity providers also refuse embedded webviews (general
knowledge, not tested). Since `HIKYO_SECOND_FACTOR` defaults to `required`, this
option locks out passkey-only users unless ceremonies move elsewhere.

**C. Native session (recommended).** Two variants:

- **C1: per-connection custom origin, with native proxy (recommended for the spike).**
  The shell serves `hikyo-app://<connection-id>/`. Its protocol handler forwards the
  document, assets and `/api/*` to that instance, adding the bearer. The SPA stays
  "same-origin" to its own custom origin, so the 231 singleton sites do not change.
  Each connection gets its own storage. The token stays out of JavaScript only if the
  handler also removes rotated tokens from responses, which the
  [spike](./wails-shell-spike-2026-11-04.md) found and does. TLS can
  be SPKI-pinned like the CLI, which the
  [multi-instance ADR notes a browser cannot do](../adr/multi-instance.md).
- **C2: real-origin webview with a brokered cookie.** The handoff mints a *browser*
  artifact and the shell places it in the webview's cookie jar. `location.origin`,
  CSRF and session epoch remain correct, with shorter clocks and no proxy, and
  Electron may allow inline Touch ID. Costs: a new server path minting browser
  sessions, which is security-sensitive, and no SPKI pinning unless the framework
  provides it.

**D. Workspace bearer.** This is what the ADRs describe, but it needs a login every
4 hours and an admin allowlist entry on every instance, and it reaches 6 of 33
routes. Fixing `AuthProvider`'s dependency on `whoami` would require weakening a
deliberate refusal.

**E. Hosted static UI.** The allowlist's meaning is "trust the code served at that
origin" (`docs/adr/multi-instance.md`). Asking self-hosters to trust a vendor-hosted
UI with secret values cuts against the product's stance. It also inherits all of D.

## 5. How the recommended design would work

```
 Hikyo.app (thin shell, no secrets on disk except via OS keychain)
 +---------------------------------------------------------------+
 | Connection list: origin, name, trust mode, SPKI pin           |
 | Keychain: one session per connection                          |
 |                                                               |
 | Renderer per connection, isolated, no Node access             |
 |   hikyo-app://conn-a/   hikyo-app://conn-b/                   |
 |            |                     |                            |
 |   protocol handler: adds bearer, verifies pin, strips cookies |
 +------------|---------------------|----------------------------+
              | HTTPS               | HTTPS
              v                     v
       instance A (SPA + API)   instance B (SPA + API)

 Sign-in and passkey step-up: system browser -> instance's own origin
   /login/cli, /reauth/cli -> loopback callback -> shell redeems with PKCE
```

**Flows:**

1. **Add connection.** The user enters a URL. The shell reads `/api/v1/meta`,
   confirms it speaks Hikyo, and applies the CLI's trust choice (system CA or SPKI
   pin, `internal/cli/trust.go:53-79`).
2. **Sign in.** Reuse the existing loopback or device handoff
   (`internal/service/cli_login.go:83-85`). Every login method runs in the system
   browser on the real origin.
3. **Use.** The renderer loads the instance's own SPA, so the UI and the server
   version always match. This keeps "the SPA ships in the same artifact" true
   (`docs/adr/api-cli-surface.md:165`) and removes the skew problem.
4. **Sign out or revoke.** The instance's session list and revocation keep working.

**SPA changes this needs** (roughly a dozen sites, to confirm in the spike):

| Change | Where |
|---|---|
| App-mode detection, and handing unauthenticated state to the shell | `AuthProvider`, `web/src/routes/Login.tsx` |
| Route passkey and OIDC step-up through the system browser | `web/src/api/values.ts:243,350-396`, `web/src/api/stepup.ts:185`, `web/src/api/account.ts:350`, `web/src/api/selfConfig.ts:96` |
| Use the instance's origin rather than `location.origin` in links | `web/src/routes/Members.tsx:603,738,754`, `web/src/routes/OpenRegistration.tsx:250` |
| Self-connection check, and hiding workspace launch | `web/src/routes/Remotes.tsx:218,561` |
| Session epoch, which relies on a readable cookie and goes inert | `web/src/api/sessionEpoch.ts:38-43,90-94` |
| External links and `target=_blank` toasts open in the system browser | `web/src/app/notifications.tsx:99` |

69 source files are hash-pinned for security review (`web/src/api/sensitiveInventory.json`),
so edits to these need re-review.

**Framework.** C1 needs no webview WebAuthn, so the WKWebView limit no longer
decides this.

| | Electron | Tauri | Wails v3 |
|---|---|---|---|
| Precedent | T3 Code, which you already run | None here | None here |
| Custom scheme with streaming | Privileges exist, and T3 Code sets `stream` | Not researched | **Confirmed by the spike** |
| Security update burden | **Ships Chromium**, so its CVEs oblige releases | System webview | System webview |
| Reuses Go `internal/cli` trust code | No | No | Only as a nested module in this repo, and after moving that code to small packages |
| Downloads, popups, navigation policy | Yes, as API | Not researched | None built in on macOS in v3.1.0. The spike added them from its own native code |
| Status | Stable | Stable (not researched further) | Stable: v3.1.0, released 2026-11-01. The earlier "beta" finding is out of date |

You chose to spike Wails. [The results](./wails-shell-spike-2026-11-04.md) confirm C1's
core, and the Wails gap closed with about 120 lines of our own native code.

## 6. Settings impact

Full tables are in [Appendix A](#appendix-a-server-and-operator-settings) and
[Appendix B](#appendix-b-ui-settings-and-browser-storage).

**Server settings: 173 inventoried. Almost none change.**

| Class | Count | Meaning |
|---|---|---|
| SERVER-ONLY | 143 | Unaffected |
| CLIENT-RELEVANT | 18 | CLI or client already reads these, or an app needs an equivalent |
| ORIGIN-COUPLED | 10 | Assumes one shared origin for the UI and API |
| UI-SERVING | 2 | Both are build-time: the `ui` tag and Vite `demo` mode |

Chart and API rows are groups rather than leaf values. Classifying
`HIKYO_UPDATE_CHANNEL`, `HIKYO_CLI_SOCKET` and the workspace allowlist as
CLIENT-RELEVANT is a judgement call. The self-configuration catalogue's own counts
are stale: it says 65 inputs and 27 keys where code has 79 and 32
(`internal/config/variables.go:78-158`, `internal/runtimeconfig/catalogue.go:33-85`).

**There is no runtime no-UI setting, only the build tag.** There are also no
operator settings for CSP, cookies or session lifetimes. Those are constants.

**Where decoupling changes meaning:**

| Setting | Under option C1 | If UI and API origins are truly split |
|---|---|---|
| `HIKYO_EXTERNAL_ORIGIN` | **Unchanged.** The SPA is still served from it | Splits in two. See below |
| `/instance/workspace-origins` | Not used by the app. Remains for instance-to-instance workspaces | Must accept the app origin on every server |
| OIDC, OAuth2 and SAML providers | Unchanged. They run on the real origin | Redirects are relative (`internal/server/oidc.go:224`, `internal/server/saml.go:143`) and land on the API origin |
| WebAuthn relying party | Unchanged. Ceremonies stay on the real origin | Fails the expected-origin check |
| `HIKYO_REAUTH_WINDOW_SECONDS` | **More important.** Default `0` means every reveal needs a passkey ceremony | Same |
| Project reveal reauth window | The practical mitigation. Protected environments stay at `0` | Same |
| Instance invitations and open registration | SPA fix needed: `window.location.origin` would name the app | Same |
| `HIKYO_UPDATE_CHANNEL` | Server release track only. App needs its own | Same |
| `HIKYO_MCP_ALLOWED_ORIGINS` | Unchanged. Governs MCP clients, not the SPA | Same |
| Maintenance window | Works through the proxy | Cross-origin UI cannot read the 503s (`internal/server/runtime_status.go:25-37`) |

`HIKYO_EXTERNAL_ORIGIN` today means both things. In a real split:

- **Stays with the API origin:** OIDC and OAuth2 redirect URIs, SAML ACS and entity
  ID, pairing invitations, CLI login `Instance`, MCP canonical origin, HSTS.
- **Must follow the UI:** WebAuthn RP, signup mail link `/signup/verify`, device
  login `/login/cli?device=1`, self-config repair.

This table is the strongest argument for C1: it avoids that split completely.

**UI settings.** Almost all are server-persisted and stay per-instance: account,
org, project and instance settings simply appear per connection. Three browser keys
would need per-connection scoping (`hikyo.last-sign-in`,
`hikyo:update-dismissed:…`, `hikyo.lastProject`). Under C1 that happens
automatically, because each connection has its own origin and storage. `hikyo.theme`
should become app-wide.

**New settings a UI-only app needs, which nothing holds today:**

1. The connection list: origin, display name, trust mode, pin.
2. Default or last-active connection.
3. Session custody per connection (OS keychain).
4. The app's own update channel.
5. Global theme.
6. External link handling, including which browser runs ceremonies.

**The Remotes page changes role.** In the app, the connection list replaces the
"viewing instance" (`web/src/routes/Remotes.tsx`). Directory entries and pairing
stay available on each server, for server-to-server use, but the app does not need
pairing for v1: it connects as a human. If it ever does pair, the pairing ADR
already specifies that a native client must make its own key and must not reuse a
server's sealed state (`docs/adr/remote-instance-pairing.md:106-114`). Fleet update
notices need a new source, because they read the home server's remotes today.

## 7. Feature impact

| Area | Under C1 | Note |
|---|---|---|
| Matrix, values, history, transit, machine access, audit, settings | Full, subject to spike | Workspace tier only reaches 6 of 33 routes |
| Instance admin, org settings, SCIM | Full | Authorization is unchanged |
| Add a passkey or TOTP | **Needs care** | The CLI session passes `Authenticate`, but `navigator.credentials` fails off-origin. Send enrolment to the instance page in the system browser |
| Reveal with passkey | System-browser hop per reveal | Largest UX cost |
| Reveal with TOTP | Inline, when the reveal window is above 0 | Configuration dependent |
| Downloads: KRL, bundle, audit export | Should work via proxy | Webview download handling unverified |
| Live advisory stream (fetch-based SSE) | Should work with stream support | Key spike test |
| QR import in pairing | Hidden in app | Camera and secure-context behaviour unverified |
| Clipboard auto-clear | Needs a webview check | `web/src/app/clipboard.ts:21,55-57` |
| Audit trail | Shows CLI-class sessions | See section 8 |

## 8. Security impact

| Topic | Impact | Suggested handling |
|---|---|---|
| Durable credential on a laptop | 30 d idle / 90 d absolute is longer than a browser session | Keychain custody. Consider a new `app` artifact with browser clocks |
| Current CLI custody | Plain 0600 file (`internal/cli/state.go:137`) | App should not reuse it. Do not share sessions with the CLI |
| XSS in the renderer | Can use the session while open, but cannot extract it. Same class as HttpOnly cookies. Addresses the reason persistence was rejected, since one connection's XSS cannot reach another | Handler keyed strictly by connection host. Needs an ADR amendment to say so |
| Remote code in a native shell | Instance compromise reaches the renderer, the same as a browser | No Node access, context isolation, minimal preload, no devtools in release |
| Local proxy as a confused deputy | A loopback HTTP proxy would let any local process use the session | **Use an in-process protocol handler, never a local port** |
| App auto-update | A new root of trust outside `release/trust` | Must meet the same signed, verified standard. Decide before shipping |
| Crash dumps | Could capture revealed values | Disable or scrub crash reporting |
| Audit | Sessions appear as CLI. An operator cannot distinguish them | Additive server label or the new artifact |
| Threat model | New client class with local storage | Amend `docs/adr/threat-model.md` |

Reusing the CLI artifact is fine for the spike. Before shipping, I would add a
distinct `app` artifact, because shared labels and clocks mislead operators. That
is an additive server change, gated by `x-hikyo-min-revision`.

**Later reversal.** Review of the proposed ADR found that a new artifact is not additive:
the session list's `artifact` is a closed enum, and migrations and an audit enum would have
to change. Any label would also be asserted by the client. The
[ADR](../adr/desktop-app.md) therefore keeps the `cli` artifact.

## 9. Client-only CLI and server without UI

These are separate from the app. Neither is needed for it.

**Client-only CLI.** `internal/cli` depends on no store, server, app, service,
authz, webui or config package, and on no pgx, goose or sqlite. Dependency graph:
2045 packages for `./cmd/hikyo`, 1537 for `./internal/cli`, about 799 without
`internal/importer`. No build tag splits verbs, and `cmd/hikyo/main.go:23` imports
`internal/app` unconditionally, so this needs a new `main` or a new tag.

| Build (darwin arm64, `-trimpath -ldflags=-w`, this commit unless noted) | Raw | gzip |
|---|---|---|
| Client only: `internal/cli`, built with a throwaway `main` | 88.8 MB | 24.7 MB |
| Full binary, no `ui` tag | 146.5 MB | 40.8 MB |
| Published v1.0.0 tar.gz, with UI (different commit) | | 42.9 MB |

So a client-only build would save about 40%: 58 MB raw, 16 MB compressed. The SPA
itself is roughly 2 MB compressed (a rough figure across two commits), so **removing
the UI from the server saves almost nothing**. About 89 MB would remain, largely
because `internal/importer` keeps client-go, sops, vault and openbao. Removing that
was not measured.

`docs/adr/system-architecture.md:40-46` considered exactly this and rejected it on
purpose: "a workstation CLI download contains server code it never runs" is listed
as an accepted cost, and the shared release train limits the security gain.

**Server without UI.** **A no-UI build returns JSON 404 for every page. This was run, not inferred:**
`hikyo server --dev` built without the tag answered `404 application/json` with
`{"error":{"code":"not_found",...}}` for `/`, `/login`, `/login/cli?device=1`,
`/auth/oidc/done`, `/auth/saml/done`, `/signup/verify` and `/workspace/approve`, even
with `Accept: text/html`. It still advertised `loopback-handoff` and `device-code` in
`/api/v1/meta`.

- In a no-UI build, these server-emitted paths have no page: `/signup/verify`,
  `/login/cli?device=1`, `/auth/oidc/done`, `/auth/saml/done`, `/workspace/approve`.
- `/api/v1/meta` still advertises `loopback-handoff` and `device-code`
  (`internal/server/api.go:318`), which that build cannot complete.
- The release pipeline assumes the UI: `scripts/release/smoke-image-ui.sh:67-92`.
- **Trap:** archive names carry no flavour (`internal/selfupdate/selfupdate.go:131-165`),
  so a headless install would upgrade itself into the UI archive.

It is usable only where no human ever signs in, which contradicts Hikyo's
onboarding. If you want this, the honest version is a minimal "ceremony-only"
document, not an API-only binary. I do not see a case for doing it now.

**App + server on a Mac.** T3 Code does this by spawning a sidecar. For Hikyo that
means either a dev-mode playground (cheap, but not for real secrets), or making
macOS a supported server host with production boot, root key custody, backups and
upgrades (expensive). Suggest the playground only, or skipping it.

## 10. Packaging and release impact

**Hazard, read before touching the manifest.** Field binaries fail on unknown kinds:
`internal/releasetrust/release.go:240` returns `unknown artifact kind`. Nightly
readers also reject non-canonical DMG names and a second binary per platform
(`internal/releasetrust/nightly.go:136-155`). Adding an app to the `hikyo` manifest
would therefore need a bridge release teaching field verifiers first.

**Recommended route: a separate release lane.**

| Choice | Result |
|---|---|
| Same repo, tag like `app-v1.0.0` | Field update checks skip it, since it is not strict semver (`internal/updatecheck/updatecheck.go:87-90`). It should be published with "latest" off, because readers also query `releases/latest` (`internal/updatecheck/source.go:25,147`). **Every field binary still parses its assets:** one asset with an unsafe or duplicate name, an unexpected URL, zero size or a malformed digest makes the whole listing fail, which breaks update checks for all users (`internal/updatecheck/source.go:248-274`). `v*` rulesets do not apply, so add one |
| Separate `hikyo-app` repo | No interaction with the field at all. Shell only, so the rejected "SPA in a separate repository" (`docs/adr/oss-mechanics.md:84`) does not apply under C1. Signing and CI are set up again |
| Source here as a nested module, releases in a separate repo (recommended after the spike) | No interaction with field update checks. Allows `internal/` imports and same-PR SPA changes. CI handling of a second `go.mod` not checked |
| Inside the `hikyo` manifest | 27 scripts, verifiers and workflows, 23 tests and 7 docs change ([Appendix C](#appendix-c-files-changed-if-the-app-joins-the-hikyo-manifest)), plus the bridge. `install.sh` would also download app payloads on every CLI install (`install/install.sh.in:135-150`) |

**CI.** Webview builds likely need a native runner per OS (inference; nothing today
builds this way). Current builds are Linux-hosted with `CGO_ENABLED=0`
(`docs/handoffs/unattended-apple-releases.md:11-12`). A new `.app` needs nesting,
entitlements and its own notarization, which `scripts/release/sign-apple-artifacts.sh`
does not cover. New jobs join `scripts/ci/ci-job-registry.json` and the conservation
baseline (`docs/development/ci.md:127`). Playwright runs against the binary
(`.github/workflows/ci.yml:950,966`), so the shell needs its own lane.

**Version skew.**

| Design | Skew | What's needed |
|---|---|---|
| C1 loading SPA from instance | **None** for the UI. The shell is a separate, slow-moving surface | Small shell-to-server contract, a capability in `/api/v1/meta`, and a browser fallback for older servers |
| Bundled SPA | Full matrix | The API is additive-only (`docs/adr/api-cli-surface.md:144`) and old clients are protected, but new-client-on-old-server must degrade through per-operation revisions. **The SPA has no such gate.** It exists only in the CLI (`internal/cli/client.go:415-458`), despite 408 annotated entries in the contract |

## 11. ADRs and promises to amend

**These ADRs need formal amendment:**

| ADR | Statement affected | Under C1 |
|---|---|---|
| `docs/adr/system-architecture.md` (#22) | "One artifact, one version string, one install story." (`:38`) | Amend: `hikyo` stays one artifact, and a shell is added beside it |
| `docs/adr/api-cli-surface.md` (#25) | "the SPA is just another versioned client shipping in the same artifact" (`:165`) | **Still true.** Amend for the app as a client and for session eligibility |
| `docs/adr/human-auth.md` (#16) | Session classes | Only if adding an `app` artifact |
| `docs/adr/threat-model.md` (#8) | "Go single binary" (`:34`), enumerated clients | Amend |
| `docs/adr/oss-mechanics.md` (#33) | "one tag = one ceremony" (`:45`) | Amend for a second lane |
| `docs/adr/mvp-boundary.md` (#26) | "Single multicall binary, embedded SPA" (`:114`). This is an executable test input (`api/acceptance_ledger_test.go`) | Likely unchanged if treated as a post-MVP addition. Confirm |
| `docs/adr/multi-instance.md` (#71) | Workspace tier | **Unchanged under C1**, which is a further point in its favour |

**Amendment procedure conflict.** A locked ADR is amended only by "running the same
adversarial cross-model review that locked it" (`docs/adr/oss-mechanics.md:164`).
Cross-model review is not currently available to this project, while the
repository's governance text still requires it.
Owner waivers have precedent (`docs/adr/api-cli-surface.md:11`). This needs your
call.

**Public statements to revise:** `README.md:94,108,161`,
`docs/site/src/pages/index.astro:49,434,443`,
`docs/site/src/content/docs/docs/installation.mdx:40,178,365`,
`docs/site/src/content/docs/docs/architecture.mdx:7,30`,
`docs/release/apple-signing.md:257-258`. Most remain true for the server under C1.
The "not an application to drag into Applications" sentence would need rewording.

## 12. Decisions needed from you

1. **Which option?** Recommended: **A now, spike C1**, and reject D and E.
2. **Does "server only" have to mean no UI assets?** Recommended: **no**. It means
   no desktop app, which already exists. An API-only server blocks onboarding.
3. **Is app + server required for v1?** Recommended: **no**, or a clearly labelled
   dev playground. Real secrets on a Mac server is a separate, large project.
4. **Same repo or separate repo for the shell?** Updated recommendation after the
   spike: **source in this repo as a nested module, releases in a separate repo.** The
   nested module can import `internal/` (tested) and keeps Wails and cgo out of the root
   `go.mod`. SPA app mode and the shell can change in one PR, which matters because the
   SPA must change for passkeys and downloads. App releases must not be published here:
   field binaries list every release in this repo and read `releases/latest`
   (`internal/updatecheck/source.go:24-25,194`), and one bad asset fails the listing
   (`:248-274`). How CI lanes treat a second `go.mod` was not checked.
5. **How should ADR amendments be reviewed**, given the governance conflict?
6. **Is a browser hop per passkey reveal acceptable?** Decided: yes. Like the CLI, the
   app opens the system browser to validate the passkey.
7. **Do you still want the client-only CLI?** Not needed for the app. The ADR
   rejected it deliberately. See the measured saving in section 9.

**Suggested phases, if you accept C1** (sizes are rough and low-confidence):

| Phase | Work | Size |
|---|---|---|
| 0 | Document "you don't need a server on your Mac": CLI + hosted instance | Hours |
| 1 | Spike C1 against the tests below | Days |
| 2 | ADR amendments, SPA app mode, meta capability | 1-2 weeks |
| 3 | Shell: connections, keychain, handoff, pinning | 2-3 weeks |
| 4 | Signing, notarization, update lane, CI lane, docs | 2-3 weeks |
| 5 | Optional `app` artifact, Windows and Linux | Later |

**Spike tests. Any failure sends you to C2 or B.** Run on 2026-11-04, results in the
[spike report](./wails-shell-spike-2026-11-04.md): tests 1, 2, 5, 7 and 8 passed, 3 passed
for reveal with a virtual authenticator, 4 was not run, and in 6 downloads pass but
clipboard auto-clear fails:

1. Under the custom origin with a CLI-class session, the SPA loads and every route
   renders against a real instance, including `AuthProvider` via `whoami`.
2. The advisory SSE stream arrives live through the protocol handler.
3. Passkey login and a passkey reveal complete through the system browser and
   return to the same view without losing state.
4. OIDC and SAML sign-in complete, including a provider that refuses webviews.
5. No operation the SPA uses rejects a CLI-class session or its missing cookie.
   This is the main unverified assumption: source shows `Authenticate` admits it,
   but no one has run the SPA this way.
6. KRL, bundle and audit downloads work. Clipboard auto-clear works.
7. A renderer for connection A cannot reach connection B's handler.
8. Server CSP, COOP and CORP headers do not break under the custom scheme.

## 13. Evidence and limits

**Run during this investigation:**

- Built `./cmd/hikyo` with no `ui` tag, and a client-only `internal/cli` binary through
  a build overlay that left the repository untouched. Sizes are in section 9.
- Ran the no-UI binary as `server --dev` on loopback and requested seven page paths:
  all returned JSON 404.
- Sent `Origin: tauri://localhost` and `Origin: http://localhost:5173` to
  `/api/v1/meta`: neither response carried any `Access-Control-*` header, confirming
  an unlisted origin gets no CORS grant. A preflight returned 204 with no grant.
- Ran the client-only binary: `server` gives `unknown command`, and `whoami` reaches
  context resolution, so the CLI does link without server packages. It was not tested
  against a live instance, and its update wiring was omitted.
- Counted `x-hikyo-artifacts` across `api/openapi.yaml` by script.
- Checked report citations by script: cited files exist and line numbers are in range. It could not resolve 8 short or external references: workflow file names, two shorthand Go files, `install.sh`, `/etc/hikyo/upgrade.json` and the T3 Code path.

**Read but not run:** all CORS, session, WebAuthn and route behaviour, except as
listed above. I directly re-read the critical sources:
`internal/service/workspace.go`, `internal/authz/session.go`,
`internal/server/cors.go`, `internal/webauthnrp/webauthnrp.go`,
`internal/releasetrust/release.go` and `internal/updatecheck/`. The three inventories
were produced by delegated research agents on the session's model (Opus 5.5).
Spot-checks matched, and a script confirmed cited files and line numbers exist, but
not every claim was individually re-read.

**Not verified:**

- No shell, webview or Electron prototype was built. **Option C1 is a source-based
  assessment and not a demonstrated result.**
- Webview `window.open`, `BroadcastChannel` partitioning, `isSecureContext`,
  clipboard and downloads.
- Whether a real instance streams `/events` to a workspace bearer. The ADR and the
  banner say polling, while source suggests SSE.
- Safari "Add to Dock" with Hikyo passkeys.
- Electron passkey status comes from user comments on an open issue, not from docs.
  WKWebView limits are from Apple forum staff replies. Applying them to Tauri is
  inference.
- Effort sizes are estimates.

**External sources:**
[T3 Code remote access](https://raw.githubusercontent.com/pingdotgg/t3code/main/docs/user/remote-access.md),
[T3 Code repository](https://github.com/pingdotgg/t3code),
[Electron #24573](https://github.com/electron/electron/issues/24573),
[Electron protocol API](https://www.electronjs.org/docs/latest/api/protocol),
[Apple forum on WKWebView WebAuthn](https://developer.apple.com/forums/thread/747334),
[passkeys.dev macOS reference](https://passkeys.dev/docs/reference/macos/),
[Wails v3 beta 32](https://github.com/wailsapp/wails/releases/tag/v3.0.0-beta.32).

## Appendix A: Server and operator settings

Shorthand: `V:` is `internal/config/variables.go`, `C:` is `internal/config/config.go`.
Forms: E env, F flag, MO managed owner key, MN managed node overlay, MB managed
bootstrap document.

| Set | SERVER-ONLY | ORIGIN-COUPLED | UI-SERVING | CLIENT-RELEVANT | Total |
|---|---|---|---|---|---|
| Recognized env inventory (`V:78-158`) | 65 | 2 | 0 | 12 | 79 |
| Managed-only canonical names | 9 | 0 | 0 | 0 | 9 |
| `HIKYO_OPERATOR_*` | 7 | 0 | 0 | 0 | 7 |
| Retired updater env | 26 | 0 | 0 | 0 | 26 |
| Compose substitution variables | 5 | 0 | 0 | 0 | 5 |
| Installer | 0 | 0 | 0 | 1 | 1 |
| Flags with no env alias | 4 | 0 | 0 | 0 | 4 |
| Other command flags and config files | 4 | 0 | 0 | 0 | 4 |
| Chart value groups | 16 | 2 | 0 | 1 | 19 |
| API or database setting groups | 7 | 6 | 0 | 4 | 17 |
| Build-time, not runtime | 0 | 0 | 2 | 0 | 2 |
| **Total** | **143** | **10** | **2** | **18** | **173** |

### Origin-coupled and client-relevant server keys

| Name | Form | Default | Evidence | Class |
|---|---|---|---|---|
| `HIKYO_EXTERNAL_ORIGIN` | E, MO, chart | Derived from listen address and TLS | `V:105`, `C:662-694` | ORIGIN-COUPLED |
| `HIKYO_PAIRING_SPKI_PIN` | E, MO | None | `V:127`, `C:630`, `internal/app/pairing_identity.go:20` | ORIGIN-COUPLED |
| `HIKYO_UPDATE_CHANNEL` | E, managed, chart | `stable` | `V:146`, `C:491-493` | CLIENT-RELEVANT |
| `HIKYO_CLI_SOCKET` | F, E | None | `V:94`, `C:411`, `C:480-485` | CLIENT-RELEVANT |

### Server-only keys

| Names | Form | Default | Evidence |
|---|---|---|---|
| `HIKYO_LISTEN` | F, E, MN | `127.0.0.1:8080` | `V:108`, `C:409`, `C:541-546` |
| `HIKYO_OPERATIONAL_LISTEN` | F, E, MN | `127.0.0.1:8081` | `V:125`, `C:410`, `C:593-598` |
| `HIKYO_TLS_CERT_FILE`, `HIKYO_TLS_KEY_FILE` | F, E, MN | None | `V:140-141`, `C:412-413`, `C:599-607` |
| `HIKYO_TRUSTED_PROXY_CIDRS` | E, MN, chart | None | `V:143`, `C:611-618` |
| `HIKYO_DB` | E, chart | Required; `--dev` uses `hikyo-dev.db` | `V:97`, `C:737-749` |
| `HIKYO_PG_POOL_MAX` | E, MN | DSN default | `V:128`, `C:750-759` |
| `HIKYO_PG_STORAGE_KUBELET_URL`, `_NAMESPACE`, `_PVC`, `_NODE` | E, chart | Off | `V:129-132`, `C:774-797` |
| `HIKYO_ROOT_KEY` | E | None | `V:135`, `C:459` |
| `HIKYO_ROOT_KEY_FILE` | F, E | None; `--dev` generates | `V:136`, `C:397`, `C:415` |
| `HIKYO_NEW_ROOT_KEY_FILE` | E | None | `V:122`, `C:460` |
| `HIKYO_ARGON2_MEMORY_KIB`, `_TIME`, `_PARALLELISM` | E, MO | 64 MiB, 3, 2 | `V:82-84`, `C:549-562` |
| `HIKYO_REAUTH_WINDOW_SECONDS` | E, MO | 0; `--dev` 900 | `V:134`, `C:563-574` |
| `HIKYO_SECOND_FACTOR` | E, MO | `required` | `V:138`, `C:505-514` |
| `HIKYO_ADMISSION_BUDGET_MIB` | E, MN | 272 | `V:81`, `C:575` |
| `HIKYO_MAX_KEYS_PER_PROJECT`, `HIKYO_DEFAULT_KEYS_PER_PROJECT` | E, MO | 10000, 1000 | `V:118`, `V:98`, `C:515-530` |
| `HIKYO_MCP_ENABLED` | E, MO, chart | Off | `V:120`, `C:696-722` |
| `HIKYO_MCP_ALLOWED_ORIGINS` | E, MO, chart | Empty | `V:119`, `C:704`, `C:860-882` |
| `HIKYO_MCP_WRITE_ENABLED` | E, MO, chart | Off | `V:121`, `C:708-718` |
| `HIKYO_AUDIT_ACCESS_RETAIN_DAYS`, `HIKYO_AUDIT_SECURITY_RETAIN_DAYS` | E, MO | 90, 365 | `V:85-86`, `C:1378-1382` |
| `HIKYO_BACKUP_DIR` | E, MN | None | `V:87`, `C:1074` |
| `HIKYO_BACKUP_RECIPIENTS` | E, MO | None | `V:89`, `C:1069` |
| `HIKYO_BACKUP_INTERVAL`, `_RPO`, `_RETAIN_COUNT`, `_RETAIN_DAYS`, `_RTO_TARGET` | E, MO | 24h, 26h, 7, 180, 30m | `V:88`, `V:90-93`, `C:1085-1095` |
| `HIKYO_MAIL_ADDR`, `_ALLOWED_CIDRS`, `_CA_FILE`, `_EHLO`, `_FROM`, `_PASSWORD`, `_PASSWORD_FILE`, `_TLS`, `_USER` | E, managed | Off when absent | `V:109-117`, `internal/runtimeconfig/catalogue.go:34` |
| `HIKYO_ADAPTER_EGRESS_POLICY_FILE`, `HIKYO_OIDC_EGRESS_POLICY_FILE`, `HIKYO_DYNAMIC_EGRESS_POLICY_FILE` | E, MN | None | `V:80`, `V:124`, `V:103` |
| `HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY` | E, MN | `deny` | `V:79`, `C:642` |
| `HIKYO_SEALED_WEBHOOK_FILE` | E | None | `V:137`, `C:646` |
| `HIKYO_DIRECTORY_PROXY` | E, MO | Direct; https only | `V:102`, `C:619-629` |
| `HIKYO_HA`, `HIKYO_NODE_ID` | E, MB, chart | Off, none | `V:106`, `V:123`, `C:888-915` |
| `HIKYO_UPGRADE_BACKUP`, `_BUNDLE`, `_EVIDENCE`, `_LEGACY_WRITERS_STOPPED`, `_OPERATOR_PUBLIC_KEY`, `_SCRATCH_POSTGRES_DSN`, `_STATE_DIR`, `_TARGET_MANIFEST`, `_UNATTENDED` | E, MB, chart, F | Off | `V:147-150`, `V:152-156`, `C:400`, `C:408`, `C:438-448` |
| `HIKYO_DEV_ADAPTER_FAKE_PROVIDER`, `HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE`, `HIKYO_DEV_SERVICE_BUDGETS_DISABLED` | E, MN | Refused outside `--dev` | `V:99-101`, `internal/config/managed_dev.go:26` |
| `HIKYO_UPGRADE_OPERATOR_INSTANCE` | E | None | `V:151`, `C:446` |
| `HIKYO_UPDATER_SOCKET` | E, retired | Refused at boot | `V:145`, `C:486-489` |
| `HIKYO_MAIL_CA_PEM`, `HIKYO_TLS_CERT_PEM`, `HIKYO_TLS_KEY_PEM`, `HIKYO_ADAPTER_EGRESS_POLICY_JSON`, `HIKYO_OIDC_EGRESS_POLICY_JSON`, `HIKYO_DYNAMIC_EGRESS_POLICY_JSON`, `HIKYO_NEW_ROOT_SOURCE`, `HIKYO_NODE_OVERRIDES`, `HIKYO_BOOTSTRAP_SOURCES` | Managed only | Absent | `internal/runtimeconfig/catalogue.go:41,84-85`, `internal/config/managed_node.go:21-29` |
| `--dev`, `--auto-migrate`, `--config-rollout-enrollment`, `--config-rollout-signing-key` | F | Off, true, none, none | `C:394`, `C:414`, `C:416-417` |
| `HIKYO_OPERATOR_NAMESPACES`, `_TRIGGER_ROLLOUTS`, `_NATIVE_SECRET_TYPES`, `_STATUS_REPORTING`, `_NAMESPACE`, `_METRICS_ADDR`, `_HEALTH_ADDR` | E, chart | See source | `internal/operator/config.go:58-115` |
| `HIKYO_SERVER_IMAGE`, `HIKYO_UPGRADE_PUBLIC_DIR`, `HIKYO_UPGRADE_INSTALLATION_DIR`, `HIKYO_SERVER_DATA_DIR`, `HIKYO_SERVER_ROOT_KEY_FILE` | Compose | Required | `install/compose/server.yaml:5-50` |
| `hikyo config-rollout --enrollment-file`, `--authority-public-key` | F | `/run/hikyo/rollout/...` | `cmd/hikyo/config_rollout.go:25-26` |
| `/etc/hikyo/upgrade.json`, `hikyo updater --config` | Config file | See source | `internal/hostupgrade/config.go:21`, `cmd/hikyo/main.go:641` |
| 26 retired updater variables (`HIKYO_UPDATE_LIB`, `_BIN`, `_TRUST_ROOT`, `_FLUX_*` and others) | Retired | Not read | `install/updater/updater.env.example:1-38` |

All of these are pointless on a client-only install.

### Client keys already separated in code

| Names | Evidence |
|---|---|
| `HIKYO_CONTEXT`, `HIKYO_INSTANCE`, `HIKYO_ORG`, `HIKYO_PROJECT`, `HIKYO_ENV` | `V:96`, `V:107`, `V:126`, `V:133`, `V:104`, `internal/cli/resolve.go:190-212` |
| `HIKYO_TOKEN`, `HIKYO_TRUST_BUNDLE`, `HIKYO_STATE_DIR`, `XDG_STATE_HOME`, `HIKYO_COMPOSE_DOCKER` | `V:142`, `V:144`, `V:139`, `V:157`, `V:95` |
| `HIKYO_INSTALL_DIR` | `install/install.sh.in:59` |

`V:9-10` and `C:353-367` already mark a client audience. The SPA reads no environment
at runtime. `HIKYO_TOKEN` and `HIKYO_CONTEXT` could seed app connections, but that
is not needed for v1.

### Chart values

| Value (`chart/hikyo/values.yaml`) | Line | Class |
|---|---|---|
| `externalOrigin` | 105 | ORIGIN-COUPLED |
| `network.allowPlaintextOrigin` | 140 | ORIGIN-COUPLED |
| `updates.channel` | 209 | CLIENT-RELEVANT |
| `replicaCount`, `ha.*`, `terminationGracePeriodSeconds`, `image.*`, `database.*`, `rootKey.*`, `upgrade.*`, `mcp.*`, `tls.*`, `service.port`, `network.trustedProxyCIDRs`, `networkPolicy.*`, `releaseTrust.*`, `resources`, `operator.*`, `rollout.*` | 1-246 | SERVER-ONLY |

The chart has no UI toggle and no Ingress template.

### Instance settings over the API

Lines are in `api/openapi.yaml`.

| Surface | Line | Class |
|---|---|---|
| `/instance/workspace-origins` | 10230 | CLIENT-RELEVANT |
| `/instance/remotes`, `/remotes/pair`, `/connections`, `/directory` | 9883, 9717, 10084, 9835 | CLIENT-RELEVANT |
| `/instance/update-status`, `/update`, `/updates/{job}` | 6284, 6314, 6361 | CLIENT-RELEVANT |
| `/api/v1/meta` | 158 | CLIENT-RELEVANT |
| `/instance/pairing-invites`, `/pairing-requests` | 9609, 9569 | ORIGIN-COUPLED |
| `/instance/oidc-providers` | 5992 | ORIGIN-COUPLED |
| `/instance/oauth2-providers` | 6123 | ORIGIN-COUPLED |
| `/instance/saml-providers`, `/saml-sp-keys` | 6391, 6614 | ORIGIN-COUPLED |
| `/instance/registration-policy` and org variant | 2305, 2808 | ORIGIN-COUPLED |
| `/instance/invitations` | 2256 | ORIGIN-COUPLED |
| `/instance/config`, `/adoption`, `/apply`, `/config/mail/test` | 6982, 7019, 7098, 7141 | SERVER-ONLY |
| `/instance/mail`, `/mail/test` | 4922, 4885 | SERVER-ONLY |
| `/instance/federation-issuers` | 7256 | SERVER-ONLY |
| `/instance/credential-policy` | 7184 | SERVER-ONLY |
| `/instance/developer-credential-policy` | 15147 | SERVER-ONLY |
| `/instance/pki/*` | 13981 | SERVER-ONLY |
| `/orgs/{org}/scim-bindings` | 7820 | SERVER-ONLY |

**Hard-coded, so not settings:** CSP, COOP, CORP and Permissions-Policy
(`internal/server/spa.go:45-86`); HSTS derived from the origin; session lifetimes
(`internal/service/auth.go:36-42`, `internal/remotefetch/bounds.go:65-66`); cookie names
and attributes (`internal/server/browser.go:58-74`).

**Dev mode (`--dev`):** `hikyo-dev.db`, generated root key beside it, 900 s reauth,
text logs, derived `http://localhost:8080`, `HIKYO_DEV_*` admitted, unattended
upgrades refused. Cookies stay `Secure` and `__Host-`.

## Appendix B: UI settings and browser storage

### Browser storage (complete)

No IndexedDB and no Cache API.

| Key | Store | Evidence | In a multi-server app |
|---|---|---|---|
| `hikyo.theme` | local | `web/src/app/theme.ts:18,37,49` | Global |
| `hikyo.last-sign-in` | local | `web/src/api/lastSignIn.ts:19,38,52` | Per connection |
| `hikyo:update-dismissed:<principal>:fleet:<versions>` | local | `web/src/routes/Shell.tsx:1065-1095` | Per connection |
| `hikyo-session-change` | local | `web/src/api/sessionEpoch.ts:10,121` | Internal signal |
| `hikyo.chrome-identity.<kind>.<id>.{hue,glyph,image}` | local | `web/src/routes/chrome-identity.ts:16-65` | Prototype only |
| `hikyo.lastProject` | session | `web/src/routes/Shell.tsx:95-110` | Per connection |
| `hikyo-oidc-return:<state>` | session | `web/src/api/oidcChannel.ts:1-18` | Transaction state |
| `hikyo-saml-transaction:<state>` | session | `web/src/api/samlTransaction.ts:1-38` | Transaction state |

The workspace bearer lives only in memory (`web/src/api/workspace.ts:67`).

### User-visible settings

| Surface | Settings | Storage |
|---|---|---|
| Header, Account › Preferences | Theme: system, dark, light | Browser |
| Account › Profile | Display name, username; email read-only | Server, per connection |
| Account › Factors, Recovery | Passkeys, TOTP, password, recovery codes | Server, per connection |
| Account › Sessions, Identities | Revoke session, link and unlink identity | Server, per connection |
| Org › Identity, Policy, Members, Danger | Name, retention, open registration, grants, rules, delete | Server |
| Org › SCIM | Bindings, mappings, provisioning credentials | Server |
| Project › Metadata, Identity | Name; identity read-only | Server |
| Project › Environments | Create, rename, clone, delete | Server |
| Project › Policy | Key capacity, per-environment protection, reveal reauth window, definitions source, bundle, retention | Server |
| Project › Developer credentials, Keys | Revoke, DEK, re-encrypt | Server |
| Project surfaces | Approval policies, temporary access, adapters, file targets, machine access, service accounts, dynamic providers, PKI and SSH, transit keys | Server |
| Instance › Admin | Mail test, orgs, members, credential policy, developer-credential ceiling, OIDC, GitHub OAuth2, SAML, federation issuers, PKI, retention health, crypto rotations | Server |
| Instance › Config | Adopt, apply, mail test, rollout decisions | Server |
| Remotes › Directory | Add, pair, rename, remove | Server on the viewing instance. **No owner in the app** |
| Remotes › Serving side | Workspace origins, connection credentials, pairing invites | Server |
| Update toast | Dismissal | Browser |

There is no user density or keyboard setting. Expiry-email and security-alert
preferences are prototype-only.

### Routes

33 routes (`web/src/app/navigation.ts:85-446`). Workspace tier today:

| Status | Routes |
|---|---|
| Works remotely (6) | `/instance/config`, matrix, matrix history, matrix key, values, transit keys |
| Local-only by sidebar | machine access, change approvals, temporary access, adapters, project audit, project settings, project members |
| Unreachable from a workspace | `/login`, `/signup`, `/signup/verify`, `/establish`, `/`, `/projects`, org members, settings, SCIM, audit, `/instance`, `/instance/audit`, `/instance/members`, `/settings` |
| Launches workspaces | `/remotes` |
| Ceremony pages on the instance | `/login/cli`, `/reauth/cli`, `/workspace/approve`, `/auth/oidc/done`, `/auth/saml/done` |
| Ceremony page on the viewing origin | `/workspace/callback` |

**Zero non-test uses of:** `new EventSource`, `WebSocket`, service workers,
`new Notification`, `showSaveFilePicker`, `<iframe`, `document.referrer`,
`<form action>`.

The `demo` build shows the SPA can be static, under a configurable base with hash
routing and no backend. It does not prove a real transport, since it intercepts
`fetch`, and it excludes every auth page. Five ceremony pages read `location.search`
directly and bind to `BrowserRouter`.

## Appendix C: Files changed if the app joins the `hikyo` manifest

Avoided by the separate lane in [section 10](#10-packaging-and-release-impact).

| Location | Pins |
|---|---|
| `.goreleaser.yaml:12-25,52,66,90-95` | One `hikyo` build with `-tags=ui` |
| `scripts/release/create-manifest.sh:78-85,100` | File classification |
| `internal/releasetrust/release.go:17,210,214,240` | Platforms and closed kind switch |
| `internal/releasetrust/nightly.go:136-155,181,205-218` | DMG pair and one binary per platform |
| `scripts/release/verify-bundle.sh:308-317,336,425-433` | Kinds, DMG names, 8 packages |
| `scripts/release/ceremony.sh:217-224,279,298-303` | 6 binaries, 8 packages, 6 checksums |
| `scripts/release/stable/layout.go:46-48,67-72,86-100,252-258,310` | Required counts |
| `scripts/release/create-nightly-manifest.sh:32-58,65-76` | `count == 20 + dmg_count` |
| `scripts/lib/release.sh:53-59`, `scripts/release/prepare-image-root.sh:59,80` | Provenance, image binary |
| `install/install.sh.in:135-150` | Downloads every artifact |
| `scripts/release/sign-apple-artifacts.sh:97-110,229,269-316,358-375,431,696` | Flat payload, identifier |
| `scripts/release/apple_notary_state.py:340-342`, `apple-artifact-digest.sh:13`, `verify-apple-install.sh:15-16`, `publish-homebrew-cask.sh:84`, `render-homebrew-cask.sh:54-90` | Exact Darwin names |
| `internal/selfupdate/selfupdate.go:131-165,213-232`, `trust.go:233`, `stable_workflow.go:74`, `stable_prepare.go:277`, `nightly_prepare.go:71` | Archive flavour |
| `scripts/release/verify-native-packages.go:59` | Eight packages |
| `.github/workflows/release.yml:219,281,334,541`, `nightly.yml:268,274,707,779-787,801`, `ci.yml:594`, `apple-release-recovery.yml` | Jobs and globs |

**Tests with inventories:** `scripts/release/` `snapshot-manifest_test.sh`,
`create-manifest_test.sh`, `create-nightly-manifest_test.sh`, `ceremony_test.sh`,
`sign-apple-artifacts_test.sh`, `render-homebrew-cask_test.sh`,
`publish-homebrew-cask_test.sh`, `publish-homebrew-nightly_test.sh`,
`verify-apple-install_test.sh`, `test_apple_notary_state.py`,
`prepare-image-root_test.sh`, `stable/main_test.go`; `scripts/ci/`
`apple_signing_workflow_test.go`, `nightly_apple_workflow_test.go`,
`apple_phase_transfer_test.go`, `release_symbols_test.go`,
`release_snapshot_resources_test.go`, `check-nightly-release_test.sh`,
`check-release-binary-reuse_test.sh`, `invariants_workflow_test.go`; `internal/`
`releasetrust/nightly_dmg_test.go`, `releasetrust/nightly_platform_test.go`,
`selfupdate/nightly_prepare_test.go`.

**Docs:** `docs/release/apple-signing.md`, `docs/release/signing.md`,
`docs/operations/signed-nightlies.md`, `docs/release/legacy-offline-signing.md`,
`docs/release/1.0.0.md`, `docs/handoff/package-manager-releases.md`,
`docs/site/src/content/docs/docs/installation.mdx`.

### Subcommands by role

| Class | Commands |
|---|---|
| Server/operator | `server`, `migrate`, `operator`, `config-rollout`, `updater`, `admin`, `backup`, `escrow`, `restore`, hidden stage commands |
| Depends on platform | `upgrade` |
| Local information | `version`, `about`, `welcome` |
| Client over API | `audit logout whoami org project env folder key values revision pin approval rotate-token-key rotate-scanning-key rotate-dek rotate-master-key reencrypt instance-config doctor access project-settings sa scim remote remote-credential remote-pairing remote-invite adapter dynamic-provider lease ssh-ca ssh-profile ssh-cert transit pki cert` |
| Client plus host socket | `login --local`, `account establish-credential`, `account recovery begin`, `context create --socket` |
| Client plus local state | `run`, `compose`, `file-target`, `file-sync`, `definitions`, `context` |
| Local or offline | `scan`, `update`, `import` |
| Not read in depth | `dev`, `rotate-root-key` |

Sources: `internal/multicall/modes.go:21-29`, `cmd/hikyo/main.go:124-172,621-662`,
`internal/cli/verbs.go:166-258`.
