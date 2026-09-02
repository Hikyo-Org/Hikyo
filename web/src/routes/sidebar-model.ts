import { generatePath } from 'react-router';

import { needsOrg, sectionsFor, surfaceById, type Surface } from '../app/navigation.ts';
import { withRemote } from '../api/transport.tsx';

export type SidebarLink = {
  /** The surface id, or `project-members` for the filtered members projection. */
  readonly id: string;
  readonly label: string;
  readonly to: string;
  /** Non-null renders the row as a disabled span carrying this title. */
  readonly disabledReason: string | null;
};

export type SidebarBlock = {
  readonly kind: 'project' | 'instance' | 'organisation' | 'account';
  readonly title: string;
  readonly links: readonly SidebarLink[];
};

export type SidebarModel = {
  /** The project or instance block, stacked above the organisation block. */
  readonly context: SidebarBlock | null;
  /** Null while no organisation is active — an entry is absent, never dead. */
  readonly organisation: SidebarBlock | null;
  /** Operator-only, mobile drawer only; null when `context` already IS it. */
  readonly instance: SidebarBlock | null;
  /** Mobile drawer only; desktop reaches it from the rail-foot menu. */
  readonly account: SidebarBlock;
};

const localOnly = (label: string) => `${label} is not available for remote workspaces yet`;

function link(surface: Surface, to: string, disabledReason: string | null = null): SidebarLink {
  return { id: surface.id, label: surface.label, to, disabledReason };
}

/**
 * sidebarModel is the WHOLE sidebar as data (#567). Desktop renders `context`
 * and `organisation`; the mobile drawer renders those plus `instance` and
 * `account`. Every label comes from the navigation table and nothing here is
 * hand-written per mode, so the two cannot drift.
 */
export function sidebarModel(input: {
  readonly surface: Surface | undefined;
  readonly activeOrgId: string;
  /**
   * The project the ROUTE addresses, resolved to its id, or '' on any route
   * that names no project. Never the rail's fallback: that value is never
   * empty once an organisation has a project, and keying the context block on
   * it would hide the organisation list everywhere.
   */
  readonly routeProjectId: string;
  readonly remote: string;
  readonly isInstanceOperator: boolean;
}): SidebarModel {
  const { surface, activeOrgId, routeProjectId, remote, isInstanceOperator } = input;

  // An org-scoped destination needs an organisation to point at. With none
  // active the entry is absent rather than dead: a link that resolves to
  // `/orgs//members` is a 404 dressed as navigation.
  const organisation: SidebarBlock = {
    kind: 'organisation',
    title: 'Organisation',
    links: sectionsFor('organisation')
      .filter((item) => !needsOrg(item) || activeOrgId !== '')
      .map((item) =>
        link(item, needsOrg(item) ? generatePath(item.path, { org: activeOrgId }) : item.path),
      ),
  };

  const instanceBlock: SidebarBlock | null = isInstanceOperator
    ? {
        kind: 'instance',
        title: 'Instance',
        links: sectionsFor('instance').map((item) => link(item, item.path)),
      }
    : null;

  const account: SidebarBlock = {
    kind: 'account',
    title: 'You',
    links: sectionsFor('account').map((item) => link(item, item.path)),
  };

  let context: SidebarBlock | null = null;
  if (routeProjectId !== '' && activeOrgId !== '') {
    const params = { org: activeOrgId, project: routeProjectId };
    const membersSurface = surfaceById('members');
    const membersPath = `${generatePath(membersSurface.path, { org: activeOrgId })}?project=${encodeURIComponent(routeProjectId)}`;
    const projectLinks = sectionsFor('project').map((item) => {
      const path = generatePath(item.path, params);
      if (item.id === 'matrix') return link(item, withRemote(path, remote));
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
    context = { kind: 'project', title: '', links };
  } else if (surface?.section === 'instance' && instanceBlock !== null) {
    context = instanceBlock;
  }

  return {
    context,
    organisation,
    instance: context?.kind === 'instance' ? null : instanceBlock,
    account,
  };
}

/**
 * isLinkActive replaces NavLink's own matching for the two links that share a
 * pathname: the org members page and its `?project=` projection. Every other
 * link is active on its pathname alone.
 */
export function isLinkActive(link: SidebarLink, pathname: string, search: string): boolean {
  const [linkPath, linkQuery = ''] = link.to.split('?');
  if (linkPath === undefined || pathname !== linkPath) return false;
  const wantProject = new URLSearchParams(linkQuery).get('project');
  const haveProject = new URLSearchParams(search).get('project');
  if (link.id === 'project-members') return wantProject !== null && wantProject === haveProject;
  if (link.id === 'members') return haveProject === null;
  return true;
}
