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
  readonly links: readonly SidebarLink[];
};

export type SidebarModel = {
  /** The project or instance block, stacked above the organisation block. */
  readonly context: SidebarBlock | null;
  /** Null while no organisation is active, an entry is absent, never dead. */
  readonly organisation: SidebarBlock | null;
  /** Operator-only, mobile drawer only; null when `context` already IS it. */
  readonly instance: SidebarBlock | null;
  /** Mobile drawer only; desktop reaches it from the rail-foot menu. */
  readonly account: SidebarBlock;
  /**
   * The organisation's project list takes the project block's place: an
   * organisation page with no project opened yet, projects loaded, local.
   */
  readonly orgProjectList: boolean;
  /**
   * One link back to a project, shown only when neither `context` nor the
   * org-page project list renders (account settings today): the org's
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
 * sidebarModel is the WHOLE sidebar as data (#567). Desktop renders `context`
 * and `organisation`; the mobile drawer renders those plus `instance` and
 * `account`. Every label comes from the navigation table and nothing here is
 * hand-written per mode, so the two cannot drift.
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
  const organisation: SidebarBlock = {
    kind: 'organisation',
    title: 'Organisation',
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
          : link(item, needsOrg(item) ? generatePath(item.path, { org: activeOrgId }) : item.path),
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
  if (projectId !== '' && activeOrgId !== '') {
    const params = { org: activeOrgId, project: projectId };
    const membersSurface = surfaceById('members');
    const membersPath = `${generatePath(membersSurface.path, { org: activeOrgId })}?project=${encodeURIComponent(projectId)}`;
    const projectLinks = sectionsFor('project')
      .filter((item) => !systemProject || !SYSTEM_SCOPE_REFUSES.has(item.id))
      .map((item) => {
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

  const orgProjectList = context === null && onOrgPage && activeOrgId !== '' && remote === '' && projects !== null;
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
    instance: context?.kind === 'instance' ? null : instanceBlock,
    account,
    orgProjectList,
    backToProject,
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
