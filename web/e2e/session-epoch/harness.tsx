import { listMyOrgsOp } from '@hikyo/operations';
import { QueryClient, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { createRoot } from 'react-dom/client';

import { parsed } from '../../src/api/client.ts';
import { announceSessionChange, checkSessionCookie } from '../../src/api/sessionEpoch.ts';
import { rememberWorkspace, useWorkspaces } from '../../src/api/workspace.ts';
import { AuthProvider, useAuth } from '../../src/app/AuthProvider.tsx';

const clients: QueryClient[] = [];

function Probe() {
  const auth = useAuth();
  const client = useQueryClient();
  if (!clients.includes(client)) clients.push(client);
  const [disclosure, setDisclosure] = useState('');
  const workspaces = useWorkspaces();
  const orgs = useQuery({
    queryKey: ['private-orgs'],
    queryFn: () => parsed(listMyOrgsOp, {}),
    enabled: auth.state.status === 'authenticated',
  });
  const retired = clients.filter((held) => held !== client);

  return <>
    <output data-testid="owner">{auth.identity?.principal.display_name ?? auth.state.status}</output>
    <output data-testid="query">{orgs.data?.items.map((org) => org.name).join(',') ?? ''}</output>
    <output data-testid="disclosure">{disclosure}</output>
    <output data-testid="workspace">{workspaces.map((workspace) => workspace.value).join(',')}</output>
    <output data-testid="retired-queries">{retired.reduce((total, held) => total + held.getQueryCache().getAll().filter((query) => query.state.data !== undefined).length, 0)}</output>
    <output data-testid="retired-mutations">{retired.reduce((total, held) => total + held.getMutationCache().getAll().length, 0)}</output>
    <button onClick={() => {
      setDisclosure('A display-once secret');
      client.setQueryData(['private-marker'], 'A cached secret');
      client.getMutationCache().build(client, { mutationKey: ['private-mutation'] });
      rememberWorkspace({
        origin: 'https://peer.example',
        value: 'A workspace bearer',
        session: 'ses_workspace',
        idleExpiresAt: '2099-01-01T00:00:00Z',
        absoluteExpiresAt: '2099-01-01T00:00:00Z',
      });
    }}>Reveal A</button>
    <button onClick={() => {
      document.body.dataset.lateResult = 'pending';
      void parsed(listMyOrgsOp, {}).then((result) => {
        document.body.dataset.lateResult = 'delivered';
        client.setQueryData(['private-orgs'], result);
        setDisclosure('late A secret');
      }).catch(() => { document.body.dataset.lateResult = 'rejected'; });
    }}>Start delayed A request</button>
    <button onClick={() => {
      announceSessionChange();
      // The initiating tab also observes the shared cookie at the same guard
      // used by real action dispatch. Peer tabs receive the browser event.
      checkSessionCookie();
    }}>Announce replacement</button>
  </>;
}

const root = document.getElementById('root');
if (root === null) throw new Error('Missing harness root');
createRoot(root).render(<AuthProvider><Probe /></AuthProvider>);
