import { describe, expect, it } from 'vitest';

import {
  allowsAnonymousSession,
  needsOrg,
  needsProject,
  routeRegistryViolations,
  sectionsFor,
  SURFACES,
  type SectionId,
} from './navigation.ts';

describe('the route policy registry', () => {
  it('ships a registry with no duplicate ids, duplicate paths, or misplaced chrome', () => {
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
      'change-approvals',
      'adapters',
      'project-audit',
      'project-settings',
    ]);
    expect(sectionsFor('instance').map((s) => s.label)).toEqual([
      'Instance settings',
      'Hikyo configuration',
      'Instance members',
    ]);
    expect(sectionsFor('account').map((s) => s.id)).toEqual(['settings']);
  });

  it('puts every shell surface with a section in exactly one section', () => {
    const sectioned = SURFACES.filter((s) => s.section !== null).map((s) => s.id);
    const kinds: readonly SectionId[] = ['project', 'instance', 'organisation', 'account'];
    const listed = kinds.flatMap((k) => sectionsFor(k).map((s) => s.id));
    expect([...listed].sort()).toEqual([...sectioned].sort());
    expect(new Set(listed).size).toBe(listed.length);
  });

  it('marks project-scoped surfaces by their path, not by declaration', () => {
    const byId = (id: string) => {
      const found = SURFACES.find((s) => s.id === id);
      if (found === undefined) throw new Error(`no surface ${id}`);
      return found;
    };
    expect(needsProject(byId('matrix'))).toBe(true);
    expect(needsOrg(byId('matrix'))).toBe(true);
    expect(needsProject(byId('members'))).toBe(false);
    expect(needsOrg(byId('instance-members'))).toBe(false);
  });

  it('makes public and session-establishing ceremony routes anonymous-reachable, not required ones', () => {
    expect(
      allowsAnonymousSession({
        id: 'establishing-ceremony',
        path: '/establishing-ceremony',
        label: 'Establishing ceremony',
        section: null,
        mode: 'ceremony',
        chrome: 'none',
        session: 'establish-or-reuse',
      }),
    ).toBe(true);
    expect(
      allowsAnonymousSession({
        id: 'required-ceremony',
        path: '/required-ceremony',
        label: 'Required ceremony',
        section: null,
        mode: 'ceremony',
        chrome: 'none',
        session: 'required',
      }),
    ).toBe(false);
  });

  it('rejects duplicate route ids and paths', () => {
    expect(
      routeRegistryViolations([
        {
          id: 'duplicate',
          path: '/duplicate',
          label: 'First',
          section: null,
          mode: 'public',
          chrome: 'none',
        },
        {
          id: 'duplicate',
          path: '/duplicate',
          label: 'Second',
          section: null,
          mode: 'public',
          chrome: 'none',
        },
      ]),
    ).toEqual([
      'route id "duplicate" is declared more than once',
      'route path "/duplicate" is declared more than once',
    ]);
  });

  it('rejects a chromeless route placed in shell navigation', () => {
    expect(
      routeRegistryViolations([
        {
          id: 'chromeless-navigation',
          path: '/chromeless-navigation',
          label: 'Chromeless navigation',
          section: 'account',
          mode: 'public',
          chrome: 'none',
        },
      ]),
    ).toContain('route "chromeless-navigation" has no chrome but appears in section "account"');
  });
});
