# Chrome + Settings Unification (#567) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One navigation table drives every sidebar (desktop and mobile) with a stacked context block; every scope is a {Members, Settings} pair including a new `/instance/members` surface; InstanceAdmin becomes "Instance settings" without prototype fiction.

**Architecture:** `navigation.ts` gains a typed `section` union and a `sectionsFor()` helper. A new pure module `sidebar-model.ts` turns (matched surface, params, operator flag, orgs, projects, remote) into sidebar blocks; `Shell.tsx` renders those blocks for desktop and the mobile drawer alike. `Members.tsx` takes a scope (`org | org+project | instance`) instead of reading its scope from the route directly, and `/instance/members` renders it at instance scope.

**Tech Stack:** React 19, React Router 8, TanStack Query 5, Zod 4, Vitest (happy-dom), Playwright 1.62, plain CSS (`web/src/styles/app.css`), TypeScript. Node 24 via fnm.

**Spec:** `docs/superpowers/specs/2026-09-01-chrome-settings-invite-design.md` (PR1 section). Ticket: #567.

## Global Constraints

- Every commit DCO signed: `git -c commit.gpgsign=false commit -s` (worktree has no gpg pinentry).
- Node/pnpm: `eval "$(fnm env)" && fnm use 24` before any `pnpm` in `web/`.
- No `as` casts; parse, don't cast. No new dependencies. No `z.any`.
- en-GB **Organisation** in every user-visible string (identifiers untouched).
- One label per surface, taken from `navigation.ts`; no hand-written sidebar link text anywhere.
- Pinned chrome geometry must hold: sidebar 218px, rail 56px, header 61px, context links 38px min-height / 13px, org avatar 28px, group rows 38px.
- The route registry closure: every `chrome: 'shell'` surface must be claimed by a flow in `web/e2e/registry.ts`, and a new surface's flow must ride a spec file already listed in a `ci.yml` group on main (`instance-admin.spec.ts` is in group 3).
- Playwright locally: `NODE_OPTIONS=--dns-result-order=ipv4first`, claim unused `HIKYO_E2E_PORT*` values (check `lsof -nP -iTCP -sTCP:LISTEN` first), sandbox off.
- Fix everything found on the way (campsite rule); no follow-up tickets for things this PR touches.

---

### Task 1: Navigation table — typed sections, `instance-members` surface, `sectionsFor`

**Files:**
- Modify: `web/src/app/navigation.ts`
- Modify: `web/src/app/App.tsx:63-110` (ELEMENTS)
- Modify: `web/e2e/registry.ts:66-70` (claim the new surface)
- Test: `web/src/app/navigation.test.ts`

**Interfaces:**
- Produces: `export type SectionId = 'project' | 'instance' | 'organisation' | 'account'`;
  `SurfaceBase.section: SectionId | null`;
  `export function sectionsFor(section: SectionId): readonly Surface[]` (surfaces in table order);
  `export function needsProject(surface: Surface): boolean`;
  surface ids `'instance-members'` (path `/instance/members`, label `Instance members`);
  changed labels: `instance-admin` → `Instance settings`, `members` → `Members`.
- Consumes: nothing new.

- [ ] **Step 1: Write the failing tests**

Replace the first test's expected list and add three tests in `web/src/app/navigation.test.ts`:

```ts
import { describe, expect, it } from 'vitest';

import {
  allowsAnonymousSession,
  needsOrg,
  needsProject,
  routeRegistryViolations,
  sectionsFor,
  SURFACES,
} from './navigation.ts';

describe('the route policy registry', () => {
  it('gives every current route one explicit access and chrome policy', () => {
    expect(SURFACES.map((surface) => `${surface.id}:${surface.mode}:${surface.chrome}`)).toEqual([
      'login:public:none',
      'overview:authenticated:shell',
      'projects:authenticated:shell',
      'remotes:authenticated:shell',
      'members:authenticated:shell',
      'org-settings:authenticated:shell',
      'scim:authenticated:shell',
      'audit:authenticated:shell',
      'instance-admin:authenticated:shell',
      'instance-members:authenticated:shell',
      'settings:authenticated:shell',
      'matrix:authenticated:shell',
      'history:authenticated:shell',
      'key-detail:authenticated:shell',
      'values:authenticated:shell',
      'machine-access:authenticated:shell',
      'project-settings:authenticated:shell',
      'cli-reauth:ceremony:none',
      'workspace-approve:ceremony:none',
      'workspace-callback:public:none',
      'oidc-done:public:none',
    ]);
    expect(routeRegistryViolations(SURFACES)).toEqual([]);
  });

  it('derives every sidebar section from the table, in table order', () => {
    expect(sectionsFor('organisation').map((s) => s.label)).toEqual([
      'Overview',
      'Projects',
      'Remotes',
      'Members',
      'Organisation settings',
      'SCIM provisioning',
      'Audit',
    ]);
    expect(sectionsFor('project').map((s) => s.id)).toEqual([
      'matrix',
      'machine-access',
      'project-settings',
    ]);
    expect(sectionsFor('instance').map((s) => s.label)).toEqual([
      'Instance settings',
      'Instance members',
    ]);
    expect(sectionsFor('account').map((s) => s.id)).toEqual(['settings']);
  });

  it('puts every shell surface with a section in exactly one section', () => {
    const sectioned = SURFACES.filter((s) => s.section !== null).map((s) => s.id);
    const listed = (['project', 'instance', 'organisation', 'account'] as const).flatMap((k) =>
      sectionsFor(k).map((s) => s.id),
    );
    expect([...listed].sort()).toEqual([...sectioned].sort());
    expect(new Set(listed).size).toBe(listed.length);
  });

  it('marks project-scoped surfaces by their path, not by declaration', () => {
    expect(needsProject(SURFACES.find((s) => s.id === 'matrix')!)).toBe(true);
    expect(needsOrg(SURFACES.find((s) => s.id === 'matrix')!)).toBe(true);
    expect(needsProject(SURFACES.find((s) => s.id === 'members')!)).toBe(false);
    expect(needsOrg(SURFACES.find((s) => s.id === 'instance-members')!)).toBe(false);
  });
  // …keep the remaining existing tests unchanged, except the last one:
  // `section: 'Account'` becomes `section: 'account'` and the expected message
  // becomes `appears in section "account"`.
});
```

Note: the `!` non-null assertions above are in test code only; production code keeps the no-cast rule.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && eval "$(fnm env)" && fnm use 24 && pnpm vitest run src/app/navigation.test.ts`
Expected: FAIL — `sectionsFor`/`needsProject` not exported; expected list mismatch.

- [ ] **Step 3: Implement the table**

In `web/src/app/navigation.ts`:

1. Add the union and change `SurfaceBase`:

```ts
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
```

`RouteCandidate.section` stays `string | null` (the violation checker accepts loose input).

2. Change the entries:
   - `overview`, `projects`, `remotes`, `members`, `org-settings`, `scim`, `audit`: `section: 'organisation'`.
   - `members` label → `'Members'`.
   - Move the `project-settings` entry to directly AFTER `machine-access` (table order is sidebar order: matrix, machine access, project settings).
   - `matrix`, `machine-access`, `project-settings`: `section: 'project'`. Rewrite each of their comments to say: *"Project-scoped: the sidebar's project context block fills `:org` and `:project` from the route, exactly as the organisation block fills `:org`. This reverses the earlier 'no static entry could know which project' reasoning — the entry is no longer static."*
   - `instance-admin`: label `'Instance settings'`, `section: 'instance'`.
   - Add after `instance-admin`:

```ts
  // Instance members (#567): the Members surface at instance scope, so every
  // scope is a {Members, Settings} pair. Operator-only, like instance settings.
  {
    id: 'instance-members',
    path: '/instance/members',
    label: 'Instance members',
    section: 'instance',
    mode: 'authenticated',
    chrome: 'shell',
  },
```
   - `settings`: `section: 'account'`.
   - `history`, `key-detail`, `values` keep `null` (their comments already say they are reached from the matrix).

3. Replace `SECTIONS` with:

```ts
/** sectionsFor is the sidebar, derived so it cannot drift from the surface list. */
export function sectionsFor(section: SectionId): readonly Surface[] {
  return SURFACES.filter((surface) => surface.section === section);
}

export function needsProject(surface: Surface): boolean {
  return surface.path.includes(':project');
}
```

Delete the `Section` type and `SECTIONS` export. In `Shell.tsx` keep it compiling with a LOCAL temporary table that Task 2 deletes: replace the `SECTIONS` import with `sectionsFor`, and above `Shell` add

```ts
// Temporary until the sidebar model lands (Task 2 of the #567 plan).
const SECTIONS = [
  { title: 'Organisation', items: sectionsFor('organisation') },
  { title: 'Instance', items: sectionsFor('instance') },
  { title: 'Account', items: sectionsFor('account') },
] as const;
```

`grep -rn "SECTIONS" web/src` must then show only Shell.

4. In `web/src/app/App.tsx` ELEMENTS add `'instance-members': withRouteFallback(<Members />),` right after `'instance-admin'`. (Task 3 changes it to `<Members scope={{ kind: 'instance' }} />`; until then Members reads an empty `:org` and renders its refusal states, which is acceptable for a non-shipped intermediate commit.)

5. In `web/e2e/registry.ts` change the instance-admin flow to claim both:

```ts
  // Instance members (#567) is a new SURFACE, so the S3 closure demands a flow,
  // but it cannot get its own spec FILE (the merge gate loads `ci.yml` from the
  // base branch); it rides `instance-admin.spec.ts` — already in group 3 and
  // the operator sibling surface.
  {
    id: 'instance-admin',
    spec: 'flows/instance-admin.spec.ts',
    surfaces: ['instance-admin', 'instance-members'],
  },
```

- [ ] **Step 4: Run tests and typecheck**

Run: `cd web && pnpm vitest run src/app && pnpm typecheck`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/app/navigation.ts web/src/app/navigation.test.ts web/src/app/App.tsx web/e2e/registry.ts
git -c commit.gpgsign=false commit -s -m "feat(web): typed sidebar sections and the instance-members surface (#567)"
```

---

### Task 2: Sidebar model + Shell renders one table for desktop and mobile

**Files:**
- Create: `web/src/routes/sidebar-model.ts`
- Create: `web/src/routes/sidebar-model.test.ts`
- Modify: `web/src/routes/Shell.tsx` (lines 27, 210-235 crumbs, 340-372 rail action, 376-520 sidebar, 650-860 `ProjectNavigation`/`MobileAccountNavigation`, 1155-1168 `chromeCrumbLabel`)
- Modify: `web/src/styles/app.css:650-663, 724-725, 794-900` (rename `.project-sidebar*` → `.context-sidebar*`)
- Modify e2e pins: `web/e2e/flows/shell.spec.ts:55-98,300-306`, `web/e2e/flows/members.spec.ts:122-140,448,462`, `web/e2e/flows/matrix.spec.ts:64,391,480`, `web/e2e/flows/scanning.spec.ts:133`

**Interfaces:**
- Consumes: `sectionsFor`, `needsOrg`, `needsProject`, `surfaceById`, `Surface` from Task 1; `withRemote` from `../api/transport.tsx`.
- Produces:

```ts
export type SidebarLink = {
  readonly id: string;            // surface id, or 'project-members' for the filtered projection
  readonly label: string;
  readonly to: string;            // href
  readonly end: boolean;          // NavLink `end`
  readonly disabledReason: string | null; // non-null → rendered as the disabled span
};
export type SidebarBlock = {
  readonly kind: 'project' | 'instance' | 'organisation' | 'account';
  readonly title: string;         // 'Organisation' | 'Instance' | 'You'; project block titles itself in Shell
  readonly links: readonly SidebarLink[];
};
export type SidebarModel = {
  readonly context: SidebarBlock | null;   // project or instance block, or null
  readonly organisation: SidebarBlock | null; // null while no org is active
  readonly instance: SidebarBlock | null;  // operator-only; null when context IS the instance block
  readonly account: SidebarBlock;
};
export function sidebarModel(input: {
  readonly surface: Surface | undefined;
  readonly activeOrgId: string;
  readonly routeProjectId: string;
  readonly activeProjectId: string;
  readonly remote: string;
  readonly isInstanceOperator: boolean;
}): SidebarModel;
export function isLinkActive(link: SidebarLink, pathname: string, search: string): boolean;
```

- [ ] **Step 1: Write the failing test**

`web/src/routes/sidebar-model.test.ts`:

```ts
import { describe, expect, it } from 'vitest';

import { surfaceById } from '../app/navigation.ts';
import { isLinkActive, sidebarModel } from './sidebar-model.ts';

const base = {
  activeOrgId: 'org_1',
  routeProjectId: '',
  activeProjectId: 'prj_1',
  remote: '',
  isInstanceOperator: true,
};

describe('sidebarModel', () => {
  it('shows only the organisation block on an org-scoped route', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('projects') });
    expect(model.context).toBeNull();
    expect(model.organisation?.links.map((l) => l.label)).toEqual([
      'Overview', 'Projects', 'Remotes', 'Members', 'Organisation settings', 'SCIM provisioning', 'Audit',
    ]);
    expect(model.organisation?.links.map((l) => l.to)).toEqual([
      '/', '/projects', '/remotes', '/orgs/org_1/members', '/orgs/org_1/settings', '/orgs/org_1/scim', '/orgs/org_1/audit',
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
      ['Members', '/orgs/org_1/members?project=prj_1'],
      ['Project settings', '/orgs/org_1/projects/prj_1/settings'],
    ]);
    expect(model.organisation).not.toBeNull();
  });

  it('disables the local-only project destinations for a remote workspace', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('matrix'), routeProjectId: 'prj_1', remote: 'ew' });
    const byLabel = Object.fromEntries((model.context?.links ?? []).map((l) => [l.label, l]));
    expect(byLabel['Environment matrix']?.to).toBe('/orgs/org_1/projects/prj_1/matrix?remote=ew');
    expect(byLabel['Environment matrix']?.disabledReason).toBeNull();
    expect(byLabel['Machine access']?.disabledReason).toBe('Machine access is not available for remote workspaces yet');
    expect(byLabel['Members']?.disabledReason).toBe('Members is not available for remote workspaces yet');
    expect(byLabel['Project settings']?.disabledReason).toBe('Project settings is not available for remote workspaces yet');
  });

  it('makes the instance block the context on an instance route and does not repeat it', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('instance-members') });
    expect(model.context?.kind).toBe('instance');
    expect(model.context?.links.map((l) => l.to)).toEqual(['/instance', '/instance/members']);
    expect(model.instance).toBeNull();
    expect(model.organisation).not.toBeNull();
  });

  it('hides the instance block from non-operators and org links while no org is active', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('projects'), isInstanceOperator: false, activeOrgId: '' });
    expect(model.instance).toBeNull();
    expect(model.organisation?.links.map((l) => l.to)).toEqual(['/', '/projects', '/remotes']);
  });

  it('activates the filtered members projection only with its project query', () => {
    const model = sidebarModel({ ...base, surface: surfaceById('members'), routeProjectId: 'prj_1' });
    const members = model.context?.links.find((l) => l.id === 'project-members');
    const orgMembers = model.organisation?.links.find((l) => l.id === 'members');
    expect(members).toBeDefined();
    expect(orgMembers).toBeDefined();
    expect(isLinkActive(members!, '/orgs/org_1/members', '?project=prj_1')).toBe(true);
    expect(isLinkActive(orgMembers!, '/orgs/org_1/members', '?project=prj_1')).toBe(false);
    expect(isLinkActive(orgMembers!, '/orgs/org_1/members', '')).toBe(true);
    expect(isLinkActive(members!, '/orgs/org_1/members', '')).toBe(false);
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && pnpm vitest run src/routes/sidebar-model.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `sidebar-model.ts`**

```ts
import { generatePath } from 'react-router';

import { needsOrg, needsProject, sectionsFor, surfaceById, type Surface } from '../app/navigation.ts';
import { withRemote } from '../api/transport.tsx';

export type SidebarLink = {
  readonly id: string;
  readonly label: string;
  readonly to: string;
  readonly end: boolean;
  readonly disabledReason: string | null;
};

export type SidebarBlock = {
  readonly kind: 'project' | 'instance' | 'organisation' | 'account';
  readonly title: string;
  readonly links: readonly SidebarLink[];
};

export type SidebarModel = {
  readonly context: SidebarBlock | null;
  readonly organisation: SidebarBlock | null;
  readonly instance: SidebarBlock | null;
  readonly account: SidebarBlock;
};

const localOnly = (label: string) => `${label} is not available for remote workspaces yet`;

function link(surface: Surface, to: string, disabledReason: string | null = null): SidebarLink {
  return { id: surface.id, label: surface.label, to, end: surface.path === '/', disabledReason };
}

/**
 * sidebarModel is the WHOLE sidebar as data. Desktop renders `context` and
 * `organisation`; the mobile drawer renders those plus `instance` and
 * `account`. Nothing here is hand-written per mode, so the two cannot drift.
 */
export function sidebarModel(input: {
  readonly surface: Surface | undefined;
  readonly activeOrgId: string;
  readonly routeProjectId: string;
  readonly activeProjectId: string;
  readonly remote: string;
  readonly isInstanceOperator: boolean;
}): SidebarModel {
  const { surface, activeOrgId, routeProjectId, activeProjectId, remote, isInstanceOperator } = input;

  const organisation: SidebarBlock | null = {
    kind: 'organisation',
    title: 'Organisation',
    links: sectionsFor('organisation')
      .filter((item) => !needsOrg(item) || activeOrgId !== '')
      .map((item) =>
        link(item, needsOrg(item) ? generatePath(item.path, { org: activeOrgId }) : item.path),
      ),
  };

  const instanceBlock: SidebarBlock | null = isInstanceOperator
    ? { kind: 'instance', title: 'Instance', links: sectionsFor('instance').map((item) => link(item, item.path)) }
    : null;

  const account: SidebarBlock = {
    kind: 'account',
    title: 'You',
    links: sectionsFor('account').map((item) => link(item, item.path)),
  };

  let context: SidebarBlock | null = null;
  if (routeProjectId !== '' && activeOrgId !== '') {
    const params = { org: activeOrgId, project: activeProjectId };
    const membersPath = `${generatePath(surfaceById('members').path, { org: activeOrgId })}?project=${encodeURIComponent(activeProjectId)}`;
    const projectLinks = sectionsFor('project').map((item) => {
      const path = generatePath(item.path, params);
      if (item.id === 'matrix') return link(item, withRemote(path, remote));
      return link(item, path, remote === '' ? null : localOnly(item.label));
    });
    // The filtered members projection sits before Project settings, so the
    // block reads: matrix, machine access, members, settings — narrow to wide.
    const members: SidebarLink = {
      id: 'project-members',
      label: surfaceById('members').label,
      to: membersPath,
      end: false,
      disabledReason: remote === '' ? null : localOnly(surfaceById('members').label),
    };
    const settingsIndex = projectLinks.findIndex((l) => l.id === 'project-settings');
    const links = [...projectLinks.slice(0, settingsIndex), members, ...projectLinks.slice(settingsIndex)];
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
```

Note `needsProject` is imported for future use only if you need it in Shell; if unused, drop the import (lint fails on unused imports).

- [ ] **Step 4: Run the model tests**

Run: `cd web && pnpm vitest run src/routes/sidebar-model.test.ts`
Expected: PASS.

- [ ] **Step 5: Rewrite the Shell sidebar to render the model**

In `web/src/routes/Shell.tsx`:

1. Imports: replace `needsOrg, SECTIONS, SURFACES, surfaceById, type Surface` with `needsOrg, SURFACES, surfaceById, type Surface` and add `import { isLinkActive, sidebarModel, type SidebarBlock, type SidebarLink } from './sidebar-model.ts';`.

2. After `const onSidebarNavigate = …` compute:

```tsx
  const model = sidebarModel({
    surface: here?.surface,
    activeOrgId,
    routeProjectId,
    activeProjectId,
    remote,
    isInstanceOperator,
  });
```

3. Replace the whole `{routeProjectId !== '' ? (<ProjectNavigation …/>) : SECTIONS.map(…)}` expression with:

```tsx
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
```

4. Replace the `{routeProjectId === '' ? null : (<MobileAccountNavigation …/>)}` expression (after the mobile Projects switcher) with:

```tsx
        {model.instance === null ? null : (
          <SidebarSection block={model.instance} mobileOnly onNavigate={dismissNavigation} />
        )}
        <SidebarSection block={model.account} mobileOnly onNavigate={dismissNavigation} />
```

5. Change the mobile organisations heading text from `Organizations` to `Organisations`.

6. Delete `ProjectNavigation` and `MobileAccountNavigation`. Add these three components (below `Shell`):

```tsx
function SidebarLinkItem({ link, onNavigate }: { link: SidebarLink; onNavigate: () => void }) {
  const location = useLocation();
  if (link.disabledReason !== null) {
    return (
      <span className="sidebar__link sidebar__link--disabled" aria-disabled="true" title={link.disabledReason}>
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

function InstanceContext({ links, onNavigate }: { links: readonly SidebarLink[]; onNavigate: () => void }) {
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
        <span className="avatar context-sidebar__org-avatar" style={chromeIdentityStyle(orgIdentity)}>
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
            {/* keep the existing groups + problems buttons verbatim, renaming
                `project-sidebar__group` → `context-sidebar__group` and
                `project-sidebar__group-count` → `context-sidebar__group-count` */}
          </div>
        )}
        {rest.map((link) => (
          <SidebarLinkItem key={link.id} link={link} onNavigate={onNavigate} />
        ))}
      </nav>
    </section>
  );
}
```

Copy the groups/problems button JSX from the old `ProjectNavigation` (Shell.tsx lines 748-792) into the `context-sidebar__groups` div unchanged except for the two class renames.

7. The desktop rail action already uses `surfaceById('instance-admin')`; its `aria-label`/`title` must be the surface label, not a literal: `aria-label={surfaceById('instance-admin').label} title={surfaceById('instance-admin').label}`.

8. Breadcrumb: replace the `crumbs` memo with

```tsx
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
```

and `chromeCrumbLabel`:

```ts
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
```

9. `chooseOrg`: add `if (here.surface.section === 'instance') return;` before the `needsOrg` check is not needed (instance paths have no `:org`, `needsOrg` is false) — leave as is.

- [ ] **Step 6: Rename the CSS**

In `web/src/styles/app.css` replace every `project-sidebar` with `context-sidebar` (`sed -i '' 's/project-sidebar/context-sidebar/g' web/src/styles/app.css`). Then:
- Replace the `.context-sidebar__organisation` rules (old lines 656-663) with `.sidebar__section--organisation { margin-top: 0; }` and `.context-sidebar + .sidebar__section--organisation h2 { padding-top: 30px; }` — the organisation block follows a context block with the same 30px separation the old project mode had, and sits flush at the top otherwise.
- Verify no remaining `project-sidebar` string: `grep -rn "project-sidebar" web/src` → none.

- [ ] **Step 7: Update the Playwright pins**

`web/e2e/flows/shell.spec.ts` lines 55-82:
- `.project-sidebar__org-avatar` → `.context-sidebar__org-avatar`; `.project-sidebar__org small` → `.context-sidebar__org small`; `.project-sidebar__group` → `.context-sidebar__group`.
- `rail.getByRole('link', { name: 'Instance administration' })` → `{ name: 'Instance settings' }` (also lines 97 and 300-306).
- `sidebar.getByRole('heading', { name: 'Organization' })` → `{ name: 'Organisation' }`.
- Replace `await expect(projectNav.getByRole('link', { name: 'Machine access' })).toHaveCount(0);` with `.toBeVisible()` and add:

```ts
    await expect(projectNav.getByRole('link', { name: 'Members' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Audit' })).toBeVisible();
    await expect(sidebar.getByRole('link', { name: 'Account & security' })).toHaveCount(0);
    await expect(sidebar.getByRole('link', { name: 'Instance settings' })).toHaveCount(0);
```
- Mobile drawer test (84-113): keep `'Account & security'`; change `'Instance administration'` → `'Instance settings'` and add `await expect(drawer.getByRole('link', { name: 'Instance members' })).toBeVisible();`.
- Add a desktop test after the composition test:

```ts
  test('stacks the instance context above the organisation block on instance routes', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'desktop sidebar composition');
    await page.goto('/instance/members');
    const sidebar = page.getByRole('navigation', { name: 'Sections', exact: true });
    const instanceNav = sidebar.getByRole('navigation', { name: 'Instance' });
    await expect(instanceNav.getByRole('link', { name: 'Instance members' })).toHaveAttribute('aria-current', 'page');
    await expect(instanceNav.getByRole('link', { name: 'Instance settings' })).toBeVisible();
    await expect(sidebar.getByRole('heading', { name: 'Organisation' })).toBeVisible();
    await expect(page.getByLabel('Breadcrumb')).toContainText('Instance');
  });
```

`web/e2e/flows/members.spec.ts`:
- 129, 137: `'Members & access'` → `'Members'`.
- 135: heading `'Members & access · payments'` → `'Members · payments'` (Task 3 changes the h1; do the pin here so the file is edited once).
- 448: region name `'Organizations'` → `'Organisations'`.
- 462: `getByRole('link', { name: 'Members' })` now matches two links on a project route; this test is on `/projects`, so it stays unique — leave.

`web/e2e/flows/matrix.spec.ts`: 64 `.project-sidebar` → `.context-sidebar`; 391 `.project-sidebar__group` → `.context-sidebar__group`; 480 `{ name: 'Members & access', exact: true }` → `page.getByRole('navigation', { name: 'Project' }).getByRole('link', { name: 'Members', exact: true })`.

`web/e2e/flows/scanning.spec.ts:133`: `.project-sidebar__group` → `.context-sidebar__group`.

- [ ] **Step 8: Typecheck, unit tests, lint**

Run: `cd web && pnpm typecheck && pnpm vitest run && pnpm exec oxlint src e2e 2>/dev/null || pnpm lint`
Expected: all PASS (Shell.account/update tests still green).

- [ ] **Step 9: Commit**

```bash
git add web/src/routes/sidebar-model.ts web/src/routes/sidebar-model.test.ts web/src/routes/Shell.tsx web/src/styles/app.css web/e2e/flows/shell.spec.ts web/e2e/flows/members.spec.ts web/e2e/flows/matrix.spec.ts web/e2e/flows/scanning.spec.ts
git -c commit.gpgsign=false commit -s -m "feat(web): one sidebar table with a stacked context block (#567)"
```

---

### Task 3: Members parametrised by scope; `/instance/members`; cross-link labels

**Files:**
- Modify: `web/src/routes/Members.tsx` (lines 96-130 scope resolution, 198-235 heading/lede, 235-245 hidden copy, 380-400 actions, 640-700 `GrantModal` props)
- Modify: `web/src/app/App.tsx` (ELEMENTS `members`, `instance-members`)
- Modify: `web/src/routes/OrgSettings.tsx:79,156-172`, `web/src/routes/ProjectSettings.tsx:245-262`
- Modify e2e: `web/e2e/flows/instance-admin.spec.ts:109-118, 278-289, 405-420, 600-610, 838-870`, `web/e2e/flows/settings.spec.ts:102,661`, `web/e2e/flows/instance-admin.spec.ts:472`
- Test: `web/src/routes/Members.instance.test.tsx` (new)

**Interfaces:**
- Produces: `export type MembersScope = { readonly kind: 'org' } | { readonly kind: 'instance' };` and `export function Members({ scope }: { scope: MembersScope })`. In `org` mode the org id and the optional `?project=` still come from the route (deep links must keep working); `instance` mode reads no route params.
- Consumes: `useInstanceGrants`, `useInstanceOrgs`, `scopeValue`, `ScopeOption`, `capabilitiesAt('instance')`, `templatesAt('instance')` (all exist in `web/src/api/access.ts` / `access-templates.ts`).

- [ ] **Step 1: Write the failing test**

`web/src/routes/Members.instance.test.tsx` — mirror the harness in `web/src/routes/AccessibilityPolish.test.tsx` (it renders `Members` inside `MemoryRouter` with mocked `fetch`; copy its `renderForm`/mock setup exactly, then):

```tsx
import { screen } from '@testing-library/react';
// …same imports/mocks as AccessibilityPolish.test.tsx

describe('Members at instance scope', () => {
  it('lists instance grants under an instance heading and offers only the instance scope', async () => {
    mockFetch({
      'GET /api/v1/instance/grants': {
        items: [
          {
            id: 'grn_1', principal_id: 'prn_1', capability: 'instance-config',
            scope: {}, origins: [{ kind: 'break-glass', subject: 'host' }],
          },
        ],
        count: 1,
      },
      'GET /api/v1/orgs': { items: [], count: 0 },
    });
    render(<MemoryRouter initialEntries={['/instance/members']}><Members scope={{ kind: 'instance' }} /></MemoryRouter>);
    expect(await screen.findByRole('heading', { level: 1, name: 'Members · Instance' })).toBeInTheDocument();
    expect(await screen.findByText('instance-config')).toBeInTheDocument();
    expect(screen.getByText('break-glass: host')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'New grant' }));
    const scope = screen.getByLabelText('Scope');
    expect(within(scope).getAllByRole('option').map((o) => o.textContent)).toEqual(['This instance (every organisation)']);
  });

  it('renders a second-factor refusal as its own state', async () => {
    mockFetch({ 'GET /api/v1/instance/grants': { status: 403, body: { code: 'forbidden' } }, 'GET /api/v1/orgs': { items: [], count: 0 } });
    render(<MemoryRouter initialEntries={['/instance/members']}><Members scope={{ kind: 'instance' }} /></MemoryRouter>);
    expect(await screen.findByRole('alert')).toHaveTextContent('Instance grants require a second factor');
  });
});
```

Adapt `mockFetch` to whatever helper `AccessibilityPolish.test.tsx` uses (it already stubs grant listings by path); the assertions above are the contract.

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && pnpm vitest run src/routes/Members.instance.test.tsx`
Expected: FAIL — `scope` prop not accepted / heading missing.

- [ ] **Step 3: Implement the scope parameter**

In `Members.tsx`:

1. Add the type and change the signature:

```tsx
export type MembersScope = { readonly kind: 'org' } | { readonly kind: 'instance' };

export function Members({ scope }: { scope: MembersScope }) {
  const params = useParams();
  const [search] = useSearchParams();
  const instance = scope.kind === 'instance';
  const org = instance ? '' : params.org === undefined ? '' : params.org;
  const projectId = instance ? '' : search.get('project') ?? '';
  const orgQuery = useOrg(org);                 // disabled while org === ''
  const orgGrants = useOrgGrants(org);          // disabled while org === ''
  const instanceGrants = useInstanceGrants();   // add `enabled: instance` — see step 3.4
  const grants = instance ? instanceGrants : orgGrants;
  const topology = useOrgTopology(org);         // returns ready=false for '' — see step 3.5
  const instanceOrgs = useInstanceOrgs();       // add `enabled` param — see step 3.4
```

2. Names and options:

```tsx
  const scopeName = instance ? 'Instance' : projectId === '' ? orgName : projectName;
  const instanceOption: ScopeOption = {
    value: scopeValue({ kind: 'instance' }),
    label: 'This instance (every organisation)',
    scope: { kind: 'instance' },
    level: 'instance',
    group: 'Instance',
    isProtected: null,
  };
  const allOptions = instance ? [instanceOption] : scopeOptions(orgQuery.data?.id ?? org, orgName, topology.projects);
```

`inspectOptions` and `grantOptions` for `instance` are `[instanceOption]`; `visibleLines` for instance is `lines` unfiltered. `topology.ready` must be `true` for instance (no topology to wait for): `const topologyReady = instance ? true : topology.ready;` and use `topologyReady` where `topology.ready` gated the New grant button and the modal. Pass `topologyPending={instance ? false : topology.isPending}` / `topologyError={instance ? false : topology.isError}` to `GrantModal`.

3. Heading and copy:

```tsx
      <h1>{`Members · ${scopeName}`}</h1>
```
Lede stays. The visually-hidden paragraph under `members-list` becomes, for instance: *"Every grant written at instance scope. These inherit downward into every organisation; each origin and its subject remain visible so incident provenance is not reduced to a colour."* (this sentence carries the `inherit downward into every organisation` pin from `instance-admin.spec.ts:117`, so render it VISIBLY as `<p>` in instance mode, not visually-hidden). Empty state for instance: `'No instance-scope grants.'`.

Refusals: before the existing `grants.isError` alert add
```tsx
      {instance && grants.error instanceof ApiError && grants.error.status === 403 ? (
        <Alert>Instance grants require a second factor. Present your authenticator code or passkey in the banner above.</Alert>
      ) : instance && grants.error instanceof ApiError && grants.error.status === 404 ? (
        <p role="status">Instance grants are not disclosed to this session.</p>
      ) : grants.isError ? (
        <Alert>{membershipFailureText(grants.error)}</Alert>
      ) : null}
```
(import `ApiError` from `../api/client.ts`). Skip the `orgQuery.isError` alert when `instance`.

`GrantModal` gets `orgName={scopeName}`; nothing else in it needs to know about instance because `options` already carries the level (`capabilitiesAt('instance')`, `templatesAt('instance')` resolve from `chosen.level`). The org-scope blast-radius branch (`chosen.level === 'org'`) does not fire for instance — instance grants reach everything and the modal's hint must say so: when `chosen?.level === 'instance'` render the hint *"Instance scope reaches every organisation, current and future."* in place of the "Narrowest first…" hint.

`Inspect` capability options: for instance use `capabilitiesAt('instance')`; pass a new prop `level: 'org' | 'instance'` and pick `capabilitiesAt(level)` (keep the project filtering branch as is).

Remove the prototype-only `✉ invite member` button and its comment (lines 391-402). Also delete the sentence "There is no invitation flow in this version, so an account exists before it can hold a grant." from the principal hint (PR2 adds the real flow; PR1 must not claim there is none) — replace with *"The principal id of a person or a service account; the list offers the ones already holding something here."*

4. In `web/src/api/access.ts` give `useInstanceGrants` an `enabled` parameter defaulting to `true`: `export function useInstanceGrants(enabled = true)` → `enabled`. Same for `useInstanceOrgs(enabled = true)` in `settings.ts`. `InstanceAdmin.tsx` keeps calling them without arguments.

5. Confirm `useOrgTopology('')` is inert: `useProjects('')` is `enabled: org !== ''` (settings.ts:124-133) and `orgTopologyReadiness('', …)` returns `ready: false` — read `settings.ts:315-380` and, if `ready` is not `false` for an empty org, guard it (`if (org === '') return { projects: [], isPending: false, isError: false, ready: false }` at the top of `useOrgTopology` is NOT allowed — hooks order; instead pass the empty-org result through `orgTopologyReadiness`, which already handles it — verify with the unit test in step 1).

6. `App.tsx`: `members: withRouteFallback(<Members scope={{ kind: 'org' }} />)`, `'instance-members': withRouteFallback(<Members scope={{ kind: 'instance' }} />)`.

7. Cross-links and page titles:
   - `OrgSettings.tsx:79` `Org settings · ` → `Organisation settings · `; `:159` row title `Org members & grants` → `Members`.
   - `ProjectSettings.tsx:248` `Members & grants` → `Members`.
   - `AccessibilityPolish.test.tsx`: update any `Org settings ·` / `Org members` expectation it holds (`grep -n "Org " web/src/routes/AccessibilityPolish.test.tsx`).

- [ ] **Step 4: Run unit tests and typecheck**

Run: `cd web && pnpm vitest run src/routes/Members.instance.test.tsx src/routes/AccessibilityPolish.test.tsx && pnpm typecheck`
Expected: PASS.

- [ ] **Step 5: Move the instance-grant e2e tests to the new surface**

In `web/e2e/flows/instance-admin.spec.ts`:

- Test at 109-118 ("shows instance grants with the origin that holds them"): start with `await page.goto('/instance/members');` and replace `page.locator('#instance-grants')` with `page.locator('#members-list')`; keep the four expectations (the `inherit downward into every organisation` sentence is now visible on that panel).
- Test at 278-289 ("creates and revokes an instance grant with visible provenance"): rewrite against the Members UI:

```ts
  test('creates and revokes an instance grant with visible provenance', async ({ page }) => {
    await page.goto('/instance/members');
    await page.getByRole('button', { name: 'New grant' }).click();
    const dialog = page.getByRole('dialog');
    await dialog.getByLabel('Principal').fill(INSTANCE_GRANT_TARGET);
    await dialog.getByLabel('Scope').selectOption('instance');
    await dialog.getByRole('checkbox', { name: 'read' }).check();
    await dialog.getByRole('button', { name: 'Grant' }).click();
    const notice = page.locator('.notice').filter({ hasText: `Grant results for ${INSTANCE_GRANT_TARGET}` });
    await expectStatusIsTextAndAria(page, notice);
    const row = page.getByRole('row').filter({ hasText: INSTANCE_GRANT_TARGET });
    await expect(row).toContainText(`manual: ${seed.principal}`);
    await row.getByRole('button', { name: `Revoke read on instance for ${INSTANCE_GRANT_TARGET}` }).click();
    const revoked = page.locator('.notice').filter({ hasText: 'confirms it is absent' });
    await expectStatusIsTextAndAria(page, revoked);
    await expect(row).toHaveCount(0);
  });
```
Check the exact revoke `aria-label` and the "absent" notice wording in `Members.tsx` (`revokeLabel`, `revokeOutcomeText` in `access.ts:379`) and use them verbatim.

- Test at 291-450 ("applies the instance role template…"): the UI part at 414-420 becomes: go to `/instance/members`, open `New grant`, fill principal, select `Scope` → `instance`, choose the `Apply a role template` radio, select `operator`, click `Grant`, then assert `.notice` contains `Applied operator to ${principal}`. The revoke loop that follows uses the same row/button pattern as above.
- Second-factor panels list (600-610): delete the `['#instance-grants', …]` line and add, after that loop, a navigation to `/instance/members` asserting `page.getByRole('alert')` contains `Instance grants require a second factor`.
- Heading pins: every `{ name: 'Instance administration', level: 1 }` → `'Instance settings'` (lines 95, 191, 848). Line 472: `` `Org settings · ${name}` `` → `` `Organisation settings · ${name}` ``.
- Pinned assertion set (842-870): `.factor__meta` → `page.locator('.settings-row__detail').first()`; `.badge` → `page.locator('.settings-tag').first()` (role `'badge'`; if the computed radius is not the badge token, fix `.settings-tag` in CSS to `var(--radius-badge)` rather than the test). Add a sibling pinned block for the new surface, copied from `members.spec.ts:493-540` with `flow: 'instance-admin'`, `surface: 'instance-members'`, `page.goto('/instance/members')`, heading `Members · Instance`.

`web/e2e/flows/settings.spec.ts`: 102 `/Org settings ·/` → `/Organisation settings ·/`; 661 `/^Org settings · /` → `/^Organisation settings · /`.

- [ ] **Step 6: Commit**

```bash
git add web/src/routes/Members.tsx web/src/routes/Members.instance.test.tsx web/src/app/App.tsx web/src/api/access.ts web/src/api/settings.ts web/src/routes/OrgSettings.tsx web/src/routes/ProjectSettings.tsx web/src/routes/AccessibilityPolish.test.tsx web/e2e/flows/instance-admin.spec.ts web/e2e/flows/settings.spec.ts
git -c commit.gpgsign=false commit -s -m "feat(web): Members at instance scope; every scope is a {Members, Settings} pair (#567)"
```

---

### Task 4: Instance settings — purge fiction, drop the grants panel, fold per-id CSS

**Files:**
- Modify: `web/src/routes/InstanceAdmin.tsx` (lines 10-20 imports, 86-127 hooks/state, 128-145 heading+jump, 177-220 grants panel, 233-241 keys panel, 243-251 connected card, 274-299 policy panel prototype rows)
- Modify: `web/src/styles/app.css:4246-4298` (per-id overrides), `4875-4879` (`.origin-chips`)
- Modify: `web/prototype/mock-api.ts` (add POST stubs)
- Test: `web/src/routes/InstanceAdmin.crypto.test.tsx` (existing; must stay green), `web/e2e/prototype-mock.test.ts` (existing)

**Interfaces:**
- Consumes: nothing new. `InstanceAdmin` keeps its export name; only its h1/panels change.

- [ ] **Step 1: Write the failing test**

Add to `web/src/routes/InstanceAdmin.crypto.test.tsx` (same harness) one test:

```tsx
  it('is titled Instance settings and carries no grants panel or prototype fiction', async () => {
    renderInstanceAdmin(); // whatever the file's existing render helper is called
    expect(await screen.findByRole('heading', { level: 1, name: 'Instance settings' })).toBeInTheDocument();
    expect(screen.queryByText(/decided round 3/)).toBeNull();
    expect(screen.queryByRole('heading', { name: /Instance grants/ })).toBeNull();
    expect(screen.queryByRole('heading', { name: /Connected instances/ })).toBeNull();
    expect(screen.queryByText(/rotated 2026-06-20/)).toBeNull();
    expect(screen.getByRole('link', { name: 'Instance members' })).toHaveAttribute('href', '/instance/members');
  });
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd web && pnpm vitest run src/routes/InstanceAdmin.crypto.test.tsx`
Expected: FAIL on the heading name.

- [ ] **Step 3: Implement**

In `InstanceAdmin.tsx`:
1. Delete the imports that only the grants panel used: `capabilitiesAt, grantFailureText, grantOutcomeSummary, grantScopeLabel, templatesAt, useApplyTemplate, useCreateGrants, useInstanceGrants, useRevokeGrant`, `type Grant`, and the `SurfaceMessage` class if nothing else throws it (search first: `instanceFailureText` uses it — keep `SurfaceMessage` only if still referenced after the deletions; otherwise simplify `instanceFailureText` to `settingsFailureText(error, 'set-credential-policy')`).
2. Delete the state/hooks `grants, createGrants, applyTemplate, revokeGrant, principalId, grantPrincipal, grantTemplate, selectedCapabilities, instanceCapabilities, instanceTemplates, revoke`.
3. Heading and lede:

```tsx
    <h1>Instance settings</h1>
    <p className="page__lede">
      Instance-wide policy, identity providers, federation and key custody. Every operation here is the same grant-evaluated network operation the CLI verb calls. Membership and instance grants live under{' '}
      <Link to={surfaceById('instance-members').path}>Instance members</Link>.
    </p>
```
4. Jump index: remove the `instance-connected` entry; keep the rest (order: orgs, settings, oidc, federation, keys, saml providers, sp keys, retention — add `{ id: 'instance-retention', label: 'Retention health' }` which was missing).
5. Delete the whole `{prototypeMode ? null : <Panel id="instance-grants" …>}` block and the `<Panel id="instance-connected" …>` block.
6. Keys panel: delete the `prototypeMode ? <>…fake rows…</> :` branch; render `<CryptoMaintenance …/>` unconditionally.
7. `CredentialPolicyPanel`: title → `"Policy"`; delete the `query.isSuccess && prototypeMode` branch; render the real rows unconditionally (drop `&& !prototypeMode`). Delete the `prototypeMode` const if unused after this; `grep -n prototypeMode web/src/routes/InstanceAdmin.tsx` must be empty or only guard `OidcProvidersPanel`/`FederationIssuersPanel`/`SamlProvidersPanel`/`SamlSpKeysPanel` — and those four guards must go too: the panels render against the mock, which serves `instance/oidc-providers`, `saml-providers`, `saml-sp-keys`, `federation-issuers` (if any of these GETs is missing in `mock-api.ts`, add an empty-list fixture `{ items: [], count: 0 }` for it in `prototypeReadFixture`).
8. Organisations rows: the detail `prototypeMode ? '3 projects' : 'Organisation settings'` → `'Organisation settings'`.

In `app.css`:
- Delete the six `.page--chrome #instance-* { order: n; }` rules (panel order is now source order).
- Replace `.page--chrome #instance-orgs .settings-row__title` and `.page--chrome #instance-orgs > .settings-row` with class-based rules: add `className="settings-row settings-row--compact"` to the org rows in `InstanceAdmin.tsx` and `className="settings-row__title settings-row__title--link"` to the org `Link`; CSS:

```css
.page--chrome .settings-row--compact { padding-top: 7px; padding-bottom: 7px; }
.page--chrome .settings-row__title--link { color: var(--tx); text-decoration: none; }
```
- Replace `#account-profile { gap: 8px }` and `#project-metadata { gap: 7px }` with one `.panel--tight { gap: 8px; }` and add `className` support: `Panel` in `Sections.tsx` gains `tight?: boolean` → appends ` panel--tight`; use it on `account-profile` and `project-metadata`. (7px vs 8px was drift; 8px wins.)
- Replace `#account-factors > .settings-row` with `.settings-row--compact` on those rows in `AccountSecurity.tsx:255,275,307,318` (8px vs 7px — keep 7px in the shared rule; verify axe/touch pins still pass).
- Delete `.origin-chips` (no remaining user) and `.instance-create-row` only if `SamlProvidersPanel.tsx` no longer uses it (it does — keep).
- `grep -n "#instance-\|#account-\|#project-" web/src/styles/app.css` → none.

`mock-api.ts`: add POST handlers for `/api/v1/instance/rotate-scanning-key`, `/rotate-master-key`, `/rotate-dek`, `/reencrypt`, `/rotate-root-key` following the `rotate-token-key` handler at line 884; bodies must satisfy the matching `z…Response` schemas from `@hikyo/zod` (import them beside `zCreateOrgRequest`). Run `pnpm vitest run e2e/prototype-mock.test.ts` to confirm the mock still type-checks and answers.

- [ ] **Step 4: Run tests**

Run: `cd web && pnpm vitest run && pnpm typecheck && pnpm build`
Expected: PASS; `pnpm prototype` boots and `/instance` renders every panel without console errors (check quickly with `curl -s localhost:5173/instance | head -1` and the browser if available).

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/InstanceAdmin.tsx web/src/routes/InstanceAdmin.crypto.test.tsx web/src/routes/Sections.tsx web/src/routes/AccountSecurity.tsx web/src/routes/ProjectSettings.tsx web/src/styles/app.css web/prototype/mock-api.ts
git -c commit.gpgsign=false commit -s -m "refactor(web): instance settings without prototype fiction; shared panel modifiers replace per-id CSS (#567)"
```

---

### Task 5: Organisation spelling sweep, docs, handoff

**Files:**
- Modify: every `web/src/**/*.{ts,tsx}` user-visible string containing `Organization` / `organization` (11 occurrences: `grep -rnE "[Oo]rganization" web/src --include='*.ts' --include='*.tsx'`)
- Modify: `docs/status/ledger.json:118`, `docs/handoff/60-chrome-surfaces.md` (append a pointer), create `docs/handoff/567-chrome-settings-unification.md`
- Modify: `DESIGN.md` Components section (one bullet)

- [ ] **Step 1: Sweep**

For each hit: change the string; leave identifiers, CSS class names and API field names untouched. Then `grep -rnE "[Oo]rganization" web/src web/e2e` must return only lines inside API paths/identifiers (expected: none in strings).

- [ ] **Step 2: Docs**

- `docs/status/ledger.json:118` `"implemented"` → `"Members at organisation, project and instance scope, organisation/project/instance settings, account security, and browser step-up"`, and add `{ "label": "#567 handoff", "path": "docs/handoff/567-chrome-settings-unification.md" }` to `evidence`.
- `DESIGN.md` → Components: add `- Sidebar: one table (\`navigation.ts\`) renders desktop and mobile; a context block (project or instance) stacks above the organisation block; instance and account destinations live in the rail on desktop and in the drawer on mobile.`
- `docs/handoff/567-chrome-settings-unification.md`: what changed (table, model, Members scope, instance settings), the pins that moved and why, the reversed "no static entry" reasoning, the PR2 seam (Invite/Reset buttons go into `panel__actions` on `Members`; display-once panel shared), and how to run e2e locally (ports, ipv4first).

- [ ] **Step 3: Commit**

```bash
git add -A web/src docs DESIGN.md
git -c commit.gpgsign=false commit -s -m "docs(web): Organisation spelling sweep, ledger and handoff for #567"
```

---

### Task 6: Full verification, review rounds, PR

- [ ] **Step 1: Unit + types + lint + build**

Run in `web/`: `pnpm typecheck && pnpm vitest run && pnpm build`. Run at repo root: `go test ./internal/webui/...` (embedded dist) and `go vet ./...`.
Expected: all PASS.

- [ ] **Step 2: Playwright desktop + mobile**

```bash
cd web && export NODE_OPTIONS=--dns-result-order=ipv4first
# claim free ports (check lsof first) e.g.
export HIKYO_E2E_PORT=45900 HIKYO_E2E_PORT_B=45901 HIKYO_E2E_PORT_C=45902
pnpm exec playwright test --project=desktop e2e/flows/shell.spec.ts e2e/flows/members.spec.ts e2e/flows/instance-admin.spec.ts e2e/flows/settings.spec.ts e2e/flows/matrix.spec.ts e2e/flows/scanning.spec.ts e2e/flows/account.spec.ts
pnpm exec playwright test --project=mobile  e2e/flows/shell.spec.ts e2e/flows/members.spec.ts e2e/flows/instance-admin.spec.ts
pnpm vitest run e2e/registry.test.ts
```
Expected: PASS, including the closure test (the `instance-members` pinned set ran on both schemes).

- [ ] **Step 3: Push and open the PR**

```bash
git push -u origin HEAD
gh pr create --title "Chrome + settings unification: one sidebar table, {Members, Settings} per scope, /instance/members (#567)" --body-file docs/handoff/567-chrome-settings-unification.md
```
Append `🤖 Generated with [Claude Code](https://claude.com/claude-code)` to the PR body. Do not merge.

- [ ] **Step 4: Codex review R1–R3 (blocking)**

Use the `cross-model-review` skill per `~/.claude/CODEX.md`: write the diff to a gitignored path inside the repo (`.xreview/567-r1.diff`), run `codex exec -m gpt-5.6-sol -c model_reasoning_effort=high -s read-only … < /dev/null` with `run_in_background: true`, brief states round N of 3. Fix every finding; R3 must return CLEAN or an explicit blocking list for Marc.

- [ ] **Step 5: Human merge gate**

CI green + review CLEAN + preview verified → stop and ask Marc for one-click merge (AskUserQuestion). PR2 (#568) branches from this PR's head.
