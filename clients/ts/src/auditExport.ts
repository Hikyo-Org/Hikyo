import { z } from 'zod';

import { zAuditEvent, zAuditExportReceipt } from './generated/zod.gen.ts';
import { parseJson } from './parseJson.ts';

/** Shared with the Go consumer. Larger events require an explicit protocol change. */
export const AUDIT_EXPORT_MAX_LINE_BYTES = 1024 * 1024;

const integer = z.union([z.number().int().nonnegative().safe().transform(BigInt), z.bigint().nonnegative()])
  .pipe(z.bigint().max(9223372036854775807n));
const receiptSchema = zAuditExportReceipt.extend({
  started_event_id: z.string().min(1),
  rows_streamed: integer,
}).strict();
const receiptLineSchema = z.object({ export_receipt: receiptSchema }).strict();
// Event envelopes are extensible. Keep the original bytes in the destination.
const eventSchema = zAuditEvent.extend({ seq: integer, id: z.string().min(1) });

export type AuditExportReceipt = z.infer<typeof receiptSchema>;

/**
 * Stream into an UNCOMMITTED destination. On rejection discard it and retain the
 * last complete collection state. A successful callback is not completion proof.
 * Only the resolved receipt, after clean EOF, permits destination publication.
 */
export async function consumeAuditExport(
  response: Response,
  write: (line: Uint8Array<ArrayBuffer>) => void | Promise<void>,
): Promise<AuditExportReceipt> {
  const reader = response.body?.getReader();
  let complete = false;
  try {
    if (response.status !== 200 || response.headers.get('Hikyo-Audit-Receipt') !== 'v1' ||
      response.headers.get('Content-Type')?.split(';')[0]?.trim().toLowerCase() !== 'application/x-ndjson') {
      throw new Error('Audit export completion is unavailable. No verified export was saved.');
    }
    const id = response.headers.get('Hikyo-Audit-Export-ID');
    const snapshot = response.headers.get('Hikyo-Audit-Snapshot-Time');
    if (!id || !snapshot || !reader) throw new Error('Audit export identity or snapshot is missing.');
    // Allocate once. No repeated concatenation when the network splits one line
    // into tiny chunks, and no unbounded line or whole-response accumulator.
    const pending = new Uint8Array(AUDIT_EXPORT_MAX_LINE_BYTES);
    const decoder = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true });
    let used = 0;
    let rows = 0n;
    let receipt: AuditExportReceipt | undefined;
    for (;;) {
      const chunk = await reader.read();
      if (chunk.done) break;
      let offset = 0;
      while (offset < chunk.value.length) {
        if (receipt) throw new Error('Audit export has data after its terminal receipt.');
        const newline = chunk.value.indexOf(10, offset);
        const end = newline === -1 ? chunk.value.length : newline + 1;
        const part = chunk.value.subarray(offset, end);
        if (used + part.length > pending.length) throw new Error('Audit export line exceeds the 1 MiB limit.');
        pending.set(part, used);
        used += part.length;
        offset = end;
        if (newline === -1) continue;
        const bytes = pending.slice(0, used);
        const line = parseJson(decoder.decode(bytes.subarray(0, used - 1)));
        if (typeof line === 'object' && line !== null && 'export_receipt' in line) {
          receipt = receiptLineSchema.parse(line).export_receipt;
          if (receipt.started_event_id !== id || receipt.snapshot_time !== snapshot || receipt.rows_streamed !== rows) {
            throw new Error('Audit export receipt does not match its identity, snapshot or row count.');
          }
          if (receipt.outcome !== 'success' || !receipt.outcome_recorded || receipt.cause !== undefined) {
            throw new Error(`Audit export is incomplete (${receipt.outcome}). No verified export was saved.`);
          }
        } else {
          eventSchema.parse(line);
          await write(bytes);
          rows++;
        }
        used = 0;
      }
    }
    if (used !== 0 || !receipt) throw new Error('Audit export ended without a complete terminal receipt.');
    complete = true;
    return receipt;
  } finally {
    if (reader) {
      if (!complete) await reader.cancel().catch(() => undefined);
      reader.releaseLock();
    }
  }
}
