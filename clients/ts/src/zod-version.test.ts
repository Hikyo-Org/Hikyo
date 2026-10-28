import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { z } from 'zod';

const clientManifest = z.object({ devDependencies: z.object({ zod: z.string() }) });
const webManifest = z.object({ dependencies: z.object({ zod: z.string() }) });

test('generated client and web consumer pin the same Zod version', () => {
  const client = clientManifest.parse(
    JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8')),
  );
  const web = webManifest.parse(
    JSON.parse(readFileSync(new URL('../../../web/package.json', import.meta.url), 'utf8')),
  );

  // Web code passes generated client schemas to its own Zod APIs. Different
  // versions force the compiler to compare incompatible internal schema types.
  assert.equal(client.devDependencies.zod, web.dependencies.zod);
});
