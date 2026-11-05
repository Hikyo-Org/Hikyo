// @vitest-environment happy-dom
import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';

import { surfaceById } from '../app/navigation.ts';
import { breadcrumbs, ProjectContext } from './Shell.tsx';

const base = {
  activeOrgId: 'org_1',
  activeOrgName: 'acme',
  routeProjectId: '',
  activeProjectName: '',
  remote: '',
};

const orgCrumb = { label: 'acme', to: '/projects' };

describe('breadcrumbs', () => {
  it('ends every trail with the surface label from the navigation table, the organisation linking to its projects', () => {
    expect(breadcrumbs({ ...base, surface: surfaceById('members') })).toEqual([
      { label: 'Hikyo' },
      orgCrumb,
      { label: 'Members' },
    ]);
    expect(breadcrumbs({ ...base, surface: surfaceById('org-settings') })).toEqual([
      { label: 'Hikyo' },
      orgCrumb,
      { label: 'Organisation settings' },
    ]);
    expect(breadcrumbs({ ...base, surface: surfaceById('settings') })).toEqual([
      { label: 'Hikyo' },
      orgCrumb,
      { label: 'Account & security' },
    ]);
    expect(breadcrumbs({ ...base, surface: surfaceById('projects') })).toEqual([
      { label: 'Hikyo' },
      orgCrumb,
      { label: 'Projects' },
    ]);
  });

  it('keeps the project crumb inside a remote workspace', () => {
    expect(
      breadcrumbs({
        ...base,
        surface: surfaceById('transit-keys'),
        routeProjectId: 'prj_1',
        activeProjectName: 'demo',
        remote: 'ew',
      })[2],
    ).toEqual({ label: 'demo', to: '/orgs/org_1/projects/prj_1/matrix?remote=ew' });
  });

  it('appends the surface after the project on project-scoped routes, the project linking to its matrix', () => {
    expect(
      breadcrumbs({
        ...base,
        surface: surfaceById('matrix'),
        routeProjectId: 'prj_1',
        activeProjectName: 'demo',
      }),
    ).toEqual([
      { label: 'Hikyo' },
      orgCrumb,
      { label: 'demo', to: '/orgs/org_1/projects/prj_1/matrix' },
      { label: 'Environment matrix' },
    ]);
  });

  it('roots instance surfaces under Instance and never under an organisation', () => {
    expect(breadcrumbs({ ...base, surface: surfaceById('instance-admin') })).toEqual([
      { label: 'Hikyo' },
      { label: 'Instance' },
      { label: 'Instance settings' },
    ]);
    expect(breadcrumbs({ ...base, surface: surfaceById('instance-members') })).toEqual([
      { label: 'Hikyo' },
      { label: 'Instance' },
      { label: 'Instance members' },
    ]);
  });

  it('names an unmatched route honestly', () => {
    expect(breadcrumbs({ ...base, activeOrgId: '', surface: undefined })).toEqual([
      { label: 'Hikyo' },
      { label: 'Not found' },
    ]);
  });
});

describe('ProjectContext', () => {
  const render = (qualifiers: readonly string[]) => {
    const container = document.createElement('div');
    container.innerHTML = renderToStaticMarkup(
      <MemoryRouter>
        <ProjectContext
          block={{ kind: 'project', title: 'Project', qualifiers, links: [] }}
          state={{
            groups: [
              { id: 'g1', name: 'db', keyCount: 3, problemCount: 0, hidden: false },
              { id: 'g2', name: 'mail', keyCount: 2, problemCount: 2, hidden: false },
            ],
            problemCount: 2,
            problemsActive: false,
            onSelectGroup: () => undefined,
            onToggleProblems: () => undefined,
          }}
          onNavigate={() => undefined}
        />
      </MemoryRouter>,
    );
    return container;
  };

  it('heads the block with the project, and the remote boundary in a workspace, with no organisation row', () => {
    const local = render(['demo']);
    expect(local.querySelector('h2')?.textContent).toBe('Project · demo');
    expect(local.querySelector('h2')?.getAttribute('title')).toBe('Project · demo');
    expect(render(['on edge', 'prj_1']).querySelector('h2')?.textContent).toBe('Project · on edge · prj_1');
  });

  it('marks a problem count with a glyph so the pill is never colour alone', () => {
    const container = render(['demo']);
    const [keys, problems] = [...container.querySelectorAll('.context-sidebar__group')];
    expect(keys?.querySelector('.context-sidebar__group-count')?.textContent).toBe('3');
    expect(keys?.querySelector('.count__glyph')).toBeNull();
    expect(problems?.querySelector('.matrix__count .count__glyph')?.getAttribute('aria-hidden')).toBe(
      'true',
    );
    expect(problems?.querySelector('.matrix__count')?.textContent).toBe('2');
    expect(problems?.querySelector('.matrix__count .count__glyph')?.getAttribute('data-glyph')).toBe('warn');
  });
});
