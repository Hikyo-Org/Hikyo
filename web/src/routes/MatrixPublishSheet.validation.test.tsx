// @vitest-environment happy-dom
import { expect, it, vi } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { MatrixPublishSheet } from './MatrixPublishSheet.tsx';

vi.mock('./useProtectedPublishCeremony.ts', () => ({
  useProtectedPublishCeremony: () => ({ error: null, request: null }),
}));

it('shows deferred template validation before publishing', async () => {
  const view = await renderForm(
    <MatrixPublishSheet
      refData={{ org: 'org_a', project: 'prj_a' }}
      environments={[{
        id: 'env_a', org_id: 'org_a', project_id: 'prj_a', name: 'preview',
        display_order: 0, created_at: '2026-09-12T00:00:00Z',
      }]}
      revisions={new Map([['env_a', 1n]])}
      pendingByEnvironment={new Map([['env_a', [{
        versionId: 'pcv_a', keyId: 'key_a', name: 'PORT', classification: 'config',
        operation: 'set', configPreview: '${PORT}', validationDeferred: true,
      }]]])}
      problems={[]} protectedEnvironmentIds={[]} busy={false} mutationError={null}
      onPublish={vi.fn()} onClose={vi.fn()}
    />,
  );
  try {
    expect(view.container.textContent).toContain('Validated at fetch: invalid resolved config refuses delivery.');
    expect(view.container.textContent).toContain('template schemas are checked with each fetch');
    expect(view.container.textContent).not.toContain('✓ ready');
  } finally {
    await view.unmount();
  }
});
