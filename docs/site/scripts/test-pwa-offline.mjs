import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { extname, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from '@playwright/test';

const dist = resolve(fileURLToPath(new URL('../dist/', import.meta.url)));
const contentTypes = new Map([
  ['.css', 'text/css; charset=utf-8'],
  ['.html', 'text/html; charset=utf-8'],
  ['.js', 'text/javascript; charset=utf-8'],
  ['.json', 'application/json; charset=utf-8'],
  ['.png', 'image/png'],
  ['.svg', 'image/svg+xml'],
  ['.txt', 'text/plain; charset=utf-8'],
  ['.webmanifest', 'application/manifest+json; charset=utf-8'],
  ['.woff', 'font/woff'],
  ['.woff2', 'font/woff2'],
]);

const server = createServer(async (request, response) => {
  try {
    const pathname = decodeURIComponent(new URL(request.url ?? '/', 'http://localhost').pathname);
    if (!pathname.endsWith('/') && extname(pathname) === '') {
      const directoryIndexPath = resolve(dist, `.${pathname}/index.html`);
      if (!directoryIndexPath.startsWith(`${dist}${sep}`)) {
        response.writeHead(403).end('Forbidden');
        return;
      }
      try {
        await readFile(directoryIndexPath);
        response.writeHead(301, { Location: `${pathname}/` }).end();
        return;
      } catch (error) {
        const code = error instanceof Error && 'code' in error ? error.code : undefined;
        if (code !== 'ENOENT') throw error;
      }
    }

    const assetPath = pathname.endsWith('/') ? `${pathname}index.html` : pathname;
    const filePath = resolve(dist, `.${assetPath}`);
    if (!filePath.startsWith(`${dist}${sep}`)) {
      response.writeHead(403).end('Forbidden');
      return;
    }

    const body = await readFile(filePath);
    response.setHeader('Content-Type', contentTypes.get(extname(filePath)) ?? 'application/octet-stream');
    response.setHeader('Cache-Control', 'no-store');
    response.writeHead(200).end(body);
  } catch (error) {
    const code = error instanceof Error && 'code' in error ? error.code : undefined;
    response.writeHead(code === 'ENOENT' ? 404 : 500).end(code === 'ENOENT' ? 'Not found' : 'Server error');
  }
});

function listen() {
  return new Promise((resolveListen, rejectListen) => {
    server.once('error', rejectListen);
    server.listen(0, '127.0.0.1', resolveListen);
  });
}

function closeServer() {
  if (!server.listening) return Promise.resolve();
  return new Promise((resolveClose, rejectClose) => {
    server.close((error) => error ? rejectClose(error) : resolveClose());
  });
}

await listen();
const address = server.address();
if (address === null || typeof address === 'string') {
  throw new Error('PWA test server did not expose a TCP port');
}

const origin = `http://127.0.0.1:${address.port}`;
let browser;

try {
  browser = await chromium.launch();
  const context = await browser.newContext({ serviceWorkers: 'allow' });
  const page = await context.newPage();

  const onlineResponse = await page.goto(`${origin}/docs/getting-started/`, {
    waitUntil: 'networkidle',
  });
  assert.ok(onlineResponse?.ok(), 'online route did not load successfully');
  await page.evaluate(async () => {
    await Promise.race([
      navigator.serviceWorker.ready,
      new Promise((_, rejectReady) => {
        window.setTimeout(
          () => rejectReady(new Error('service worker registration did not become ready')),
          10_000,
        );
      }),
    ]);
    if (navigator.serviceWorker.controller) return;
    await new Promise((resolveControl, rejectControl) => {
      const timeout = window.setTimeout(
        () => rejectControl(new Error('service worker did not control the page')),
        10_000,
      );
      navigator.serviceWorker.addEventListener('controllerchange', () => {
        window.clearTimeout(timeout);
        resolveControl();
      }, { once: true });
    });
  });

  const slashlessDocsResponse = await page.goto(`${origin}/docs/getting-started`, {
    waitUntil: 'networkidle',
  });
  assert.ok(slashlessDocsResponse?.ok(), 'slashless docs route did not load successfully');
  assert.equal(
    await page.title(),
    'Getting started — Hikyo',
    'service worker navigation fallback replaced the slashless docs route',
  );

  const prototypeResponse = await page.goto(`${origin}/prototypes/`, {
    waitUntil: 'networkidle',
  });
  assert.ok(prototypeResponse?.ok(), 'online prototype hub did not load successfully');
  assert.equal(
    await page.title(),
    'Hikyo prototypes',
    'service worker navigation fallback replaced the prototype hub',
  );

  const generatedPrototype = await page.goto(`${origin}/prototype/hikyo/11/`, { waitUntil: 'networkidle' });
  assert.ok(generatedPrototype?.ok(), 'generated prototype must remain available online');
  assert.ok(await page.locator('h1').isVisible(), 'generated prototype did not render online');
  const prototypeStyles = await page.locator('link[rel="stylesheet"]').evaluateAll((links) =>
    links.map((link) => link.getAttribute('href')).filter((href) => href !== null));
  const cachedPrototypeStyles = await page.evaluate(async (styles) => {
    const urls = new Set();
    for (const name of await caches.keys()) {
      for (const request of await (await caches.open(name)).keys()) urls.add(new URL(request.url).pathname);
    }
    return styles.filter((style) => urls.has(new URL(style, location.href).pathname));
  }, prototypeStyles);
  assert.ok(cachedPrototypeStyles.length > 0, 'production CSS shared with a prototype must remain precached');
  assert.ok(cachedPrototypeStyles.length < prototypeStyles.length, 'prototype-only emitted CSS leaked into the precache');

  const cacheNames = await page.evaluate(() => caches.keys());
  assert.ok(cacheNames.length > 0, 'service worker created no runtime cache');

  // Check emitted CSS independently of the build graph. A graph missing
  // CSS-to-font edges can otherwise pass HTML/title checks with fallback fonts.
  const missingCssAssets = await page.evaluate(async () => {
    const cacheNames = await caches.keys();
    const cachedUrls = new Set();
    for (const name of cacheNames) {
      for (const request of await (await caches.open(name)).keys()) {
        const url = new URL(request.url);
        cachedUrls.add(`${url.origin}${url.pathname}`);
      }
    }
    const missing = [];
    for (const url of cachedUrls) {
      if (!url.endsWith('.css')) continue;
      const css = await (await fetch(url)).text();
      for (const match of css.matchAll(/url\(\s*['"]?([^'"\s)]+)['"]?\s*\)/g)) {
        const asset = new URL(match[1], url);
        if (asset.origin === location.origin && !cachedUrls.has(`${asset.origin}${asset.pathname}`)) {
          missing.push(asset.pathname);
        }
      }
    }
    return missing;
  });
  assert.deepEqual(missingCssAssets, [], 'a precached stylesheet references an uncached font or image');

  await context.setOffline(true);
  await closeServer();
  const offlineFailures = [];
  page.on('requestfailed', (request) => offlineFailures.push(request.url()));
  await page.goto(`${origin}/docs/architecture/`, { waitUntil: 'domcontentloaded' });
  assert.equal(await page.title(), 'Architecture — Hikyo');
  assert.match(await page.locator('body').innerText(), /Architecture/);
  await page.evaluate(() => document.fonts.ready);
  await page.waitForFunction(() => document.querySelector('astro-island[client="load"][ssr]') === null);
  const offlineSearchAvailable = await page.evaluate(async () => {
    const response = await fetch('/api/search.json');
    return response.ok && (await response.text()).length > 0;
  });
  assert.ok(offlineSearchAvailable, 'complete docs search index must remain available offline');
  await page.keyboard.press('ControlOrMeta+k');
  await page.getByRole('dialog').getByPlaceholder('Search').fill('configuration');
  await page.getByRole('dialog').locator('button[aria-selected]').first().waitFor();
  assert.deepEqual(offlineFailures, [], 'offline docs hydration, styles and fonts must not fall back to the network');

  await context.close();
  console.log('docs PWA browser gate: unvisited precached route loaded offline');
} finally {
  await browser?.close();
  await closeServer();
}
