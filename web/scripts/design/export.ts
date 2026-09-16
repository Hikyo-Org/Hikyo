// For every design('Title/Variant') in a story, render that node of
// hikyo.pen to design/exports/<slug>.png with the headless CLI. Unresolved
// node = exit 1: a story must not point at a design that no longer exists.
import { execFile } from 'node:child_process';
import { glob, mkdir, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { promisify } from 'node:util';

import { collectDesignNodes, parseQueryOutput, slugFor } from './lib.ts';

const run = promisify(execFile);
const here = (p: string) => resolve(import.meta.dirname, '../..', p);
const PEN = here('design/hikyo.pen');
const OUT = here('design/exports');
const CLI = here('node_modules/.bin/openpencil');

// Same set of story files Storybook loads: keep in step with `stories` in .storybook/main.ts.
const files: string[] = [];
for await (const f of glob('src/**/*.stories.{js,jsx,mjs,ts,tsx}', { cwd: here('.') })) files.push(here(f));
const nodes = collectDesignNodes(await Promise.all(files.map((f) => readFile(f, 'utf8'))));
await mkdir(OUT, { recursive: true });

const failures: string[] = [];
for (const node of nodes) {
  // `query` (XPath) throws in @open-pencil/cli 0.14.0 because fontoxpath is not shipped with it,
  // so this uses `find`, whose --name is a case-insensitive substring: the exact filter below
  // is what makes the match, and the duplicate count, correct.
  const { stdout } = await run(CLI, ['find', PEN, '--name', node, '--limit', '1000', '--json']);
  const matches = parseQueryOutput(stdout).filter((m) => m.name === node);
  const match = matches[0];
  if (matches.length !== 1 || match === undefined) {
    failures.push(`${node}: ${matches.length === 0 ? 'not found' : `${matches.length} nodes share this name`} in hikyo.pen`);
    continue;
  }
  const out = resolve(OUT, `${slugFor(node)}.png`);
  await run(CLI, ['export', PEN, '--node', match.id, '-s', '2', '-o', out]);
  console.log(`design: ${node} -> ${out}`);
}
if (failures.length > 0) {
  console.error('Design export failed:');
  for (const f of failures) console.error(`  ${f}`);
  process.exit(1);
}
console.log(`design: ${nodes.length} node(s) exported`);
