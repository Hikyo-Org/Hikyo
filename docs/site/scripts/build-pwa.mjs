import { fileURLToPath } from 'node:url';
import { generateSW } from 'workbox-build';
import { productionManifest } from './pwa-asset-graph.mjs';

export async function buildPwa(distUrl, graph) {
  const dist = fileURLToPath(distUrl);
  const { count, size, warnings } = await generateSW({
    cleanupOutdatedCaches: true,
    clientsClaim: true,
    globDirectory: dist,
    globIgnores: ['sw.js', 'prototypes/**/*', 'prototype/**/*'],
    globPatterns: ['**/*.{css,html,js,json,png,svg,txt,webmanifest,woff,woff2}'],
    manifestTransforms: [async (manifest) => ({
      manifest: await productionManifest(manifest, distUrl, graph),
      warnings: [],
    })],
    ignoreURLParametersMatching: [/^deployment$/, /^utm_/, /^fbclid$/],
    inlineWorkboxRuntime: true,
    // The complete docs search index exceeds Workbox's 2 MiB default. Keep
    // offline search available, with an explicit ceiling and fatal warnings.
    maximumFileSizeToCacheInBytes: 3 * 1024 * 1024,
    skipWaiting: true,
    sourcemap: false,
    swDest: fileURLToPath(new URL('sw.js', distUrl)),
  });

  if (warnings.length > 0) {
    throw new Error(`PWA build warnings:\n${warnings.join('\n')}`);
  }
  if (count === 0) {
    throw new Error('PWA build produced an empty precache manifest');
  }

  console.log(`PWA precached ${count} files (${size} bytes)`);
}
