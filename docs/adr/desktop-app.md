# Hikyo desktop app ([#1215](https://github.com/Hikyo-Org/Hikyo/issues/1215), 2026-11-04)

Status: **owner-locked on 2026-11-05; operative when
[#1214](https://github.com/Hikyo-Org/Hikyo/pull/1214) merges.**
Each amendment banner has its own [trigger](#amendments). Nothing here is implemented,
and this ADR authorizes no implementation PR, release, `Hikyo-Org/hikyo-app`
repository or signing identity.

1. **Owner acceptance.** Offered the first three [decisions below](#owner-decisions) and told
   that choosing the waiver and accepting the design would lead to the banners and reopened
   tickets, the owner replied "a", "as recommended" and "yes" on 2026-11-04. That reply did
   not separately name D1 to D15. On 2026-11-05, asked whether to accept and merge #1214
   knowing the `cli` session limit recorded in the owner decisions, the owner replied "1. yes".
   That reply is the explicit acceptance of #1214, which contains D1 to D15.
2. **Review.** Cross-provider review is SKIPPED by explicit owner instruction (2026-11-04)
   and is not CLEAN. The waiver covers this decision only and is not a general policy
   change. [GOVERNANCE.md](../../GOVERNANCE.md#amendment-procedure) has no waiver clause, so
   this departs from its procedure, as earlier owner-stated waivers did. See the [review record](#review-record).
3. **Tickets.** [#22](https://github.com/Hikyo-Org/Hikyo/issues/22), [#16](https://github.com/Hikyo-Org/Hikyo/issues/16), [#8](https://github.com/Hikyo-Org/Hikyo/issues/8) and [#33](https://github.com/Hikyo-Org/Hikyo/issues/33) are reopened for
   this amendment only, [#25](https://github.com/Hikyo-Org/Hikyo/issues/25) was already open, and [#1215](https://github.com/Hikyo-Org/Hikyo/issues/1215) tracks the
   work. Each reopened ticket closes again when its banner is operative.

Evidence: the [decoupling report](../reports/server-webapp-decoupling-2026-11-04.md)
and the [Wails shell spike](../reports/wails-shell-spike-2026-11-04.md). The
implementation sequence is the
[desktop app plan](../superpowers/plans/2026-11-04-desktop-app.md).

## Context

A Mac install of Hikyo places one `hikyo` executable and runs no server. No desktop
app exists, so one UI across several hosted instances currently means running a local
server and using Remotes. The goal is an app that connects to hosted instances and
contains no server.

The spike showed the core works on macOS: a native window ran an instance's own SPA on
a CLI session, and a passkey reveal completed through the system browser. It was driven
by scripts with a virtual authenticator against a local instance. It did not cover real
Touch ID, a hosted instance, TLS pinning, OIDC, SAML, Windows or Linux.

**Most mechanisms below are design, not results.** The spike built the proxy, rotation
interception, native handlers and the reveal hop. It did not build the keychain, the
connection list, sign-in, the save panel, clipboard clearing or the route limits in D9.

## Decision

The app is a thin native shell. It holds a list of connections and one session per
connection in the OS keychain. For each connection it opens an isolated renderer that
loads that instance's own SPA through an in-process proxy.

```
Hikyo.app
  renderer, connection A  ->  in-process scheme handler  ->  HTTPS  ->  instance A
  renderer, connection B  ->  in-process scheme handler  ->  HTTPS  ->  instance B

Sign-in and passkey ceremonies: system browser -> instance's real origin
  -> loopback callback -> shell redeems with PKCE
```

### D1. Shape

- The shell contains no server, datastore, keyring or bundled SPA.
- It runs only while the user has it open. It installs no launch item and no background
  agent, which keeps [system-architecture.md](./system-architecture.md)'s "no resident
  client-side agent" true.
- `hikyo` stays one artifact. The server keeps its embedded SPA, because sign-in, passkey
  and approval pages live there. An API-only server is rejected: it could not onboard a
  human.
- Running a server inside the app for real secrets is not a v1 goal.

### D2. Connections

- Each connection gets its own origin from a random identifier that is stored with the
  connection and never reused. Sequential identifiers are refused: the spike's `conn-N`
  would hand a deleted connection's browser storage to whichever instance next received
  that number.
- Removing a connection revokes its session on the server first. If that fails, the app
  says so plainly, because the credential stays live until it expires.
- It then deletes the connection's data, browser storage and keychain item together.

### D3. Session

- The app holds an ordinary `cli` session, obtained through the existing handoffs. This
  is what the spike ran on.
- It adds no session class. A new class would need a closed enum to grow
  (`ActiveSession.artifact`), a database check and handoff column to change, and a
  token type and audit enum to change. It would still be asserted by the client, since
  login handoffs start unauthenticated.
- The shell stores the session's id and principal with the connection, and updates the id
  when the session is reissued (D4). A sign-in that returns a different principal needs a
  native confirmation before the connection uses it.
- The session keeps the `cli` clocks: idle 30 d, absolute 90 d. They are the same clocks
  a laptop's CLI has today, with better custody.
- The shell sets `User-Agent: Hikyo-Desktop/<version>`. Session rows record it, and the
  session API and audit view expose it. The account page's session list doesn't show it
  yet, so the plan adds that. It is a label, not a guarantee.
- The session is never shared with the CLI, since either client's rotation would sign
  the other out.
- Custody is the OS keychain only, with access restricted to the signed app. The CLI's
  `0600` state file is not used.

### D4. Proxy

- The proxy is an in-process scheme handler. A loopback HTTP proxy is refused, because
  any local process could spend the session through it.
- Each request is bound to the connection of the window that made it, and an unbound
  request fails closed.
- It adds the bearer, and strips `Cookie`, `Origin` and `Referer` from requests and
  `Set-Cookie` from responses.
- It never follows a redirect with the bearer attached.
- It refuses page requests to these routes, none of which a flow in the window needs:
  - `/api/v1/auth/cli-login/*` and `/api/v1/auth/cli-reauth/*`. The shell calls the
    handoffs itself.
  - `/api/v1/auth/workspace/*`. The approval accepts a `cli` bearer, and the app hides
    workspace launch.
  - `POST /api/v1/orgs/{org}/projects/{project}/environments/{environment}/developer-credentials`,
    and any `POST /api/v1/auth/reauth/totp` whose `purpose` is `developer-credential`. The
    body check decodes the JSON and fails closed on a parse error or duplicate key. Mint
    stays a terminal action under [#807](https://github.com/Hikyo-Org/Hikyo/issues/807).
  - Every other route that returns a `session_token` and isn't on the rotation and reissue list below.
    This covers the login routes, so a page can't sign the connection in as another
    account, and the passkey finish routes, which can't run in the window.

**Rotation and reissue.** A CLI session receives a new token in the JSON body on step-up and
on account-security changes.

- The shell takes a new token only from these routes under `/api/v1/auth/`:
  - **Rotate, keeping the session id:** `POST totp/step-up`, `POST reauth/totp`.
  - **Reissue, deleting every session of the principal and creating a new one with a new
    id:** `PUT password`, `POST totp/enrol/confirm`, `DELETE totp`,
    `POST recovery-codes/regenerate`, `DELETE identities/{id}`,
    `DELETE webauthn/credentials/{id}`.
- The token must parse as a CLI token. The other check depends on the route:
  - A rotate response's `session_id` or `session.id` must match the connection's stored id.
    `ReauthResult` has no principal field, so the id is the check there. Rotation refuses an
    account that doesn't own the session, so a match implies the same principal. The shell
    applies this rule to its own reauth redeem.
  - A reissue response's `principal.id` or `login.principal.id` must match the connection's
    stored principal. The shell then replaces the stored session id with `session.id` or
    `login.session.id`.
- It reads the top level and `login`, and leaves nested keys alone, because one could be a
  secret's name. The token is removed from the response, so it never reaches the page. A
  `session_token` on any other response is removed and treated as an error.
- A failed check stops the connection with a visible error, asks for a new sign-in and, if
  the token parses, tries to revoke that session. It still releases the response body without
  the token, because recovery codes in it can't be fetched again.
- The check runs before the keychain write. The token and session id are stored in one
  keychain item, so a crash can't leave them out of step.
- The proxy asks for identity encoding, because it can't remove a token from a body it can't
  read. A 2xx from a listed route that can't be parsed fails closed.
- It writes the token to the keychain, then swaps it in, then releases the response.
- One runs at a time. A new token is only known after its response arrives, so
  requests already in flight can get a 401. Idempotent ones are retried once with the
  new token, and the rest surface the error. Open event streams reconnect.
- If the keychain write fails, the connection stops with a visible error and asks for a
  new sign-in, and the shell tries to revoke the new session. It still releases the
  response body without the token, because recovery codes in it can't be fetched again.

### D5. Trust

Each connection uses the CLI's trust choices: system CA or an SPKI pin
([api-cli-surface.md](./api-cli-surface.md) § explicit HTTPS trust). There is no
insecure-skip mode.

That section requires a controlling terminal at first contact, which a GUI doesn't have.
Instead, the user selects the trust mode first, then the shell shows the origin and SPKI
fingerprint in a native confirmation, before it sends any other request, including the
`/api/v1/meta` read in D8. The confirmation states the key-rotation tradeoff and asks the
user to verify the fingerprint independently. The pin is stored with the connection.

**The pin's protection ends at the browser hop.** The browser uses its own trust store, so
an attacker holding a certificate the browser trusts for that origin isn't stopped by the
pin. If the browser can't reach the origin, the shell gets no signal, only a timeout, so
the app can only suggest that as a possible cause.

### D6. Sign-in

Sign-in always runs in the system browser on the instance's real origin, using the
existing loopback handoff with PKCE, with the device flow as a fallback. The SPA's login
form never runs in the app. When a session dies, the SPA signals the shell, which confirms
it and starts a new handoff (D9). Browser passkey enrolment and identity linking delete
every session of the principal, including the app's, so the app signs in again after them.
This also applies to a session confined until enrolment.

### D7. Ceremonies

Passkeys cannot run on an app origin, so the system browser performs them, like the CLI.
Page state stays intact. The reveal waits until the user returns to the window, because
the existing focus gate still applies and the shell cannot take focus back.

| Flow | v1 behaviour |
|---|---|
| Purposes the reauth handoff accepts: `adapter`, `reveal`, `copy`, `publish`, `approve`, `reject`, `bypass`, `self-config` | Browser hop, then back to the same view. Only `reveal` has been run |
| `developer-credential` | Refused in the app. It remains a terminal action |
| TOTP where the instance offers it, for any row but `developer-credential` | In the window, unchanged. This takes precedence over the rows below it |
| `mint` and `access` without TOTP | Continue in the browser |
| Session step-up by passkey | Continue in the browser |
| Passkey enrolment | Continue in the browser |
| Identity link and establish | Continue in the browser |

"Continue in the browser" opens the current page on the real origin, under a separate
browser session. The reasons differ:

- **`mint` and `access`:** `purpose` and `operation` on `CLIReauthStartRequest` are closed
  enums, and closed enums never grow under the `/api/v1` freeze (`api/freeze.go`). Serving
  them in the app needs a new additive operation, which is a later decision.
- **Enrolment:** `navigator.credentials.create` needs the real origin.
- **Identity link and establish:** these set a browser cookie, which the proxy strips.
- **Step-up:** this is a v1 scope choice, not a limit. A reauth redeem already raises the
  CLI session's assurance, so a later version could serve it with a new operation.

### D8. App mode

- The instance advertises a new `protocol_capabilities` token, `desktop-app/1`, meaning
  its SPA implements the shell handoff protocol. An instance built without the SPA doesn't
  advertise it. The enum is open, so this isn't a freeze problem, but the locked meta
  clause limits this list to authentication flows. The api-cli-surface banner supersedes
  that clause for this token. [#683](https://github.com/Hikyo-Org/Hikyo/issues/683) is a
  loose precedent.
- The shell reads it from `/api/v1/meta` before signing in. On an instance without it,
  the shell refuses by name, says which server version is needed, and offers to open the
  instance in the browser. There is no degraded mode, because a partly working secrets UI
  is not a graceful state.
- Connections are distinct by origin. The app needs no other identifier.

### D9. Shell routes

`/__shell/*` is a closed list of routes answered by the shell and never forwarded to the
instance. They are not API endpoints, so the "one surface" rule for `/api/v1` is
unaffected. One SPA module owns app-mode detection and all shell calls.

A compromised instance can call these for its own connection, so each must be safe under
that assumption. All accept requests only from the owning window, parse strict JSON, and
reach no other connection.

| Route | Accepts | Limits |
|---|---|---|
| `POST reauth` | Purpose, operation, unit | The D7 purposes only. One at a time per connection, and rate limited. The ceremony's lifetime doesn't depend on the page's request, so aborting frees no slot. Only the browser ceremony can approve |
| `POST sign-in` | Nothing | The shell first confirms the session is dead by calling the instance. One at a time, and rate limited |
| `POST open-in-browser` | A path and query | On this connection's real origin only, never a URL. Refuses the handoff and approval pages, directly or through a `returnTo` (see below). Rate limited |
| `POST download` | A proxied path or bytes, and a filename | Always a save panel, one at a time, and rate limited, with a sanitized filename. Proxied paths obey D4's refusals. Cancel writes nothing |
| `POST clipboard` | Nothing | Arms clearing only after a pasteboard change the shell observed. See D12 |
| `GET origin` | Nothing | Returns the instance's real origin, for links the SPA builds |

The handoff and approval pages are `/login/cli`, `/reauth/cli` and `/workspace/approve`.
`open-in-browser` refuses any `returnTo` parameter, because `/login?returnTo=...` and
`/signup?returnTo=...` send a signed-in user straight to its target, and the SPA resolves
that target with a URL parser after decoding it. It also refuses non-canonical input: dot
segments, raw or encoded; backslashes; control characters or whitespace; a leading `//`;
and a query that fails strict decoding. It then checks that the parsed origin is the
connection's. The SPA's router ignores case and trailing slashes, so the comparison
percent-decodes, lowercases and strips trailing slashes, as `web/src/app/loginReturn.ts`
does after parsing the URL.

The page cannot reach Wails' own runtime routes under `/wails/`.

### D10. Renderer policy

- A window may navigate only within its own connection.
- `https` links open in the system browser and are rate limited. Links to this
  connection's handoff and approval pages are cancelled, under D9's rules. All other schemes are
  cancelled. A user-gesture requirement applies where WebKit exposes one, which has not
  been verified.
- Developer tools are off in release builds.
- The page gets no native capability beyond D9. Wails injects `window._wails` and an
  `external` message handler into every page. Before release these must be disabled, or
  listed here as exposed surface with an abuse test.

### D11. Downloads

Every download, whether from a blob or fetched through the proxy, opens a save panel,
uses the filename from the click and is bound to the window that owns the connection.
Saved files carry the `com.apple.quarantine` attribute. Silent writes to a fixed
directory, as the spike does, are refused.

The panel is the consent. A user gesture can't be the gate, because `Audit.tsx` creates
its blob after an `await`, when the click may no longer count.

### D12. Clipboard

Auto-clear after copying a secret moves to the shell, because WebKit refuses the clipboard
read the SPA relies on. The shell records the pasteboard's change count when it sees the
copy, and clears later only if the count hasn't moved, so it never wipes something else.
This has not been built or run.

### D13. Repository and releases

- Source lives in this repository as a nested Go module,
  `github.com/Hikyo-Org/hikyo/desktop`. It can import `internal/` packages (tested), and
  SPA and shell changes can land in one PR. Wails and cgo never enter the root `go.mod`.
- Binaries are published from a separate repository, `Hikyo-Org/hikyo-app`, under their
  own tags. They never enter the signed `hikyo` manifest or this repository's releases.
  Field verifiers reject unknown artifact kinds (`internal/releasetrust/release.go`), and
  field update checks read every release here, so one bad asset fails the listing
  (`internal/updatecheck/source.go`).
- The app ships as a notarized Developer ID build. **v1 has no updater and no update
  check.** An updater is a new root of trust, and
  [threat-model.md](./threat-model.md) allows no unsolicited update check. A later version
  needs its own decision, off by default and listed in the docs.

### D14. Framework

- The shell uses Wails v3, pinned to an exact version. The contract above does not depend
  on Wails, so changing framework needs no change to this ADR's other decisions.
- Wails v3.1.0 has no navigation, popup or download handling on macOS. The shell adds it
  with its own native code on Wails' delegate class. This relies on Wails internals, so
  the shell checks at startup that the methods are installed and refuses to open a window
  otherwise. Every Wails upgrade needs a recheck.
- Electron is the fallback. It provides these APIs and ships Chromium, along with the duty
  to release for its CVEs.

### D15. Platforms and egress

- macOS first. Windows and Linux need their own spike.
- The shell connects only to configured instances. It sends no crash reports and no
  telemetry. Logs never carry tokens or response bodies. A page's own subresource requests
  depend on that instance's CSP, so a compromised instance can send them elsewhere.

## Amendments

Each amendment is a banner at the top of the ADR it names, and takes effect only when the
pull request that makes it true merges. No banner is operative before its trigger.

| ADR | Operative when |
|---|---|
| [api-cli-surface.md](./api-cli-surface.md) | Plan PR 1 merges (capability). Its first-contact clause waits for plan PR 3 |
| [system-architecture.md](./system-architecture.md) | Plan PR 3 merges (shell) |
| [human-auth.md](./human-auth.md) | Plan PR 3 merges |
| [threat-model.md](./threat-model.md) | Plan PR 3 merges |
| [oss-mechanics.md](./oss-mechanics.md) | Plan PR 5 merges (release lane), together with both copies of its "sole exception" sentence and their policy check |

**Deliberately not amended:**

- [mvp-boundary.md](./mvp-boundary.md): the app is a post-MVP addition, and criterion O1
  still describes the `hikyo` artifact.
- [multi-instance.md](./multi-instance.md): the workspace tier is unchanged, and the app
  does not use it.
- [ops-spec.md](./ops-spec.md) and [machine-identities.md](./machine-identities.md): no
  clock or token type changes.
- [remote-instance-pairing.md](./remote-instance-pairing.md): its "future native client"
  clause concerns pairing keys. The app runs the instance's own pairing page and adds no
  pairing client of its own.

## Frozen API impact

| Change | Why it is permitted |
|---|---|
| `protocol_capabilities` gains `desktop-app/1` | Open enum |

By design, no closed enum grows, no endpoint is added and no operation's authorization
changes. `/auth/cli-reauth/start` documents an "acting CLI session", and the app's session
is one.

The browser pages say "Authorize CLI" and refer to a terminal, which would mislead someone
approving from the app. The fix stays in the SPA: it rewords its own pages and maps the
server's detail strings that mention the CLI to neutral text. Server strings don't change,
so error semantics don't either.

## Rejected

- **The workspace tier as the base.** Its bearer lasts 4 hours at most, may only live in
  memory, works on 6 of 33 routes, and needs each server's admin to allowlist the app's
  origin.
- **A bundled SPA.** It creates a full version-skew matrix, and the SPA has no
  per-operation revision gate.
- **WebAuthn in the window.** WebKit refuses it (`SecurityError`) because the origin has
  no valid RP ID.
- **A new `app` session class.** See D3. The decoupling report recommended it before this
  cost was known.
- **A `client_class` response field.** It adds a field to carry the same client-asserted
  label that the user-agent already carries.
- **Adding `mint` and `access` to the reauth enums.** Closed enums never grow.
- **A client-only CLI build.** The app does not need it, and the ADR that rejected it
  stands.

## Residual risks

- **Renderer script** in a compromised SPA can use the session while the window is open,
  as in a browser tab. By design, it can't read the token or reach another connection.
- **Approval on the real origin can be phished.** D9 and D10 refuse same-origin paths to
  the handoff pages, but an external link can redirect there. An attacker can start a
  device flow elsewhere, since `cli-login/device/start` is unauthenticated, and a hostile
  page can send the user to approve it, right after the app has taught them to continue in
  the browser. A browser tab has similar exposure. The far-end copy must say plainly that
  a separate device is being approved.
- **The hop shows opaque IDs.** The browser ceremony requires a click, but it lists raw
  environment and key IDs, and in the app the page chooses them. A hostile page could
  request a production `bypass` the user can't recognize. The plan adds names from existing
  reads where possible.
- **A compromised instance** receives the token on every request. It can replay it after
  the window closes, until it expires or is revoked, for up to 30 days idle and 90 days
  absolute. This exceeds a browser session's 7 and 30 days, and matches the CLI's exposure.
- **A process running as the same user** may reach the keychain item through a prompt or a
  bypass of the app's code signature. It is the same trust boundary as the user's browser
  profile.
- **Operators can't reliably tell the app from the CLI.** The label is client-asserted.
- **A session can outlive its holder.** If a revocation fails, or the app crashes or loses a
  response after the server commits, the new session stays live with no holder for up to
  30 days idle and 90 days absolute, unless it is revoked from another signed-in session.
- **Some work stays in the browser** in v1 (D7), so app users keep a browser session too.
  Passkey enrolment and identity linking there sign the app out (D6).
- **The app works only with upgraded servers** (D8).
- **The native handlers depend on Wails internals** (D14).

## Acceptance before release

None of these has been observed yet. Each must be observed by hand on a signed build:

1. Sign-in and a passkey reveal with real Touch ID.
2. A hosted instance on system CA, and another on an SPKI pin, including first contact.
3. OIDC and SAML sign-in through the system browser, and OIDC reauth.
4. Browser hops for `copy`, `publish`, `approve`, `reject`, `bypass`, `adapter` and
   `self-config`.
5. Save panel, filename and quarantine attribute on a real download.
6. Clipboard auto-clear, including when the user has copied something else since.
7. Rotation under concurrent requests, a reissue such as a password change, recovery-code
   regeneration, the enrolment gate, a keychain write failure, and a relaunch: the session survives or stops
   visibly, recovery codes are shown, and the page never holds a token.
8. A refused connection to an instance without `desktop-app/1`.
9. Removing and re-adding a connection: the session is revoked on the server and no
   browser storage remains.
10. Two connections open together stay isolated.
11. A hostile page calling every `/__shell/*` route: no silent file write, no browser-tab
    flood, no wiped clipboard, no reach into another connection, no way to the handoff or
    approval pages by a same-origin path or `returnTo`.
12. Keychain access is refused to an unsigned process.
13. The shell refuses to start when a native handler is missing.
14. A hostile page can't reach any D4 refused route, including a TOTP reauth for
    `developer-credential` and a local login as another account.
15. A sign-in returning a different principal asks for confirmation.

## Owner decisions

Settled on 2026-11-04:

1. **Review route:** a per-decision waiver of cross-provider review, accepting the
   same-provider rounds below.
2. **D3:** keep the `cli` session class. No freeze exception for an `app` class.
3. **D7:** the browser fallbacks are acceptable in v1, including `mint` and `access`.

Settled on 2026-11-05:

4. **#1214:** accepted for merge. It contains D1 to D15, the banners and the plan.
5. **D3 consequence:** keep `cli`, knowing that a `cli` session can't finish recovery-code
   regeneration or the enrolment gate in the instance's UI. The server has already replaced
   the codes by then, so the old ones are void and the new ones are never shown.

Author's note, not part of the owner's reply: the repair is a required row in
[plan PR 1](../superpowers/plans/2026-11-04-desktop-app.md). The gate fails at its first step,
which issues the codes. This comes from reading source and matches an iOS simulator run in
the spike report's third round. It has not been run in the macOS shell.

## Review record

- **Cross-model review:** not run. SKIPPED by explicit owner instruction (2026-11-04), for
  this decision only, and not CLEAN.
- **Independent same-provider review:** three rounds by separate fresh Claude Opus 5.5
  agents, each on an earlier draft, reading source without running anything. The first
  reversed the session class decision in D3 and reshaped D4, D9 and D13. The second found
  no blocker and three high findings in D4 and D9. The third was limited to those repairs
  and found one more high finding, in D4's rotation check. Its repair was itself wrong, as
  the next round found.
- **Fourth same-provider review, at `7ef5235882895f4af667d69ac4fe072f1b068bce`:** limited to
  D4, D9, D10, the banners and the status text. It found no blocker, one high finding (six
  of the eight rotation routes reissue the session, so the id check failed on them), two
  medium (path normalization and the `owner-locked` label) and six low. All are repaired.
- **Fifth same-provider review, at `f525883879b5da6a6603b6b8ada649b11d920562`:** limited to
  those D4, D6 and D9 repairs. It confirmed the route classification, response fields and
  D6 against source. It found one high finding (decoded `returnTo` forms still reached the
  handoff pages), one medium (a session left with no holder) and three low. The high, the
  medium and two of the low are repaired in this text and have not had a further review. The
  third low is an OpenAPI description, left to plan PR 1. No round ran anything except small
  parser probes, and the shell these rules govern doesn't exist yet.
- **Final edits:** the plan's recovery-code note and the owner acceptance text were reviewed
  together by one fresh Claude Opus 5.5 agent, reading source without running anything. It
  found one high (the banners and plan gate still said not owner-locked), three medium and
  three low. All are repaired in this text and have not had a further review.
- **Owner statements (2026-11-04):** asked for a Wails spike of the recommended design; said
  the app should open the native browser for passkeys, like the CLI; asked whether Hikyo
  should be a monorepo with separate build paths; approved writing ADR amendments and an
  app-mode plan; settled the first three decisions above.
