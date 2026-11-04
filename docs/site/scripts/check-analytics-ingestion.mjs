import assert from 'node:assert/strict';
import { gunzipSync } from 'node:zlib';

// The SDK drops events before before_send when the user agent, client hint brands
// or navigator.webdriver look automated, so headless Chromium must clear all three.
const userAgent = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36';

function decodeEvents(request) {
  const body = request.postDataBuffer();
  if (!body) return [];
  // The SDK picks its encoding at runtime and its URL need not declare gzip, so
  // read the body itself: gzip magic bytes, a base64 `data=` form, or plain JSON.
  const text = body[0] === 0x1f && body[1] === 0x8b ? gunzipSync(body).toString('utf8') : body.toString('utf8');
  const data = text.startsWith('data=') ? new URLSearchParams(text).get('data') : null;
  const parsed = JSON.parse(data ? Buffer.from(data, 'base64').toString('utf8') : text);
  return [parsed.batch ?? parsed].flat();
}

/**
 * Accept consent and read what the real SDK would send. A before_send that drops
 * a required property fails silently in the browser, so only the outgoing request
 * proves ingestion. The loader comes from the network; nothing reaches PostHog
 * because every other request to its origin is answered here.
 */
export async function checkAnalyticsIngestion(browser, origin) {
  const context = await browser.newContext({ userAgent, serviceWorkers: 'block' });
  try {
    await context.addInitScript(() => {
      Object.defineProperty(navigator, 'webdriver', { get: () => false });
      Object.defineProperty(navigator, 'userAgentData', { get: () => undefined });
      // Routes do not intercept beacons, so an unload flush would reach PostHog.
      Object.defineProperty(navigator, 'sendBeacon', { value: () => true });
    });
    const page = await context.newPage();
    // The query and hash must not reach PostHog through any URL field.
    await page.goto(`${origin}/?utm_source=ingestion-check#hash`, { waitUntil: 'domcontentloaded' });
    const consent = page.locator('[data-posthog-consent]');
    if (await consent.count() === 0) {
      assert.notEqual(process.env.POSTHOG_REQUIRED, 'true', 'required analytics consent is missing');
      console.log('analytics ingestion: skipped because this build disables analytics');
      return;
    }
    const apiKey = await consent.getAttribute('data-posthog-key');
    const apiHost = await consent.getAttribute('data-posthog-host');
    assert.ok(apiKey && apiHost, 'analytics consent must carry its key and host');

    const events = [];
    await context.route(`${apiHost}/**`, async (route) => {
      const request = route.request();
      if (new URL(request.url()).pathname === '/static/array.js') return route.continue();
      events.push(...decodeEvents(request));
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        headers: { 'access-control-allow-origin': '*' },
        body: '{"status":"Ok"}',
      });
    });

    await page.getByRole('button', { name: 'Allow analytics', exact: true }).click();
    await assert.doesNotReject(
      page.waitForFunction(() => window.posthog?.__loaded === true, null, { timeout: 15_000 }),
      'PostHog SDK did not load after consent',
    );
    await page.evaluate(() => {
      document.addEventListener('click', (event) => event.preventDefault(), { once: true });
      document.querySelector('[data-posthog-event="documentation_cta_clicked"][data-posthog-placement="hero"]')?.click();
    });

    const deadline = Date.now() + 15_000;
    const find = (name) => events.find((event) => event.event === name);
    while (Date.now() < deadline && !(find('$pageview') && find('documentation_cta_clicked'))) {
      await page.waitForTimeout(250);
    }

    const pageview = find('$pageview');
    const click = find('documentation_cta_clicked');
    assert.ok(pageview, 'consented visit must send $pageview');
    assert.ok(click, 'consented CTA click must send its event');
    assert.equal(click.properties.placement, 'hero', 'CTA event must keep its placement');
    assert.equal(pageview.properties.placement, undefined, 'pageview must not carry a placement');
    for (const event of events) {
      const { placement: _placement, ...properties } = event.properties;
      assert.ok(properties.distinct_id, 'event must carry a distinct_id');
      assert.equal(event.$set, undefined, 'event must not set person properties');
      assert.equal(event.$set_once, undefined, 'event must not set person properties');
      // Every event received during this visit must carry exactly this set.
      assert.deepEqual(properties, {
        $current_url: `${origin}/`,
        $geoip_disable: true,
        $host: new URL(origin).host,
        $pathname: '/',
        $process_person_profile: false,
        distinct_id: properties.distinct_id,
        token: apiKey,
      }, 'event must carry the required fields and nothing else');
    }
    console.log('analytics ingestion: consented pageview and CTA events carry required fields and nothing extra');
  } finally {
    await context.close();
  }
}
