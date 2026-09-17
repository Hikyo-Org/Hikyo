// Fails the build when route markup hand-writes what a web/src/ui atom emits.
// See scripts/design/markup.ts for the banned list and the allowlist rule.
import { readdir, readFile } from 'node:fs/promises';
import { join, relative, resolve } from 'node:path';

import { scanMarkup } from './markup.ts';

const web = resolve(import.meta.dirname, '../..');
const root = join(web, 'src');

let failed = false;
for (const dir of ['routes', 'app']) {
  const entries = await readdir(join(root, dir), { recursive: true, withFileTypes: true });
  for (const entry of entries) {
    const { name } = entry;
    if (!entry.isFile() || !name.endsWith('.tsx')) continue;
    if (name.includes('.test.') || name.includes('.stories.')) continue;
    const path = join(entry.parentPath, name);
    for (const hit of scanMarkup((await readFile(path, 'utf8')).split('\n'))) {
      failed = true;
      const where = `${relative(web, path)}:${hit.line}`;
      if (hit.atom === undefined) console.error(`${where}: ${hit.note}: ${hit.text}`);
      else console.error(`${where}: hand-written markup, use ${hit.atom}: ${hit.text}`);
    }
  }
}
if (failed) process.exit(1);
console.log('markup-check: routes and app use the ui/ atoms');
