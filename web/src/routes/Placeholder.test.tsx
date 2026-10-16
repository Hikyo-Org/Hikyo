// @vitest-environment happy-dom
import { MemoryRouter, Outlet, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { Overview } from './Placeholder.tsx';

afterEach(() => {
  vi.unstubAllGlobals();
});

async function landing() {
  return renderForm(
    <MemoryRouter initialEntries={['/']}>
      <Routes>
        <Route element={<Outlet context={{ activeOrgId: 'org_a' }} />}>
          <Route path="/" element={<Overview />} />
          <Route path="/orgs/:org/projects/:project/matrix" element={<h1>matrix</h1>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

it('lands on the only project’s matrix when the organisation has one', async () => {
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(Response.json({
    items: [{
      id: 'prj_123e4567-e89b-12d3-a456-426614174000',
      org_id: 'org_123e4567-e89b-12d3-a456-426614174001',
      name: 'Payments',
      created_at: '2026-01-01T00:00:00Z',
    }],
    count: 1,
  }))));
  const view = await landing();
  try {
    await settleTask();
    expect(view.container.querySelector('h1')?.textContent).toBe('matrix');
  } finally {
    await view.unmount();
    view.client.clear();
  }
});
