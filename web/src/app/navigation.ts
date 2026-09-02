/**
 * The closed list of surfaces this build serves.
 *
 * This is not a convenience table. It is the SOURCE OF TRUTH the flow
 * registry closes over (e2e/registry.ts): every surface named here must be
 * covered by a Playwright flow, and the closure check fails the build when
 * one is not. Adding a route without adding it here would defeat that, so the
 * router is built FROM this list — there is no second place to declare a
 * route.
 *
 * `section` is null for surfaces that are not navigation destinations (the
 * login page is reached by not being signed in, never by choosing it).
 */

export type RouteMode = 'public' | 'ceremony' | 'authenticated';
export type ChromeMode = 'none' | 'shell';
export type CeremonySessionPolicy = 'establish-or-reuse' | 'required';

/**
 * The four sidebar sections. `project` and `instance` are CONTEXT blocks: they
 * render only while the route is so scoped and sit above the organisation
 * block. `organisation` is always present once an organisation is active.
 * `account` never renders in the desktop sidebar (the rail-foot menu and the
 * header link are canonical); the mobile drawer lists it under "You".
 */
export type SectionId = 'project' | 'instance' | 'organisation' | 'account';

type SurfaceBase = {
  readonly id: string;
  readonly path: string;
  readonly label: string;
  readonly section: SectionId | null;
};

export type RouteDefinition = SurfaceBase &
  (
    | { readonly mode: 'public'; readonly chrome: 'none' }
    | {
        readonly mode: 'ceremony';
        readonly chrome: 'none';
        readonly session: CeremonySessionPolicy;
      }
    | { readonly mode: 'authenticated'; readonly chrome: 'shell' }
  );

export type RouteCandidate = SurfaceBase & {
  readonly mode: string;
  readonly chrome: string;
  readonly session?: string;
};

/** Returns every invalid or ambiguous route-policy declaration. */
export function routeRegistryViolations(routes: readonly RouteCandidate[]): string[] {
  const problems: string[] = [];
  const ids = new Set<string>();
  const paths = new Set<string>();

  for (const route of routes) {
    if (ids.has(route.id)) {
      problems.push(`route id "${route.id}" is declared more than once`);
    }
    ids.add(route.id);
    if (paths.has(route.path)) {
      problems.push(`route path "${route.path}" is declared more than once`);
    }
    paths.add(route.path);

    if (route.chrome === 'none' && route.section !== null) {
      problems.push(
        `route "${route.id}" has no chrome but appears in section "${route.section}"`,
      );
    }

    switch (route.mode) {
      case 'public':
        if (route.chrome !== 'none') {
          problems.push(`route "${route.id}" is public but uses shell chrome`);
        }
        if (route.session !== undefined) {
          problems.push(
            `route "${route.id}" declares a ceremony session policy outside ceremony mode`,
          );
        }
        break;
      case 'ceremony':
        if (route.chrome !== 'none') {
          problems.push(`route "${route.id}" is a ceremony but uses shell chrome`);
        }
        if (route.session !== 'establish-or-reuse' && route.session !== 'required') {
          problems.push(
            `route "${route.id}" is a ceremony without an explicit session policy`,
          );
        }
        break;
      case 'authenticated':
        if (route.chrome !== 'shell') {
          problems.push(`route "${route.id}" is authenticated but has no shell chrome`);
        }
        if (route.session !== undefined) {
          problems.push(
            `route "${route.id}" declares a ceremony session policy outside ceremony mode`,
          );
        }
        break;
      default:
        problems.push(`route "${route.id}" has unknown access mode "${route.mode}"`);
    }
  }

  return problems;
}

function defineSurfaceRegistry<const Routes extends readonly RouteDefinition[]>(
  routes: Routes,
): Routes {
  const problems = routeRegistryViolations(routes);
  if (problems.length !== 0) {
    throw new Error(`invalid route registry:\n${problems.join('\n')}`);
  }
  return routes;
}

export const SURFACES = defineSurfaceRegistry([
  {
    id: 'login',
    path: '/login',
    label: 'Sign in',
    section: null,
    mode: 'public',
    chrome: 'none',
  },
  // Credential establishment (#568): where an invitee, or the target of a
  // credential reset, turns a display-once authority into a password. Public
  // and chromeless like login — the holder has no session yet — and reached
  // from the login page and the invitation hand-off, never from the sidebar.
  {
    id: 'establish-credential',
    path: '/establish',
    label: 'Establish credential',
    section: null,
    mode: 'public',
    chrome: 'none',
  },
  {
    id: 'overview',
    path: '/',
    label: 'Overview',
    section: 'organisation',
    mode: 'authenticated',
    chrome: 'shell',
  },
  {
    id: 'projects',
    path: '/projects',
    label: 'Projects',
    section: 'organisation',
    mode: 'authenticated',
    chrome: 'shell',
  },
  {
    id: 'remotes',
    path: '/remotes',
    label: 'Remotes',
    section: 'organisation',
    mode: 'authenticated',
    chrome: 'shell',
  },
  // The two org-scoped chrome surfaces (#60). Their org is ROUTE DATA, not
  // chrome state: a members page and a settings page each administer ONE
  // organisation, and a path that did not name it would make a deep link, a
  // reload and a shared URL depend on which circle the rail happened to have
  // active. They still appear in the sidebar — the rail's active organisation
  // is what fills the parameter, and the entry is absent while there is no
  // organisation to fill it with, which is the honest rendering of the
  // zero-organisation state rather than a link that resolves to nothing.
  {
    id: 'members',
    path: '/orgs/:org/members',
    label: 'Members',
    section: 'organisation',
    mode: 'authenticated',
    chrome: 'shell',
  },
  {
    id: 'org-settings',
    path: '/orgs/:org/settings',
    label: 'Organisation settings',
    section: 'organisation',
    mode: 'authenticated',
    chrome: 'shell',
  },
  // SCIM provisioning administration (#501, #73). Org-scoped for the same
  // reason members is: `manage-members@org` addresses ONE organisation, so the
  // org is route data and the rail's active circle fills it. The binding under
  // administration is a `?binding=` query parameter — an id, never a secret —
  // so a reload and a shared link resolve the same binding, exactly as the
  // matrix's per-key filter does.
  {
    id: 'scim',
    path: '/orgs/:org/scim',
    label: 'SCIM provisioning',
    section: 'organisation',
    mode: 'authenticated',
    chrome: 'shell',
  },
  // Audit trail (#502). Org-scoped like members and scim: an org proof reads
  // the WHOLE org trail (project and environment events included), so the org
  // is route data filled by the rail's active circle, and the within-org
  // filters — principal, operation, outcome, resource — are query controls, not
  // separate surfaces. It sits in the Organisation section because that is the
  // scope its capability (`audit-read@org`) addresses.
  {
    id: 'audit',
    path: '/orgs/:org/audit',
    label: 'Audit',
    section: 'organisation',
    mode: 'authenticated',
    chrome: 'shell',
  },
  // The instance pair (#567): every scope is a {Members, Settings} pair, and
  // the instance is no exception. Both are operator-only; the sidebar's
  // instance context block lists them while the route is instance-scoped and
  // the desktop rail's cog is their entry point.
  {
    id: 'instance-admin',
    path: '/instance',
    label: 'Instance settings',
    section: 'instance',
    mode: 'authenticated',
    chrome: 'shell',
  },
  {
    id: 'instance-members',
    path: '/instance/members',
    label: 'Instance members',
    section: 'instance',
    mode: 'authenticated',
    chrome: 'shell',
  },
  {
    id: 'settings',
    path: '/settings',
    label: 'Account & security',
    section: 'account',
    mode: 'authenticated',
    chrome: 'shell',
  },
  // The environment matrix addresses one whole project. Project-scoped: the
  // sidebar's project context block fills `:org` and `:project` from the
  // route, exactly as the organisation block fills `:org`. This reverses the
  // earlier "no static entry could know which project" reasoning — the entry
  // is no longer static (#567).
  {
    id: 'matrix',
    path: '/orgs/:org/projects/:project/matrix',
    label: 'Environment matrix',
    section: 'project',
    mode: 'authenticated',
    chrome: 'shell',
  },
  // The revision-history drawer (#59). It is the matrix WITH its history drawer
  // open — the locked prototype's list+detail panes render over the matrix, not
  // instead of it — so the path nests under the matrix and the element is the
  // same component. `section: null` for the matrix's own reason: it addresses
  // one project, and the environment and the per-key filter are query
  // parameters, because per-key history is a filter and not a second surface.
  {
    id: 'history',
    path: '/orgs/:org/projects/:project/matrix/history',
    label: 'Revision history',
    section: null,
    mode: 'authenticated',
    chrome: 'shell',
  },
  // The catalogue declaration detail (#491). It is the matrix WITH one key's
  // full declaration open, exactly as `history` is the matrix with its drawer
  // open, so the path nests under the matrix and the element is the same
  // component reading the key id as route data. `section: null` for the
  // matrix's own reason: it addresses one key of one project, reached by
  // clicking the key name and by deep link, never a static sidebar entry that
  // could not know which key to mean. The key is addressed by its immutable
  // id, never its mutable name — a rename must not break a bookmarked link.
  {
    id: 'key-detail',
    path: '/orgs/:org/projects/:project/matrix/keys/:key',
    label: 'Key declaration',
    section: null,
    mode: 'authenticated',
    chrome: 'shell',
  },
  // The reveal / copy / write-only-edit surface (#58). `section: null` because
  // it is not a navigation destination: it addresses one environment of one
  // project, so it is reached from the matrix and by deep link, never from a
  // static sidebar entry that could not know which environment to mean.
  {
    id: 'values',
    path: '/orgs/:org/projects/:project/environments/:environment/values',
    label: 'Values',
    section: null,
    mode: 'authenticated',
    chrome: 'shell',
  },
  // The machine-access surface (#67). Project-scoped (#567): the sidebar's
  // project context block fills `:org` and `:project` from the route, so the
  // entry is no longer static and the earlier "could not know which project"
  // reasoning no longer applies. It is also reached from the project and by
  // deep link.
  {
    id: 'machine-access',
    path: '/orgs/:org/projects/:project/machine-access',
    label: 'Machine access',
    section: 'project',
    mode: 'authenticated',
    chrome: 'shell',
  },
  // Project settings addresses ONE project, exactly like the matrix, and is
  // project-scoped for the same reason (#567): the context block fills the
  // parameters from the route. Table order is sidebar order — matrix, machine
  // access, project settings.
  {
    id: 'project-settings',
    path: '/orgs/:org/projects/:project/settings',
    label: 'Project settings',
    section: 'project',
    mode: 'authenticated',
    chrome: 'shell',
  },
  // Browser half of the CLI's purpose-bound reauthentication handoff. The
  // opaque transaction state stays in the query string so login can render on
  // this same route without losing it.
  {
    id: 'cli-reauth',
    path: '/reauth/cli',
    label: 'Authorize CLI',
    section: null,
    mode: 'ceremony',
    chrome: 'none',
    session: 'establish-or-reuse',
  },
  // The two workspace handoff pages. Neither is a navigation destination and
  // neither wears the chrome: they are the two ends of the handoff's front
  // channel, and both are reached by a redirect, never by choosing them.
  //
  // `workspace-approve` is served by the SERVING instance and is where the
  // popup lands — the human authenticates there with that instance's own
  // ceremonies, on that instance's own origin, which is the whole architecture
  // in one route. `workspace-callback` is served by the VIEWING instance and
  // is the same-origin return path that exists because the popup is opened
  // with `noopener` and therefore has no `window.opener` to talk back through.
  {
    id: 'workspace-approve',
    path: '/workspace/approve',
    label: 'Authorize workspace',
    section: null,
    mode: 'ceremony',
    chrome: 'none',
    session: 'establish-or-reuse',
  },
  {
    id: 'workspace-callback',
    path: '/workspace/callback',
    label: 'Returning',
    section: null,
    mode: 'public',
    chrome: 'none',
  },
  {
    id: 'oidc-done',
    path: '/auth/oidc/done',
    label: 'Returning from identity provider',
    section: null,
    mode: 'public',
    chrome: 'none',
  },
]);

export type Surface = (typeof SURFACES)[number];
export type SurfaceId = Surface['id'];

/** sectionsFor is the sidebar, derived so it cannot drift from the surface list. */
export function sectionsFor(section: SectionId): readonly Surface[] {
  return SURFACES.filter((surface) => surface.section === section);
}

/** Whether a route may render before the SPA has a live session. */
export function allowsAnonymousSession(surface: RouteDefinition): boolean {
  switch (surface.mode) {
    case 'public':
      return true;
    case 'ceremony':
      // Establishing ceremonies render Login in place. This preserves their
      // state-bearing URL; required-session ceremonies instead follow the
      // ordinary anonymous fallback to /login.
      return surface.session === 'establish-or-reuse';
    case 'authenticated':
      return false;
  }
}

/**
 * needsOrg reports whether a surface's path carries the active organisation.
 *
 * Derived from the path rather than declared beside it: a second flag is a
 * second thing to forget, and the parameter is the fact.
 */
export function needsOrg(surface: Surface): boolean {
  return surface.path.includes(':org');
}

/** needsProject reports whether a surface's path carries a project, path-derived like needsOrg. */
export function needsProject(surface: Surface): boolean {
  return surface.path.includes(':project');
}

export function surfaceById(id: SurfaceId): Surface {
  const found = SURFACES.find((s) => s.id === id);
  if (found === undefined) {
    throw new Error(`unknown surface ${id}`);
  }
  return found;
}
