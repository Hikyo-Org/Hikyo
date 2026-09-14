import { readFile } from 'node:fs/promises';
import { posix } from 'node:path';
import { fileURLToPath } from 'node:url';
import { z } from 'zod';

const assetGraphSchema = z.record(z.string(), z.array(z.string()))
  .refine((graph) => Object.keys(graph).length > 0, 'PWA asset graph must not be empty');

// Astro builds CSS in the prerender environment and scripts in the client
// environment. Capture both graphs before Astro discards its Vite manifests.
// Generate the worker in the same build hook, so stale graph files cannot prune
// a later build and no private metadata gets published or precached.
export function pwaAssetGraph() {
  const graph = new Map();
  const stylesheets = new Map();
  return {
    name: 'hikyo:pwa-asset-graph',
    hooks: {
      'astro:build:start': () => {
        graph.clear();
        stylesheets.clear();
      },
      'astro:config:setup': ({ updateConfig }) => {
        updateConfig({
          vite: {
            plugins: [{
              name: 'hikyo:pwa-asset-graph',
              apply: 'build',
              generateBundle: {
                order: 'post',
                handler(_options, bundle) {
                  for (const output of Object.values(bundle)) {
                    const dependencies = graph.get(output.fileName) ?? new Set();
                    if (output.type === 'chunk') {
                      for (const file of [...output.imports, ...output.dynamicImports, ...(output.referencedFiles ?? [])]) {
                        dependencies.add(file);
                      }
                    }
                    for (const file of output.viteMetadata?.importedCss ?? []) dependencies.add(file);
                    for (const file of output.viteMetadata?.importedAssets ?? []) dependencies.add(file);
                    graph.set(output.fileName, dependencies);
                    if (output.type === 'asset' && output.fileName.endsWith('.css')) {
                      stylesheets.set(output.fileName, typeof output.source === 'string'
                        ? output.source : new TextDecoder().decode(output.source));
                    }
                  }
                },
              },
            }],
          },
        });
      },
      'astro:build:done': async ({ dir }) => {
        const assets = Object.fromEntries(
          [...graph].map(([file, dependencies]) => [file, [...dependencies]]),
        );
        // Vite attaches CSS font/image metadata to server chunks, which HTML
        // never references. Connect the emitted CSS to its actual asset URLs
        // as well, including relative URLs, without inferring ownership from
        // font families, entry names, or a particular hashing convention.
        for (const [file, css] of stylesheets) {
          assets[file].push(...referencedAssets(assets, [css]));
        }
        const { buildPwa } = await import('./build-pwa.mjs');
        await buildPwa(dir, assets);
      },
    },
  };
}

export function isPrototype(file) {
  return /^(?:prototype|prototypes)\//.test(file);
}

export function referencedAssets(graph, documents) {
  // Match emitted filenames conservatively wherever Astro serializes them:
  // href/src/srcset, island component-url/renderer-url, and inline module imports.
  // The build graph supplies the filenames, so this needs no hashed-name guesses
  // and accepts root-relative, relative, and base-prefixed URLs. A mention in
  // prose can retain an extra asset; it cannot remove a needed dependency.
  return Object.keys(graph).filter((file) => {
    const name = posix.basename(file);
    return documents.some((source) => source.includes(name) || source.includes(encodeURI(name)));
  });
}

export function productionAssets(graph, htmlDocuments) {
  const reachable = new Set();
  const pending = referencedAssets(graph, htmlDocuments);
  while (pending.length > 0) {
    const file = pending.pop();
    if (reachable.has(file)) continue;
    reachable.add(file);
    for (const dependency of graph[file] ?? []) pending.push(dependency);
  }
  return reachable;
}

export async function productionManifest(manifest, dist, graph) {
  const validatedGraph = assetGraphSchema.parse(graph);
  const production = manifest.filter(({ url }) => !isPrototype(url));
  const htmlDocuments = await Promise.all(production
    .filter(({ url }) => url.endsWith('.html'))
    .map(({ url }) => readFile(fileURLToPath(new URL(url, dist)), 'utf8')));
  const reachable = productionAssets(validatedGraph, htmlDocuments);
  // Public files and complete offline docs/search stay in the precache. Only
  // emitted assets proven unreachable from every production page are removed.
  return production.filter(({ url }) => !Object.hasOwn(validatedGraph, url) || reachable.has(url));
}
