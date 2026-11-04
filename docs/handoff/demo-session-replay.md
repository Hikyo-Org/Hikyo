# Session replay on the hosted demo

## Result

The hosted demo at hikyo.app/demo can record sessions in PostHog project 254133, after
the visitor allows it in the demo toolbar. The normal application stays free of
analytics. Branch: `feat/demo-session-replay`.

- `web/src/demo/replay.ts` holds consent, SDK loading and masking. It is imported only
  behind literal `MODE === 'demo'` checks.
- `posthog-js` is pinned and bundled, so `script-src 'self'` is unchanged.
  `connect-src` allows only the ingestion and assets hosts, and only in builds given a
  host and token.
- `web/scripts/check-demo-isolation.mjs` fails the normal build if PostHog or the
  recorder reaches its assets.

## Decisions

- Replay has its own consent, because the landing page choice promises no session
  recording. Without consent, or with a decline, the SDK is never downloaded.
- Typed values are masked wherever they are rendered again, because input masking alone
  does not cover a published value shown as cell text.
- Events other than recordings use an allowlist, as the landing page does, so `$referrer`
  is not sent. Query parameters other than `account` are removed from the page address
  at startup, whatever the consent state, because the recorder expands in-page links to full URLs and
  the SDK's URL masking does not reach those. Stopping replaces the tab's ids with unlinked ones.
- `advanced_disable_flags` stays off, because it also disables the remote config that
  starts recording.
- The SDK uses `fetch` through a narrow adapter exception. XHR logged a browser error
  per request, because PostHog does not expose `Retry-After` to the page.

## Remaining work

1. Pass the merge queue, then confirm the deployed demo's `connect-src` lists the two
   PostHog hosts.
2. After deploy, allow recording on hikyo.app/demo, interact, and open the recording in
   PostHog. The SDK uploads nothing until the visitor interacts after recording starts.
3. Check the player renders the demo's styles and fonts from hikyo.app.

## Known limits

- Only visitors who opt in are recorded. Expect partial coverage.
- Two test recordings from `http://127.0.0.1:5197/demo/` remain in the project from the
  pre-merge check.
- A second recording in one page load has no `$pageview`, because only the first load
  captures one.
- Recording settings are shared with the project. `session_recording_opt_in` is on, and
  the landing page stays out through its own `disable_session_recording`.
- Consent has no expiry. A visitor who allowed recording stays opted in on that browser
  until they stop it or clear site data.
- The fetch adapter and CSP allow the PostHog hosts even for visitors who declined. No
  code uses them then, and `script-src 'self'` stands.
- A visitor who pastes a value and has it rendered in a transformed form, such as split
  across elements, is not matched by the typed value mask.
- The chunk is 694 kB, or 210 kB gzipped, and only consenting visitors download it.
