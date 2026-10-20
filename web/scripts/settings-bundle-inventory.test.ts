import { describe, expect, it } from 'vitest';

import { assertSettingsRouteIsolation, type SettingsChunk } from './settings-bundle-inventory.ts';

const settings = ['AccountSecurity', 'OrgSettings'];
const account: SettingsChunk = {
  file: 'assets/AccountSecurity-entry.js', imports: [], dynamicImports: [],
  routes: ['web/src/routes/AccountSecurity.tsx'],
};
const shared: SettingsChunk = {
  file: 'assets/shared-dependency.js', imports: [], dynamicImports: [],
  routes: ['web/src/routes/Shell.tsx'],
};
const fetched = [`/${account.file}`, `/${shared.file}`];

describe('settings route chunk isolation', () => {
  it('allows shared dependencies that contain no other settings screen', () => {
    expect(() => assertSettingsRouteIsolation(fetched, [account, shared], 'AccountSecurity', settings)).not.toThrow();
  });

  it('rejects another screen hidden inside a generically named shared chunk', () => {
    const contaminated = { ...shared, routes: [...shared.routes, 'web/src/routes/OrgSettings.tsx'] };
    expect(() => assertSettingsRouteIsolation(fetched, [account, contaminated], 'AccountSecurity', settings))
      .toThrow('OrgSettings');
  });

  it('refuses a fetched asset absent from the current build inventory', () => {
    expect(() => assertSettingsRouteIsolation([...fetched, '/assets/unknown.js'], [account, shared], 'AccountSecurity', settings))
      .toThrow('absent from current production inventory');
  });

  it('requires module ownership even when the entry filename looks correct', () => {
    expect(() => assertSettingsRouteIsolation(fetched, [{ ...account, routes: [] }, shared], 'AccountSecurity', settings))
      .toThrow('No module ownership');
  });

  it('retains the addressed entry filename check', () => {
    expect(() => assertSettingsRouteIsolation([`/${shared.file}`], [account, shared], 'AccountSecurity', settings))
      .toThrow('Missing addressed settings entry');
  });

  it('allows other settings chunks when they were not fetched', () => {
    const unrelated = { ...shared, file: 'assets/OrgSettings-other.js', routes: ['web/src/routes/OrgSettings.tsx'] };
    expect(() => assertSettingsRouteIsolation(fetched, [account, shared, unrelated], 'AccountSecurity', settings)).not.toThrow();
  });
});
