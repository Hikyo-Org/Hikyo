import { describe, expect, it } from 'vitest';

import { surfaceById } from '../app/navigation.ts';
import { isLinkActive, sidebarModel, type SidebarLink } from './sidebar-model.ts';

const base = {
  activeOrgId: 'org_1',
  projectId: '',
  remote: '',
  isInstanceOperator: true,
  activeOrgName: 'Acme',
  projectName: 'Billing',
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
      'Members',
      'Organisation settings',
      'SCIM provisioning',
      'Audit',
    ]);
    expect(model.organisation?.links.map((l) => l.to)).toEqual([
      '/',
      '/projects',
      '/orgs/org_1/members',
      '/orgs/org_1/settings',
      '/orgs/org_1/scim',
      '/orgs/org_1/audit',
    ]);
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
      ['Transit keys', '/orgs/org_1/projects/prj_1/transit-keys'],
      ['Change approvals', '/orgs/org_1/projects/prj_1/change-approvals'],
      ['Temporary access', '/orgs/org_1/projects/prj_1/temporary-access'],
      ['Deployment adapters', '/orgs/org_1/projects/prj_1/adapters'],
      ['Project audit', '/orgs/org_1/projects/prj_1/audit'],
      ['Members', '/orgs/org_1/members?project=prj_1'],
      ['Project settings', '/orgs/org_1/projects/prj_1/settings'],
    ]);
    expect(model.organisation).not.toBeNull();
    expect(model.instance).not.toBeNull();
    expect(model.context?.title).toBe('Project');
    expect(model.context?.qualifiers).toEqual(['Billing']);
    expect(model.organisation?.title).toBe('Organisation');
    expect(model.organisation?.qualifiers).toEqual(['Acme']);
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
    expect(mustFind(links, 'transit-keys').to).toBe('/orgs/org_1/projects/prj_1/transit-keys?remote=ew');
    expect(mustFind(links, 'transit-keys').disabledReason).toBeNull();
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

  it('shows the remote boundary in the headings and disables org-scoped organisation links in a workspace', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('matrix'), projectId: 'prj_1', remote: 'edge' });
    expect(model.context?.qualifiers).toEqual(['on edge', 'prj_1']);
    expect(model.organisation?.qualifiers).toEqual(['on edge']);
    const links = model.organisation?.links;
    for (const id of ['members', 'org-settings', 'scim', 'audit']) {
      const row = mustFind(links, id);
      expect(row.disabledReason).toBe(`${row.label} is local-instance only`);
    }
    // No org in their path: still the way back to local.
    expect(mustFind(links, 'overview').disabledReason).toBeNull();
    expect(mustFind(links, 'projects').disabledReason).toBeNull();
    for (const row of sidebarModel({ ...base, surface: surfaceById('projects') }).organisation?.links ?? []) {
      expect(row.disabledReason).toBeNull();
    }
  });

  describe('instance tier', () => {
    const tier = (input: Partial<Parameters<typeof sidebarModel>[0]>) =>
      sidebarModel({ ...base, surface: surfaceById('projects'), ...input }).instance;
    const noInstanceGrants = { isInstanceOperator: false, canReadInstanceAudit: false, canReadInstanceDirectory: false };

    it('collapses an operator to Remotes and the entry row off the instance routes, and expands in place on them', () => {
      expect(tier({})?.title).toBe('This instance');
      expect(tier({})?.links.map((l) => l.id)).toEqual(['remotes', 'instance-admin']);
      const expanded = sidebarModel({ ...base, surface: surfaceById('instance-members') });
      expect(expanded.instance?.links.map((l) => [l.id, l.to])).toEqual([
        ['remotes', '/instance/remotes'],
        ['instance-admin', '/instance'],
        ['instance-config', '/instance/config'],
        ['instance-audit', '/instance/audit'],
        ['instance-members', '/instance/members'],
      ]);
      // Collapsed rows are a prefix of the expanded ones: nothing jumps when the tier opens.
      expect(expanded.instance?.links.slice(0, 2).map((l) => l.id)).toEqual(tier({})?.links.map((l) => l.id));
      // The tier is never the top context block.
      expect(expanded.context).toBeNull();
    });

    it('keeps Remotes for an operator holding no instance-directory, since connection custody lives there', () => {
      expect(tier({ canReadInstanceDirectory: false })?.links.map((l) => l.id)).toContain('remotes');
    });

    it('gives an audit-only reader the audit trail and, expanded, nothing configurable', () => {
      const audit = { ...noInstanceGrants, canReadInstanceAudit: true };
      expect(tier(audit)?.links.map((l) => l.id)).toEqual(['instance-audit']);
      const onInstance = sidebarModel({ ...base, ...audit, surface: surfaceById('instance-audit') });
      expect(onInstance.instance?.links.map((l) => l.id)).toEqual(['instance-audit']);
    });

    it('gives a directory-only holder Remotes alone, collapsed or expanded', () => {
      const directory = { ...noInstanceGrants, canReadInstanceDirectory: true };
      expect(tier(directory)?.links.map((l) => l.id)).toEqual(['remotes']);
      expect(sidebarModel({ ...base, ...directory, surface: surfaceById('remotes') }).instance?.links.map((l) => l.id)).toEqual(['remotes']);
    });

    it('gives a directory and audit reader both rows', () => {
      const both = { ...noInstanceGrants, canReadInstanceAudit: true, canReadInstanceDirectory: true };
      expect(tier(both)?.links.map((l) => l.id)).toEqual(['remotes', 'instance-audit']);
    });

    it('shows an ordinary member no tier at all, and no Remotes row anywhere', () => {
      const model = sidebarModel({ ...base, ...noInstanceGrants, surface: surfaceById('instance-admin') });
      expect(model.instance).toBeNull();
      expect(model.organisation?.links.map((l) => l.id)).not.toContain('remotes');
    });

    it('renders for an operator with no organisation, whose way in the rail cog used to be', () => {
      const model = tier({ activeOrgId: '' });
      expect(model?.links.map((l) => l.id)).toEqual(['remotes', 'instance-admin']);
    });

    it('stays local and enabled in a remote workspace', () => {
      const links = tier({ remote: 'edge' })?.links;
      expect(links?.map((l) => [l.to, l.disabledReason])).toEqual([
        ['/instance/remotes', null],
        ['/instance', null],
      ]);
    });
  });

  it('offers the way back to a project on instance routes, with the organisation block above the tier', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('instance-config'), lastProjectId: 'prj_1', projects: [{ id: 'prj_1', name: 'Billing platform' }] });
    expect(model.context).toBeNull();
    expect(model.backToProject).toEqual({ label: 'Billing platform', to: '/orgs/org_1/projects/prj_1/matrix' });
    expect(model.organisation).not.toBeNull();
    expect(model.orgProjectList).toBe(false);
  });

  it('hides org links while no org is active, keeping the instance tier', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('projects'), isInstanceOperator: false, activeOrgId: '' });
    expect(model.context).toBeNull();
    expect(model.organisation?.links.map((l) => l.to)).toEqual(['/', '/projects']);
    expect(model.organisation?.qualifiers).toEqual([]);
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

  it('keeps the project section present for loading and error feedback on an org page', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('projects'), onOrgPage: true, projects: null });
    expect(model.orgProjectList).toBe(true);
    expect(model.backToProject).toBeNull();
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

  it('never marks a local row current on the remote page at the same path', () => {
    const config: SidebarLink = { id: 'instance-config', label: 'Hikyo configuration', to: '/instance/config', disabledReason: null };
    expect(isLinkActive(config, '/instance/config', '')).toBe(true);
    expect(isLinkActive(config, '/instance/config', '?remote=edge')).toBe(false);
    expect(isLinkActive(config, '/instance/config', '?remote=')).toBe(true);
    const matrix: SidebarLink = { id: 'matrix', label: 'Environment matrix', to: '/m?remote=edge', disabledReason: null };
    expect(isLinkActive(matrix, '/m', '?remote=edge')).toBe(true);
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
      'Transit keys',
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
