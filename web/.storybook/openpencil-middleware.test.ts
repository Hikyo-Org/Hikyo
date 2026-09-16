import type { DiscoveryInfo } from '@open-pencil/mcp/discovery';
import { describe, expect, it, vi } from 'vitest';

import { openInOpenPencil } from './openpencil-middleware.ts';

const info: DiscoveryInfo = {
  pid: 1,
  socketPath: null,
  httpPort: 7600,
  authRequired: true,
  authToken: 'secret',
  version: '0.14.0',
  startedAt: '',
};

describe('openInOpenPencil', () => {
  it('opens the file, selects the node, zooms', async () => {
    const rpc = vi.fn(async (_i: DiscoveryInfo, command: string, args: Record<string, unknown>) => {
      if (command === 'tool' && args.name === 'find_nodes') return { nodes: [{ id: 'T3Um0', name: 'Button/Primary' }] };
      return {};
    });
    const r = await openInOpenPencil('Button/Primary', { discovery: async () => info, rpc, penPath: '/repo/web/design/hikyo.pen' });
    expect(r.status).toBe(200);
    expect(rpc.mock.calls.map((c) => c[1])).toEqual(['open_file', 'tool', 'tool', 'tool']);
    expect(rpc.mock.calls[0][2]).toEqual({ path: '/repo/web/design/hikyo.pen' });
    expect(rpc.mock.calls[2][2]).toEqual({ name: 'select_nodes', args: { ids: ['T3Um0'] } });
    expect(rpc.mock.calls[3][2]).toEqual({ name: 'viewport_zoom_to_fit', args: { ids: ['T3Um0'] } });
  });
  it('503 when the app is not running', async () => {
    const rpc = vi.fn(async () => ({}));
    const r = await openInOpenPencil('Button/Primary', { discovery: async () => null, rpc, penPath: '/x.pen' });
    expect(r.status).toBe(503);
    expect(r.message).toMatch(/OpenPencil is not running/);
    expect(rpc).not.toHaveBeenCalled();
  });
  it('404 when the node is missing, without leaking the token', async () => {
    const rpc = vi.fn(async () => ({ nodes: [] }));
    const r = await openInOpenPencil('Button/Nope', { discovery: async () => info, rpc, penPath: '/x.pen' });
    expect(r.status).toBe(404);
    expect(JSON.stringify(r)).not.toContain('secret');
  });
});
