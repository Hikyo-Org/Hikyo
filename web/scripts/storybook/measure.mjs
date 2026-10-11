// Separate the app's initial static import closure from its lazy chunks and
// from the optional catalogue. Run against immutable before/after directories.
import { readFile, readdir } from 'node:fs/promises';
import { dirname, extname, join, resolve } from 'node:path';
import { gzipSync } from 'node:zlib';
import { z } from 'zod';
import { initialAssets } from './initial-assets.ts';
import { staticImports } from './static-imports.ts';

async function files(directory) {
  const result = [];
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    result.push(...(entry.isDirectory() ? await files(path) : [path]));
  }
  return result;
}

async function size(paths) {
  let bytes = 0;
  let gzipBytes = 0;
  for (const path of paths) {
    const data = await readFile(path);
    bytes += data.length;
    gzipBytes += gzipSync(data, { level: 9 }).length;
  }
  return { files: paths.length, bytes, gzipBytes };
}

async function measure(appDirectory, storybookDirectory) {
  const app = resolve(appDirectory);
  const catalogue = resolve(storybookDirectory);
  const html = await readFile(join(app, 'index.html'), 'utf8');
  const initial = new Set();
  async function visit(path) {
    if (initial.has(path)) return;
    initial.add(path);
    if (extname(path) !== '.js') return;
    const source = await readFile(path, 'utf8');
    // Static imports only. Dynamic import() belongs to lazy output, not startup.
    for (const dependency of staticImports(source)) {
      await visit(resolve(dirname(path), dependency));
    }
  }
  for (const path of await initialAssets(html)) {
    await visit(resolve(app, path.replace(/^\//, '')));
  }
  const appFiles = await files(app);
  const catalogueFiles = await files(catalogue);
  const index = z.object({ entries: z.record(z.string(), z.object({ type: z.enum(['story', 'docs']) })) }).parse(
    JSON.parse(await readFile(join(catalogue, 'index.json'), 'utf8')),
  );
  return {
    appInitialJS: await size([...initial].filter((path) => extname(path) === '.js')),
    appInitialCSS: await size([...initial].filter((path) => extname(path) === '.css')),
    appTotalJS: await size(appFiles.filter((path) => extname(path) === '.js')),
    appTotalCSS: await size(appFiles.filter((path) => extname(path) === '.css')),
    storybookJS: await size(catalogueFiles.filter((path) => extname(path) === '.js')),
    storybookCSS: await size(catalogueFiles.filter((path) => extname(path) === '.css')),
    storybookAssets: await size(catalogueFiles.filter((path) => !/\.(?:js|css)$/.test(path))),
    stories: Object.values(index.entries).filter((entry) => entry.type === 'story').length,
    docs: Object.values(index.entries).filter((entry) => entry.type === 'docs').length,
  };
}

const [appDirectory, storybookDirectory] = process.argv.slice(2);
if (!appDirectory || !storybookDirectory) throw new Error('Usage: node scripts/storybook/measure.mjs APP_BUILD STORYBOOK_BUILD');
console.log(JSON.stringify(await measure(appDirectory, storybookDirectory), null, 2));
