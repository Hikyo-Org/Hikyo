// @vitest-environment happy-dom
import { afterEach, expect, it, vi } from 'vitest';
import { MemoryRouter } from 'react-router';
import * as workspace from '../api/workspace.ts';
import { RemoteCard } from '../routes/Remotes.tsx';
import { renderForm, settleTask } from '../testkit/renderForm.tsx';
import { DemoSurface } from './DemoSurface.tsx';

vi.mock('./config.ts', async (importOriginal) => ({
  ...await importOriginal<typeof import('./config.ts')>(),
  isDemo: true,
}));

afterEach(() => vi.restoreAllMocks());

it('shows the real remote directory card without preparing an external connection', async () => {
  const prepare = vi.spyOn(workspace, 'prepareWorkspace').mockRejectedValue(new Error('External handoff must not start'));
  const { container, unmount } = await renderForm(
    <MemoryRouter>
      <DemoSurface id="remotes">
        <ul><RemoteCard remote={{
          id: 'rmt_123e4567-e89b-12d3-a456-426614174000',
          name: 'Sample production', url: 'https://production.example/', spki_pin: 'sample-pin',
          created_at: '2026-10-10T10:00:00Z', created_by: 'sample-admin', state: 'ok',
          last_attempt_at: '2026-10-10T10:00:00Z', stale: false,
          org_count: 1, project_count: 2,
          orgs: [{ name: 'Sample team', projects: ['Web app', 'API'] }],
        }} duplicateIdentity={false} /></ul>
      </DemoSurface>
    </MemoryRouter>,
  );
  try {
    await settleTask();
    expect(prepare).not.toHaveBeenCalled();
    expect(container.textContent).toContain('Sample production');
    expect(container.textContent).toContain('Web app, API');
    expect(container.querySelector('[role="alert"]')).toBeNull();
    const launch = [...container.querySelectorAll('button')].find((button) => button.textContent === 'Open workspace');
    expect(launch?.disabled).toBe(true);
  } finally {
    await unmount();
  }
});
