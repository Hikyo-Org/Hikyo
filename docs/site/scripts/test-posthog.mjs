import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = resolve(fileURLToPath(new URL('../dist/', import.meta.url)));
const landingPage = await readFile(resolve(dist, 'index.html'), 'utf8');
const docsPage = await readFile(resolve(dist, 'docs/index.html'), 'utf8');

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
  assert.match(page, /data-posthog-consent/, 'analytics consent control is missing');
  assert.match(page, /Only this choice is saved/, 'consent persistence disclosure is missing');
  assert.match(page, /disable_persistence:\s*true/, 'browser persistence is not disabled');
  assert.match(page, /autocapture:\s*false/, 'autocapture is not disabled');
  assert.match(page, /disable_session_recording:\s*true/, 'session recording is not disabled');
  assert.match(page, /disable_surveys:\s*true/, 'surveys are not disabled');
  assert.match(page, /advanced_disable_flags:\s*true/, 'remote config and feature flags are not disabled');
  assert.match(page, /capture_exceptions:\s*false/, 'exception capture is not disabled');
}

console.log('PostHog gate: bootstrap and landing-page conversion events passed');
