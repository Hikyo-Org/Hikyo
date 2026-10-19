import { relative } from 'node:path';
import { fileURLToPath } from 'node:url';

import { build, type Plugin } from 'vite';

export interface SettingsChunk {
  file: string;
  imports: string[];
  dynamicImports: string[];
  routes: string[];
}

/** Capture ownership from the production build without adding a shipped asset. */
export async function buildSettingsInventory(options: { write: boolean }): Promise<SettingsChunk[]> {
  const repo = fileURLToPath(new URL('../../', import.meta.url));
  const root = fileURLToPath(new URL('../', import.meta.url));
  const routes = fileURLToPath(new URL('../src/routes/', import.meta.url));
  const chunks: SettingsChunk[] = [];
  const inventory: Plugin = {
    name: 'settings-bundle-measurement',
    generateBundle(_options, bundle) {
      for (const item of Object.values(bundle)) {
        if (item.type !== 'chunk') continue;
        chunks.push({
          file: item.fileName,
          imports: item.imports,
          dynamicImports: item.dynamicImports,
          routes: Object.keys(item.modules).filter((id) => id.startsWith(routes)).map((id) => relative(repo, id)),
        });
      }
    },
  };
  await build({ root, plugins: [inventory], build: { write: options.write } });
  return chunks;
}

export function assertSettingsRouteIsolation(
  fetched: readonly string[],
  chunks: readonly SettingsChunk[],
  addressed: string,
  settings: readonly string[],
): void {
  if (!fetched.some((file) => file.startsWith(`/assets/${addressed}-`))) {
    throw new Error(`Missing addressed settings entry: ${addressed}`);
  }
  for (const other of settings) {
    if (other !== addressed && fetched.some((file) => file.startsWith(`/assets/${other}-`))) {
      throw new Error(`Fetched unrelated settings entry: ${other}`);
    }
  }
  const byFile = new Map(chunks.map((chunk): [string, SettingsChunk] => [`/${chunk.file}`, chunk]));
  const owners = new Map(settings.map((name): [string, string] => [`web/src/routes/${name}.tsx`, name]));
  let addressedFound = false;
  for (const file of fetched) {
    const chunk = byFile.get(file);
    if (chunk === undefined) throw new Error(`Fetched asset absent from current production inventory: ${file}`);
    for (const route of chunk.routes) {
      const owner = owners.get(route);
      if (owner === addressed) addressedFound = true;
      else if (owner !== undefined) throw new Error(`Fetched unrelated settings module ${owner} in ${file}`);
    }
  }
  if (!addressedFound) throw new Error(`No module ownership for addressed settings route: ${addressed}`);
}
