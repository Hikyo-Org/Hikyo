// Fails the build when hikyo.pen has drifted from tokens.css. CSS is the source of truth.
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import { compareTokens, parsePenVariables, parseTokensCss } from './lib.ts';

const here = (p: string) => resolve(import.meta.dirname, '../..', p);

const css = parseTokensCss(await readFile(here('src/styles/tokens.css'), 'utf8'));
const pen = parsePenVariables(await readFile(here('design/hikyo.pen'), 'utf8'));
const errors = compareTokens(css, pen);
if (errors.length > 0) {
  console.error('Design tokens drifted from tokens.css. Fix the CSS first, then run `pnpm run design:seed`.');
  for (const e of errors) console.error(`  ${e}`);
  process.exit(1);
}
console.log(`design tokens: ${pen.size} variables match tokens.css`);
