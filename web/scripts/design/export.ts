// For every design('Title/Variant') in a story, render that node of
// hikyo.pen to design/exports/<slug>.png with the headless CLI. Unresolved
// node = exit 1: a story must not point at a design that no longer exists.
import { execFile } from 'node:child_process';
import { glob, mkdir, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { promisify } from 'node:util';

import { collectDesignNodes, findDuplicateNodeIds, parseQueryOutput, slugFor } from './lib.ts';

const run = promisify(execFile);
const here = (p: string) => resolve(import.meta.dirname, '../..', p);
const PEN = here('design/hikyo.pen');
const OUT = here('design/exports');
const CLI = here('node_modules/.bin/openpencil');

// Same set of story files Storybook loads: keep in step with `stories` in .storybook/main.ts.
const files: string[] = [];
for await (const f of glob('src/**/*.stories.{js,jsx,mjs,ts,tsx}', { cwd: here('.') })) files.push(here(f));
const nodes = collectDesignNodes(await Promise.all(files.map(async (f) => ({ path: f, text: await readFile(f, 'utf8') }))));
await mkdir(OUT, { recursive: true });

// The reader registers nodes by id, so a subtree pasted as a sibling without
// fresh ids aliases the original's entries and renders the wrong node. Refuse
// the document before any export can make that look like a design.
const duplicateIds = findDuplicateNodeIds(await readFile(PEN, 'utf8'));
if (duplicateIds.length > 0) {
  console.error(`hikyo.pen: duplicate node ids ${duplicateIds.join(', ')}; every node needs a unique id (see the design-loop skill on copying a component)`);
  process.exit(1);
}

// `find` truncates at --limit, so a run that comes back full may have hidden a
// duplicate past the cap and uniqueness can no longer be proven: that fails too.
const LIMIT = 1000;

const failures: string[] = [];
for (const node of nodes) {
  // `query` (XPath) is unusable in @open-pencil/cli 0.14.0 and 0.15.0: its CJS
  // default-export interop bug throws (`evaluateXPathToNodes is not a function`)
  // before the selector runs. So this uses `find`, whose --name
  // is a case-insensitive substring: the exact filter below is what makes the
  // match, and the duplicate count, correct.
  const { stdout } = await run(CLI, ['find', PEN, '--name', node, '--limit', String(LIMIT), '--json']);
  const found = parseQueryOutput(stdout);
  if (found.length >= LIMIT) {
    failures.push(`${node}: find hit the ${LIMIT}-result cap, uniqueness cannot be proven`);
    continue;
  }
  const [match, ...rest] = found.filter((m) => m.name === node);
  if (match === undefined || rest.length > 0) {
    failures.push(`${node}: ${match === undefined ? 'not found' : `${rest.length + 1} nodes share this name`} in hikyo.pen`);
    continue;
  }
  const out = resolve(OUT, `${slugFor(node)}.png`);
  // `--font-policy strict`: a font the renderer would have to substitute fails
  // the export (the CLI's default, `warn`, only writes to stderr and exits 0,
  // and a wrong-font design image is worse than no build). Anything the CLI
  // still says on stderr is passed on, never dropped with the child result.
  try {
    const { stderr } = await run(CLI, ['export', PEN, '--node', match.id, '-s', '2', '-o', out, '--font-policy', 'strict']);
    if (stderr.trim() !== '') console.error(stderr.trim());
  } catch (error) {
    failures.push(`${node}: export failed${error instanceof Error ? `: ${error.message.trim()}` : ''}`);
    continue;
  }
  console.log(`design: ${node} -> ${out}`);
}
if (failures.length > 0) {
  console.error('Design export failed:');
  for (const f of failures) console.error(`  ${f}`);
  process.exit(1);
}
console.log(`design: ${nodes.length} node(s) exported`);
