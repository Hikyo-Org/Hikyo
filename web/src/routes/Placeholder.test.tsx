// @vitest-environment happy-dom
import { MemoryRouter, Outlet, Route, Routes } from 'react-router';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { Overview } from './Placeholder.tsx';

const mocks = vi.hoisted(() => ({ items: [] as { id: string; name: string }[] }));

vi.mock('../api/settings.ts', async (original) => ({
  ...(await original<typeof import('../api/settings.ts')>()),
  useProjects: () => ({ data: { items: mocks.items } }),
}));

afterEach(() => {
  mocks.items = [];
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
  mocks.items = [{ id: 'prj_a', name: 'Payments' }];
  const view = await landing();
  try {
    expect(view.container.querySelector('h1')?.textContent).toBe('matrix');
  } finally {
    await view.unmount();
  }
});
