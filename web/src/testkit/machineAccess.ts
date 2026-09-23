import type { DynamicProvider } from '../api/dynamic.ts';
import type { ServiceAccount } from '../api/identities.ts';
import { authenticatedIdentity } from './identity.ts';

// The machine-access fixtures every dialog story shares: one active postgres
// provider with a credential set, and one workload account with two live
// credentials. Authority and creator are the testkit operator (identity.ts).

export const dynamicProvider: DynamicProvider = {
  id: 'dpv_123e4567-e89b-12d3-a456-426614174050',
  kind: 'postgres',
  origin: 'db.internal.example.com:5432',
  tls_mode: 'verify-full',
  grant_role: 'hikyo_leases',
  credential_present: true,
  credential_set_at: '2026-09-01T00:00:00Z',
  authority_principal_id: authenticatedIdentity.principal.id,
  state: 'active',
  created_at: '2026-08-20T00:00:00Z',
};

export const serviceAccount: ServiceAccount = {
  id: 'msa_123e4567-e89b-12d3-a456-426614174020',
  principal_id: 'prn_123e4567-e89b-12d3-a456-426614174020',
  name: 'api-gateway',
  kind: 'workload',
  created_at: '2026-08-01T00:00:00Z',
  created_by: authenticatedIdentity.principal.id,
  live_credentials: 2,
};
