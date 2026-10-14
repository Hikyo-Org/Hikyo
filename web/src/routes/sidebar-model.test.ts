import { describe, expect, it } from 'vitest';

import { surfaceById } from '../app/navigation.ts';
import { isLinkActive, sidebarModel, type SidebarLink } from './sidebar-model.ts';

const base = {
  activeOrgId: 'org_1',
  projectId: '',
  remote: '',
  isInstanceOperator: true,
  systemScope: null,
  projects: [],
  onOrgPage: false,
  lastProjectId: '',
};

function mustFind(links: readonly SidebarLink[] | undefined, id: string): SidebarLink {
  const found = links?.find((l) => l.id === id);
  if (found === undefined) throw new Error(`no link ${id}`);
  return found;
}

describe('sidebarModel', () => {
  it('shows only the organisation block on an org-scoped route', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('projects') });
    expect(model.context).toBeNull();
    expect(model.organisation?.links.map((l) => l.label)).toEqual([
      'Overview',
      'Projects',
      'Remotes',
      'Members',
      'Organisation settings',
      'SCIM provisioning',
      'Audit',
    ]);
    expect(model.organisation?.links.map((l) => l.to)).toEqual([
      '/',
      '/projects',
      '/remotes',
      '/orgs/org_1/members',
      '/orgs/org_1/settings',
      '/orgs/org_1/scim',
      '/orgs/org_1/audit',
    ]);
    expect(model.instance?.links.map((l) => l.to)).toEqual(['/instance', '/instance/config', '/instance/members']);
    expect(model.account.links.map((l) => l.to)).toEqual(['/settings']);
  });

  it('greys out Overview when the organisation has exactly one project', () => {
    const overview = (count: number | null) => {
      const projects = count === null ? null : Array.from({ length: count }, (_, i) => ({ id: `prj_${String(i)}`, name: `P${String(i)}` }));
      return mustFind(sidebarModel({ ...base, surface: surfaceById('projects'), projects }).organisation?.links, 'overview');
    };
    expect(overview(1)).toMatchObject({
      label: 'Overview',
      disabledReason: 'This organisation has one project, so it opens on its matrix.',
      disabledNote: 'one project',
    });
    for (const count of [null, 0, 2]) {
      expect(overview(count).disabledReason).toBeNull();
      expect(overview(count).to).toBe('/');
    }
  });

  it('stacks the project block above the organisation block on a project route', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('matrix'), projectId: 'prj_1' });
    expect(model.context?.kind).toBe('project');
    expect(model.context?.links.map((l) => [l.label, l.to])).toEqual([
      ['Environment matrix', '/orgs/org_1/projects/prj_1/matrix'],
      ['Machine access', '/orgs/org_1/projects/prj_1/machine-access'],
      ['Change approvals', '/orgs/org_1/projects/prj_1/change-approvals'],
      ['Temporary access', '/orgs/org_1/projects/prj_1/temporary-access'],
      ['Deployment adapters', '/orgs/org_1/projects/prj_1/adapters'],
      ['Project audit', '/orgs/org_1/projects/prj_1/audit'],
      ['Members', '/orgs/org_1/members?project=prj_1'],
      ['Project settings', '/orgs/org_1/projects/prj_1/settings'],
    ]);
    expect(model.organisation).not.toBeNull();
    expect(model.instance).not.toBeNull();
  });

  it('disables the local-only project destinations for a remote workspace', () => {
    const model = sidebarModel({
      ...base,
      surface: surfaceById('matrix'),
      projectId: 'prj_1',
      remote: 'ew',
    });
    const links = model.context?.links;
    expect(mustFind(links, 'matrix').to).toBe('/orgs/org_1/projects/prj_1/matrix?remote=ew');
    expect(mustFind(links, 'matrix').disabledReason).toBeNull();
    expect(mustFind(links, 'machine-access').disabledReason).toBe(
      'Machine access is local-instance only',
    );
    expect(mustFind(links, 'project-members').disabledReason).toBe(
      'Members is local-instance only',
    );
    expect(mustFind(links, 'project-settings').disabledReason).toBe(
      'Project settings is local-instance only',
    );
  });

  it('makes the instance block the context on an instance route and does not repeat it', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('instance-members') });
    expect(model.context?.kind).toBe('instance');
    expect(model.context?.links.map((l) => l.to)).toEqual(['/instance', '/instance/config', '/instance/members']);
    expect(model.instance).toBeNull();
    expect(model.organisation).not.toBeNull();
  });

  it('hides the instance block from non-operators and org links while no org is active', () => {
    const model = sidebarModel({
      ...base,
      surface: surfaceById('projects'),
      isInstanceOperator: false,
      activeOrgId: '',
    });
    expect(model.instance).toBeNull();
    expect(model.context).toBeNull();
    expect(model.organisation?.links.map((l) => l.to)).toEqual(['/', '/projects', '/remotes']);
  });

  it('never shows the instance context to a non-operator even on its route', () => {
    const model = sidebarModel({
      ...base,
      surface: surfaceById('instance-admin'),
      isInstanceOperator: false,
    });
    expect(model.context).toBeNull();
    expect(model.instance).toBeNull();
  });

  it('links back to the last-opened project on a page with no context block and no org-page fallback (account settings)', () => {
    const model = sidebarModel({
      ...base,
      surface: surfaceById('settings'),
      lastProjectId: 'prj_1',
      projects: [{ id: 'prj_1', name: 'Billing platform' }],
    });
    expect(model.context).toBeNull();
    expect(model.backToProject).toEqual({ label: 'Billing platform', to: '/orgs/org_1/projects/prj_1/matrix' });
  });

  it('falls back to the project list when no project is remembered', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('settings') });
    expect(model.backToProject).toEqual({ label: 'Projects', to: '/projects' });
  });

  it('shows no back-link once a context block renders', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('matrix'), projectId: 'prj_1' });
    expect(model.backToProject).toBeNull();
  });

  it('shows no back-link where the org-page project list already covers it', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('projects'), onOrgPage: true });
    expect(model.context).toBeNull();
    expect(model.orgProjectList).toBe(true);
    expect(model.backToProject).toBeNull();
  });

  it('shows the back-link on an org page whose project list is not actually rendering (still loading)', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('projects'), onOrgPage: true, projects: null });
    expect(model.orgProjectList).toBe(false);
    expect(model.backToProject).toEqual({ label: 'Projects', to: '/projects' });
  });

  it('shows no back-link with no organisation active', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('settings'), activeOrgId: '' });
    expect(model.backToProject).toBeNull();
  });

  it('activates the filtered members projection only with its project query', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('members'), projectId: 'prj_1' });
    const members = mustFind(model.context?.links, 'project-members');
    const orgMembers = mustFind(model.organisation?.links, 'members');
    expect(isLinkActive(members, '/orgs/org_1/members', '?project=prj_1')).toBe(true);
    expect(isLinkActive(orgMembers, '/orgs/org_1/members', '?project=prj_1')).toBe(false);
    expect(isLinkActive(orgMembers, '/orgs/org_1/members', '')).toBe(true);
    expect(isLinkActive(members, '/orgs/org_1/members', '')).toBe(false);
    expect(isLinkActive(members, '/orgs/org_1/settings', '?project=prj_1')).toBe(false);
  });
});

describe('sidebarModel in the Hikyo system scope', () => {
  // The self-configuration hierarchy refuses machine consumers, adapters and
  // SCIM by profile (permission-model ADR, 2026-09-06 amendment), so the
  // sidebar must not offer them there: a link to a page that always refuses
  // is a 404 dressed as navigation.
  const systemScope = { org: 'org_1', project: 'prj_1' };

  it('drops machine access and adapters from the system project block', () => {
    const model = sidebarModel({
      ...base,
      surface: surfaceById('matrix'),
      projectId: 'prj_1',
      systemScope,
    });
    expect(model.context?.links.map((l) => l.label)).toEqual([
      'Environment matrix',
      'Change approvals',
      'Temporary access',
      'Project audit',
      'Members',
      'Project settings',
    ]);
  });

  it('drops SCIM provisioning from the system organisation block', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('projects'), systemScope });
    expect(model.organisation?.links.map((l) => l.id)).toEqual([
      'overview',
      'projects',
      'remotes',
      'members',
      'org-settings',
      'audit',
    ]);
  });

  it('leaves every other organisation and project untouched', () => {
    const model = sidebarModel({
      ...base,
      surface: surfaceById('matrix'),
      activeOrgId: 'org_2',
      projectId: 'prj_2',
      systemScope,
    });
    expect(model.context?.links.map((l) => l.id)).toContain('machine-access');
    expect(model.organisation?.links.map((l) => l.id)).toContain('scim');
  });
});
