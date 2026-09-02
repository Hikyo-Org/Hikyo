import { describe, expect, it } from 'vitest';

import { auditExportUrl, emptyAuditFilter, type AuditScope } from './audit.ts';

describe('auditExportUrl', () => {
  const org: AuditScope = { kind: 'org', org: 'org_1' };
  const project: AuditScope = { kind: 'project', org: 'org_1', project: 'prj_1' };
  const env: AuditScope = { kind: 'env', org: 'org_1', project: 'prj_1', environment: 'env_1' };

  it('names the export path of each scope', () => {
    expect(auditExportUrl(org, emptyAuditFilter)).toBe('/api/v1/orgs/org_1/audit/export');
    expect(auditExportUrl(project, emptyAuditFilter)).toBe(
      '/api/v1/orgs/org_1/projects/prj_1/audit/export',
    );
    expect(auditExportUrl(env, emptyAuditFilter)).toBe(
      '/api/v1/orgs/org_1/projects/prj_1/environments/env_1/audit/export',
    );
  });

  it('drops empty filter fields and adds no query string when the filter is empty', () => {
    expect(auditExportUrl(project, emptyAuditFilter)).not.toContain('?');
    const url = auditExportUrl(project, { ...emptyAuditFilter, outcome: 'denied', actor: '' });
    expect(url).toBe('/api/v1/orgs/org_1/projects/prj_1/audit/export?outcome=denied');
  });

  it('encodes scope identifiers so a slash cannot escape the path', () => {
    const nasty: AuditScope = { kind: 'project', org: 'a/b', project: 'c d' };
    expect(auditExportUrl(nasty, emptyAuditFilter)).toBe(
      '/api/v1/orgs/a%2Fb/projects/c%20d/audit/export',
    );
  });
});
