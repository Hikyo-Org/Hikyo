import { describe, expect, it } from 'vitest';

import { withPasskeyLifecycle } from './passkey.ts';

// A repair can advance the shared credential's signature counter. Loading the
// old counter first would both reject the next ceremony and overwrite the file.
describe('passkey session preparation', () => {
  it('loads the repaired counter before work and persists it before session repair', async () => {
    const calls: string[] = [];
    let storedCounter = 7;
    let installedCounter = 0;
    await withPasskeyLifecycle(
      async () => { calls.push('prepare'); storedCounter += 1; },
      async () => {
        calls.push('install'); installedCounter = storedCounter;
        return async () => { calls.push('persist'); storedCounter = installedCounter; };
      },
      async () => { calls.push('use'); expect(installedCounter).toBe(8); installedCounter += 1; },
      async () => { calls.push('repair'); expect(storedCounter).toBe(9); },
    );
    expect(calls).toEqual(['prepare', 'install', 'use', 'persist', 'repair']);
    expect(storedCounter).toBe(9);
  });

  it('never loads or uses the authenticator after failed preparation', async () => {
    const calls: string[] = [];
    await expect(withPasskeyLifecycle(
      async () => { throw new Error('session probe failed'); },
      async () => { calls.push('install'); return async () => { calls.push('persist'); }; },
      async () => { calls.push('use'); },
      async () => { calls.push('repair'); },
    )).rejects.toThrow('session probe failed');
    expect(calls).toEqual([]);
  });

  it('persists the authenticator and repairs the shared session after work fails', async () => {
    const calls: string[] = [];
    await expect(withPasskeyLifecycle(
      async () => { calls.push('prepare'); },
      async () => { calls.push('install'); return async () => { calls.push('persist'); }; },
      async () => { throw new Error('operation failed'); },
      async () => { calls.push('repair'); },
    )).rejects.toThrow('operation failed');
    expect(calls).toEqual(['prepare', 'install', 'persist', 'repair']);
  });
});
