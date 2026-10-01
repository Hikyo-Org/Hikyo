// @vitest-environment happy-dom
import { StrictMode } from 'react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';

import { rememberSAMLTransaction } from '../api/samlTransaction.ts';
import { renderForm } from '../testkit/renderForm.tsx';
import { SAMLDone } from './SAMLDone.tsx';

const announce = vi.hoisted(() => vi.fn());
vi.mock('../api/sessionEpoch.ts', () => ({ announceSessionChange: announce }));

beforeEach(() => {
  globalThis.sessionStorage.clear();
  globalThis.history.replaceState({}, '', '/auth/saml/done?state=saml-state');
  announce.mockReset();
  vi.spyOn(globalThis.location, 'replace').mockImplementation(() => undefined);
});
afterEach(() => { vi.restoreAllMocks(); });

it('adopts the browser session once and restores the bound workspace approval', async () => {
  rememberSAMLTransaction('saml-state', '/workspace/approve?state=workspace-state');
  const { unmount } = await renderForm(<StrictMode><SAMLDone /></StrictMode>);
  expect(announce).toHaveBeenCalledOnce();
  expect(globalThis.location.replace).toHaveBeenCalledExactlyOnceWith('/workspace/approve?state=workspace-state');
  expect(globalThis.sessionStorage.getItem('hikyo-saml-transaction:saml-state')).toBeNull();
  await unmount();
  const second = await renderForm(<SAMLDone />);
  expect(announce).toHaveBeenCalledOnce();
  expect(second.container.querySelector('[role="alert"]')?.textContent).toContain('already completed');
  await second.unmount();
});

it('ignores URL purpose and destination instead of using them as authority', async () => {
  rememberSAMLTransaction('saml-state');
  globalThis.history.replaceState({}, '', '/auth/saml/done?state=saml-state&purpose=reauth&returnTo=https://attacker.example');
  const { unmount } = await renderForm(<SAMLDone />);
  expect(globalThis.location.replace).toHaveBeenCalledWith('/');
  await unmount();
});

it.each([
  ['/auth/saml/done', null],
  ['/auth/saml/done?state=saml-state', null],
  ['/auth/saml/done?state=saml-state', '{bad json'],
  ['/auth/saml/done?state=saml-state', JSON.stringify({ purpose: 'reauth', returnTo: '/' })],
  ['/auth/saml/done?state=saml-state', JSON.stringify({ purpose: 'login', returnTo: '//attacker.example' })],
])('refuses a missing or invalid initiating transaction: %s %s', async (url, record) => {
  globalThis.history.replaceState({}, '', url);
  if (record !== null) globalThis.sessionStorage.setItem('hikyo-saml-transaction:saml-state', record);
  const { container, unmount } = await renderForm(<SAMLDone />);
  expect(container.querySelector('[role="alert"]')?.textContent).toContain('transaction is missing');
  expect(announce).not.toHaveBeenCalled();
  expect(globalThis.location.replace).not.toHaveBeenCalled();
  await unmount();
});

it('refuses foreign sign-in continuations before any redirect can start', () => {
  expect(() => rememberSAMLTransaction('saml-state', 'https://attacker.example')).toThrow('on this instance');
  expect(() => rememberSAMLTransaction('', '/')).toThrow('transaction');
});
