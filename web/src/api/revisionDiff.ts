import { diffRevisionsOp, revealRevisionDiffOp } from '@hikyo/operations';
import { zRevisionDiff } from '@hikyo/zod';
import { z } from 'zod';
import { parsed } from './client.ts';
import { revisionNumber } from './history.ts';
import type { EnvRef } from './keys.ts';
import { useSensitiveMutation } from './sensitiveMutation.ts';
import { useTransport } from './transport.tsx';

export type RevisionDiff = z.infer<typeof zRevisionDiff>;
export type RevisionDiffRow = RevisionDiff['items'][number];
export const zMaskedRevisionDiff = zRevisionDiff.superRefine((diff, context) => {
  diff.items.forEach((row, index) => {
    if (row.classification === 'secret' && (row.revealed || row.before !== undefined || row.after !== undefined || row.status === 'changed' || row.status === 'unchanged')) {
      context.addIssue({ code: 'custom', path: ['items', index], message: 'Secret diff rows must expose write-presence only.' });
    }
  });
});

/** Both calls are uncached: config values and disclosed secrets stay owned by
 * the open diff panel, with pending responses revoked on session retirement. */
export function useRevisionDiff(env: EnvRef, left: bigint, right: bigint) {
  const transport = useTransport();
  const body = { left_revision: revisionNumber(left), right_revision: revisionNumber(right) };
  const compare = useSensitiveMutation({ mutationFn: async () => zMaskedRevisionDiff.parse(await parsed(diffRevisionsOp, { path: { ...env }, body, ...transport })) });
  const reveal = useSensitiveMutation({ mutationFn: async (keyId: string) => {
    const result = await parsed(revealRevisionDiffOp, { path: { ...env }, body: { ...body, key_id: keyId }, ...transport });
    if (result.items.length !== 1 || result.items[0]?.key_id !== keyId || !result.items[0].revealed) throw new Error('The diff disclosure did not match the requested key.');
    return result.items[0];
  } });
  return { compare, reveal };
}
