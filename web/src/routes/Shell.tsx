import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import {
  generatePath,
  Link,
  matchPath,
  NavLink,
  Outlet,
  useLocation,
  useNavigate,
} from 'react-router';

import { useLogout, useOrgs, type WhoAmI } from '../api/session.ts';
import { useProjects } from '../api/settings.ts';
import { retentionBanner, storageBanner, useRetentionHealth } from '../api/retention.ts';
import {
  useRemoteUpdateStatuses,
  useServerVersion,
  useUpdateStatus,
  type UpdateStatus,
} from '../api/updates.ts';
import { useWorkspaces } from '../api/workspace.ts';
import { withRemote } from '../api/transport.tsx';
import { effectiveTheme, prefersDark, useThemeChoice, type Theme } from '../app/theme.ts';
import { needsOrg, SURFACES, surfaceById, type Surface } from '../app/navigation.ts';
import { notifyUpdate } from '../app/notifications.tsx';
import {
  CHROME_IDENTITY_EVENT,
  chromeIdentityMark,
  chromeIdentityStyle,
  chromeMonogram,
  chromeRailIdentityStyle,
  readChromeIdentity,
} from './chrome-identity.ts';
import { isLinkActive, sidebarModel, type SidebarBlock, type SidebarLink } from './sidebar-model.ts';
import { StepUpBanner } from './StepUpBanner.tsx';

export type ProjectSidebarGroup = {
  readonly id: string;
  readonly name: string;
  readonly keyCount: number;
  readonly problemCount: number;
  readonly hidden: boolean;
};

export type ProjectSidebarState = {
  readonly groups: readonly ProjectSidebarGroup[];
  readonly problemCount: number;
  readonly problemsActive: boolean;
  readonly onSelectGroup: (groupId: string) => void;
  readonly onToggleProblems: () => void;
};

const ProjectSidebarPublisher = createContext<
  ((state: ProjectSidebarState | null) => void) | null
>(null);

/** Lets a project surface fill the shell-owned project navigation without duplicating chrome. */
export function useProjectSidebar(state: ProjectSidebarState): void {
  const publish = useContext(ProjectSidebarPublisher);
  useEffect(() => {
    publish?.(state);
    return () => publish?.(null);
  }, [publish, state]);
}

/** Human-readable GiB for the storage high-water banner. */
function formatGiB(bytes: number): string {
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GiB`;
}

/**
 * The application chrome skeleton (prototype/app-chrome iteration 15, sidebar
 * treatment e from iteration 18).
 *
 * Skeleton means: the structure and the navigation are real, the content
 * wells are placeholders. The deep surfaces — environment matrix, reveal and
 * editing, version history, machine access — are their own tickets and arrive
 * as routes into the well, not as changes to this file.
 *
 * Three things here are load-bearing rather than decorative and must survive
 * later edits: the org rail owns organisation switching (iteration 4), the
 * account entry sits at the rail's foot, and the whole thing collapses to a
 * single column with a nav disclosure at 700px.
 */
export function Shell({ session }: { session: WhoAmI }) {
  const orgs = useOrgs(true);
  // whoami tells us whether this caller holds instance-config authority, so the
  // operator-only chrome polls never fire for an ordinary member (each would
  // only be refused with a 403). The reads still swallow a 403 as belt-and-
  // suspenders; this just stops us provoking it.
  const isInstanceOperator = session.capabilities.instance_operator;
  const retentionHealth = useRetentionHealth(isInstanceOperator);
  const updateStatus = useUpdateStatus(isInstanceOperator);
  const serverVersion = useServerVersion();
  const workspaces = useWorkspaces();
  const remoteUpdateStatuses = useRemoteUpdateStatuses(workspaces);
  const location = useLocation();
  const navigate = useNavigate();
  const [navOpen, setNavOpen] = useState(false);
  const [chosenOrgId, setChosenOrgId] = useState('');
  const [chosenProjectId, setChosenProjectId] = useState('');
  const [projectSidebar, setProjectSidebar] = useState<ProjectSidebarState | null>(null);
  const [, setIdentityRevision] = useState(0);
  const navToggleRef = useRef<HTMLButtonElement>(null);
  const sidebarRef = useRef<HTMLElement>(null);

  // A navigation on a phone must close the sheet it was chosen from.
  useEffect(() => setNavOpen(false), [location.pathname]);
  useEffect(() => {
    const refreshIdentity = () => setIdentityRevision((revision) => revision + 1);
    window.addEventListener(CHROME_IDENTITY_EVENT, refreshIdentity);
    return () => window.removeEventListener(CHROME_IDENTITY_EVENT, refreshIdentity);
  }, []);

  const items = orgs.data === undefined ? [] : orgs.data.items;
  const here = matchedSurface(location.pathname);
  const routeOrgId = here?.params.org === undefined ? '' : here.params.org;
  const search = new URLSearchParams(location.search);
  const pathProjectId = here?.params.project === undefined ? '' : here.params.project;
  const membersProjectId = here?.surface.id === 'members' ? search.get('project') ?? '' : '';
  const routeProjectId = pathProjectId === '' ? membersProjectId : pathProjectId;
  const remote = search.get('remote') ?? '';

  // A deep link is a selection too. Persist it only after the organisation
  // listing confirms the id, then unscoped destinations keep the same tenant.
  useEffect(() => {
    const routedOrg = items.find((org) => org.id === routeOrgId || org.name === routeOrgId);
    if (routedOrg !== undefined) {
      setChosenOrgId(routedOrg.id);
    }
  }, [items, routeOrgId]);

  /**
   * The active organisation is the ROUTE's when the route names one, and the
   * rail's choice otherwise.
   *
   * The route wins for a reason worth keeping: an org-scoped surface is
   * addressed by its path, so a deep link, a reload and a shared URL all land
   * on the same organisation — and a breadcrumb that named the rail's last
   * choice while the page below administered a different organisation would be
   * a lie in the one place a human checks for it.
   */
  const chosenOrg = items.find((org) => org.id === chosenOrgId);
  const fallbackOrg = chosenOrg === undefined ? items[0] : chosenOrg;
  const routedOrg = items.find((org) => org.id === routeOrgId || org.name === routeOrgId);
  const activeOrgId =
    routeOrgId !== '' ? routedOrg?.id ?? routeOrgId : fallbackOrg === undefined ? '' : fallbackOrg.id;
  const activeOrgName = items.find((org) => org.id === activeOrgId)?.name ?? activeOrgId;
  // Shell sits outside WorkspaceScope, so a remote project name is not fetched
  // from this instance. Its route id remains the honest label until the remote
  // workspace can publish chrome identity through the same explicit seam.
  const projects = useProjects(remote === '' ? activeOrgId : '');
  const projectItems = projects.data?.items ?? [];
  useEffect(() => {
    const routedProject = projectItems.find(
      (project) => project.id === routeProjectId || project.name === routeProjectId,
    );
    if (routedProject !== undefined) {
      setChosenProjectId(routedProject.id);
    }
  }, [projectItems, routeProjectId]);
  const chosenProject = projectItems.find((project) => project.id === chosenProjectId);
  const fallbackProject = chosenProject === undefined ? projectItems[0] : chosenProject;
  const routedProject = projectItems.find(
    (project) => project.id === routeProjectId || project.name === routeProjectId,
  );
  const activeProjectId =
    routeProjectId !== ''
      ? routedProject?.id ?? routeProjectId
      : fallbackProject === undefined
        ? ''
        : fallbackProject.id;
  const activeProjectName =
    projectItems.find((project) => project.id === activeProjectId)?.name ?? activeProjectId;
  const accountName = session.principal.display_name ?? session.principal.id;
  // Roles are templates, not stored identities. Production therefore uses the
  // honest membership label; prototype mode owns its illustrative admin copy.
  const isPrototype = import.meta.env.MODE === 'prototype';
  const activeOrgRole = isPrototype ? 'org admin' : 'Organisation member';
  const visibleRetentionHealth = retentionHealth.data?.health;
  const showInstanceAdministration = isInstanceOperator;
  const pruneWarning = retentionBanner(visibleRetentionHealth, retentionHealth.isError);
  const storageWarning = storageBanner(visibleRetentionHealth);
  const availableUpdate = updateStatus.data?.available === true ? updateStatus.data : null;
  const availableRemoteUpdates = remoteUpdateStatuses.flatMap(({ origin, status }) =>
    status?.available === true ? [{ origin, status }] : [],
  );
  const remoteUpdateFailures = remoteUpdateStatuses.filter(({ error }) => error !== null);
  const badgeVersions = [
    ...(availableUpdate?.latest_version === undefined ? [] : [availableUpdate.latest_version]),
    ...availableRemoteUpdates.flatMap(({ origin, status }) =>
      status.latest_version === undefined ? [] : [`${origin}: ${status.latest_version}`],
    ),
  ];

  /**
   * chooseOrg is what a rail circle does. Setting the state is only half of
   * it: while the current surface is addressed BY organisation, switching has
   * to move the address too, or the rail would mark one organisation while the
   * page kept administering another. A deeper org-scoped route (a project's
   * settings, the matrix) carries parameters the new organisation has no
   * values for, so switching lands on its project list — the surface a human
   * arriving in an organisation actually wants.
   */
  const chooseOrg = (org: string) => {
    setChosenOrgId(org);
    if (here === undefined || !needsOrg(here.surface)) {
      return;
    }
    const extra = Object.keys(here.params).filter((key) => key !== 'org' && key !== '*');
    void navigate(
      extra.length === 0
        ? generatePath(here.surface.path, { ...here.params, org })
        : surfaceById('projects').path,
    );
  };

  const chooseProject = (project: string) => {
    setChosenProjectId(project);
    void navigate(
      generatePath(surfaceById('matrix').path, { org: activeOrgId, project }),
    );
  };

  const dismissNavigation = useCallback(() => {
    setNavOpen(false);
    window.requestAnimationFrame(() => navToggleRef.current?.focus());
  }, []);

  useEffect(() => {
    if (!navOpen) {
      return;
    }
    const drawer = sidebarRef.current;
    const destination =
      drawer?.querySelector<HTMLElement>('[aria-current="page"]') ??
      drawer?.querySelector<HTMLElement>('a[href], button:not(:disabled)');
    destination?.focus();
    const handleDrawerKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        dismissNavigation();
        return;
      }
      if (event.key !== 'Tab' || drawer === null) {
        return;
      }
      const controls = Array.from(
        drawer.querySelectorAll<HTMLElement>('a[href], button:not(:disabled)'),
      ).filter((control) => control.getClientRects().length > 0);
      const first = controls[0];
      const last = controls.at(-1);
      if (event.shiftKey && document.activeElement === first && last !== undefined) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last && first !== undefined) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener('keydown', handleDrawerKey);
    return () => document.removeEventListener('keydown', handleDrawerKey);
  }, [dismissNavigation, navOpen]);

  useEffect(() => {
    if (!navOpen) {
      return;
    }
    const mobile = window.matchMedia('(max-width: 700px)');
    const releaseDrawerOnDesktop = () => {
      if (mobile.matches) {
        return;
      }
      setNavOpen(false);
      window.requestAnimationFrame(() => {
        document.querySelector<HTMLButtonElement>('.chrome__account button')?.focus();
      });
    };
    releaseDrawerOnDesktop();
    mobile.addEventListener('change', releaseDrawerOnDesktop);
    return () => mobile.removeEventListener('change', releaseDrawerOnDesktop);
  }, [navOpen]);

  const crumbs = useMemo(() => {
    const result = ['hikyo'];
    if (here?.surface.section === 'instance') {
      result.push('Instance', chromeCrumbLabel(here.surface));
      return result;
    }
    if (activeOrgId !== '') result.push(activeOrgName);
    if (routeProjectId !== '') result.push(activeProjectName);
    if (routeProjectId === '') result.push(chromeCrumbLabel(here?.surface));
    return result;
  }, [activeOrgId, activeOrgName, activeProjectName, here?.surface, routeProjectId]);
  const onSidebarNavigate = navOpen ? dismissNavigation : () => setNavOpen(false);
  const model = sidebarModel({
    surface: here?.surface,
    activeOrgId,
    routeProjectId,
    activeProjectId,
    remote,
    isInstanceOperator,
  });

  return (
    <div className="chrome" data-nav={navOpen ? 'open' : 'closed'}>
      <a className="skip" href="#content" tabIndex={navOpen ? -1 : undefined}>
        Skip to content
      </a>

      <nav className="rail" aria-label="Organisations" inert={navOpen ? true : undefined}>
        {/* "Unauthorized is nonexistent" (permission ADR #15) applied to the
            chrome: a principal who holds one organisation is not being shown a
            switcher with one option, they are shown no switcher. There is
            nothing to switch to, and an affordance that cannot do anything is a
            question the reader has to answer before dismissing it. */}
        {items.length < 2 ? null : (
          <ul className="rail__orgs">
            {items.map((org) => {
              const identity = readChromeIdentity('org', org.id, isPrototype);
              return <li key={org.id}>
                <button
                  type="button"
                  className="avatar"
                  style={chromeRailIdentityStyle(identity, org.id === activeOrgId)}
                  aria-current={org.id === activeOrgId}
                  aria-label={`Organisation ${org.name}`}
                  title={org.name}
                  onClick={() => chooseOrg(org.id)}
                >
                  {chromeIdentityMark(identity, org.name)}
                </button>
              </li>;
            })}
          </ul>
        )}
        {projectItems.length === 0 ? null : (
          <>
            {items.length < 2 ? null : <span className="rail__divider" aria-hidden="true" />}
            <ul className="rail__projects" aria-label="Projects">
              {projectItems.map((project) => {
                const identity = readChromeIdentity('project', project.id, isPrototype);
                return <li key={project.id}>
                  <button
                    type="button"
                    className="project-avatar"
                    style={chromeRailIdentityStyle(identity, project.id === activeProjectId)}
                    aria-current={project.id === activeProjectId}
                    aria-label={`Project ${project.name}`}
                    title={project.name}
                    onClick={() => chooseProject(project.id)}
                  >
                    {chromeIdentityMark(identity, project.name)}
                  </button>
                </li>;
              })}
            </ul>
          </>
        )}
        <span className="rail__spacer" />
        <FleetUpdateNotice
          local={availableUpdate}
          remotes={availableRemoteUpdates}
          principalId={session.principal.id}
        />
        {showInstanceAdministration ? (
          <NavLink
            className="rail__action"
            to={surfaceById('instance-admin').path}
            aria-label={surfaceById('instance-admin').label}
            title={surfaceById('instance-admin').label}
          >
            <span aria-hidden="true">⚙</span>
          </NavLink>
        ) : null}
        <span className="rail__account-space" aria-hidden="true" />
      </nav>

      <nav
        id="sidebar"
        className="sidebar"
        aria-label="Sections"
        data-open={navOpen}
        ref={sidebarRef}
      >
        <button
          type="button"
          className="btn btn--icon sidebar__close sidebar__mobile-only"
          aria-label="Close navigation"
          onClick={dismissNavigation}
        >
          <span aria-hidden="true">×</span>
        </button>
        {items.length < 2 ? null : (
          <section
            className="sidebar__section sidebar__mobile-only sidebar__mobile-organisations"
            aria-labelledby="mobile-organisations-title"
          >
            <h2 id="mobile-organisations-title">Organisations</h2>
            <ul className="sidebar__items">
              {items.map((org) => {
                const identity = readChromeIdentity('org', org.id, isPrototype);
                return <li key={org.id}>
                  <button
                    type="button"
                    className="sidebar__link sidebar__switcher"
                    aria-current={org.id === activeOrgId ? 'page' : undefined}
                    onClick={() => {
                      chooseOrg(org.id);
                      dismissNavigation();
                    }}
                  >
                    <span
                      className="avatar sidebar__switcher-avatar"
                      style={chromeIdentityStyle(identity)}
                    >
                      {chromeIdentityMark(identity, org.name)}
                    </span>
                    <span>{org.name}</span>
                    {org.id === activeOrgId ? (
                      <span className="sidebar__switcher-check">✓</span>
                    ) : null}
                  </button>
                </li>;
              })}
            </ul>
          </section>
        )}
        {orgs.isSuccess && items.length === 0 ? (
          // The zero-org state (prototype iteration 14). It is a real state,
          // not an error: a principal whose grants name no organisation has
          // nowhere to navigate yet, and saying so is the whole of it. An
          // instance operator is in exactly this state until someone grants
          // them membership — their enumeration surface is elsewhere and
          // behind its own second factor.
          <p className="sidebar__empty" role="status">
            No organisations yet. An instance administrator creates one under Instance
            administration or with <code>hikyo org create</code>; you will see it here once you
            are granted access to it.
          </p>
        ) : null}
        {orgs.isError ? (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>Your organisations could not be loaded. Reload to try again.</span>
          </p>
        ) : null}
        {/* The context block (project or instance) stacks ABOVE the organisation
            block, which is never hidden: every destination stays reachable in
            every mode, and both blocks render from the same table (#567). The
            project block keys on `routeProjectId`, not `activeProjectId`: the
            latter falls back to the org's first project so the rail always has
            a tile to mark, and keying on it made the org block vanish
            everywhere. */}
        {model.context?.kind === 'project' ? (
          <ProjectContext
            org={activeOrgId}
            orgName={activeOrgName}
            projectName={activeProjectName}
            orgRole={activeOrgRole}
            links={model.context.links}
            state={projectSidebar}
            onNavigate={onSidebarNavigate}
          />
        ) : null}
        {model.context?.kind === 'instance' ? (
          <InstanceContext links={model.context.links} onNavigate={onSidebarNavigate} />
        ) : null}
        {model.organisation === null ? null : (
          <SidebarSection block={model.organisation} onNavigate={onSidebarNavigate} />
        )}
        {activeOrgId === '' ? null : (
          <section
            className="sidebar__section sidebar__mobile-only sidebar__mobile-projects"
            aria-labelledby="mobile-projects-title"
          >
            <h2 id="mobile-projects-title">Projects</h2>
            <ul className="sidebar__items">
              {projectItems.map((project) => {
                const identity = readChromeIdentity('project', project.id, isPrototype);
                return <li key={project.id}>
                  <button
                    type="button"
                    className="sidebar__link sidebar__switcher"
                    aria-current={project.id === routeProjectId ? 'page' : undefined}
                    onClick={() => {
                      chooseProject(project.id);
                      dismissNavigation();
                    }}
                  >
                    <span
                      className="sidebar__switcher-avatar sidebar__switcher-avatar--project"
                      style={chromeIdentityStyle(identity)}
                    >
                      {chromeIdentityMark(identity, project.name)}
                    </span>
                    <span>{project.name}</span>
                  </button>
                </li>;
              })}
            </ul>
          </section>
        )}
        {/* Mobile only: the destinations the desktop rail carries (instance
            cog, account menu). Derived from the same table as everything above. */}
        {model.instance === null ? null : (
          <SidebarSection block={model.instance} mobileOnly onNavigate={dismissNavigation} />
        )}
        <SidebarSection block={model.account} mobileOnly onNavigate={dismissNavigation} />
        <SidebarVersion version={serverVersion.data} />
      </nav>

      <div className="chrome__account" inert={navOpen ? true : undefined}>
        <AccountEntry session={session} updateVersions={badgeVersions} />
      </div>

      <div
        className="nav-scrim"
        aria-hidden="true"
        onClick={dismissNavigation}
      />

      <div className="main" inert={navOpen ? true : undefined}>
        <header className="header">
          <button
            type="button"
            className="btn nav-toggle"
            ref={navToggleRef}
            aria-expanded={navOpen}
            aria-controls="sidebar"
            onClick={() => setNavOpen((open) => !open)}
          >
            Menu
          </button>
          <ol className="header__crumbs" aria-label="Breadcrumb">
            {crumbs.map((crumb, index) => (
              <li
                key={`${crumb}-${String(index)}`}
                data-crumb={index === 0 ? 'root' : index === crumbs.length - 1 ? 'surface' : 'scope'}
              >
                {index === 0 ? null : (
                  <span className="header__crumb-separator" aria-hidden="true">
                    /
                  </span>
                )}
                <span>{crumb}</span>
              </li>
            ))}
          </ol>
          <span className="header__spacer" />
          {isPrototype ? (
            /* The prototype's persona switcher had three principals and drove
               the whole chrome from them. There is one here and the mock serves
               one, so this states it rather than offering a choice: a select
               with a single option and no handler is a control that cannot do
               the one thing its shape promises. */
            <span className="header__identity">
              <span className="header__identity-label">acting as</span>
              <span className="mono">alex · 2 orgs · org+instance admin</span>
            </span>
          ) : (
            <span className="header__identity">
              <span className="header__identity-label">Signed in as</span>
              <NavLink to={surfaceById('settings').path}>{accountName}</NavLink>
            </span>
          )}
          <ThemeToggle />
        </header>
        {pruneWarning?.kind === 'error' ? (
          <p className="retention-warning" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>Retention health could not be checked. Reload to try again.</span>
          </p>
        ) : null}
        {updateStatus.isError || remoteUpdateFailures.length > 0 ? (
          <p className="retention-warning" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>
              Update checks failed for{' '}
              {updateStatus.isError ? 'this instance' : `${remoteUpdateFailures.length} remote instance${remoteUpdateFailures.length === 1 ? '' : 's'}`}
              {updateStatus.isError && remoteUpdateFailures.length > 0
                ? ` and ${remoteUpdateFailures.length} remote instance${remoteUpdateFailures.length === 1 ? '' : 's'}`
                : ''}
              . Reload to retry.
            </span>
          </p>
        ) : null}
        {pruneWarning?.kind === 'stale' ? (
          <p className="retention-warning" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>
              {pruneWarning.lastPruneSuccess === null ? (
                <>Payload pruning has never succeeded — retention bounds are not being enforced.</>
              ) : (
                <>
                  Payload pruning has not succeeded since{' '}
                  <time dateTime={pruneWarning.lastPruneSuccess}>
                    {new Date(pruneWarning.lastPruneSuccess).toLocaleString()}
                  </time>{' '}
                  — retention bounds are not being enforced.
                </>
              )}
            </span>
          </p>
        ) : null}
        {storageWarning !== null ? (
          <p className="retention-warning" role="alert">
            <span className="alert__glyph" aria-hidden="true">
              !
            </span>
            <span>
              A project has reached {formatGiB(storageWarning.peakProjectBytes)} of stored payload —
              new publishes are refused at 4 GiB. Lower the project&apos;s retention window or
              release pinned revisions to reclaim space.
            </span>
          </p>
        ) : null}
        {/* This is the app's scroll container. `tabIndex=0` both gives the skip
            link a focus target and lets keyboard users operate the region when
            its content overflows on a phone. */}
        <main className="content" id="content" tabIndex={0}>
          <StepUpBanner session={session} />
          <ProjectSidebarPublisher.Provider value={setProjectSidebar}>
            <Outlet context={{ activeOrgId }} />
          </ProjectSidebarPublisher.Provider>
        </main>
      </div>
    </div>
  );
}

function SidebarLinkItem({ link, onNavigate }: { link: SidebarLink; onNavigate: () => void }) {
  const location = useLocation();
  if (link.disabledReason !== null) {
    return (
      <span
        className="sidebar__link sidebar__link--disabled"
        aria-disabled="true"
        title={link.disabledReason}
      >
        {`${link.label} · local only`}
      </span>
    );
  }
  // Members and its `?project=` projection share a pathname, so NavLink's own
  // matching would mark both; the model decides which one is current.
  if (link.id === 'members' || link.id === 'project-members') {
    return (
      <Link
        className="sidebar__link"
        to={link.to}
        aria-current={isLinkActive(link, location.pathname, location.search) ? 'page' : undefined}
        onClick={onNavigate}
      >
        {link.label}
      </Link>
    );
  }
  return (
    <NavLink className="sidebar__link" to={link.to} end={link.end} onClick={onNavigate}>
      {link.label}
    </NavLink>
  );
}

function SidebarSection({
  block,
  mobileOnly = false,
  onNavigate,
}: {
  block: SidebarBlock;
  mobileOnly?: boolean;
  onNavigate: () => void;
}) {
  const id = `sidebar-${block.kind}-title`;
  return (
    <section
      className={`sidebar__section${mobileOnly ? ' sidebar__mobile-only' : ''} sidebar__section--${block.kind}`}
      aria-labelledby={id}
    >
      <h2 id={id}>{block.title}</h2>
      <ul className="sidebar__items">
        {block.links.map((link) => (
          <li key={link.id}>
            <SidebarLinkItem link={link} onNavigate={onNavigate} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function InstanceContext({
  links,
  onNavigate,
}: {
  links: readonly SidebarLink[];
  onNavigate: () => void;
}) {
  return (
    <section className="context-sidebar" aria-labelledby="context-sidebar-title">
      <h2 id="context-sidebar-title">Instance</h2>
      <nav aria-label="Instance">
        {links.map((link) => (
          <SidebarLinkItem key={link.id} link={link} onNavigate={onNavigate} />
        ))}
      </nav>
    </section>
  );
}

function ProjectContext({
  org,
  orgName,
  projectName,
  orgRole,
  links,
  state,
  onNavigate,
}: {
  org: string;
  orgName: string;
  projectName: string;
  orgRole: string;
  links: readonly SidebarLink[];
  state: ProjectSidebarState | null;
  onNavigate: () => void;
}) {
  const orgIdentity = readChromeIdentity('org', org, import.meta.env.MODE === 'prototype');
  const matrix = links.find((link) => link.id === 'matrix');
  const rest = links.filter((link) => link.id !== 'matrix');
  return (
    <section className="context-sidebar" aria-labelledby="context-sidebar-title">
      <div className="context-sidebar__org">
        <span
          className="avatar context-sidebar__org-avatar"
          style={chromeIdentityStyle(orgIdentity)}
        >
          {chromeIdentityMark(orgIdentity, orgName)}
        </span>
        <span>
          <strong>{orgName}</strong>
          <small>{orgRole}</small>
        </span>
      </div>
      <h2 id="context-sidebar-title">
        <span>Project · </span>
        {projectName}
      </h2>
      <nav aria-label="Project">
        {matrix === undefined ? null : <SidebarLinkItem link={matrix} onNavigate={onNavigate} />}
        {state === null ? null : (
            <div className="context-sidebar__groups">
              {state.groups.map((group) => (
                <button
                  type="button"
                  className="matrix__group-link context-sidebar__group"
                  key={group.id}
                  disabled={group.hidden}
                  title={group.hidden ? 'hidden by the problems filter' : undefined}
                  aria-label={`${group.name}/ · ${String(group.keyCount)} ${group.keyCount === 1 ? 'key' : 'keys'}${group.problemCount === 0 ? '' : ` · ${String(group.problemCount)} problems`}`}
                  onClick={() => {
                    state.onSelectGroup(group.id);
                    onNavigate();
                  }}
                >
                  {/* The trailing slash is what says "folder", not "key" — the
                      matrix's own jump index carried it before this list moved
                      into the sidebar, and the flow reads groups by it. */}
                  <span className="mono">{`${group.name}/`}</span>
                  {group.problemCount === 0 ? (
                    <span className="context-sidebar__group-count">{String(group.keyCount)}</span>
                  ) : (
                    <span className="matrix__count count">{String(group.problemCount)}</span>
                  )}
                </button>
              ))}
              <button
                type="button"
                className="matrix__group-link context-sidebar__group"
                aria-pressed={state.problemsActive}
                title={state.problemsActive ? 'back to all keys' : 'show only keys with problems'}
                onClick={() => {
                  state.onToggleProblems();
                  onNavigate();
                }}
              >
                <span>
                  <span aria-hidden="true">⚠ </span>
                  problems
                </span>
                <span className="matrix__count count">{String(state.problemCount)}</span>
              </button>
            </div>
          )}
        {rest.map((link) => (
          <SidebarLinkItem key={link.id} link={link} onNavigate={onNavigate} />
        ))}
      </nav>
    </section>
  );
}

/**
 * The running build's version, pinned to the foot of the sidebar. It reads the
 * contract's own `server_version` (`dev` for an unreleased build) and renders
 * nothing until it resolves — a footer that flashed a placeholder would be
 * noisier than one that simply arrives.
 */
function SidebarVersion({ version }: { version: string | undefined }) {
  if (version === undefined) {
    return null;
  }
  return (
    <p className="sidebar__version">
      <span className="sidebar__version-label">Version</span>
      <span className="mono">{version}</span>
    </p>
  );
}

export function AccountEntry({
  session,
  updateVersions,
}: {
  session: WhoAmI;
  updateVersions: readonly string[];
}) {
  const [open, setOpen] = useState(false);
  const entryRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuItemRef = useRef<HTMLAnchorElement>(null);
  const logout = useLogout();
  const location = useLocation();
  const name = session.principal.display_name ?? session.principal.id;

  useEffect(() => setOpen(false), [location.pathname]);

  useEffect(() => {
    if (!open) {
      return;
    }

    menuItemRef.current?.focus();
    const dismissOutside = (event: PointerEvent) => {
      const entry = entryRef.current;
      if (entry !== null && !event.composedPath().includes(entry)) {
        setOpen(false);
      }
    };
    document.addEventListener('pointerdown', dismissOutside);
    return () => document.removeEventListener('pointerdown', dismissOutside);
  }, [open]);

  return (
    <div
      className="account-entry"
      ref={entryRef}
      onBlur={(event) => {
        const next = event.relatedTarget;
        if (!(next instanceof Node) || !event.currentTarget.contains(next)) {
          setOpen(false);
        }
      }}
      onKeyDown={(event) => {
        if (event.key !== 'Escape' || !open) {
          return;
        }
        event.preventDefault();
        event.stopPropagation();
        setOpen(false);
        triggerRef.current?.focus();
      }}
    >
      <button
        type="button"
        className="avatar"
        ref={triggerRef}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`Account: ${name}${updateVersions.length === 0 ? '' : `; ${updateVersions.length} update${updateVersions.length === 1 ? '' : 's'} available`}`}
        onClick={() => setOpen((v) => !v)}
      >
        {chromeMonogram(name)}
        {updateVersions.length === 0 ? null : (
          <ProfileUpdateBadge version={updateVersions.join(', ')} />
        )}
      </button>
      {open ? (
        <div className="menu" role="menu" aria-label="Account">
          <p className="menu__label" role="none">
            Signed in as <span className="mono">{name}</span>
          </p>
          <NavLink
            role="menuitem"
            className="menu__item"
            ref={menuItemRef}
            to={surfaceById('settings').path}
          >
            Account &amp; security
          </NavLink>
          <button
            type="button"
            role="menuitem"
            className="menu__item"
            onClick={() => logout.mutate()}
            disabled={logout.isPending}
          >
            Sign out
          </button>
        </div>
      ) : null}
    </div>
  );
}

const dismissedUpdatePrefix = 'hikyo:update-dismissed:';

function fleetDismissalKey(
  local: UpdateStatus | null,
  remotes: Array<{ origin: string; status: UpdateStatus }>,
  principalId: string,
): string | null {
  const versions = remotes.flatMap(({ origin, status }) =>
    status.latest_version === undefined
      ? []
      : [`${origin}:${status.channel}:${status.latest_version}`],
  );
  if (local?.latest_version !== undefined) {
    versions.push(`local:${local.channel}:${local.latest_version}`);
  }
  return versions.length === 0
    ? null
    : `${dismissedUpdatePrefix}${principalId}:fleet:${versions.sort().join('|')}`;
}

function wasDismissed(key: string): boolean {
  try {
    return window.localStorage.getItem(key) === 'true';
  } catch {
    return false;
  }
}

function rememberDismissal(key: string): void {
  try {
    window.localStorage.setItem(key, 'true');
  } catch {
    // Storage can be unavailable in hardened browsers. Dismissal still lasts
    // for the current page because the toast store itself is cleared.
  }
}

export function FleetUpdateNotice({
  local,
  remotes,
  principalId,
}: {
  local: UpdateStatus | null;
  remotes: Array<{ origin: string; status: UpdateStatus }>;
  principalId: string;
}) {
  const availableRemotes = remotes.filter(({ status }) => status.latest_version !== undefined);
  const availableLocal = local?.latest_version === undefined ? null : local;
  const count = availableRemotes.length + (availableLocal === null ? 0 : 1);
  const key = fleetDismissalKey(availableLocal, availableRemotes, principalId);
  const message =
    count === 1 && availableLocal !== null
      ? `Hikyo ${availableLocal.latest_version} is available on the ${availableLocal.channel} channel.`
      : count === 1
        ? `Hikyo ${availableRemotes[0]?.status.latest_version} is available for ${availableRemotes[0]?.origin}.`
        : `${count} Hikyo environments have updates available.`;
  const href =
    availableRemotes.length === 0 && availableLocal?.release_url !== undefined
      ? availableLocal.release_url
      : '/instance/remotes';
  const label = availableRemotes.length === 0 ? 'View release' : 'Review updates';
  useEffect(() => {
    if (key === null || wasDismissed(key)) {
      return;
    }
    notifyUpdate(message, href, () => rememberDismissal(key), label);
  }, [href, key, label, message]);
  return null;
}

export function ProfileUpdateBadge({ version }: { version: string }) {
  return (
    <span
      className="account-update-badge"
      aria-label={`Update ${version} available`}
      title={`Update ${version} available`}
    />
  );
}

/**
 * The header theme toggle: a binary sun/moon that flips light↔dark. Choosing
 * `system` is left to the account's Preferences panel; the quick control in the
 * chrome is a two-state switch, which is what the polymorphing icon expresses.
 *
 * The icon shows the theme actually painted, so while the choice is `system` it
 * tracks the OS preference live — otherwise a mid-session OS flip would leave a
 * sun over a dark page.
 */
function ThemeToggle() {
  const [choice, setChoice] = useThemeChoice();
  const [systemDark, setSystemDark] = useState(() => prefersDark());

  useEffect(() => {
    const query = globalThis.matchMedia?.('(prefers-color-scheme: dark)');
    if (query === undefined) {
      return;
    }
    const sync = () => setSystemDark(query.matches);
    query.addEventListener('change', sync);
    return () => query.removeEventListener('change', sync);
  }, []);

  const current = effectiveTheme(choice, systemDark);
  const next: Theme = current === 'dark' ? 'light' : 'dark';

  return (
    <button
      type="button"
      className="btn btn--icon"
      onClick={() => setChoice(next)}
      // The label states the ACTION, and the icon the current theme by shape —
      // so the state survives forced-colors, where the fills are repainted.
      aria-label={`Switch to ${next} theme`}
    >
      <ThemeIcon dark={current === 'dark'} />
    </button>
  );
}

/**
 * ThemeIcon is the polymorphing sun↔moon (author: Marc Went). The morph, the
 * ray draw-in and the moon shimmer are CSS in app.css — the CSP forbids inline
 * `<style>`, so nothing renders one here. The sun path is baked as an attribute
 * so a browser without the CSS `d` property still shows a static sun rather
 * than nothing; the CSS overrides it where supported.
 */
function ThemeIcon({ dark }: { dark: boolean }) {
  return (
    <svg
      className={dark ? 'theme-icon theme-icon--dark' : 'theme-icon'}
      viewBox="0 0 100 100"
      width="24"
      height="24"
      fill="none"
      aria-hidden="true"
      focusable="false"
    >
      <path
        className="theme-icon__shine"
        d="M70 49.5C70 60.8218 60.8218 70 49.5 70C38.1782 70 29 60.8218 29 49.5C29 38.1782 38.1782 29 49.5 29C39 45 49.5 59.5 70 49.5Z"
      />
      <g className="theme-icon__rays">
        <path d="M50 2V11" pathLength="1" />
        <path d="M85 15L78 22" pathLength="1" />
        <path d="M98 50H89" pathLength="1" />
        <path d="M85 85L78 78" pathLength="1" />
        <path d="M50 98V89" pathLength="1" />
        <path d="M23 78L16 84" pathLength="1" />
        <path d="M11 50H2" pathLength="1" />
        <path d="M23 23L16 16" pathLength="1" />
      </g>
      <path
        className="theme-icon__shape"
        d="M70 49.5C70 60.8218 60.8218 70 49.5 70C38.1782 70 29 60.8218 29 49.5C29 38.1782 38.1782 29 49.5 29C60 29 69.5 38 70 49.5Z"
      />
    </svg>
  );
}

/**
 * matchedSurface resolves the current path against the CLOSED surface list —
 * the same table the router is generated from, so the breadcrumb and the
 * organisation the chrome believes it is in can never drift from the route
 * that is actually rendered.
 */
function matchedSurface(
  pathname: string,
): { surface: Surface; params: Record<string, string | undefined> } | undefined {
  for (const surface of SURFACES) {
    const match = matchPath({ path: surface.path, end: true }, pathname);
    if (match !== null) {
      return { surface, params: match.params };
    }
  }
  return undefined;
}

/** The compact surface names fixed by the app-chrome breadcrumb treatment. */
export function chromeCrumbLabel(surface: Surface | undefined): string {
  switch (surface?.id) {
    case 'members':
    case 'instance-members':
      return 'members';
    case 'org-settings':
    case 'instance-admin':
      return 'settings';
    case 'settings':
      return 'account';
    default:
      return surface?.label ?? 'Not found';
  }
}
