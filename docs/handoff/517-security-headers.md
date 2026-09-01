# Issue 517: proxy HSTS and browser security headers

## Outcome

- HSTS follows the configured external-origin scheme and remains disabled for
  browser-visible loopback hosts.
- Every public response carries popup-compatible COOP, same-origin CORP, and a
  minimal deny-list Permissions Policy.
- Operator docs and Helm guidance identify Hikyo as the HSTS owner by default
  and explain how to avoid conflicting proxy headers.

## Decisions

- `Cross-Origin-Opener-Policy` is `same-origin-allow-popups`, because the OIDC
  and workspace flows navigate popups across origins before returning through
  same-origin `BroadcastChannel` handoffs.
- `Permissions-Policy` denies camera, microphone, and geolocation only.
  WebAuthn features keep their browser-default same-origin availability.
- Hikyo emits `Strict-Transport-Security: max-age=31536000` when the validated
  external origin uses HTTPS and its browser-visible host is not loopback,
  regardless of whether TLS terminates natively or at a reverse proxy whose
  Hikyo backend binds to loopback.

## Review

- Native Codex round 1 found that the bind listener cannot decide HSTS for a
  browser-visible origin; fixed by gating on the external-origin host and
  covered with an application boot test for a same-host proxy.
- Round 2 found case and trailing-dot loopback aliases; fixed by normalizing
  the external-origin hostname before the loopback decision.
- Round 3 found browser-canonicalized legacy IPv4 aliases such as `127.1`;
  fixed with URL-Standard-compatible IPv4 parsing and regression controls. The
  three-round cap ended there, and parent verification covers the final fix.

## Verification

- `go test ./internal/config ./internal/server ./internal/app`
- `go test ./...`
- `pnpm --dir web run e2e` on the repository-pinned Node runtime; desktop and
  mobile claims include OIDC login and workspace popup handoffs.

## Delivery

Issue: <https://github.com/Hikyo-Org/Hikyo/issues/517>
