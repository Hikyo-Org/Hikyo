// Writes or refreshes the `variables` block of hikyo.pen from tokens.css.
// Direction is always CSS → design. Everything else in the .pen is preserved.
import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { z } from 'zod';

import { cssToPenVariables, parseTokensCss, type PenVariable } from './lib.ts';

const here = (p: string) => resolve(import.meta.dirname, '../..', p);
const PEN = here('design/hikyo.pen');
const CSS = here('src/styles/tokens.css');

// `children` is unknown on purpose: this script does not interpret nodes, it round-trips them untouched.
const penShell = z.looseObject({
  version: z.string(),
  children: z.array(z.unknown()),
  themes: z.record(z.string(), z.array(z.string())),
});

const css = parseTokensCss(await readFile(CSS, 'utf8'));
// Only the read is guarded: a missing .pen is seeded from scratch, but a malformed one must always throw.
let existing: string | undefined;
try {
  existing = await readFile(PEN, 'utf8');
} catch (error) {
  if (!(error instanceof Error && 'code' in error && error.code === 'ENOENT')) throw error;
}
const doc: z.infer<typeof penShell> & { variables?: Record<string, PenVariable> } =
  existing === undefined
    ? { version: '2.8', children: [], themes: { Mode: ['Light', 'Dark'] } }
    : penShell.parse(JSON.parse(existing));
const variables = cssToPenVariables(css);
doc.themes = { ...doc.themes, Mode: ['Light', 'Dark'] };
doc.variables = variables;
await writeFile(PEN, `${JSON.stringify(doc, null, 2)}\n`);
console.log(`hikyo.pen: ${Object.keys(variables).length} variables written from tokens.css`);
