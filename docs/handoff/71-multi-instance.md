# Handoff: #71 multi-instance — directory tier + workspace tier

Issue: https://github.com/Hikyo-Org/Hikyo/issues/71. ADRs:
[`docs/adr/multi-instance.md`](../adr/multi-instance.md) (locked 2026-08-06) and
the MVP gate M6 in [`docs/adr/mvp-boundary.md`](../adr/mvp-boundary.md).

**Outcome: COMPLETE on current main.** Foundation, store, service, audit, API,
CORS/CSP, CLI, the Go E2E lifecycle, and the two-instance harness landed in
PR #115. PR #259 completed the browser-to-remote workspace data path and the
remote step-up flow; PRs #308 and #310 supplied and integrated the root browser
session-epoch owner that had kept #71 open. All six M6 criteria now have
executable coverage. The final browser slice and the session-epoch closure
evidence live in
[`71-multi-instance-workspace.md`](./71-multi-instance-workspace.md), the
second handoff under this issue number (the reopened workspace tier).

This handoff records the outcome only; the implementation-time progress log,
session diaries, rebase notes and per-round review transcripts were removed once
the work shipped.

## Decisions carried from the build log

- **Migration 00020 rebuilds the `sessions` table on both engines** to widen the
  `artifact` CHECK to `('cli','browser','workspace')` and add `requesting_origin`
  + `handoff_id` with a CHECK tying them to the workspace artifact. The rebuild
  restates the shape `sessions` had reached by 00014 (a rebuild copying only
  00005's columns would silently drop `csrf_verifier`, `provider_id` and the
  singular-provenance CHECK), and it drops in-flight `reauth_windows` rows
  explicitly (`DELETE FROM reauth_windows`) on both engines rather than as an
  sqlite-only incidental cascade. Later CHECK tightening for the workspace tier
  was folded into 00020/00021 rather than a new migration, deliberately: those
  migrations were new in this branch and never shipped, so the immutability rule
  (which protects released migrations) did not apply.
- **Instance identity is minted by the migration**, not by a boot mint site:
  boot's system-proof operation set is closed by invariant 11, and the only
  correct moment for the value to exist is the moment the schema does.
- **Principal + credential are one row (`instance_connections`)**: one credential
  per principal ever, and revoke retires both. It is deliberately not a
  `service_accounts` row (that table's kind CHECK admits only workload/automation
  and its composite FK demands a project this principal has not got).
- **`remote rename` is API + UI only, never a CLI verb** (the #25 amendment's
  closed verb list is `remote add|list|show|remove`; the display name is the one
  mutable field and `remote.renamed` is a named audit event). The grant for the
  connection principal is written at the store layer by the connection minter,
  not through the grants API, so `mintableOrigins` stays untouched.
- **Allowlist rows are addressed by opaque id or request body, never by a
  URL-shaped path parameter** — a path parameter naming an origin is one review
  lapse away from one naming a target (`passthroughParams` in
  `api/noproxy_test.go` pins this).
- **`instance_identity` is read proof-free** by `Resolver.InstanceIdentity` on
  the resolution surface (self-connection refusal and directory-serve are both
  authn-adjacent, and the value is neither tenant data nor secret); recorded as a
  knowing deviation in `Resolver.RemoteOrigins`' doc comment rather than
  re-annotated `class=authn`.

## Owner-ratified bounds (2026-08-13)

**The original gap was real and circular.** The #32 amendment header declares
that the composable-maxima catalogue gains the directory outbound client's
bounds and the workspace session's lifetimes — but the catalogue's table (rows
1-20) never gained the row, while multi-instance.md delegates the concrete
values *to* that catalogue. The two documents pointed at each other until the
owner ratified all nine bounds on 2026-08-13, with the workspace pair changed to
hard-short.

They remain pinned as named constants in **one place** —
`internal/remotefetch/bounds.go`. If a value moves, it moves there and nowhere
else.

| Bound | Value | Rhymes with |
|---|---|---|
| Per-remote deadline | 10 s | row 17, HTTP header/read timeout scale |
| Response size cap | 1 MiB | row 19, adapter response cap |
| Remote count cap | 50 | row 13, environment cap scale |
| Parallel fan-out cap | 4 | rows 17/19, 4-concurrent-per-org pattern |
| Coalescing window | 5 s | — (bounds the crowd; the per-viewer rate bounds one human) |
| Per-viewer trigger rate | 6/min | human card-refresh scale |
| Instance-wide aggregate rate | 60/min | row 17, fail-closed default budget |
| Workspace session idle / absolute | 15 min / 4 h | row 5 — reveal window/cap; a workspace is a disclosure surface |
| Handoff transaction expiry | 10 min | row 7, authorization-code analogue |

Two of these deserve a sentence in the ratified record:

- **The workspace lifetimes are HARD-SHORT, ratified by the owner on
  2026-08-13.** The header-borne bearer is extractable and replays outside the
  browser until expiry or revocation, so the pair deliberately reuses row 5's
  15-minute reveal window and 4-hour cap. The shell's current 5-second liveness
  poll keeps an open tab active; idle expiry therefore means 15 minutes after
  the tab closes.
- **The aggregate rate is the one that actually protects the fleet.** Per-viewer
  limiting alone does not bound many principals — fifty viewers each politely
  under their own limit is still fifty times the traffic at every remote.

The three session/handoff values stay colocated in `remotefetch` though they are
not outbound-client concerns: they are one ADR catalogue addition, and splitting
them across packages would make the ratified set easy to drift.

**Where each constant gets consumed — read this before hunting for struct
fields.** Only three of the nine are `remotefetch.Config` fields (`Deadline`,
`ResponseCap`, `FanOut`), and `DefaultConfig()` already wires them. The other
six are **service-layer wiring and have no `Config` field, by design**:

| Constant | Consumer |
|---|---|
| `RemoteCount` | `remote add` — refuse the 51st entry |
| `CoalesceWindow` | the on-view fetch loop — `singleflight`, `golang.org/x/sync` is already in `go.mod` |
| `ViewerTriggerRate`, `AggregateTriggerRate` | the on-view fetch loop's two rate gates |
| `WorkspaceSessionIdle`, `WorkspaceSessionAbsolute` | workspace session issuance |
| `HandoffExpiry` | handoff transaction creation |

Six then-unreferenced constants could read as dead code. They were the directed
bounds table whose consumers are the service-layer wiring named in the table
above; all are now wired and owner-ratified.

## What landed

### 1. The `AddWorkspaceOrigin` wart — FIXED
`Workspace.AddOrigin` now returns the `OriginView` it wrote, so the handler
builds its 201 body from it. No second authorization, no spurious
`remote.origin_allowlist_read` per add. `canonicalOrEmpty` deleted with it.

### 2. The workspace session's assurance record — CLOSED (migration 00021)
`internal/store/migrations/{sqlite,postgres}/00021_workspace_assurance.sql`:
`ALTER TABLE workspace_handoffs ADD COLUMN factors TEXT NOT NULL DEFAULT '[]'`.
No new table, so no `hikyo:table` directive and no PG reset drop-list change.

- Written at **approve** from `caller.Assurance.Factors` (the ceremony the human
  actually performed, in the popup, on this instance's origin), read at
  **redeem** into `MintSession`. The column exists because approval and
  redemption are two requests minutes apart and the approving session may be
  gone by then — the transaction row is the only honest carrier.
- `ApproveWorkspaceHandoff` gained a `factors` parameter through the resolver
  and the `TxAuthorizer` passthrough. `annotated_queries.json` repinned (358).
- **Query column ORDER matters**: `factors` is selected LAST in
  `WorkspaceHandoffByState|ByCode` so sqlc's row struct stays convertible to the
  table model (`sqlitegen.WorkspaceHandoff(row)`). Put it anywhere else and the
  resolver stops compiling.

### 3. The step-up elevation path — BUILT (supersedes the earlier plan to refuse step-up by name)
`Workspace.elevate` (`internal/service/workspace.go`). A redeemed **step-up**
now elevates the workspace session it bound instead of being refused by name:

- Opens a sliding reauthentication window over `(bound session, bound
  environment)` through the human-auth service's own seam (`Workspace.Reauth
  *Auth`, wired in `internal/app/app.go`), so a lowered environment is honoured
  here exactly as it is for TOTP/OIDC/WebAuthn. Fails closed at a 0 window.
- **Never single-decision**: `ConsumeReauthWindow` resolves a single-decision
  window back to a WebAuthn ceremony row for byte-exact unit matching, and a
  handoff transaction is not one.
- **Rotates the bound session's bearer** (`RotateSessionFactors`) and writes the
  approving ceremony's factors onto it. Same session id, same clocks, same
  origin binding — one session, new value. Rotation is deliberate: a bearer
  stolen before an elevation must not become an elevated bearer after it. The
  redemption response carries the new value (`elevated`, `environment`,
  `window_expires_at`, all additive and optional in the contract).
- **The security check that is easy to miss**: `StartHandoff` is
  pre-authentication, so anyone may open a step-up naming any session id. The
  bound session is therefore resolved WITHIN THE APPROVING PRINCIPAL'S OWN
  SESSIONS (`SessionsForPrincipal(h.PrincipalID)`). Without that, a stolen
  workspace bearer could be elevated with the thief's factors.
- Refusals, all uniform `ErrHandoffInvalid` with the cause on the trail only:
  `step-up-not-environment-scoped`, `step-up-assurance-unreadable`,
  `step-up-assurance-inadequate` (a password-only popup demonstrates nothing),
  `step-up-session-unknown`, `step-up-not-a-workspace-session`,
  `step-up-origin-mismatch`, `step-up-session-expired`, `step-up-window-closed`.
- Audit: the elevation emits `auth.reauthenticated`, added to the redeem route's
  `wireEvents`.
- Tests: `internal/isolation/workspace_stepup_test.go`, both engines, plus
  `TestElevationHonoursTheEnvironmentsOwnWindow`.

### 4. A latent #54 defect fixed at the root
`InsertReauthWindow` was a plain INSERT against a table with
`UNIQUE (session_id, environment_id)`, so the **second** ceremony over one
(session, environment) — a window that lapsed and was re-earned, or a repeated
workspace step-up — answered a fault instead of re-arming the window. Now an
UPSERT on both engines: the newer ceremony's clocks, factor class and epoch
replace the older ones and `consumed_at` is cleared. This fixes all four
openers (TOTP, OIDC, WebAuthn, workspace), not just the new one.

### 5. A real CORS/routing bug the flow found
**A preflight must MATCH a route or chi never runs the group's middleware.** The
contract declares no OPTIONS operations (correctly), so `OPTIONS
/api/v1/auth/workspace/start` fell through to the router's not-found handler,
which knows nothing about CORS and answered with no headers — the browser
reports that as a missing `Access-Control-Allow-Origin`, which reads like an
allowlist problem and is not. Every cross-origin **POST** of the workspace tier
was unreachable while the GETs worked. Fixed with a catch-all OPTIONS route
inside the API group (`internal/server/server.go`); `workspaceCORS` still
answers the preflight itself.

### 6. Web UI
Three new surfaces in `web/src/app/navigation.ts` (the flow registry closes over
it, so surface + route + flow landed together):

| Surface | Path | Section |
|---|---|---|
| `remotes` | `/remotes` | Organisation |
| `workspace-approve` | `/workspace/approve` | null (chromeless) |
| `workspace-callback` | `/workspace/callback` | null (chromeless) |

- `CHROMELESS` is DERIVED from `section === null`; `App.tsx` routes those
  outside the shell and — this is the non-obvious part — serves
  `workspace-approve` in the SIGNED-OUT branch too. A first establishment lands
  in a popup with no cookies for the serving instance, and bouncing it to
  `/login` would throw away the `state` the transaction is addressed by. The
  approve page renders `<Login/>` itself instead and the URL survives.
- `web/src/routes/Remotes.tsx` — directory cards (state sentence per the
  7-member enum, `credential-rejected` as its own loud state, "showing the last
  known directory, N old", identity/version/org+project names and counts),
  add/rename/remove, the workspace launcher, and the serving side's origin
  allowlist with `sessions_revoked` reported out loud.
- the account settings panel (since absorbed into
  `web/src/routes/AccountSecurity.tsx`; the standalone `Sessions.tsx` was
  removed) lists every artifact holding the account, workspace sessions as their
  own artifact type carrying their requesting origin, with revoke.
- `web/src/api/remotes.ts` — same-origin react-query hooks over the generated
  client, `refetchInterval` polling (NOT EventSource).
- `web/src/api/workspace.ts` — the cross-origin half. Never goes through
  `api/client.ts`: `credentials: 'omit'`, `mode: 'cors'`, generated Zod schemas
  on every response, live `/api/v1/meta` skew check before establishing, PKCE
  S256, nonce-named `BroadcastChannel` (`hikyo.workspace.<state>`), bearer in a
  module-level Map and nowhere else.

**Two UI decisions worth review:**

1. **The ceremony prepares eagerly before its popup-launch click.** `window.open`
   only survives the popup blocker inside the task of a real user gesture, while
   the handoff transaction needs a network round trip first. The launcher stays
   disabled and says `Contacting…` until `prepareWorkspace` completes; its
   enabled, origin-labelled action then calls `openPrepared` synchronously. The
   human still reads which origin they are about to visit before a window opens.
2. **A liveness poll, and it is how both kill switches become visible.**
   `probeWorkspace` GETs the remote's `/api/v1/me/sessions` with the bearer
   every 5s. A 401/403 drops it. An OPAQUE failure counts a strike and two
   consecutive strikes drop it — because de-allowlisting withdraws the CORS
   headers with the consent, so "consent withdrawn" and "host down" are
   indistinguishable from script, and a card claiming an unusable workspace is
   open is the one thing it must not do.

### 7. Playwright: the two-instance harness
`web/e2e/fixtures/instance.ts` now starts **two** real instances and
`web/e2e/flows/workspace.spec.ts` drives the whole arc.

**DECISION (the one left open): the browser leg is loopback HTTP.**
`service.CanonicalOrigin` accepts http so a loopback ORIGIN is representable as
an allowlist entry, while `remotefetch.ValidateRemoteURL` refuses plaintext so a
REMOTE URL never is. What the browser leg proves — popup on the remote's origin,
CORS, `noopener` + BroadcastChannel, header-borne bearer, both kill switches —
is origin-shaped, not pin-shaped, so http exercises it honestly and NO
certificate exception is needed anywhere (no `ignoreHTTPSErrors`, no SPKI launch
flag). The pinned TLS half is proven where it belongs, in
`internal/isolation/two_instance_test.go`, against two real routers over real
TLS.

**Two hosts, not two ports.** A is `http://127.0.0.1:45789`, B is
`http://localhost:45790`. Cookies are NOT partitioned by port: one hostname
would mean one cookie jar and B's login would silently destroy A's session.

**Two store-level seams, both documented in the fixture:**
- `repointRemoteAtB` — the entry is CREATED through the real API against a
  pinned TLS front that proxies B's own directory (so the credential is really
  sealed and the snapshot is really B's), then its URL is repointed at B's
  loopback http origin. An entry can only be created over https, correctly.
  The card then renders the `unreachable — last known` state honestly.
- `seedDirectoryGrant` — `instance-directory` is deliberately not in the
  operator set, and granting it through the API is a three-ceremony detour
  (MFA-mandatory surface; granting to oneself invalidates one's own sessions),
  at one 30-second TOTP step boundary per ceremony. The grant surface is #55's
  to prove. Written before the first sign-in so it cannot invalidate a session.

### Traps worth knowing when changing this subsystem

1. **Every instance-scope surface is MFA-mandatory**, so the flow suite's
    bootstrap administrator cannot open the remotes page at all with a password.
    The fixture enrols a REAL TOTP factor and steps up
    (`web/e2e/fixtures/totp.ts`).
2. **A TOTP code is single-use PER STEP** (`last_step < ?`, strictly) and the
    validation window is only ±1 step wide, so two ceremonies inside the same 30
    seconds have no code that is both fresh and acceptable. The refusal is
    `unauthenticated`, indistinguishable from a wrong code, and it reads exactly
    like a clock skew that is not there (the server's `Date` header agrees with
    Node to the second, and both implementations generate identical codes for
    identical instants — checked against `pquerna/otp` directly). `presentTotp`
    walks steps 0..+2 and waits for a boundary between rounds.
3. **A killed flow run leaves a server holding the port**, and the next run's
    health probe cannot tell it from its own — the bootstrap then writes to a
    datastore nobody is serving and the first authenticated call answers 401,
    which reads like a credential bug. There is now a `portTaken` guard that
    fails loud, and `stopInstance` uses SIGKILL.
4. **`getByLabel('Origin')` matches the `aria-labelledby` REGION too.** Use
    `getByRole('textbox', { name: 'Origin' })`.

## Verification

```
gofmt -l .        # empty
go vet ./...      # clean
go build ./...    # success
go test ./...     # 1183 passed, 35 packages (sqlite)
HIKYO_TEST_POSTGRES_DSN=postgres://hikyo:hikyo@127.0.0.1:5432/hikyo_71d \
  go test -count=1 ./...   # 1702 passed, BOTH ENGINES
go tool sqlc generate                                              # clean
go tool oapi-codegen --config api/oapi-codegen.yaml api/openapi.yaml  # clean
(cd clients/ts && pnpm run generate && pnpm run typecheck && pnpm run test)  # 4 passed
(cd web && pnpm run typecheck && pnpm run test)                     # 20 passed
(cd web && pnpm run e2e --project=mobile)                           # 26 passed
(cd web && pnpm run e2e --project=desktop --grep "kill switches")   # PASSES
```

## The full-run crash, found and fixed

The first shape of the preflight fix — a catch-all `OPTIONS /api/v1/*` route
registered INSIDE the API group — made `pnpm run e2e` fail with
`net::ERR_CONNECTION_REFUSED` mid-suite. It also tripped three CI invariants at
once — classification totality, audit completeness and contract/router agreement
— all telling the truth: a route the contract does not describe has no business
existing.

The correct shape is not a route at all. `workspaceCORS` moved to the TOP of the
router's middleware chain (`r.Use`), where it runs on every request whether or
not one matches, and answers the preflight itself. No new route, no contract
entry, no invariant exemptions.

**Honesty note on the causal claim**: the route was removed and the full suite
went green TWICE in a row (52 passed both times), where it had failed with the
route. That is correlation plus a mechanism that is plausible but not proven —
a handler returning 204 has no obvious way to end a process. If
`ERR_CONNECTION_REFUSED` ever returns mid-suite, do not assume this was the
cause: read the instance's stderr for a panic first.

## Status against the six locked criteria

| # | State |
|---|---|
| 1 | **MET**, and now visible: the directory card renders state, identity, version, org/project names and counts, the `credential-rejected` loud state and the "showing the last known directory, N old" sentence, asserted by a Playwright flow against a real second instance. |
| 2 | **MET.** The popup lands on B's own origin — asserted by comparing the popup's origin against B's in the flow — and A's server originates nothing during the arc (`two_instance_test.go` measures `remotefetch.Dials()` across it). |
| 3 | **MET at every layer this ticket owns, with one caveat stated inline.** The handoff transaction, the popup ceremony, the workspace session, CORS, CSP and the atomic origin-removal kill switch are exercised end to end in a browser; the assurance record now travels (00021) and a step-up ELEVATES the bound session under a real reauthentication window instead of being refused by name, proven on BOTH engines in `workspace_stepup_test.go`. **Caveat: the elevation has no UI consumer yet** — the reveal surface that would call it is #50/#58's ticket, so "reveals under B's ceremony" is proven at the service layer and not in the browser. |
| 4 | **MET.** Unchanged. |
| 5 | **MET**, and now visible: the settings panel lists a workspace session as its own artifact type carrying its requesting origin, and revoking it there ends the workspace in the other instance's shell within one liveness poll — asserted in the flow. |
| 6 | **MET.** CI half (no-proxy closure over the pinned surface + the wire-registry half + live dial instrumentation) and harness half (`two_instance_test.go`) unchanged. The browser leg adds a third observation: the workspace ceremony completes while the viewing server cannot reach the serving one at all. |

## Left for the reviewer, not hidden

1. The two Playwright store-level seams (`repointRemoteAtB`, `seedDirectoryGrant`),
   both documented at their call sites with the reason.
2. The `InsertReauthWindow` upsert touches #54's locked machinery. It fixes a
   real latent fault for all four openers; it is worth a reviewer's eye.
3. The elevation ROTATES the workspace bearer. That is the same act the human
   step-up performs, and the redemption response carries the new value — but it
   means a shell that ignores the response loses its session.
4. `remote.workspace_session_expired` and `remote.auth_failed` remain
   unregistered for the reasons already recorded (no honest emitter).

## Cross-model review outcome

Reviewed by Codex (`gpt-5.6-sol`, high) R1-R3; findings fixed before merge. Five
parallel R1 passes returned 20 findings (7 HIGH, 13 MEDIUM), all fixed in-tree
and none deferred, each behavioural fix driven test-first. R2 closed the one
PARTIAL (p3-1) in full; R3 (the 3-round cap) returned SOUND, with one
bearer-custody item dispositioned in the flow (the popup's stores are snapshotted
at the last moment on B's origin, since the bearer is minted by the shell's
redemption only after the popup leaves B).

### R1 disposition

| # | Finding | Disposition |
|---|---|---|
| p1a-1 | ws bearer's origin binding not enforced at authn | FIXED |
| p1a-2 | 00021 fabricates `[]` assurance for live handoffs | FIXED |
| p1a-3 | `ic` decoy not work-uniform with revoked credentials | FIXED |
| p1a-4 | snapshot CHECK admits impossible rows; `ok` on the failure path | FIXED |
| p1b-1 | step-up does not require a fresh ceremony | FIXED |
| p1b-2 / p2-1 | operation + key-set binding stored, never enforced | FIXED |
| p1b-3 | `/me/sessions` reachable by `ws` and `ic` | FIXED, with one accepted deviation |
| p1b-4 | CI invariants blind to operation-less endpoints | FIXED |
| p1b-5 | audit actor guarantees not upheld or tested | FIXED |
| p2-2 | postgres READ COMMITTED races (redemption, `AddRemote`) | FIXED |
| p2-3 | round timeout fabricates unreachable results | FIXED |
| p2-4 | "instance-wide" budgets are process-local | FIXED (invariant enforced by documentation + ratification note) |
| p2-5 | plaintext proxies, proxy TLS uses the remote's pin, no wiring, `//api` | FIXED (four parts) |
| p3-1 | no-proxy closure bypassable by naming | FIXED |
| p3-2 | `remote add` permits plaintext, confirms a blank fingerprint | FIXED (claim verified first) |
| p3-3 | `ReadPassword` nil default; no interrupt-safe terminal restore | FIXED |
| p3-4 | `Vary: Origin` missing on denied / no-origin branches | FIXED |
| p3-5 | CSP `connect-src` admits `https://*.example` | FIXED |
| p4-1 | stale probes delete a replacement bearer | FIXED |
| p4-2 | liveness probe bypasses Zod, treats failure as success | FIXED |
| p4-3 | signed-out popup arrival never exercised | FIXED |
| p4-4 | `noopener` claimed but not asserted | FIXED |
| p4-5 | bearer-persistence assertion cannot prove memory-only custody | FIXED |

## Owner dispositions (2026-08-13)

1. **RESOLVED.** Owner ratified all nine bounds; `remotefetch/bounds.go` records the 2026-08-13 disposition.
2. **RESOLVED.** Owner agreed with the hard-short residual; workspace sessions now idle at 15 minutes and cap at 4 hours.
3. **RESOLVED.** The #58 ceremony seam now derives the matching authz operation from every reauthentication purpose, so a correctly bound workspace window is spendable by the real reveal path and a different operation still refuses.
4. **ACKNOWLEDGED — NO CHANGE.** The single-serving-process invariant remains documented at `fetchGate`; no distributed limiter was requested.
5. **ACKNOWLEDGED — NO CHANGE.** `remote.auth_failed` and `remote.workspace_session_expired` remain unregistered because neither has an honest emitter.

