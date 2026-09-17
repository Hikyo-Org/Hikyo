// Fails the build when a stylesheet states more literal sizes than its
// budget allows. The budget is a ratchet: it can only go down. ui.css (the
// design system's own stylesheet) is held at zero; app.css carries its
// legacy count until the migration series (#761, #762) retires the rules.
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { z } from 'zod';

import { scanRawSizes } from './adherence.ts';

const here = (p: string) => resolve(import.meta.dirname, '../..', p);
const budgets = z.record(z.string(), z.number().int().nonnegative()).parse(
  JSON.parse(await readFile(here('scripts/design/adherence-budget.json'), 'utf8')),
);

let failed = false;
for (const [file, budget] of Object.entries(budgets)) {
  const found = scanRawSizes(await readFile(here(file), 'utf8'));
  if (found.length > budget) {
    failed = true;
    console.error(`${file}: ${found.length} literal sizes, budget ${budget}. Use a token from src/styles/tokens.css:`);
    for (const r of found.slice(0, 40)) console.error(`  ${file}:${r.line}  ${r.property}: ${r.value}`);
    if (found.length > 40) console.error(`  … ${found.length - 40} more`);
  } else if (found.length < budget) {
    console.log(`${file}: ${found.length} literal sizes, budget ${budget}. Lower the budget in scripts/design/adherence-budget.json so it cannot creep back.`);
  } else {
    console.log(`${file}: ${found.length} literal sizes, at budget`);
  }
}
if (failed) process.exit(1);
