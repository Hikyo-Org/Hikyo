// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { SignupVerify } from './SignupVerify.tsx';

const verify = vi.hoisted(() => vi.fn());
vi.mock('../api/signup.ts', async (original) => ({ ...await original(), verifySignup: verify }));
afterEach(() => vi.clearAllMocks());

function Location() { const location = useLocation(); return <output>{location.pathname}{location.hash}</output>; }
async function mount(landing = 'none', extra = '') {
  return renderForm(<MemoryRouter initialEntries={[`/signup/verify#token=su_test&email=alex%40example.com&landing=${landing}${extra}`]}><Location /><Routes><Route path="/signup/verify" element={<SignupVerify />} /><Route path="/login" element={<p>Sign in page</p>} /></Routes></MemoryRouter>);
}
function field(root: HTMLElement, name: string) { const input = root.querySelector(`input[name="${name}"]`); if (!(input instanceof HTMLInputElement)) throw new Error(`missing ${name}`); return input; }
async function fill(root: HTMLElement, repeat = 'twelvecharacters') {
  await act(async () => {
    typeInto(field(root, 'display_name'), 'Alex');
    typeInto(field(root, 'password'), 'twelvecharacters');
    typeInto(field(root, 'repeat'), repeat);
    const org = root.querySelector('input[name="org_name"]');
    if (org instanceof HTMLInputElement) typeInto(org, 'New organisation');
  });
  await act(async () => root.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
  await settleTask();
}

it('scrubs the fragment, shows readonly email, and rejects mismatched passwords before sending', async () => {
  const view = await mount();
  try {
    expect(view.container.querySelector('output')?.textContent).toBe('/signup/verify');
    expect(field(view.container, 'email').readOnly).toBe(true);
    expect(field(view.container, 'email').value).toBe('alex@example.com');
    expect(view.container.querySelector('[name="org_name"]')).toBeNull();
    await fill(view.container, 'differentpassword');
    expect(verify).not.toHaveBeenCalled();
    expect(view.container.textContent).toContain('The two passwords differ');
  } finally { await view.unmount(); }
});

it('sends the fresh-org name and returns to login without creating a session', async () => {
  verify.mockResolvedValue(undefined);
  const view = await mount('fresh-org');
  try {
    await fill(view.container);
    expect(verify).toHaveBeenCalledWith(expect.objectContaining({ token: 'su_test', display_name: 'Alex', password: 'twelvecharacters', org_name: 'New organisation' }));
    expect(view.container.textContent).toContain('Sign in page');
  } finally { await view.unmount(); }
});

it('keeps the token retryable after a name collision but retires it after uniform refusal', async () => {
  verify.mockRejectedValueOnce(new ApiError(400, 'collision', 'Choose another organisation name. Your link is still valid.')).mockRejectedValueOnce(new ApiError(401, 'refused'));
  const view = await mount('fresh-org');
  try {
    await fill(view.container);
    expect(view.container.textContent).toContain('Your link is still valid');
    expect(field(view.container, 'password').value).toBe('');
    await fill(view.container);
    expect(verify).toHaveBeenCalledTimes(2);
    expect(view.container.textContent).toContain("This link can't be used");
    expect(view.container.querySelector('[name="password"]')).toBeNull();
  } finally { await view.unmount(); }
});

it('returns a refused org-scoped verification to the same encoded signup scope', async () => {
  verify.mockRejectedValueOnce(new ApiError(401, 'refused'));
  const view = await mount('org', '&org=org_acme%2F%26');
  try {
    await fill(view.container);
    expect(view.container.querySelector('output')?.textContent).toBe('/signup/verify');
    expect(view.container.querySelector('a')?.getAttribute('href')).toBe('/signup?org=org_acme%2F%26');
    expect(verify).toHaveBeenCalledWith({ token: 'su_test', password: 'twelvecharacters', display_name: 'Alex', landing: 'org' });
    expect(view.container.querySelector('[name="password"]')).toBeNull();
  } finally { await view.unmount(); }
});

it('returns a refused instance-scoped verification to signup without an org query', async () => {
  verify.mockRejectedValueOnce(new ApiError(401, 'refused'));
  const view = await mount();
  try {
    await fill(view.container);
    expect(view.container.querySelector('a')?.getAttribute('href')).toBe('/signup');
    expect(view.container.querySelector('output')?.textContent).toBe('/signup/verify');
  } finally { await view.unmount(); }
});
