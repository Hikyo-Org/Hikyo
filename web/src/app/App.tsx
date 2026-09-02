import { lazy, Suspense, type ReactElement } from 'react';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router';

import { NotFound, Overview } from '../routes/Placeholder.tsx';
import { Shell } from '../routes/Shell.tsx';
import {
  allowsAnonymousSession,
  SURFACES,
  surfaceById,
  type SurfaceId,
} from './navigation.ts';
import { ToastViewport } from './notifications.tsx';
import { useAuth } from './AuthProvider.tsx';

const loadAuthRoutes = () => import('./route-groups/auth.ts');
const loadWorkspaceRoutes = () => import('./route-groups/workspace.ts');
const loadSettingsRoutes = () => import('./route-groups/settings.ts');

const Login = lazy(() => loadAuthRoutes().then((routes) => ({ default: routes.Login })));
const EstablishCredential = lazy(() =>
  loadAuthRoutes().then((routes) => ({ default: routes.EstablishCredential })),
);
const CLIReauth = lazy(() => loadAuthRoutes().then((routes) => ({ default: routes.CLIReauth })));
const OIDCDone = lazy(() => loadAuthRoutes().then((routes) => ({ default: routes.OIDCDone })));
const WorkspaceApprove = lazy(() => loadAuthRoutes().then((routes) => ({ default: routes.WorkspaceApprove })));
const WorkspaceCallback = lazy(() => loadAuthRoutes().then((routes) => ({ default: routes.WorkspaceCallback })));

const Matrix = lazy(() => loadWorkspaceRoutes().then((routes) => ({ default: routes.Matrix })));
const Projects = lazy(() => loadWorkspaceRoutes().then((routes) => ({ default: routes.Projects })));
const Remotes = lazy(() => loadWorkspaceRoutes().then((routes) => ({ default: routes.Remotes })));
const Values = lazy(() => loadWorkspaceRoutes().then((routes) => ({ default: routes.Values })));
const WorkspaceScope = lazy(() => loadWorkspaceRoutes().then((routes) => ({ default: routes.WorkspaceScope })));

const AccountSecurity = lazy(() => loadSettingsRoutes().then((routes) => ({ default: routes.AccountSecurity })));
const Audit = lazy(() => loadSettingsRoutes().then((routes) => ({ default: routes.Audit })));
const ChangeApprovals = lazy(() => loadSettingsRoutes().then((routes) => ({ default: routes.ChangeApprovals })));
const InstanceAdmin = lazy(() => loadSettingsRoutes().then((routes) => ({ default: routes.InstanceAdmin })));
const MachineAccess = lazy(() => loadSettingsRoutes().then((routes) => ({ default: routes.MachineAccess })));
const Adapters = lazy(() => loadSettingsRoutes().then((routes) => ({ default: routes.Adapters })));
const Members = lazy(() => loadSettingsRoutes().then((routes) => ({ default: routes.Members })));
const OrgSettings = lazy(() => loadSettingsRoutes().then((routes) => ({ default: routes.OrgSettings })));
const ProjectSettings = lazy(() => loadSettingsRoutes().then((routes) => ({ default: routes.ProjectSettings })));
const ScimProvisioning = lazy(() => loadSettingsRoutes().then((routes) => ({ default: routes.ScimProvisioning })));

const withRouteFallback = (element: ReactElement) => (
  <Suspense fallback={<p className="login" role="status">Loading…</p>}>
    {element}
  </Suspense>
);

/**
 * ELEMENTS is what each locked surface renders.
 *
 * `Record<SurfaceId, …>` is the structural half of the flow registry's
 * closure: a surface added to `navigation.ts` does not compile until it has an
 * element here, and an element cannot exist for a surface the table does not
 * name. The routes below are then GENERATED from the table, so there is no
 * hand-written `<Route path=…>` for a new page to hide in — which is what the
 * S3 gate needs to stay true rather than merely be true today.
 */
const ELEMENTS: Record<SurfaceId, ReactElement> = {
  login: withRouteFallback(<Login />),
  'establish-credential': withRouteFallback(<EstablishCredential />),
  overview: <Overview />,
  projects: withRouteFallback(<Projects />),
  remotes: withRouteFallback(<Remotes />),
  // Keyed per scope: the two Members routes are siblings under the same
  // Outlet, and React would otherwise carry an open grant dialog and its draft
  // from one scope into the other.
  members: withRouteFallback(<Members key="members-org" scope={{ kind: 'org' }} />),
  'org-settings': withRouteFallback(<OrgSettings />),
  scim: withRouteFallback(<ScimProvisioning />),
  audit: withRouteFallback(<Audit key="audit-org" />),
  // Keyed per scope for the same reason as Members: the two audit routes are
  // siblings under one Outlet, and a picked environment or an applied filter
  // must not ride from the org trail into the project one.
  'project-audit': withRouteFallback(<Audit key="audit-project" />),
  'project-settings': withRouteFallback(<ProjectSettings />),
  'change-approvals': withRouteFallback(<ChangeApprovals />),
  'instance-admin': withRouteFallback(<InstanceAdmin />),
  'instance-members': withRouteFallback(<Members key="members-instance" scope={{ kind: 'instance' }} />),
  settings: withRouteFallback(<AccountSecurity />),
  // The three product surfaces are wrapped in WorkspaceScope: reached with a
  // `?remote=<name>` parameter they operate that remote over its bearer, and
  // without one they render exactly as before against this instance (#71).
  matrix: withRouteFallback(
    <WorkspaceScope>
      <Matrix />
    </WorkspaceScope>
  ),
  // The same surface with its history drawer open. The route table is the only
  // place that knows the path, so the element reads the state as a prop rather
  // than sniffing the location.
  history: withRouteFallback(
    <WorkspaceScope>
      <Matrix historyOpen />
    </WorkspaceScope>
  ),
  // The catalogue declaration detail is the matrix with one key's declaration
  // open — same component, same workspace wrapper as `history`, so a `?remote`
  // deep link resolves the key against the workspace's instance, not this one.
  'key-detail': withRouteFallback(
    <WorkspaceScope>
      <Matrix keyDetailOpen />
    </WorkspaceScope>
  ),
  values: withRouteFallback(
    <WorkspaceScope>
      <Values />
    </WorkspaceScope>
  ),
  'machine-access': withRouteFallback(<MachineAccess />),
  adapters: withRouteFallback(<Adapters />),
  'cli-reauth': withRouteFallback(<CLIReauth />),
  'workspace-approve': withRouteFallback(<WorkspaceApprove />),
  'workspace-callback': withRouteFallback(<WorkspaceCallback />),
  'oidc-done': withRouteFallback(<OIDCDone />),
};

/**
 * Chrome and session access both come from the route registry. `section`
 * remains navigation placement only: `values` has no sidebar section but is
 * still an authenticated shell route.
 */
const shellSurfaces = SURFACES.filter((surface) => surface.chrome === 'shell');

/**
 * Routes reachable WITHOUT a session. Public routes need no session;
 * establishing ceremonies render Login in place so their state-bearing URL
 * survives authentication.
 *
 * The approve page is here deliberately and it is the non-obvious one: a first
 * establishment lands in a popup carrying no cookies for this instance at all,
 * so bouncing it to `/login` would throw away the `state` the whole transaction
 * is addressed by. It renders the sign-in form itself instead, and the URL —
 * with its state — survives.
 *
 * The callback page is here for a duller reason: it only reads two query
 * parameters and shouts them down a channel, and making that depend on a
 * session would break the one arc where the human has no session yet.
 */
const anonymousSurfaces = SURFACES.filter(allowsAnonymousSession);

/** Every non-login route that renders without shell chrome after sign-in. */
const sessionChromelessSurfaces = SURFACES.filter(
  (surface) => surface.chrome === 'none' && surface.id !== 'login',
);

/**
 * The application root.
 *
 * AuthProvider resolves and revalidates the one root identity. Checking and
 * transitions render no chrome, so an old session cannot keep painting while
 * its replacement is being bound to a fresh cache.
 */
export function App() {
  const auth = useAuth();

  if (auth.failure !== null) {
    return <>
      <main className="login">
        <p className="alert" role="alert">
          <span className="alert__glyph" aria-hidden="true">
            !
          </span>
          <span>Could not reach the server. Reload once it is back.</span>
        </p>
      </main>
      <ToastViewport />
    </>;
  }

  let live: typeof auth.identity = null;
  switch (auth.state.status) {
    case 'checking':
    case 'transitioning':
      // Deliberately quiet: this resolves in one round trip against a local
      // server, and a spinner that appears for 20ms is noise, not feedback.
      return <><p className="login" role="status">Loading…</p><ToastViewport /></>;
    case 'anonymous':
      break;
    case 'authenticated':
      live = auth.identity;
      if (live === null) {
        throw new Error('authenticated root state has no identity');
      }
  }

  return <>
    <BrowserRouter>
      {live === null ? (
        <Routes>
          {anonymousSurfaces.map((surface) => (
            <Route key={surface.id} path={surface.path} element={ELEMENTS[surface.id]} />
          ))}
          <Route path="*" element={<Navigate to={surfaceById('login').path} replace />} />
        </Routes>
      ) : (
        <Routes>
          <Route
            path={surfaceById('login').path}
            element={<Navigate to={surfaceById('projects').path} replace />}
          />
          {sessionChromelessSurfaces.map((surface) => (
            <Route key={surface.id} path={surface.path} element={ELEMENTS[surface.id]} />
          ))}
          <Route element={<Shell session={live} />}>
            {shellSurfaces.map((surface) => (
              <Route key={surface.id} path={surface.path} element={ELEMENTS[surface.id]} />
            ))}
            <Route path="*" element={<NotFound />} />
          </Route>
        </Routes>
      )}
    </BrowserRouter>
    <ToastViewport />
  </>;
}
