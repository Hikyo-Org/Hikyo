// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';

import { SURFACES } from '../app/navigation.ts';
import { FleetUpdateNotice } from './Shell.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const notifyUpdate = vi.hoisted(() => vi.fn());
vi.mock('../app/notifications.tsx', () => ({ notifyUpdate }));

it('links a fleet update to the registered Remotes route, not a path that matches nothing', async () => {
  const container = document.createElement('div');
  const root = createRoot(container);
  await act(async () =>
    root.render(
      <FleetUpdateNotice
        local={null}
        remotes={[{ origin: 'https://edge.example.com', status: { available: true, channel: 'stable', current_version: '1.0.0', latest_version: '1.3.1', prerelease: false, apply_supported: false } }]}
        principalId="prn_fleet_notice_test"
      />,
    ),
  );
  const href = notifyUpdate.mock.calls[0]?.[1];
  expect(SURFACES.find((surface) => surface.path === href)?.id).toBe('remotes');
  await act(async () => root.unmount());
});
