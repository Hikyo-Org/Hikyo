import assert from 'node:assert/strict';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import { test } from 'node:test';
import { productionAssets, productionManifest, referencedAssets } from './pwa-asset-graph.mjs';

const graph = {
  '_astro/docs.js': ['_astro/shared.js', '_astro/search.js', '_astro/docs.css'],
  '_astro/shared.js': ['_astro/shared.css'],
  '_astro/search.js': ['_astro/search-worker.js'],
  '_astro/search-worker.js': [],
  '_astro/docs.css': ['_astro/docs-font.woff2'],
  '_astro/shared.css': ['_astro/shared-font.woff2'],
  '_astro/docs-font.woff2': [],
  '_astro/shared-font.woff2': [],
  '_astro/prototype.js': ['_astro/shared.js', '_astro/prototype.css'],
  '_astro/prototype.css': ['_astro/prototype-font.woff2'],
  '_astro/prototype-font.woff2': [],
};

test('all production islands, dynamic imports, shared CSS and fonts remain reachable at a base path', () => {
  const reachable = productionAssets(graph, [
    '<astro-island component-url="/manual/_astro/docs.js"></astro-island>',
  ]);
  assert.deepEqual([...reachable].sort(), [
    '_astro/docs.js', '_astro/docs.css', '_astro/docs-font.woff2',
    '_astro/search.js', '_astro/search-worker.js', '_astro/shared.js',
    '_astro/shared.css', '_astro/shared-font.woff2',
  ].sort());
});

test('a production reference preserves an asset even when its source was a prototype', () => {
  const reachable = productionAssets(graph, ['<link href="../_astro/prototype.css">']);
  assert.ok(reachable.has('_astro/prototype.css'));
  assert.ok(reachable.has('_astro/prototype-font.woff2'));
  assert.ok(!reachable.has('_astro/prototype.js'));
});

test('cyclic chunk imports terminate and keep every dependency', () => {
  assert.deepEqual([...productionAssets({ 'a.js': ['b.js'], 'b.js': ['a.js'] }, ['"a.js"'])].sort(), ['a.js', 'b.js']);
});

test('pruning keeps unvisited docs, search, public assets, and shared chunks but excludes both prototype families', async () => {
  const dir = await mkdtemp(join(tmpdir(), 'hikyo-pwa-assets-'));
  const dist = pathToFileURL(`${dir}/`);
  try {
    await mkdir(join(dir, 'docs', 'unvisited'), { recursive: true });
    await writeFile(join(dir, 'index.html'), '<h1>Landing</h1>');
    await writeFile(join(dir, 'docs', 'unvisited', 'index.html'), '<astro-island component-url="/manual/_astro/docs.js"></astro-island>');
    const publicFiles = ['index.html', 'docs/unvisited/index.html', 'api/search.json', 'manifest.webmanifest', 'pwa-512x512.png'];
    const manifest = [...publicFiles, ...Object.keys(graph), 'prototypes/index.html', 'prototype/index.html']
      .map((url) => ({ url, revision: `revision:${url}` }));
    const result = await productionManifest(manifest, dist, graph);
    assert.ok(publicFiles.every((file) => result.some(({ url }) => url === file)));
    assert.ok(result.some(({ url }) => url === '_astro/search-worker.js'));
    assert.ok(result.some(({ url }) => url === '_astro/shared-font.woff2'));
    assert.ok(!result.some(({ url }) => url.includes('prototype')));
    assert.ok(result.every(({ url, revision }) => revision === `revision:${url}`), 'Workbox revision metadata must survive filtering');
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test('missing, empty, or malformed in-memory graph fails instead of silently changing precaching', async () => {
  await assert.rejects(productionManifest([], new URL('file:///unused/'), undefined));
  await assert.rejects(productionManifest([], new URL('file:///unused/'), {}), /must not be empty/);
  await assert.rejects(productionManifest([], new URL('file:///unused/'), { 'entry.js': 'not-a-dependency-array' }));
});

test('emitted CSS retains assets with relative, base-prefixed and encoded URLs', () => {
  const assets = { 'assets/one.woff2': [], 'assets/two.woff2': [], 'assets/three space.svg': [], 'assets/unused.woff2': [] };
  const css = '@font-face{src:url(./one.woff2)} @font-face{src:url("/manual/assets/two.woff2")} .icon{background:url(three%20space.svg)}';
  assert.deepEqual(referencedAssets(assets, [css]), ['assets/one.woff2', 'assets/two.woff2', 'assets/three space.svg']);
});
