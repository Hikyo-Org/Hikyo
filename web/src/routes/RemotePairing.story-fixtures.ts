import {
  zCreateRemotePairingInviteRequest,
  zDirectoryListing,
  zInstanceConnectionList,
  zMeta,
  zMyOrgList,
  zPairRemoteRequest,
  zPrepareRemotePairingRequest,
  zRemote,
  zRemoteList,
  zRemotePairingInviteCreated,
  zRemotePairingInviteList,
  zRemotePairingPrepared,
  zWorkspaceOriginList,
} from '@hikyo/zod';

import type { MockRoute } from '../../.storybook/withApp.tsx';
import { authenticatedIdentity } from '../testkit/identity.ts';

// Public, deterministic UI samples. These values carry no pairing authority.
export const pairingSample = {
  phrase: 'abacus bamboo candle daisy ember forest',
  bundle: btoa(JSON.stringify({ version: 1, id: 'public-invitation-fixture', origin: 'https://remote.example', spki_pin: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=', capability: 'hik_1_hc_publicFixtureWithoutPairingAuthority', expires_at: new Date(Date.now() + 3_600_000).toISOString() })).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', ''),
  origin: 'https://remote.example',
  pin: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=',
};

export const pairingIdentity = {
  ...authenticatedIdentity,
  principal: { ...authenticatedIdentity.principal, display_name: 'Alex Rivera' },
  capabilities: { instance_operator: false, delivery_report_grant: { instance: false, orgs: [] } },
};

/** Per-story transport storage; no keys, PAKE exchange or remote connection. */
export function createPairingFixture() {
  let remotes = zRemoteList.parse({ items: [], count: 0 }).items;
  let invites = zRemotePairingInviteList.parse({ items: [], count: 0, origin: pairingSample.origin }).items;
  let prepared = false;
  const responses: readonly MockRoute[] = [
    { url: '/api/v1/me/orgs', body: zMyOrgList.parse({ items: [], count: 0 }) },
    { url: '/api/v1/meta', body: zMeta.parse({ server_version: 'storybook-local', api_revision: 12, protocol_capabilities: [], instance_identity: 'storybook-local-instance' }) },
    { url: '/api/v1/instance/directory', body: zDirectoryListing.parse({ identity: 'storybook-local-instance', version: 'storybook-local', orgs: [], org_count: 0, project_count: 0 }) },
    { url: '/api/v1/instance/connections', body: zInstanceConnectionList.parse({ items: [], count: 0 }) },
    { url: '/api/v1/instance/workspace-origins', body: zWorkspaceOriginList.parse({ items: [], count: 0 }) },
    { url: '/api/v1/instance/remotes', handler: () => Response.json(zRemoteList.parse({ items: remotes, count: remotes.length })) },
    { url: '/api/v1/instance/pairing-invites', handler: () => Response.json(zRemotePairingInviteList.parse({ items: invites, count: invites.length, origin: pairingSample.origin })) },
    {
      url: '/api/v1/instance/pairing-requests', method: 'POST',
      handler: async (request) => {
        const raw = await request.text();
        request.signal.throwIfAborted();
        const input = zPrepareRemotePairingRequest.parse(raw === '' ? {} : JSON.parse(raw));
        const words = pairingSample.phrase.split(' ');
        const phrase = input.phrase ?? Array.from({ length: input.word_count ?? 6 }, (_, index) => words[index % words.length]).join(' ');
        prepared = true;
        return Response.json(zRemotePairingPrepared.parse({ phrase, requester_state: 'opaque-local-continuation-fixture', expires_at: new Date(Date.now() + 3_600_000).toISOString() }), { status: 201 });
      },
    },
    {
      url: '/api/v1/instance/pairing-invites', method: 'POST',
      handler: async (request) => {
        const input = zCreateRemotePairingInviteRequest.parse(await request.json());
        request.signal.throwIfAborted();
        const now = Date.now();
        const result = zRemotePairingInviteCreated.parse({
          invite: { id: `invite-sample-${invites.length + 1}`, label: input.label, origin: pairingSample.origin, spki_pin: pairingSample.pin, created_at: new Date(now).toISOString(), expires_at: new Date(now + input.invite_lifetime_seconds * 1_000).toISOString(), credential_expires_at: new Date(now + input.credential_lifetime_seconds * 1_000).toISOString() },
          invite_bundle: pairingSample.bundle,
        });
        invites = [...invites, result.invite];
        return Response.json(result, { status: 201 });
      },
    },
    {
      url: /^https?:\/\/[^/]+\/api\/v1\/instance\/pairing-invites\/[^/?]+$/, method: 'DELETE',
      handler: (request) => {
        request.signal.throwIfAborted();
        const id = decodeURIComponent(new URL(request.url).pathname.split('/').at(-1) ?? '');
        invites = invites.map((invite) => invite.id === id ? { ...invite, revoked_at: new Date().toISOString() } : invite);
        return new Response(null, { status: 204 });
      },
    },
    {
      url: '/api/v1/instance/remotes/pair', method: 'POST',
      handler: async (request) => {
        const input = zPairRemoteRequest.parse(await request.json());
        request.signal.throwIfAborted();
        if (!prepared || input.requester_state !== 'opaque-local-continuation-fixture' || input.invite_bundle !== pairingSample.bundle) {
          return Response.json({ error: { code: 'bad_request', message: 'Use the sample invitation from the approval panel after preparing pairing.' } }, { status: 400 });
        }
        const now = new Date().toISOString();
        const remote = zRemote.parse({ id: 'rem_00000000-0000-7000-8000-000000000001', name: input.name, url: pairingSample.origin, spki_pin: pairingSample.pin, created_at: now, created_by: pairingIdentity.principal.id, last_attempt_at: now, observed_at: now, state: 'ok', stale: false, identity: 'storybook-peer-instance', version: 'storybook-local', org_count: 1, project_count: 2, orgs: [{ name: 'Platform', projects: ['api', 'web'] }] });
        remotes = [remote];
        prepared = false;
        return Response.json(remote, { status: 201 });
      },
    },
    // RemoteCard eagerly prepares a workspace launcher. Refuse this separate
    // human authentication ceremony explicitly; never invent a signed-in user.
    { url: '/api/v1/auth/workspace/start', method: 'POST', status: 403, body: { error: { code: 'forbidden', message: 'Workspace authentication is outside this UI sample.' } } },
  ];
  return {
    responses,
    reset() { remotes = []; invites = []; prepared = false; },
  };
}
