import { generatePath } from 'react-router';

import { needsOrg, sectionsFor, surfaceById, type Surface } from '../app/navigation.ts';
import { SYSTEM_SCOPE_REFUSED_SURFACES, type SystemScope } from '../api/selfConfig.ts';
import { withRemote } from '../api/transport.tsx';

export type SidebarLink = {
  /** The surface id, or `project-members` for the filtered members projection. */
  readonly id: string;
  readonly label: string;
  readonly to: string;
  /** Non-null renders the row as a disabled span carrying this title. */
  readonly disabledReason: string | null;
  /** The disabled row's short visible note; "local only" when absent. */
  readonly disabledNote?: string;
};

export type SidebarBlock = {
  readonly kind: 'project' | 'instance' | 'organisation' | 'account';
  readonly title: string;
  /** Scope qualifiers after the title, rendered `Title · a · b`: the name, or the remote boundary. */
  readonly qualifiers: readonly string[];
  readonly links: readonly SidebarLink[];
};

export type SidebarModel = {
  /** The project block, stacked above the organisation block; null off project routes. */
  readonly context: SidebarBlock | null;
  /** Org-scoped rows are absent while no organisation is active, never dead. */
  readonly organisation: SidebarBlock | null;
  /**
   * The instance tier at the foot, wider than the organisation block: collapsed
   * to Remotes plus one entry row off the instance routes, every permitted row
   * on them. Null when the principal holds nothing instance-scoped.
   */
  readonly instance: SidebarBlock | null;
  /** Mobile drawer only; desktop reaches it from the rail-foot menu. */
  readonly account: SidebarBlock;
  /**
   * The organisation's project list takes the project block's place: an
   * organisation page with no project opened yet, local. Keep the section
   * present while its query loads or fails so the shell can explain the state.
   */
  readonly orgProjectList: boolean;
  /**
   * One link back to a project, shown only when neither `context` nor the
   * org-page project list renders (account settings and instance pages): the org's
   * last-opened project while it still exists, else the project list. Null
   * everywhere `context` or that list already offers a way back.
   */
  readonly backToProject: { readonly label: string; readonly to: string } | null;
};

const localOnly = (label: string) => `${label} is local-instance only`;

/** Every other project and organisation surface stays reachable in the system scope. */
const SYSTEM_SCOPE_REFUSES: ReadonlySet<string> = new Set(SYSTEM_SCOPE_REFUSED_SURFACES);

function link(surface: Surface, to: string, disabledReason: string | null = null): SidebarLink {
  return { id: surface.id, label: surface.label, to, disabledReason };
}

/**
 * sidebarModel is the WHOLE sidebar as data (#567), narrow to wide: project,
 * organisation, instance. Desktop and the mobile drawer render all three from
 * this one model; the drawer adds `account`. Every label comes from the
 * navigation table and nothing here is hand-written per mode, so the two
 * cannot drift.
 */
export function sidebarModel(input: {
  readonly surface: Surface | undefined;
  readonly activeOrgId: string;
  /**
   * The project the project block shows, resolved to its id: the one the
   * route addresses, or on an organisation page the one last opened in this
   * organisation; '' for none. Never the rail's fallback (the org's first
   * project): an organisation page with no project opened yet shows the
   * project list instead.
   */
  readonly projectId: string;
  readonly remote: string;
  readonly isInstanceOperator: boolean;
  readonly canReadInstanceAudit?: boolean;
  readonly canReadInstanceDirectory?: boolean;
  /** Display names for the block headings; the project name is unused in a remote workspace. */
  readonly activeOrgName: string;
  readonly projectName: string;
  /**
   * The Hikyo system organisation and project (the self-configuration
   * binding), or null while unknown. The protected profile refuses machine
   * consumers, adapters and SCIM there by design (permission-model ADR,
   * 2026-09-06 amendment), so those entries are absent rather than dead.
   */
  readonly systemScope: SystemScope | null;
  /** Projects in the active organisation, or null while unknown. */
  readonly projects: readonly { readonly id: string; readonly name: string }[] | null;
  /** True on an organisation-section route with no project in the URL: the
   * route the project list falls back on when `context` is null. */
  readonly onOrgPage: boolean;
  /** The org's last-opened project id (`hikyo.lastProject`), or '' for none. */
  readonly lastProjectId: string;
}): SidebarModel {
  const {
    surface,
    activeOrgId,
    projectId,
    remote,
    isInstanceOperator,
    canReadInstanceAudit = false,
    canReadInstanceDirectory = false,
    activeOrgName,
    projectName,
    systemScope,
    projects,
    onOrgPage,
    lastProjectId,
  } = input;
  const systemOrg = systemScope !== null && systemScope.org === activeOrgId;
  const systemProject = systemOrg && systemScope.project === projectId;

  // An org-scoped destination needs an organisation to point at. With none
  // active the entry is absent rather than dead: a link that resolves to
  // `/orgs//members` is a 404 dressed as navigation.
  // In a remote workspace the organisation id is the REMOTE's, so an
  // org-scoped path would open a LOCAL page with a foreign id: those rows are
  // disabled like the project block's. Overview and Projects carry no org in
  // the path and stay as the way back to local.
  const organisation: SidebarBlock = {
    kind: 'organisation',
    title: 'Organisation',
    qualifiers: remote !== '' ? [`on ${remote}`] : activeOrgId === '' ? [] : [activeOrgName],
    links: sectionsFor('organisation')
      .filter((item) => !needsOrg(item) || activeOrgId !== '')
      .filter((item) => !systemOrg || !SYSTEM_SCOPE_REFUSES.has(item.id))
      .map((item) =>
        // With exactly one project there is nothing to overview: landing goes
        // straight to its matrix, and the entry stays visible but greyed so
        // the menu does not change shape as projects come and go.
        item.id === 'overview' && projects?.length === 1
          ? {
              ...link(item, item.path, 'This organisation has one project, so it opens on its matrix.'),
              disabledNote: 'one project',
            }
          : needsOrg(item)
            ? link(item, generatePath(item.path, { org: activeOrgId }), remote === '' ? null : localOnly(item.label))
            : link(item, item.path),
      ),
  };

  // The instance tier is always local: its links never carry `?remote`. Each
  // row has its own gate. Remotes also admits the operator, who may hold no
  // instance-directory yet owns connection custody (instance-config) on that page.
  const mayShow = (id: string): boolean => {
    switch (id) {
      case 'remotes':
        return isInstanceOperator || canReadInstanceDirectory;
      case 'instance-audit':
        return isInstanceOperator || canReadInstanceAudit;
      default:
        return isInstanceOperator;
    }
  };
  const expanded = surface?.section === 'instance';
  const entryId = isInstanceOperator ? 'instance-admin' : 'instance-audit';
  const instanceLinks = sectionsFor('instance')
    .filter((item) => mayShow(item.id))
    .filter((item) => expanded || item.id === 'remotes' || item.id === entryId)
    .map((item) => link(item, item.path));
  const instanceBlock: SidebarBlock | null =
    instanceLinks.length === 0
      ? null
      : { kind: 'instance', title: 'This instance', qualifiers: [], links: instanceLinks };

  const account: SidebarBlock = {
    kind: 'account',
    title: 'You',
    qualifiers: [],
    links: sectionsFor('account').map((item) => link(item, item.path)),
  };

  let context: SidebarBlock | null = null;
  if (projectId !== '' && activeOrgId !== '') {
    const params = { org: activeOrgId, project: projectId };
    const membersSurface = surfaceById('members');
    const membersPath = `${generatePath(membersSurface.path, { org: activeOrgId })}?project=${encodeURIComponent(projectId)}`;
    const projectLinks = sectionsFor('project')
      .filter((item) => !systemProject || !SYSTEM_SCOPE_REFUSES.has(item.id))
      .map((item) => {
        const path = generatePath(item.path, params);
        if (item.id === 'matrix' || item.id === 'transit-keys') return link(item, withRemote(path, remote));
        return link(item, path, remote === '' ? null : localOnly(item.label));
      });
    // The filtered members projection sits before Project settings, so the
    // block reads narrow to wide: matrix, machine access, members, settings.
    const members: SidebarLink = {
      id: 'project-members',
      label: membersSurface.label,
      to: membersPath,
      disabledReason: remote === '' ? null : localOnly(membersSurface.label),
    };
    const settingsIndex = projectLinks.findIndex((l) => l.id === 'project-settings');
    const links =
      settingsIndex === -1
        ? [...projectLinks, members]
        : [...projectLinks.slice(0, settingsIndex), members, ...projectLinks.slice(settingsIndex)];
    context = {
      kind: 'project',
      title: 'Project',
      qualifiers: remote === '' ? [projectName] : [`on ${remote}`, projectId],
      links,
    };
  }

  const orgProjectList = context === null && onOrgPage && activeOrgId !== '' && remote === '';
  const lastProject = projects?.find((item) => item.id === lastProjectId);
  const backToProject =
    context === null && activeOrgId !== '' && !orgProjectList
      ? {
          label: lastProject?.name ?? 'Projects',
          to:
            lastProject === undefined
              ? generatePath(surfaceById('projects').path, { org: activeOrgId })
              : generatePath(surfaceById('matrix').path, { org: activeOrgId, project: lastProject.id }),
        }
      : null;

  return {
    context,
    organisation,
    instance: instanceBlock,
    account,
    orgProjectList,
    backToProject,
  };
}

/**
 * isLinkActive replaces NavLink's own matching where a pathname is shared: the
 * org members page and its `?project=` projection, and a local row against the
 * remote page at the same path. Every other link is active on its pathname alone.
 */
export function isLinkActive(link: SidebarLink, pathname: string, search: string): boolean {
  const [linkPath, linkQuery = ''] = link.to.split('?');
  if (linkPath === undefined || pathname !== linkPath) return false;
  const want = new URLSearchParams(linkQuery);
  const have = new URLSearchParams(search);
  // A local row is never the current page of a remote workspace.
  if ((have.get('remote') ?? '') !== '' && !want.has('remote')) return false;
  const wantProject = want.get('project');
  const haveProject = have.get('project');
  if (link.id === 'project-members') return wantProject !== null && wantProject === haveProject;
  if (link.id === 'members') return haveProject === null;
  return true;
}
