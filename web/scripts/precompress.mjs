import { readdir, readFile, writeFile } from 'node:fs/promises';
import { extname, join, resolve } from 'node:path';
import { promisify } from 'node:util';
import { brotliCompress, constants, gzip } from 'node:zlib';

const compressBrotli = promisify(brotliCompress);
const compressGzip = promisify(gzip);
const compressibleExtensions = new Set(['.css', '.html', '.js', '.json', '.map', '.svg', '.txt', '.webmanifest', '.xml']);

async function filesBelow(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = await Promise.all(entries.map(async (entry) => {
    const name = join(directory, entry.name);
    return entry.isDirectory() ? filesBelow(name) : [name];
  }));
  return files.flat();
}

async function writeSmallerSidecar(file, suffix, compressed) {
  const source = await readFile(file);
  const output = await compressed(source);
  if (output.length < source.length) {
    await writeFile(file + suffix, output);
    return source.length - output.length;
  }
  return 0;
}

const outputDirectory = process.argv[2];
if (outputDirectory === undefined) {
  throw new Error('usage: node scripts/precompress.mjs <vite-output-directory>');
}

const root = resolve(outputDirectory);
const files = [join(root, 'index.html'), ...await filesBelow(join(root, 'assets'))]
  .filter((file) => compressibleExtensions.has(extname(file)));
const savings = await Promise.all(files.flatMap((file) => [
  writeSmallerSidecar(file, '.br', (source) => compressBrotli(source, {
    params: { [constants.BROTLI_PARAM_QUALITY]: 11 },
  })),
  writeSmallerSidecar(file, '.gz', (source) => compressGzip(source, { level: 9 })),
]));
const written = savings.filter((saved) => saved > 0);
console.log(`precompressed ${written.length} representations; saved ${written.reduce((total, saved) => total + saved, 0)} bytes`);
