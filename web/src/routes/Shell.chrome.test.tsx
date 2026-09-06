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
};

describe('breadcrumbs', () => {
  it('ends every trail with the surface label from the navigation table', () => {
    expect(breadcrumbs({ ...base, surface: surfaceById('members') })).toEqual([
      'hikyo',
      'acme',
      'Members',
    ]);
    expect(breadcrumbs({ ...base, surface: surfaceById('org-settings') })).toEqual([
      'hikyo',
      'acme',
      'Organisation settings',
    ]);
    expect(breadcrumbs({ ...base, surface: surfaceById('settings') })).toEqual([
      'hikyo',
      'acme',
      'Account & security',
    ]);
    expect(breadcrumbs({ ...base, surface: surfaceById('projects') })).toEqual([
      'hikyo',
      'acme',
      'Projects',
    ]);
  });

  it('appends the surface after the project on project-scoped routes', () => {
    expect(
      breadcrumbs({
        ...base,
        surface: surfaceById('matrix'),
        routeProjectId: 'prj_1',
        activeProjectName: 'demo',
      }),
    ).toEqual(['hikyo', 'acme', 'demo', 'Environment matrix']);
  });

  it('roots instance surfaces under Instance and never under an organisation', () => {
    expect(breadcrumbs({ ...base, surface: surfaceById('instance-admin') })).toEqual([
      'hikyo',
      'Instance',
      'Instance settings',
    ]);
    expect(breadcrumbs({ ...base, surface: surfaceById('instance-members') })).toEqual([
      'hikyo',
      'Instance',
      'Instance members',
    ]);
  });

  it('names an unmatched route honestly', () => {
    expect(breadcrumbs({ ...base, activeOrgId: '', surface: undefined })).toEqual([
      'hikyo',
      'Not found',
    ]);
  });
});

describe('ProjectContext', () => {
  const render = (orgRole: string | null) => {
    const container = document.createElement('div');
    container.innerHTML = renderToStaticMarkup(
      <MemoryRouter>
        <ProjectContext
          org="org_1"
          orgName="acme"
          projectName="demo"
          orgRole={orgRole}
          links={[]}
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

  it('states no organisation role outside prototype mode, and the prototype one when given', () => {
    expect(render(null).querySelector('.context-sidebar__org small')).toBeNull();
    expect(render('org admin').querySelector('.context-sidebar__org small')?.textContent).toBe(
      'org admin',
    );
    expect(render(null).querySelector('.context-sidebar__org strong')?.textContent).toBe('acme');
  });

  it('marks a problem count with a glyph so the pill is never colour alone', () => {
    const container = render(null);
    const [keys, problems] = [...container.querySelectorAll('.context-sidebar__group')];
    expect(keys?.querySelector('.context-sidebar__group-count')?.textContent).toBe('3');
    expect(keys?.querySelector('.count__glyph')).toBeNull();
    expect(problems?.querySelector('.matrix__count .count__glyph')?.getAttribute('aria-hidden')).toBe(
      'true',
    );
    expect(problems?.querySelector('.matrix__count')?.textContent).toBe('!2');
  });
});
