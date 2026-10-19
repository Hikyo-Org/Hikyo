import { existsSync } from 'node:fs';
import { readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';

import { buildSettingsInventory } from './settings-bundle-inventory.ts';

// Build with the production configuration and serving sidecars. Record emitted
// route ownership and static dependencies, without shipping an inventory in the SPA.
const target = process.argv[2];
if (target === undefined) throw new Error('usage: node scripts/measure-settings-bundles.ts <output.json>');
const dist = new URL('../../internal/webui/dist/', import.meta.url);
const chunks = await buildSettingsInventory({ write: true });
execFileSync(process.execPath, ['scripts/check-demo-isolation.mjs'], { stdio: 'inherit' });
execFileSync(process.execPath, ['scripts/precompress.mjs', fileURLToPath(dist)], { stdio: 'inherit' });
async function sidecarBytes(file: string) {
  const url = new URL(file, dist);
  return existsSync(url) ? (await readFile(url)).length : null;
}
const measured = await Promise.all(chunks.map(async (chunk) => ({
  ...chunk,
  rawBytes: (await readFile(new URL(chunk.file, dist))).length,
  gzipBytes: await sidecarBytes(`${chunk.file}.gz`),
  brotliBytes: await sidecarBytes(`${chunk.file}.br`),
})));
await writeFile(target, JSON.stringify(measured, null, 2) + '\n');
