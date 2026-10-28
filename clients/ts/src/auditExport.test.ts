import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import test from 'node:test';

import { AUDIT_EXPORT_MAX_LINE_BYTES, consumeAuditExport } from './auditExport.ts';

const snapshot = '2026-10-28T00:00:00Z';
const headers = {
  'Content-Type': 'application/x-ndjson', 'Hikyo-Audit-Receipt': 'v1',
  'Hikyo-Audit-Export-ID': 'evt_export', 'Hikyo-Audit-Snapshot-Time': snapshot,
};
const event = '{"seq":9223372036854775807,"id":"evt_1","type":"future.event","schema_version":1,"occurred_at":"2026-01-01T00:00:00Z","occurred_asserted":false,"recorded_at":"2026-01-01T00:00:00Z","actor_class":"system","scope_class":"instance","outcome":"success","origin":"system","payload":{"message":"café"},"future_field":true}\n';
const receipt = (rows = 1) => JSON.stringify({ export_receipt: {
  version: 1, started_event_id: 'evt_export', snapshot_time: snapshot,
  rows_streamed: rows, outcome: 'success', outcome_recorded: true,
} }) + '\n';

test('accepts empty and multipage exports, preserves exact event bytes and int64', async () => {
  for (const count of [0, 1, 1101]) {
    const staged: Uint8Array[] = [];
    const result = await consumeAuditExport(new Response(event.repeat(count) + receipt(count), { headers }), (line) => {
      staged.push(line);
    });
    assert.equal(result.rows_streamed, BigInt(count));
    assert.equal(Buffer.concat(staged).toString(), event.repeat(count));
  }
});

test('validates across one-byte chunks, including split UTF-8', async () => {
  const bytes = new TextEncoder().encode(event + receipt());
  let offset = 0;
  const body = new ReadableStream({ pull(controller) {
    if (offset === bytes.length) controller.close();
    else controller.enqueue(bytes.slice(offset, ++offset));
  } });
  const staged: Uint8Array[] = [];
  await consumeAuditExport(new Response(body, { headers }), (line) => { staged.push(line); });
  assert.equal(Buffer.concat(staged).toString(), event);
});

for (const [name, body] of Object.entries({
  partial: event, empty: '', missingLF: event + receipt().trimEnd(), count: event + receipt(2),
  identity: event + receipt().replace('evt_export', 'evt_other'),
  snapshot: event + receipt().replace(snapshot, '2026-10-27T00:00:00Z'),
  failure: event + receipt().replace('"outcome":"success"', '"outcome":"failure"'),
  disconnected: event + receipt().replace('"outcome":"success"', '"outcome":"disconnected"'),
  revoked: event + receipt().replace('"outcome":"success"', '"outcome":"revoked"'),
  uncertain: event + receipt().replace('"outcome_recorded":true', '"outcome_recorded":false'),
  cause: event + receipt().replace('"version":1', '"cause":"outcome-write-failed","version":1'),
  duplicate: event + receipt() + receipt(), after: event + receipt() + event,
  blank: event + receipt() + '\n', invalid: '{}\n' + receipt(),
  missing: event + receipt().replace('"version":1,', ''),
  coercedCount: event + receipt().replace('"rows_streamed":1', '"rows_streamed":true'),
  oversized: ' '.repeat(AUDIT_EXPORT_MAX_LINE_BYTES) + event + receipt(),
})) {
  test(`refuses ${name} and retains prior collection state`, async () => {
    let published = 'previous complete export';
    const staged: Uint8Array[] = [];
    await assert.rejects(async () => {
      await consumeAuditExport(new Response(body, { headers }), (line) => { staged.push(line); });
      published = Buffer.concat(staged).toString();
    });
    assert.equal(published, 'previous complete export');
  });
}

test('refuses an unnegotiated old server before staging data and cancels its body', async () => {
  let cancelled = false;
  const body = new ReadableStream({ cancel() { cancelled = true; } });
  let wrote = false;
  await assert.rejects(consumeAuditExport(new Response(body), () => { wrote = true; }));
  assert.equal(wrote, false);
  assert.equal(cancelled, true);
});

test('requires clean EOF even after a success receipt', async () => {
  let reads = 0;
  const body = new ReadableStream({ pull(controller) {
    if (reads++ === 0) controller.enqueue(new TextEncoder().encode(event + receipt()));
    else controller.error(new Error('connection reset'));
  } });
  await assert.rejects(consumeAuditExport(new Response(body, { headers }), () => undefined), /connection reset/);
});

test('propagates staging errors and cancels unread transport', async () => {
  let cancelled = false;
  const body = new ReadableStream({ start(controller) {
    controller.enqueue(new TextEncoder().encode(event));
  }, cancel() { cancelled = true; } });
  await assert.rejects(consumeAuditExport(new Response(body, { headers }), () => { throw new Error('disk full'); }), /disk full/);
  assert.equal(cancelled, true);
});

test('actual HTTP 200 ending after valid JSONL is rejected; terminal success is accepted', async () => {
  const server = createServer((req, res) => {
    res.writeHead(200, headers);
    res.end(event + (req.url === '/complete' ? receipt() : ''));
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  try {
    const address = server.address();
    assert.ok(address && typeof address === 'object');
    const origin = `http://127.0.0.1:${address.port}`;
    await assert.rejects(consumeAuditExport(await fetch(origin + '/partial'), () => undefined), /terminal receipt/);
    assert.equal((await consumeAuditExport(await fetch(origin + '/complete'), () => undefined)).rows_streamed, 1n);
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) => server.close((err) => err ? reject(err) : resolve()));
  }
});
