import type { WhoAmI } from '../app/AuthProvider.tsx';

/** An authenticated browser owner for tests that exercise protected controls. */
export const authenticatedIdentity: WhoAmI = {
  principal: {
    id: 'prn_123e4567-e89b-12d3-a456-426614174010',
    kind: 'human',
    display_name: 'Test operator',
  },
  session: {
    id: 'ses_123e4567-e89b-12d3-a456-426614174000',
    artifact: 'browser',
    created_at: '2026-08-22T10:00:00Z',
    idle_expires_at: '2099-08-22T10:30:00Z',
    absolute_expires_at: '2099-08-22T18:00:00Z',
    assurance: {
      method: 'local-password',
      factors: ['password', 'totp'],
      authenticated_at: '2026-08-22T10:00:00Z',
    },
  },
  capabilities: { instance_operator: true },
};
