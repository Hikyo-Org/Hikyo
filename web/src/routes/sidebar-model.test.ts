import { describe, expect, it } from 'vitest';

import { surfaceById } from '../app/navigation.ts';
import { isLinkActive, sidebarModel, type SidebarLink } from './sidebar-model.ts';

const base = {
  activeOrgId: 'org_1',
  routeProjectId: '',
  remote: '',
  isInstanceOperator: true,
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
    expect(model.instance?.links.map((l) => l.to)).toEqual(['/instance', '/instance/members']);
    expect(model.account.links.map((l) => l.to)).toEqual(['/settings']);
  });

  it('stacks the project block above the organisation block on a project route', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('matrix'), routeProjectId: 'prj_1' });
    expect(model.context?.kind).toBe('project');
    expect(model.context?.links.map((l) => [l.label, l.to])).toEqual([
      ['Environment matrix', '/orgs/org_1/projects/prj_1/matrix'],
      ['Machine access', '/orgs/org_1/projects/prj_1/machine-access'],
      ['Change approvals', '/orgs/org_1/projects/prj_1/change-approvals'],
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
      routeProjectId: 'prj_1',
      remote: 'ew',
    });
    const links = model.context?.links;
    expect(mustFind(links, 'matrix').to).toBe('/orgs/org_1/projects/prj_1/matrix?remote=ew');
    expect(mustFind(links, 'matrix').disabledReason).toBeNull();
    expect(mustFind(links, 'machine-access').disabledReason).toBe(
      'Machine access is not available for remote workspaces yet',
    );
    expect(mustFind(links, 'project-members').disabledReason).toBe(
      'Members is not available for remote workspaces yet',
    );
    expect(mustFind(links, 'project-settings').disabledReason).toBe(
      'Project settings is not available for remote workspaces yet',
    );
  });

  it('makes the instance block the context on an instance route and does not repeat it', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('instance-members') });
    expect(model.context?.kind).toBe('instance');
    expect(model.context?.links.map((l) => l.to)).toEqual(['/instance', '/instance/members']);
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

  it('activates the filtered members projection only with its project query', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('members'), routeProjectId: 'prj_1' });
    const members = mustFind(model.context?.links, 'project-members');
    const orgMembers = mustFind(model.organisation?.links, 'members');
    expect(isLinkActive(members, '/orgs/org_1/members', '?project=prj_1')).toBe(true);
    expect(isLinkActive(orgMembers, '/orgs/org_1/members', '?project=prj_1')).toBe(false);
    expect(isLinkActive(orgMembers, '/orgs/org_1/members', '')).toBe(true);
    expect(isLinkActive(members, '/orgs/org_1/members', '')).toBe(false);
    expect(isLinkActive(members, '/orgs/org_1/settings', '?project=prj_1')).toBe(false);
  });
});
