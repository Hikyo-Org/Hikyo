// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ApiError } from '../api/client.ts';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { INVALID_INVITATION_TEXT, acceptanceFailureText } from '../api/emailInvitations.ts';
import { InvitationAccept } from './InvitationAccept.tsx';

const mocks = vi.hoisted(() => ({
  status: 'authenticated' as 'authenticated' | 'anonymous',
  preview: vi.fn(),
  accept: vi.fn(),
  endSession: vi.fn(),
}));

vi.mock('../app/AuthProvider.tsx', () => ({
  useAuth: () => ({
    state: { status: mocks.status },
    captureTransition: () => ({ revision: 1 }),
    endSession: mocks.endSession,
  }),
}));
vi.mock('../api/emailInvitations.ts', async (original) => ({
  ...(await original<typeof import('../api/emailInvitations.ts')>()),
  previewEmailInvitation: mocks.preview,
  acceptEmailInvitation: mocks.accept,
}));

const PREVIEW = { org: 'org_11111111-1111-4111-8111-111111111111', org_name: 'Acme', template: 'editor', expires_at: '2026-11-13T10:00:00Z' };

beforeEach(() => { mocks.status = 'authenticated'; });
afterEach(() => vi.clearAllMocks());

function Location() { const location = useLocation(); return <output>{location.pathname}{location.hash}</output>; }
function mount(entry = '/invitations/accept#token=eit_secret') {
  return renderForm(
    <MemoryRouter initialEntries={[entry]}>
      <Location />
      <Routes>
        <Route path="/invitations/accept" element={<InvitationAccept />} />
        <Route path="/projects" element={<p>Projects page</p>} />
      </Routes>
    </MemoryRouter>,
  );
}
function button(root: HTMLElement, name: string): HTMLButtonElement {
  const found = [...root.querySelectorAll('button')].find((candidate) => candidate.textContent?.trim() === name);
  if (found === undefined) throw new Error(`no ${name} button`);
  return found;
}

function SecondLink() {
  const navigate = useNavigate();
  return <button type="button" onClick={() => void navigate('/invitations/accept#token=eit_second')}>Open second link</button>;
}

describe('InvitationAccept', () => {
  it('previews a second link opened in the same tab and accepts only the token on screen', async () => {
    let resolveSecond: (value: typeof PREVIEW) => void = () => undefined;
    mocks.preview.mockImplementation((token: string) => token === 'eit_secret'
      ? Promise.resolve(PREVIEW)
      : new Promise((resolve) => { resolveSecond = resolve; }));
    mocks.accept.mockResolvedValue({ org: PREVIEW.org, org_name: 'Globex', template: 'viewer', grants_created: 1, signed_out: false });
    const view = await renderForm(
      <MemoryRouter initialEntries={['/invitations/accept#token=eit_secret']}>
        <SecondLink />
        <Routes>
          <Route path="/invitations/accept" element={<InvitationAccept />} />
        </Routes>
      </MemoryRouter>,
    );
    try {
      await settleTask();
      expect(view.container.textContent).toContain('Acme');
      await act(async () => button(view.container, 'Open second link').click());
      await settleTask();
      expect(mocks.preview).toHaveBeenLastCalledWith('eit_second');
      // The first invitation is no longer offered while the second is checked.
      expect(view.container.textContent).not.toContain('Acme');
      expect([...view.container.querySelectorAll('button')].map((b) => b.textContent)).not.toContain('Accept');
      await act(async () => resolveSecond({ ...PREVIEW, org_name: 'Globex', template: 'viewer' }));
      await settleTask();
      expect(view.container.textContent).toContain('Globex');
      await act(async () => button(view.container, 'Accept').click());
      await settleTask();
      expect(mocks.accept).toHaveBeenCalledTimes(1);
      expect(mocks.accept).toHaveBeenCalledWith('eit_second');
    } finally { await view.unmount(); }
  });

  it('does not carry a retryable Accept failure over to a second link', async () => {
    mocks.preview.mockImplementation((token: string) => Promise.resolve(token === 'eit_secret' ? PREVIEW : { ...PREVIEW, org_name: 'Globex' }));
    const throttled = new ApiError(429, 'too many requests');
    const throttledText = acceptanceFailureText(throttled);
    mocks.accept.mockRejectedValue(throttled);
    const view = await renderForm(
      <MemoryRouter initialEntries={['/invitations/accept#token=eit_secret']}>
        <SecondLink />
        <Routes>
          <Route path="/invitations/accept" element={<InvitationAccept />} />
        </Routes>
      </MemoryRouter>,
    );
    try {
      await settleTask();
      await act(async () => button(view.container, 'Accept').click());
      await settleTask();
      expect(view.container.textContent).toContain(throttledText);
      await act(async () => button(view.container, 'Open second link').click());
      await settleTask();
      expect(view.container.textContent).toContain('Globex');
      expect(view.container.textContent).not.toContain(throttledText);
    } finally { await view.unmount(); }
  });

  it('keeps the outcome of a committed Accept when a second link arrives while it is pending', async () => {
    mocks.preview.mockImplementation((token: string) => Promise.resolve(token === 'eit_secret' ? PREVIEW : { ...PREVIEW, org_name: 'Globex' }));
    let resolveAccept: (value: unknown) => void = () => undefined;
    mocks.accept.mockImplementation(() => new Promise((resolve) => { resolveAccept = resolve; }));
    const view = await renderForm(
      <MemoryRouter initialEntries={['/invitations/accept#token=eit_secret']}>
        <SecondLink />
        <Routes>
          <Route path="/invitations/accept" element={<InvitationAccept />} />
        </Routes>
      </MemoryRouter>,
    );
    try {
      await settleTask();
      await act(async () => button(view.container, 'Accept').click());
      await act(async () => button(view.container, 'Open second link').click());
      await settleTask();
      expect(mocks.preview).not.toHaveBeenCalledWith('eit_second');
      await act(async () => resolveAccept({ org: PREVIEW.org, org_name: 'Acme', template: 'editor', grants_created: 1, signed_out: true }));
      await settleTask();
      expect(mocks.endSession).toHaveBeenCalledTimes(1);
      expect(view.container.textContent).toContain('You joined Acme as editor');
    } finally { await view.unmount(); }
  });

  it('previews the same link afresh when it is reopened after a refused Accept', async () => {
    mocks.preview.mockResolvedValue(PREVIEW);
    mocks.accept.mockRejectedValue(new ApiError(404, 'not found'));
    function Reopen() {
      const navigate = useNavigate();
      return <button type="button" onClick={() => void navigate('/invitations/accept#token=eit_secret')}>Reopen link</button>;
    }
    const view = await renderForm(
      <MemoryRouter initialEntries={['/invitations/accept#token=eit_secret']}>
        <Reopen />
        <Routes>
          <Route path="/invitations/accept" element={<InvitationAccept />} />
        </Routes>
      </MemoryRouter>,
    );
    try {
      await settleTask();
      await act(async () => button(view.container, 'Accept').click());
      await settleTask();
      expect(mocks.preview).toHaveBeenCalledTimes(1);
      await act(async () => button(view.container, 'Reopen link').click());
      await settleTask();
      expect(mocks.preview).toHaveBeenCalledTimes(2);
      expect(view.container.textContent).not.toContain(INVALID_INVITATION_TEXT);
    } finally { await view.unmount(); }
  });

  it('reads the fragment token, clears it from the address and previews with it', async () => {
    mocks.preview.mockResolvedValue(PREVIEW);
    const view = await mount();
    try {
      await settleTask();
      expect(view.container.querySelector('output')?.textContent).toBe('/invitations/accept');
      expect(mocks.preview).toHaveBeenCalledTimes(1);
      expect(mocks.preview).toHaveBeenCalledWith('eit_secret');
      const text = view.container.textContent ?? '';
      for (const shown of ['Acme', 'editor']) expect(text).toContain(shown);
      expect(text).toContain('you will be signed out on every device');
      expect(text).toContain('developer credentials');
      expect(text).toContain('administrators can reset your sign-in');
      expect(mocks.accept).not.toHaveBeenCalled();
    } finally { await view.unmount(); }
  });

  it('keeps the token out of web storage', async () => {
    mocks.preview.mockResolvedValue(PREVIEW);
    const view = await mount();
    try {
      await settleTask();
      expect(JSON.stringify({ ...localStorage })).not.toContain('eit_secret');
      expect(JSON.stringify({ ...sessionStorage })).not.toContain('eit_secret');
    } finally { await view.unmount(); }
  });

  it('accepts with the token and, when signed out, says they joined and must sign in again', async () => {
    mocks.preview.mockResolvedValue(PREVIEW);
    mocks.accept.mockResolvedValue({ org: PREVIEW.org, org_name: 'Acme', template: 'editor', grants_created: 3, signed_out: true });
    const view = await mount();
    // Ending the session remounts the router: the outcome must already be in it.
    let shownAtEnd = '';
    mocks.endSession.mockImplementation(() => { shownAtEnd = view.container.textContent ?? ''; });
    try {
      await settleTask();
      await act(async () => button(view.container, 'Accept').click());
      await settleTask();
      expect(mocks.accept).toHaveBeenCalledWith('eit_secret');
      expect(mocks.endSession).toHaveBeenCalledTimes(1);
      expect(shownAtEnd).toContain('You joined Acme as editor');
      const text = view.container.textContent ?? '';
      expect(text).toContain('You joined Acme as editor');
      expect(text).toContain('sign in again');
      expect(view.container.querySelector('a[href="/login"]')).not.toBeNull();
    } finally { await view.unmount(); }
  });

  it('asks for the emailed link again when the page opens without a token', async () => {
    const view = await mount('/invitations/accept');
    try {
      await settleTask();
      expect(view.container.textContent).toContain('This page needs the link from your invitation email.');
      expect(view.container.querySelector('[role="alert"]')).toBeNull();
      expect(mocks.preview).not.toHaveBeenCalled();
    } finally { await view.unmount(); }
  });

  it('answers every uniform 404 with one sentence and retires the token', async () => {
    mocks.preview.mockRejectedValue(new ApiError(404, 'not found'));
    const view = await mount();
    try {
      await settleTask();
      expect(view.container.querySelector('[role="alert"]')?.textContent).toBe(INVALID_INVITATION_TEXT);
      expect([...view.container.querySelectorAll('button')]).toEqual([]);
    } finally { await view.unmount(); }

    mocks.preview.mockResolvedValue(PREVIEW);
    mocks.accept.mockRejectedValue(new ApiError(404, 'not found'));
    const second = await mount();
    try {
      await settleTask();
      await act(async () => button(second.container, 'Accept').click());
      await settleTask();
      expect(second.container.querySelector('[role="alert"]')?.textContent).toBe(INVALID_INVITATION_TEXT);
      expect(mocks.endSession).not.toHaveBeenCalled();
    } finally { await second.unmount(); }
  });

  it('declines with Not now without accepting', async () => {
    mocks.preview.mockResolvedValue(PREVIEW);
    const view = await mount();
    try {
      await settleTask();
      expect([...view.container.querySelectorAll('.actions button')].map((b) => b.textContent)).toEqual(['Accept', 'Not now']);
      await act(async () => button(view.container, 'Not now').click());
      await settleTask();
      expect(mocks.accept).not.toHaveBeenCalled();
      expect(view.container.textContent).toContain('Projects page');
    } finally { await view.unmount(); }
  });

  it('asks a signed-out visitor to sign in with the invited account, sends nothing and clears the fragment', async () => {
    mocks.status = 'anonymous';
    const view = await mount();
    try {
      await settleTask();
      const text = view.container.textContent ?? '';
      expect(text).toContain('sign in with the account it was sent to');
      expect(text).toContain('open the link from your email again');
      expect(view.container.querySelector('a[href="/login"]')).not.toBeNull();
      expect(view.container.querySelector('output')?.textContent).toBe('/invitations/accept');
      expect(mocks.preview).not.toHaveBeenCalled();
    } finally { await view.unmount(); }
  });
});
