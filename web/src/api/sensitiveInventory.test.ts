import { createHash } from 'node:crypto';
import { readFileSync, readdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { expect, it } from 'vitest';

import reviewed from './sensitiveInventory.json';

// Intentionally conservative: every mutation-capable source module is reviewed
// in full, including imported aliases and hand-built cache access. A new module
// or any edit needs an explicit sensitivity review before this pin is refreshed.
const mutationCapability = /\b(?:useMutation|useMutationState|MutationObserver|MutationCache|getMutationCache)\b/;
const sourceRoot = fileURLToPath(new URL('../', import.meta.url));
function inventory(root: string, prefix = ''): Record<string, string> {
  const result: Record<string, string> = {};
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    const file = `${prefix}${entry.name}`;
    if (entry.isDirectory()) Object.assign(result, inventory(`${root}/${entry.name}`, `${file}/`));
    else if (/\.tsx?$/.test(entry.name) && !/\.(?:test|d)\.tsx?$/.test(entry.name)) {
      const source = readFileSync(`${root}/${entry.name}`, 'utf8');
      if (mutationCapability.test(source)) result[file] = createHash('sha256').update(source).digest('hex');
    }
  }
  return result;
}

it('requires reviewed sensitivity inventory for every mutation-capable source', () => {
  expect(inventory(sourceRoot)).toEqual(reviewed.sources);
});
it('detects imported aliases and direct cache construction as requiring review', () => {
  for (const source of [
    "import { useMutation as save } from '@tanstack/react-query'",
    'queries.getMutationCache().build(queries, options)',
    'new MutationObserver(queries, options)',
    'new MutationCache()',
  ]) expect(mutationCapability.test(source)).toBe(true);
});
