// @vitest-environment happy-dom
import { beforeEach, expect, it } from 'vitest';

import { isLastSignIn, readLastSignIn, rememberLastSignIn } from './lastSignIn.ts';

beforeEach(() => globalThis.localStorage.clear());

it('reads nothing on a first visit', () => {
  expect(readLastSignIn()).toBeNull();
});

it('round-trips each kind, the provider by slug', () => {
  rememberLastSignIn({ kind: 'password' });
  expect(readLastSignIn()).toEqual({ kind: 'password' });
  rememberLastSignIn({ kind: 'passkey' });
  expect(readLastSignIn()).toEqual({ kind: 'passkey' });
  rememberLastSignIn({ kind: 'provider', slug: 'corp' });
  expect(readLastSignIn()).toEqual({ kind: 'provider', slug: 'corp' });
});

it('reads malformed storage as nothing rather than trusting it', () => {
  for (const junk of ['', 'provider:', 'totp', '{"kind":"password"}']) {
    globalThis.localStorage.setItem('hikyo.last-sign-in', junk);
    expect(readLastSignIn()).toBeNull();
  }
});

it('badges only the matching row', () => {
  const last = { kind: 'provider', slug: 'corp' } as const;
  expect(isLastSignIn(last, { kind: 'provider', slug: 'corp' })).toBe(true);
  expect(isLastSignIn(last, { kind: 'provider', slug: 'sso' })).toBe(false);
  expect(isLastSignIn(last, { kind: 'password' })).toBe(false);
  expect(isLastSignIn(null, { kind: 'password' })).toBe(false);
  expect(isLastSignIn({ kind: 'passkey' }, { kind: 'passkey' })).toBe(true);
});
