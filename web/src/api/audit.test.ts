import { afterEach, describe, expect, it, vi } from 'vitest';

import { auditExportUrl, downloadAuditExport, emptyAuditFilter, verifiedAuditExportBlob, type AuditScope } from './audit.ts';

afterEach(() => vi.unstubAllGlobals());

const orgScope: AuditScope = { org: 'org_1' };

// auditExportUrl runs the same auditQuery the interactive pages send, so it is
// the public surface that pins filter serialization: empty fields drop, the
// outcomes SET repeats one param per value, and actor_name/globs pass through.
describe('auditExportUrl', () => {
  it('addresses the instance trail explicitly without tenant path fields', () => {
    const url = auditExportUrl({ instance: true }, { ...emptyAuditFilter, operation: 'auth.*' });
    expect(url).toBe('/api/v1/instance/audit/export?operation=auth.*');
  });
  it('drops every empty field so an unset filter is a plain path', () => {
    expect(auditExportUrl(orgScope, emptyAuditFilter)).toBe('/api/v1/orgs/org_1/audit/export');
  });

  it('repeats the outcomes param once per selected value', () => {
    const url = auditExportUrl(orgScope, {
      ...emptyAuditFilter,
      outcomes: ['success', 'failure'],
    });
    const params = new URL(url, 'https://x').searchParams;
    expect(params.getAll('outcomes')).toEqual(['success', 'failure']);
  });

  it('emits a one-element set as a single outcomes param, never a scalar outcome', () => {
    const url = auditExportUrl(orgScope, { ...emptyAuditFilter, outcomes: ['denied'] });
    const params = new URL(url, 'https://x').searchParams;
    expect(params.getAll('outcomes')).toEqual(['denied']);
    expect(params.has('outcome')).toBe(false);
  });

  it('passes actor_name and the wildcard text fields through verbatim', () => {
    const url = auditExportUrl(orgScope, {
      ...emptyAuditFilter,
      actor: 'usr_1',
      actorName: 'Ada*',
      operation: 'value.*',
      objectType: 'ke*',
      objectId: 'key_*',
      correlationId: 'cor_1',
    });
    const params = new URL(url, 'https://x').searchParams;
    expect(params.get('actor')).toBe('usr_1');
    expect(params.get('actor_name')).toBe('Ada*');
    expect(params.get('operation')).toBe('value.*');
    expect(params.get('object_type')).toBe('ke*');
    expect(params.get('object_id')).toBe('key_*');
    expect(params.get('correlation_id')).toBe('cor_1');
  });

  it('scopes the export path to the project and environment when set', () => {
    const scoped: AuditScope = { org: 'org_1', project: 'proj_1', environment: 'env_1' };
    expect(auditExportUrl(scoped, emptyAuditFilter)).toBe(
      '/api/v1/orgs/org_1/projects/proj_1/environments/env_1/audit/export',
    );
  });
});

const snapshot = '2026-10-28T00:00:00Z';
const receiptHeaders = {
  'Content-Type': 'application/x-ndjson', 'Hikyo-Audit-Receipt': 'v1',
  'Hikyo-Audit-Export-ID': 'evt_download', 'Hikyo-Audit-Snapshot-Time': snapshot,
};
const emptyReceipt = JSON.stringify({ export_receipt: {
  version: 1, started_event_id: 'evt_download', snapshot_time: snapshot,
  rows_streamed: 0, outcome: 'success', outcome_recorded: true,
} }) + '\n';

describe('verified browser audit downloads', () => {
  it('negotiates on the URL and returns a Blob only after verified empty success', async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(new Response(emptyReceipt, { headers: receiptHeaders }));
    vi.stubGlobal('fetch', fetcher);
    const controller = new AbortController();
    const blob = await downloadAuditExport('/api/v1/orgs/org_1/audit/export?operation=grant.*', controller.signal);
    expect(blob.size).toBe(0);
    expect(blob.type).toBe('application/x-ndjson');
    const call = fetcher.mock.calls[0];
    expect(call).toBeDefined();
    if (call === undefined) throw new Error('fetch was not called');
    expect(call[0]).toBeInstanceOf(URL);
    expect(String(call[0])).toContain('receipt=v1');
    expect(String(call[0])).toContain('operation=grant.*');
    expect(call[1]).toMatchObject({ signal: controller.signal, credentials: 'same-origin', redirect: 'error', cache: 'no-store' });
  });

  it.each([
    new Response('', { headers: receiptHeaders }),
    new Response('{}\n', { headers: receiptHeaders }),
    new Response(emptyReceipt, { headers: { 'Content-Type': 'application/x-ndjson' } }),
    new Response(emptyReceipt.replace('"outcome":"success"', '"outcome":"revoked"'), { headers: receiptHeaders }),
    new Response(emptyReceipt.replace('"outcome_recorded":true', '"outcome_recorded":false'), { headers: receiptHeaders }),
    new Response(emptyReceipt, { status: 403, headers: receiptHeaders }),
  ])('rejects incomplete, uncertain and unnegotiated responses without a download', async (response) => {
    await expect(verifiedAuditExportBlob(response)).rejects.toThrow();
  });

  it('refuses off-origin URLs before sending credentials', async () => {
    const fetcher = vi.fn<typeof fetch>();
    vi.stubGlobal('fetch', fetcher);
    await expect(downloadAuditExport('https://other.invalid/audit/export')).rejects.toThrow('this server');
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('surfaces cancellation and transport failures', async () => {
    vi.stubGlobal('fetch', vi.fn<typeof fetch>().mockRejectedValue(new DOMException('Cancelled', 'AbortError')));
    await expect(downloadAuditExport('/audit/export')).rejects.toThrow('Cancelled');
  });
});
