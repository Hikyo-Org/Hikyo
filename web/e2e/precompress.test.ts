import { execFile } from 'node:child_process';
import { brotliDecompress, gunzip } from 'node:zlib';
import { mkdtemp, mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';

import { afterEach, expect, test } from 'vitest';

const run = promisify(execFile);
const decompressBrotli = promisify(brotliDecompress);
const decompressGzip = promisify(gunzip);
const script = fileURLToPath(new URL('../scripts/precompress.mjs', import.meta.url));
const fixtures: string[] = [];

afterEach(async () => {
  await Promise.all(fixtures.splice(0).map((fixture) => rm(fixture, { recursive: true })));
});

test('build precompresses the document and text assets without duplicating compressed fonts', async () => {
  const fixture = await mkdtemp(join(tmpdir(), 'hikyo-precompress-'));
  fixtures.push(fixture);
  await mkdir(join(fixture, 'assets'));
  const document = '<!doctype html>'.repeat(100);
  const javascript = 'export const value = "compress me";\n'.repeat(100);
  const font = Buffer.alloc(2048, 1);
  await Promise.all([
    writeFile(join(fixture, 'index.html'), document),
    writeFile(join(fixture, 'assets', 'app-deadbeef.js'), javascript),
    writeFile(join(fixture, 'assets', 'font-deadbeef.woff2'), font),
  ]);

  await run(process.execPath, [script, fixture]);

  await expect(decompressBrotli(await readFile(join(fixture, 'index.html.br')))).resolves.toEqual(Buffer.from(document));
  await expect(decompressGzip(await readFile(join(fixture, 'assets', 'app-deadbeef.js.gz')))).resolves.toEqual(Buffer.from(javascript));
  await expect(readFile(join(fixture, 'assets', 'font-deadbeef.woff2.br'))).rejects.toThrow();
});
