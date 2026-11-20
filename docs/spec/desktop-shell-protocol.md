# Desktop shell protocol (`desktop-app/1`)

The wire contract between the instance SPA in app mode and the desktop shell
([ADR](../adr/desktop-app.md), D8 and D9). The SPA side lives in
`web/src/app/shell.ts`; the shell side lives in `desktop/`. Both sides change
together, and a change that breaks either side needs a new capability token.

## Detection and capability

- App mode is `location.protocol === 'wails:'`. No browser serves that scheme.
- An instance advertises `desktop-app/1` in `GET /api/v1/meta`
  `protocol_capabilities` only when it serves the embedded SPA. The shell reads it
  after trust confirmation and before sign-in, and refuses an instance without it.

## Routes

All routes are same-origin to the window, answered by the shell, and never
forwarded. Requests and responses are JSON (`Content-Type: application/json`),
with unknown fields refused on both sides. Errors use one body:

```json
{ "error": "<code>", "message": "<human text>" }
```

Error codes: `refused` (the shell will not do this; for a reauth the SPA offers
"Continue in browser"), `busy` (one already running), `rate_limited`,
`cancelled` (the user cancelled, nothing happened), `failed` (the hop failed;
retryable), `invalid` (malformed request).

| Route | Request | Success |
|---|---|---|
| `POST /__shell/reauth` | `{ "purpose", "operation", "environment_ids": [ID], "key_ids"?: [ID], "self_config"?: SelfConfigReauthIntent }` | `204` once the browser ceremony was approved and redeemed |
| `POST /__shell/sign-in` | `{}` | `204` once the connection has a live session again |
| `POST /__shell/open-in-browser` | `{ "path": "/orgs/..?q=1" }` | `204` |
| `POST /__shell/download` | `{ "path": "/api/v1/...", "filename": "audit.csv" }` or `{ "bytes_base64": "...", "filename": "...", "content_type": "..." }` | `204` (saved) or `cancelled` |
| `POST /__shell/clipboard` | `{}` | `204` |
| `GET /__shell/origin` | none | `{ "origin": "https://hikyo.example" }` |

### Field rules

- `reauth`: `purpose` and `operation` take the values of `CLIReauthStartRequest`
  in `api/openapi.yaml`. `developer-credential` is `refused`. `key_ids` holds at
  most 500 entries (the handoff's limit for non-adapter purposes); a larger unit
  is `refused` by the SPA before it calls the shell. `adapter` sends the whole
  environment set in one call and no `key_ids`. `self-config` carries
  `self_config` and an empty `environment_ids`, because the handoff refuses a
  self-config request that names an environment (`internal/server/cli_reauth.go`);
  every other purpose names at least one environment and carries no `self_config`.
  The shell forwards `self_config` to the handoff unchanged. Its `revision` and
  `expected_generation` are JSON numbers, as in the handoff.
- `reauth` operations by purpose, as the SPA sends them: `reveal` is
  `value.reveal`, `copy` is `value.copy-source`, `publish` is
  `value.copy-destination`, `approve` and `reject` are `approval.vote`, `bypass` is
  `approval.bypass`, `adapter` is one of `adapter.*`, and `self-config` is
  `self-config.adopt` (action `adopt`), `self-config.apply` (`apply`,
  `rollout-restore`) or `self-config.test` (`mail-test`). `mint` and `access` have
  no handoff purpose, so the SPA never sends them: it offers "Continue in browser".
- The SPA sends the same `reauth` request from the passkey button and from the
  identity-provider button, and the far end offers the factors the person holds.
  TOTP never reaches the shell.
- `open-in-browser`: `path` is an absolute path with optional query, never a
  URL. The shell refuses `/login/cli`, `/reauth/cli`, `/workspace/approve`, any
  `returnTo` parameter and non-canonical input, as ADR D9 lists.
- `download`: `path` must start with `/api/v1/` and obeys the proxy's refusals.
  `bytes_base64` carries a body the SPA built itself. `filename` is advisory; the
  shell sanitizes it and always shows a save panel.
- `clipboard`: called right after the SPA writes a secret to the clipboard. The
  shell arms clearing only if it observed a pasteboard change. The SPA shows its
  own copy confirmation and does not wait on the clearing.
- `origin`: the SPA reads it once, before it renders anything, and refuses to
  start if the shell does not answer. The answer must be a bare origin (scheme and
  host, no path), or the SPA treats the reply as `failed`.

## Session signals

When the SPA sees a dead session (`401` from `/api/v1`, confirmed by `whoami`), it
calls `POST /__shell/sign-in` instead of routing to `/login`, then reloads the
current route on `204`. It shares one call among concurrent callers. After the
person signs out in this window, the SPA waits for a click before it calls
`sign-in`, so signing out does not immediately reopen the browser. The SPA never runs the login form, enrolment, passkey creation or
identity linking in app mode; those offer "Continue in browser", which calls `open-in-browser` with the current
page. A session confined until enrolment lands on the bare path `/login`, never
`/login?returnTo=...`, because the shell refuses any `returnTo`.

The SPA treats a reply as `failed` when the status is not the one in the table
(`204`, or `200` for `origin`) or the body is outside the contract, whatever the
shell meant by it.
