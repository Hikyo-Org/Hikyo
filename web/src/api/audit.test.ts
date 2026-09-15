import { describe, expect, it } from 'vitest';

import { auditExportUrl, emptyAuditFilter, type AuditScope } from './audit.ts';

const orgScope: AuditScope = { org: 'org_1' };

// auditExportUrl runs the same auditQuery the interactive pages send, so it is
// the public surface that pins filter serialization: empty fields drop, the
// outcomes SET repeats one param per value, and actor_name/globs pass through.
describe('auditExportUrl', () => {
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
