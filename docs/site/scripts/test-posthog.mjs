import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = resolve(fileURLToPath(new URL('../dist/', import.meta.url)));
const landingPage = await readFile(resolve(dist, 'index.html'), 'utf8');
const docsPage = await readFile(resolve(dist, 'docs/index.html'), 'utf8');
// The bootstrap now ships as a hoisted, minified module, so `data-posthog-host`
// on the rendered consent element (never emitted on disabled builds) is the
// authoritative "analytics is on" signal. The scoped consent stylesheet inlines
// even when the component is not rendered, so a bare `data-posthog-consent`
// substring is not a reliable gate.
const postHogBuilt = /data-posthog-host=/.test(landingPage);

const demoLinks = Array.from(landingPage.matchAll(/<a\b[^>]*data-posthog-event="demo_cta_clicked"[^>]*>/g), (match) => match[0]);
assert.equal(demoLinks.length, 2, 'hero and final demo CTAs must both emit conversion events');
// The home link is emitted from Astro's configured BASE_URL, independently of
// the demo destination. Preserve deployments beneath a path as well as `/`.
const homeLink = landingPage.match(/<a\b[^>]*aria-label="Hikyo home"[^>]*>/)?.[0];
const siteBase = homeLink?.match(/\shref="([^"]+)"/)?.[1];
assert.ok(siteBase?.startsWith('/') && siteBase.endsWith('/'), 'configured site base is missing');
for (const link of demoLinks) {
  assert.equal(link.match(/\shref="([^"]+)"/)?.[1], `${siteBase}demo/`, 'demo CTA must open the configured demo destination');
}
for (const placement of ['hero', 'final']) {
  assert.equal(
    demoLinks.filter((link) => link.includes(`data-posthog-placement="${placement}"`)).length,
    1,
    `${placement} demo CTA must identify its placement`,
  );
}
const editLink = docsPage.match(/<div\b[^>]*class="docs-edit-link"[^>]*>/)?.[0];
assert.ok(editLink, 'documentation edit link is missing');
assert.match(editLink, /data-posthog-event="docs_edit_on_github_clicked"/, 'documentation edit event is missing');
assert.match(editLink, /data-posthog-placement="docs_edit"/, 'documentation edit link must identify its placement');

if (!postHogBuilt) {
  assert.notEqual(process.env.POSTHOG_REQUIRED, 'true', 'required PostHog build omitted analytics');
  assert.doesNotMatch(landingPage, /\/static\/array\.js/, 'disabled analytics still ships the PostHog bootstrap');
  assert.doesNotMatch(docsPage, /data-posthog-host=/, 'documentation analytics differs from landing page');
  console.log('PostHog gate: analytics disabled because deploy configuration is not required');
  process.exit(0);
}

// Minification rewrites `true`/`false` to `!0`/`!1`, so accept either form.
const enabled = (key) => new RegExp(`${key}:\\s*(?:true|!0)\\b`);
const disabled = (key) => new RegExp(`${key}:\\s*(?:false|!1)\\b`);

assert.match(landingPage, /\/static\/array\.js/, 'landing page does not initialize PostHog');
assert.match(docsPage, /\/static\/array\.js/, 'documentation pages do not initialize PostHog');
assert.match(landingPage, /data-posthog-event="github_cta_clicked"/, 'GitHub CTA event is missing');
assert.match(
  landingPage,
  /data-posthog-event="documentation_cta_clicked"/,
  'documentation CTA event is missing',
);
assert.doesNotMatch(landingPage, /your_posthog_project_token_here/, 'PostHog token is still a placeholder');
for (const page of [landingPage, docsPage]) {
  assert.match(page, /data-posthog-host=/, 'analytics consent control is missing');
  assert.match(page, /Only this choice is saved/, 'consent persistence disclosure is missing');
  assert.match(page, enabled('disable_persistence'), 'browser persistence is not disabled');
  assert.match(page, disabled('autocapture'), 'autocapture is not disabled');
  assert.match(page, enabled('disable_session_recording'), 'session recording is not disabled');
  assert.match(page, enabled('disable_surveys'), 'surveys are not disabled');
  assert.match(page, enabled('advanced_disable_flags'), 'remote config and feature flags are not disabled');
  assert.match(page, disabled('capture_exceptions'), 'exception capture is not disabled');
}

console.log('PostHog gate: bootstrap and landing-page conversion events passed');
