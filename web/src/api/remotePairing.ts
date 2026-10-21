import {
  createRemotePairingInviteOp,
  listRemotePairingInvitesOp,
  pairRemoteOp,
  prepareRemotePairingOp,
  revokeRemotePairingInviteOp,
} from '@hikyo/operations';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ok, parsed } from './client.ts';
import { useSensitiveMutation } from './sensitiveMutation.ts';

const invitesKey = ['remote-pairing-invites'];

// The local preparation has no target address and never contacts a remote.
async function preparePairing(body?: { phrase?: string; word_count?: number }) {
  return parsed(prepareRemotePairingOp, body === undefined ? {} : { body });
}
export type PreparedPairing = Awaited<ReturnType<typeof preparePairing>>;

export function usePrepareRemotePairing() {
  return useSensitiveMutation({ mutationFn: preparePairing });
}

export function usePairRemote() {
  const queries = useQueryClient();
  return useSensitiveMutation({
    mutationFn: (body: { name: string; requester_state: string; invite_bundle: string }) =>
      parsed(pairRemoteOp, { body }),
    onSuccess: () => queries.invalidateQueries({ queryKey: ['remotes'] }),
  });
}

async function createInvite(body: {
  phrase: string;
  label: string;
  invite_lifetime_seconds: number;
  credential_lifetime_seconds: number;
}) {
  return parsed(createRemotePairingInviteOp, { body });
}
export type CreatedPairingInvite = Awaited<ReturnType<typeof createInvite>>;

export function useCreateRemotePairingInvite() {
  const queries = useQueryClient();
  return useSensitiveMutation({
    mutationFn: createInvite,
    onSuccess: () => queries.invalidateQueries({ queryKey: invitesKey }),
  });
}

// Only public metadata enters query caches, never phrase, capsule or bundle.
export function useRemotePairingInvites() {
  return useQuery({
    queryKey: invitesKey,
    queryFn: () => parsed(listRemotePairingInvitesOp, {}),
  });
}

export function useRevokeRemotePairingInvite() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (invite: string) => ok(revokeRemotePairingInviteOp, { path: { invite } }),
    onSuccess: () => queries.invalidateQueries({ queryKey: invitesKey }),
  });
}
