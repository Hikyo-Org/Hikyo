import { readdir, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

const assets = resolve(import.meta.dirname, '../../internal/webui/dist/assets');
const forbidden = /DEMO_ONLY_|browser-demo|DemoToolbar|DemoSurface|Sample data only/;
for (const file of await readdir(assets)) {
  if (!file.endsWith('.js')) continue;
  if (forbidden.test(await readFile(resolve(assets, file), 'utf8'))) {
    throw new Error(`Demo implementation leaked into the normal app build: ${file}`);
  }
}
console.log('Normal app build excludes the demo API, fixtures and UI.');
