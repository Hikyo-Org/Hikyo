# hikyo.app PostHog events dropped since the strict CSP change

## Result

PostHog was already installed on hikyo.app, but it had stopped recording. The
`before_send` allowlist from PR 720 (2026-09-10) removed `token`, so the SDK
dropped every consented event in the browser without sending a request.

The allowlist now keeps `token`, `distinct_id` and `$process_person_profile`. The
SDK enforces `token`, and the capture API requires `distinct_id`. Without
`$process_person_profile` the server would default to creating person profiles,
which the consent copy rules out.

## Evidence

- SDK debug log on the live site: `had its 'token' property removed in a beforeSend function. This property is required for ingestion, so the event will be dropped.`
- PostHog project 254133 holds 65 `$pageview` and 8 CTA events, the last on 2026-09-26. All 65 pageviews carry `$pathname`, `$host` and `$session_id`, which the allowlist strips, so no allowlisted event ever arrived. The late events came from older page copies, probably the PWA cache.
- `docs/site/scripts/check-analytics-ingestion.mjs` fails against the live site and passes against the live site with the fixed allowlist patched in. PostHog requests were answered locally, and the project shows no new events after these runs.
- Without consent, the live page makes no PostHog requests.

## Remaining work

1. Confirm hosted CI runs the new check. The local site build and full `pnpm --dir docs/site verify` did not run because the shared job queue was paused.
2. After deploy, accept analytics on hikyo.app and confirm a new `$pageview` in project 254133. No allowlisted event has reached PostHog yet, so this is the first proof that the server accepts them.

## Known limits

- Clicks and pageviews from 2026-09-10 until this deploys are not recoverable.
- `$pathname`, `$host` and `$session_id` stay stripped, so Web Analytics path and session views will not populate for new events. `$current_url` still identifies the page in product analytics. Each page load also has its own memory-only ids, so sessions cannot span pages.
- Returning visitors keep the broken build until their service worker updates.
- The new check loads the unpinned `array.js` from the PostHog host, the same file production loads. A PostHog outage or breaking SDK release therefore fails docs CI, which also blocks Pages and the release workflow until a rerun or fix.
- The check prevents CTA navigation, so the unload beacon path that real CTA clicks use is not covered.
