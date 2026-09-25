// @vitest-environment happy-dom
import { beforeEach, expect, it } from 'vitest';

import { installMemoryStorage } from '../testkit/storage.ts';
import { isLastSignIn, readLastSignIn, rememberLastSignIn, type LastSignIn } from './lastSignIn.ts';

beforeEach(() => installMemoryStorage());

it('reads nothing on a first visit', () => {
  expect(readLastSignIn()).toBeNull();
});

it('round-trips each kind, the provider by its kind and slug', () => {
  rememberLastSignIn({ kind: 'password' });
  expect(readLastSignIn()).toEqual({ kind: 'password' });
  rememberLastSignIn({ kind: 'passkey' });
  expect(readLastSignIn()).toEqual({ kind: 'passkey' });
  rememberLastSignIn({ kind: 'provider', providerKind: 'saml', slug: 'corp' });
  expect(globalThis.localStorage.getItem('hikyo.last-sign-in')).toBe('provider:saml:corp');
  expect(readLastSignIn()).toEqual({ kind: 'provider', providerKind: 'saml', slug: 'corp' });
});

it('reads malformed storage as nothing rather than trusting it', () => {
  // `provider:corp` is the older slug-only shape: it names no kind, so a
  // returning browser simply shows no badge once.
  const junk = ['', 'provider:', 'provider:corp', 'provider:oidc:', 'provider::corp', 'provider:oidc:corp:x'];
  for (const value of [...junk, 'totp', '{"kind":"password"}']) {
    globalThis.localStorage.setItem('hikyo.last-sign-in', value);
    expect(readLastSignIn()).toBeNull();
  }
});

it('reads nothing and swallows the write when storage is absent or refuses', () => {
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: undefined });
  expect(readLastSignIn()).toBeNull();
  expect(() => rememberLastSignIn({ kind: 'password' })).not.toThrow();
  const refusing = {
    getItem: () => {
      throw new Error('denied');
    },
    setItem: () => {
      throw new Error('denied');
    },
  };
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: refusing });
  expect(readLastSignIn()).toBeNull();
  expect(() => rememberLastSignIn({ kind: 'passkey' })).not.toThrow();
});

it('badges only the matching row', () => {
  const last: LastSignIn = { kind: 'provider', providerKind: 'oidc', slug: 'corp' };
  expect(isLastSignIn(last, { kind: 'provider', providerKind: 'oidc', slug: 'corp' })).toBe(true);
  expect(isLastSignIn(last, { kind: 'provider', providerKind: 'oidc', slug: 'sso' })).toBe(false);
  // A slug is unique per kind only: the SAML `corp` is another row.
  expect(isLastSignIn(last, { kind: 'provider', providerKind: 'saml', slug: 'corp' })).toBe(false);
  expect(isLastSignIn(last, { kind: 'password' })).toBe(false);
  expect(isLastSignIn(null, { kind: 'password' })).toBe(false);
  expect(isLastSignIn({ kind: 'passkey' }, { kind: 'passkey' })).toBe(true);
});
