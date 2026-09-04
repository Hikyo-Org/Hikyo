import {
  addRemoteOp,
  addWorkspaceOriginOp,
  listInstanceConnectionsOp,
  listMySessionsOp,
  listRemotesOp,
  listWorkspaceOriginsOp,
  mintInstanceConnectionOp,
  removeRemoteOp,
  removeWorkspaceOriginOp,
  renameRemoteOp,
  revokeInstanceConnectionOp,
  revokeMySessionOp,
} from '@hikyo/operations';
import {
  zInstanceConnection,
  zInstanceConnectionList,
  zMintedInstanceConnection,
  zRemote,
  zRemoteList,
  zSessionList,
  zWorkspaceOriginList,
} from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import { useState } from 'react';
import type { z } from 'zod';

import { ok, parsed, parsedPick } from './client.ts';

/**
 * The multi-instance surfaces' same-origin data (#71).
 *
 * Everything here talks to THIS instance and nothing else. The two halves it
 * serves are deliberately different instances' concerns and only look alike:
 *
 *   - `useRemotes` is the VIEWING side — the entries this instance holds and
 *     the last-known directory of each. The fetch to the remote happens on the
 *     server, under a pinned connection; the browser never touches it.
 *   - `useWorkspaceOrigins` is the SERVING side — the origins this instance
 *     consents to be operated from. Removing one is the ADR's atomic kill
 *     switch, not a headers change.
 *
 * The cross-origin half of the workspace tier lives in `workspace.ts`, because
 * it must not go through this module's client at all: that one carries cookies
 * and a synchronizer token, and neither may cross an origin.
 */

export type Remote = z.infer<typeof zRemote>;
export type RemoteList = z.infer<typeof zRemoteList>;
export type SessionList = z.infer<typeof zSessionList>;
export type ActiveSession = SessionList['items'][number];
export type InstanceConnection = z.infer<typeof zInstanceConnection>;
export type InstanceConnectionList = z.infer<typeof zInstanceConnectionList>;
/**
 * The only fields the display-once ceremony reads back. It is a NARROW pick on
 * purpose: parsing the whole `MintedInstanceConnection` would let a drift in an
 * unrelated `connection` member throw away the one value nothing in the system
 * can ever return again — the same discipline the machine-credential mint keeps
 * (`identities.ts`). The label the operator typed is carried separately, so the
 * dialog never needs the echoed `connection` object at all.
 */
export type MintedConnectionValue = Pick<
  z.infer<typeof zMintedInstanceConnection>,
  'value' | 'clamped'
>;

const remotesKey = ['remotes'] as const;
const originsKey = ['workspace-origins'] as const;
const sessionsKey = ['sessions'] as const;
const connectionsKey = ['instance-connections'] as const;

/**
 * The directory refresh cadence.
 *
 * POLLING, and that is a locked decision rather than a shortcut: the update
 * channel is `EventSource`, native `EventSource` cannot set an `Authorization`
 * header, and the ADR's answer is the polling fallback the architecture already
 * ships — never a weakened SSE authentication.
 *
 * TWENTY seconds because the per-viewer trigger budget is 6/min and a human has
 * more than one tab: 3/min per tab keeps two tabs of the same human inside it.
 * A third tab, or jitter across the window edge, spends the budget — and that
 * degrades to a snapshot marked stale with its age, which is the freshness
 * model working, not an error. It is still a rate worth staying under: a card
 * that is quietly rate-limited refreshes no faster than one that is not.
 */
const DIRECTORY_POLL_MS = 20_000;

/** useRemotes is the directory card list, refreshed on a poll. */
export function useRemotes(): UseQueryResult<RemoteList> {
  return useQuery({
    queryKey: remotesKey,
    queryFn: () => parsed(listRemotesOp, {}),
    refetchInterval: DIRECTORY_POLL_MS,
    retry: false,
  });
}

export function useAddRemote() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; url: string; spkiPin: string; credential: string }) =>
      parsed(addRemoteOp, {
          body: {
            name: input.name,
            url: input.url,
            spki_pin: input.spkiPin,
            credential: input.credential,
          },
        }),
    onSuccess: () => queries.invalidateQueries({ queryKey: remotesKey }),
  });
}

export function useRenameRemote() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: { remote: string; name: string }) =>
      parsed(renameRemoteOp, { path: { remote: input.remote }, body: { name: input.name } }),
    onSuccess: () => queries.invalidateQueries({ queryKey: remotesKey }),
  });
}

export function useRemoveRemote() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (remote: string) => ok(removeRemoteOp, { path: { remote } }),
    onSuccess: () => queries.invalidateQueries({ queryKey: remotesKey }),
  });
}

/** useWorkspaceOrigins is the serving side's consent list. */
export function useWorkspaceOrigins(): UseQueryResult<z.infer<typeof zWorkspaceOriginList>> {
  return useQuery({
    queryKey: originsKey,
    queryFn: () => parsed(listWorkspaceOriginsOp, {}),
    retry: false,
  });
}

export function useAddWorkspaceOrigin() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (origin: string) =>
      parsed(addWorkspaceOriginOp, { body: { origin } }),
    onSuccess: () => queries.invalidateQueries({ queryKey: originsKey }),
  });
}

/**
 * useRemoveWorkspaceOrigin is the KILL SWITCH. The response carries the number
 * of workspace sessions it revoked, and the UI says that number out loud: an
 * operator pulling consent needs to see what it cost, and "removed" alone hides
 * whether anyone was mid-flight.
 */
export function useRemoveWorkspaceOrigin() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (origin: string) =>
      parsed(removeWorkspaceOriginOp, { body: { origin } }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: originsKey });
      void queries.invalidateQueries({ queryKey: sessionsKey });
    },
  });
}

/**
 * The receiving side's connection credentials (#498).
 *
 * These are minted on THIS instance for a peer to hold and present at its
 * server-side directory fetch — the write-only counterpart to the `credential`
 * a `useAddRemote` entry consumes over on the viewing instance. The value is
 * disclosed exactly once at mint and never read back: every hook here trades in
 * metadata alone, and the plaintext lives only in the mint mutation's own
 * result until the caller clears it.
 */
export function useConnections(): UseQueryResult<InstanceConnectionList> {
  return useQuery({
    queryKey: connectionsKey,
    queryFn: () => parsed(listInstanceConnectionsOp, {}),
    retry: false,
  });
}

export type MintConnectionInput = {
  readonly label: string;
  readonly lifetimeSeconds?: number;
  readonly indefinite?: boolean;
};

/**
 * useMintConnection returns the display-once value, and does so WITHOUT a
 * TanStack mutation on purpose: a mutation caches its `data`, and this data is
 * an irretrievable plaintext credential that must never outlive the dialog it
 * is handed to. So the value flows straight back to the caller and touches no
 * query or mutation cache — the same discipline the machine-credential mint
 * follows. `lifetime_seconds` and `indefinite` are mutually exclusive at the
 * contract; the form makes the choice a radio so the both-named 400 is
 * unreachable from the UI.
 */
export function useMintConnection() {
  const queries = useQueryClient();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const mint = async (input: MintConnectionInput): Promise<MintedConnectionValue> => {
    setPending(true);
    setError(null);
    try {
      const minted = await parsedPick(
        mintInstanceConnectionOp,
        {
          body: {
            label: input.label,
            ...(input.lifetimeSeconds === undefined
              ? {}
              : { lifetime_seconds: input.lifetimeSeconds }),
            ...(input.indefinite === undefined ? {} : { indefinite: input.indefinite }),
          },
        },
        { value: true, clamped: true },
      );
      // Disclose FIRST, refresh the inventory second and un-awaited: the value
      // is irretrievable, so it must reach the guarded dialog the instant the
      // POST returns, never after a list refetch the operator could interrupt
      // by reloading with the credential already committed server-side.
      void queries.invalidateQueries({ queryKey: connectionsKey });
      return minted;
    } catch (caught) {
      // A mint whose response never arrived may still have COMMITTED, so the
      // inventory is refreshed on the failure path too: a lost-but-live
      // credential must at least become visible enough to revoke.
      void queries.invalidateQueries({ queryKey: connectionsKey });
      setError(caught);
      throw caught;
    } finally {
      setPending(false);
    }
  };
  return { mint, pending, error };
}

/**
 * useRevokeConnection retires the credential and its principal. It bites at the
 * peer's next directory fetch; a double revoke is a 409, surfaced rather than
 * swallowed so the operator sees an act that did not put a second event on the
 * trail.
 */
export function useRevokeConnection() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (connection: string) =>
      ok(revokeInstanceConnectionOp, { path: { connection } }),
    // Refresh on SETTLE, not just success: a 409 means another tab already
    // revoked it, so the inventory is stale in exactly that case — refetching
    // flips the row to revoked and drops its Revoke action rather than leaving
    // a credential that reads live and cannot be revoked again.
    onSettled: () => queries.invalidateQueries({ queryKey: connectionsKey }),
  });
}

/** useSessions is the caller's OWN active sessions, workspace ones included. */
export function useSessions(): UseQueryResult<SessionList> {
  return useQuery({
    queryKey: sessionsKey,
    queryFn: () => parsed(listMySessionsOp, {}),
    retry: false,
  });
}

export function useRevokeSession() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (session: string) => ok(revokeMySessionOp, { path: { session } }),
    onSuccess: () => queries.invalidateQueries({ queryKey: sessionsKey }),
  });
}

/** originOf is a remote's browser origin: the popup's destination. */
function originOf(url: string): string {
  return new URL(url).origin;
}

/** safeOriginOf never throws on a stored URL the browser cannot parse. */
export function safeOriginOf(url: string): string {
  try {
    return originOf(url);
  } catch {
    return url;
  }
}

/**
 * remoteStateText is the human sentence for each of the seven closed states.
 *
 * The card must never carry a state by colour alone, so this text IS the state
 * — the colour is decoration on top of it. `credential-rejected` is called out
 * as its own loud sentence rather than folded into "unreachable": the two have
 * completely different fixes, and an operator who reads "unreachable" will go
 * and check the network.
 */
export function remoteStateText(remote: Remote): string {
  switch (remote.state) {
    case 'ok':
      return 'Reachable';
    case 'unreachable':
      return 'Unreachable';
    case 'credential-rejected':
      return 'Credential rejected — this instance is reachable and is refusing our credential. Mint a fresh one on the peer and re-add the entry.';
    case 'pin-mismatch':
      return 'Certificate pin mismatch — the key at that URL is not the one this entry pinned. Do not re-add until you know why.';
    case 'redirect-refused':
      return 'Refused: that URL answered a redirect, and a directory fetch never follows one.';
    case 'identity-conflict':
      return 'Identity conflict — that URL now answers as a different instance than the one this entry was added for.';
    case 'self-connected':
      return 'This entry points at this instance itself.';
  }
}

/**
 * stalenessText is the "unreachable for Xh — showing last known" sentence.
 *
 * It is derived from `stale_for_seconds`, which the server computes from the
 * OUTCOME rather than from the age: a snapshot that is old because nothing
 * changed is not stale, and a snapshot that is one minute old because the last
 * fetch failed is.
 */
export function stalenessText(remote: Remote): string | null {
  if (!remote.stale) {
    return null;
  }
  const seconds = remote.stale_for_seconds ?? 0;
  return `Showing the last known directory, ${humanAge(seconds)} old.`;
}

function humanAge(seconds: number): string {
  if (seconds < 90) {
    return `${Math.max(seconds, 0)} seconds`;
  }
  if (seconds < 5400) {
    return `${Math.round(seconds / 60)} minutes`;
  }
  if (seconds < 172_800) {
    return `${Math.round(seconds / 3600)} hours`;
  }
  return `${Math.round(seconds / 86_400)} days`;
}
