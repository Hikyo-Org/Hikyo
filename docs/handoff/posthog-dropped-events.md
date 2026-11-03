# hikyo.app PostHog events dropped since the strict CSP change

## Result

PostHog was already installed on hikyo.app, but it had stopped recording. The
`before_send` allowlist from PR 720 (2026-09-10) removed `token`, so the SDK
dropped every consented event in the browser without sending a request.

PR 1147 made the allowlist keep `token`, `distinct_id` and
`$process_person_profile`. The SDK enforces `token`, and the capture API requires
`distinct_id`. Without `$process_person_profile` the server would default to
creating person profiles, which the consent copy rules out. It merged on
2026-11-02 at 23:35 UTC, and the verification visit's events reached the project.

A follow-up on branch `fix/posthog-web-analytics-fields` adds `$host` and
`$pathname`, built from `location` so they carry no query or hash, to fill the Web
Analytics page table.

## Evidence

- SDK debug log on the old live site: `had its 'token' property removed in a beforeSend function. This property is required for ingestion, so the event will be dropped.`
- Before PR 1147, PostHog project 254133 held 65 `$pageview` and 8 CTA events, the last on 2026-09-26. All 65 pageviews carried `$pathname`, `$host` and `$session_id`, which the allowlist stripped, so no allowlisted event had arrived. The late events came from older page copies, probably the PWA cache.
- The `docs` workflow on the PR 1147 merge commit ran `docs/site/scripts/check-analytics-ingestion.mjs` with `POSTHOG_REQUIRED=true` and passed.
- A consented visit to the live site at 23:49 UTC on 2026-11-02 stored a `$pageview`, a hero `documentation_cta_clicked` sent during real navigation, and a `$pageview` for `/docs/`. No PostHog requests came before consent, and no person profiles were created. The visit hid its automation signals to pass the SDK bot filter.
- Two labelled test pageviews sent directly to the capture API at 00:08 UTC on 2026-11-03 compared Web Analytics behaviour. Both appeared in the page table. The overview counted only the one with a `$session_id`, and showed it as a 100% bounce with 0 session duration.
- For the follow-up, the updated check fails against the live site and passes with the two fields patched into the live HTML, with its Content-Security-Policy removed. The site has not been built with the change locally, because the shared job queue is unavailable.

## Remaining work

1. Confirm the follow-up passes the `docs` domain in the merge queue, which is its first real build.
2. After it deploys, make a consented visit and run a Web Analytics page breakdown (`query-web-stats` with `breakdownBy: Page`) on a date range not queried in the last few minutes. An earlier empty result stayed cached after the events were stored. The visited path should appear as a row.
3. Confirm that events arrive from real visitors.

## Known limits

- Clicks and pageviews from 2026-09-10 until PR 1147 deployed are not recoverable.
- `$session_id` stays stripped for now, so the Web Analytics overview (visitors, views, sessions) does not count new events. Keeping it should fill the overview, but each page load has its own memory-only ids, so every visit would be expected to read as a one-page session with a 100% bounce.
- Unique visitors in product analytics really means page loads, for the same reason.
- Test events remain in the project: the three from the verification visit, plus `/__analytics-probe-no-session/` and `/__analytics-probe-with-session/`.
- Returning visitors keep the older build until their service worker updates.
- The check loads the unpinned `array.js` from the PostHog host, the same file production loads. A PostHog outage or breaking SDK release therefore fails docs CI, which also blocks Pages and the release workflow until a rerun or fix.
- The check prevents CTA navigation, so CI does not cover the unload path that real CTA clicks use. The live visit above exercised it once.
