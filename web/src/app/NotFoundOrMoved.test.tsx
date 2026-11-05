// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { expect, it, vi } from 'vitest';

import { NotFoundOrMoved } from './App.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

vi.mock('../routes/Placeholder.tsx', () => ({ NotFound: () => <p>not found</p>, Overview: () => null }));
vi.mock('../routes/Shell.tsx', () => ({ Shell: () => null }));

function Where() {
  const { pathname, search, hash } = useLocation();
  return <output>{`${pathname}${search}${hash}`}</output>;
}

async function renderAt(entry: string) {
  const container = document.createElement('div');
  const root = createRoot(container);
  await act(async () =>
    root.render(
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/instance/remotes" element={<Where />} />
          <Route path="*" element={<><NotFoundOrMoved /><Where /></>} />
        </Routes>
      </MemoryRouter>,
    ),
  );
  const text = container.textContent;
  await act(async () => root.unmount());
  return text;
}

it('redirects the legacy /remotes path to /instance/remotes, keeping its query and hash', async () => {
  expect(await renderAt('/remotes?remote=edge#connection-credentials')).toBe(
    '/instance/remotes?remote=edge#connection-credentials',
  );
  expect(await renderAt('/remotes/')).toBe('/instance/remotes');
  // React Router matches routes case-insensitively, so a /Remotes bookmark worked before the move.
  expect(await renderAt('/Remotes')).toBe('/instance/remotes');
});

it('still renders not-found for any other unmatched path', async () => {
  expect(await renderAt('/nope')).toBe('not found/nope');
});
