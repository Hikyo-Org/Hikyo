// @vitest-environment happy-dom
import { expect, it } from 'vitest';

import type { ScimDirectoryUser } from '../api/scim.ts';
import { renderForm } from '../testkit/renderForm.tsx';
import { DirectoryUserRow } from './ScimProvisioning.tsx';

const user: ScimDirectoryUser = {
  id: 'usr_00000000-0000-0000-0000-000000000001',
  user_name: 'ada',
  account_id: 'acc_00000000-0000-0000-0000-000000000001',
  active: false,
  groups: ['grp_00000000-0000-0000-0000-000000000001'],
  created_at: '2026-09-01T00:00:00Z',
  updated_at: '2026-09-01T00:00:00Z',
  attention: [],
};

it('flags a deprovisioned user loudly only while manual grants remain, and pluralises groups', async () => {
  const withGrants = await renderForm(
    <DirectoryUserRow
      user={{
        ...user,
        attention: [
          {
            state: 'manual_grants_remain',
            subject_ref: 'ada',
            cause: 'deprovisioned',
            entered_at: '2026-09-01T00:00:00Z',
            remediation: 'review',
          },
        ],
      }}
    />,
  );
  const badge = withGrants.container.querySelector('.badge[data-state="inactive"]');
  expect(badge?.textContent).toBe('! deprovisioned, manual grants remain');
  expect(badge?.querySelector('[aria-hidden="true"]')?.textContent).toBe('! ');
  expect(withGrants.container.textContent).toContain('in 1 group');
  await withGrants.unmount();

  const plain = await renderForm(<DirectoryUserRow user={{ ...user, groups: [...user.groups, 'grp_2'] }} />);
  expect(plain.container.querySelector('.badge[data-state="inactive"]')?.textContent).toBe('deprovisioned');
  expect(plain.container.textContent).toContain('in 2 groups');
  await plain.unmount();
});
