import {
  createDynamicProviderOp,
  deleteDynamicProviderOp,
  listDynamicProvidersOp,
  listLeasesOp,
  mintLeaseOp,
  renewLeaseOp,
  revokeDynamicProviderCredentialOp,
  revokeLeaseOp,
  setDynamicProviderCredentialOp,
  settleLeaseOp,
} from '@hikyo/operations';
import { zDynamicLease, zDynamicProvider } from '@hikyo/zod';
import {
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, ok, parsed, parsedPick } from './client.ts';
import { useTransport } from './transport.tsx';

export type DynamicLease = z.infer<typeof zDynamicLease>;

/** DynamicProvider is a project-scoped provider row. Never carries a credential. */
export type DynamicProvider = z.infer<typeof zDynamicProvider>;

/** ProjectRef addresses one project. Providers are project-scoped. */
export type ProjectRef = { readonly org: string; readonly project: string };

/** A lease paired with the name of the environment it belongs to. */
type LeaseRow = { readonly environmentName: string; readonly lease: DynamicLease };

export type LeasesView = {
  readonly rows: readonly LeaseRow[];
  readonly isPending: boolean;
  readonly isError: boolean;
};

type EnvRef = { readonly id: string; readonly name: string };

const leasesKey = (org: string, project: string, env: string) =>
  ['dynamic-leases', org, project, env] as const;

/**
 * useLeases lists the dynamic-secret leases across a project's environments.
 * Leases are environment-scoped, so the machine-access surface (project-scoped)
 * fans out over the environments and flattens the result. Status and metadata
 * only: the credential is disclosed once, at mint, and never read back.
 */
export function useLeases(
  p: { readonly org: string; readonly project: string },
  environments: readonly EnvRef[],
): LeasesView {
  const transport = useTransport();
  return useQueries({
    queries: environments.map((env) => ({
      queryKey: leasesKey(p.org, p.project, env.id),
      queryFn: () =>
        parsed(listLeasesOp, {
          path: { org: p.org, project: p.project, environment: env.id },
          ...transport,
        }),
      retry: false,
    })),
    combine: (results): LeasesView => ({
      rows: environments.flatMap((env, index) =>
        (results[index]?.data?.items ?? []).map((lease) => ({ environmentName: env.name, lease })),
      ),
      isPending: results.some((r) => r.isPending),
      isError: results.some((r) => r.isError),
    }),
  });
}

/**
 * useRefreshLeases re-reads one environment's lease listing.
 *
 * It exists for the mint's no-`onSuccess` path: the mint is a plain async call
 * (below), so a mint whose response was lost — but may have COMMITTED, leaving a
 * live role whose password is gone forever — has to be able to invalidate the
 * listing the operator will scan to find and revoke it.
 */
export function useRefreshLeases(p: ProjectRef): (environment: string) => void {
  const queries = useQueryClient();
  return (environment: string) => {
    void queries.invalidateQueries({ queryKey: leasesKey(p.org, p.project, environment) });
  };
}

// ---- Providers -----------------------------------------------------------

const providersKey = (org: string, project: string) =>
  ['dynamic-providers', org, project] as const;

/** All lease listings for a project, whatever the environment. */
const leasesForProjectKey = (org: string, project: string) =>
  ['dynamic-leases', org, project] as const;

/** useDynamicProviders lists a project's dynamic-secret providers. */
export function useDynamicProviders(
  p: ProjectRef,
): UseQueryResult<{ items: readonly DynamicProvider[] }> {
  const transport = useTransport();
  return useQuery({
    queryKey: providersKey(p.org, p.project),
    queryFn: () =>
      parsed(listDynamicProvidersOp, {
        path: { org: p.org, project: p.project },
        ...transport,
      }),
    enabled: p.org !== '' && p.project !== '',
    retry: false,
  });
}

/**
 * useRefreshProviders re-reads the provider listing. It exists because the
 * credential-bearing writes below are plain async calls (not mutations), so they
 * have no `onSuccess` — and because a write whose response never arrived may
 * still have COMMITTED, so the caller must be able to refresh on the failure path
 * too.
 */
export function useRefreshProviders(p: ProjectRef): () => void {
  const queries = useQueryClient();
  return () => {
    void queries.invalidateQueries({ queryKey: providersKey(p.org, p.project) });
  };
}

/**
 * createDynamicProvider configures a provider. The server PROBES the origin with
 * the admin credential before it stores anything: a create that succeeds is a
 * provider Hikyo could reach and authenticate against.
 *
 * Deliberately NOT a `useMutation`: TanStack retains a mutation's VARIABLES in
 * its cache until garbage collection, and the request body carries the
 * write-only admin credential in plaintext — a mutation would leave that secret
 * reachable from the query client long after the dialog closed. A plain async
 * call keeps it on the stack and nowhere else (mirrors `mintCredential`).
 */
export async function createDynamicProvider(
  p: ProjectRef,
  input: {
    kind: DynamicProvider['kind'];
    origin: string;
    grant_role: string;
    credential: string;
  },
): Promise<void> {
  await parsed(createDynamicProviderOp, {
    path: { org: p.org, project: p.project },
    body: {
      kind: input.kind,
      origin: input.origin,
      tls_mode: 'verify-full',
      grant_role: input.grant_role,
      credential: input.credential,
    },
  });
}

/**
 * useDeleteDynamicProvider removes a provider. The server refuses (409) while
 * the provider has live leases unless `revoke_all` is set, in which case it
 * queues every one of them for revocation and returns their ids.
 */
export function useDeleteDynamicProvider(p: ProjectRef) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: { provider: string; revokeAll: boolean }) =>
      parsed(deleteDynamicProviderOp, {
        path: { org: p.org, project: p.project, provider: input.provider },
        query: { revoke_all: input.revokeAll },
      }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: providersKey(p.org, p.project) });
      // A delete cascades to lease revocation across every environment, so the
      // whole project's lease listings are stale — invalidate by the shared prefix.
      void queries.invalidateQueries({ queryKey: leasesForProjectKey(p.org, p.project) });
    },
  });
}

/**
 * setDynamicProviderCredential replaces the write-only admin credential. The
 * credential is never read back; setting it re-probes the provider, so a success
 * means the new credential authenticates.
 *
 * Plain async for the same reason as `createDynamicProvider`: the request body
 * carries the plaintext credential, which a `useMutation` would cache in its
 * variables. Refresh the provider listing through `useRefreshProviders`.
 */
export async function setDynamicProviderCredential(
  p: ProjectRef,
  input: { provider: string; credential: string },
): Promise<void> {
  await ok(setDynamicProviderCredentialOp, {
    path: { org: p.org, project: p.project, provider: input.provider },
    body: { credential: input.credential },
  });
}

/**
 * useRevokeDynamicProviderCredential clears the admin credential. It does NOT
 * touch existing leases: their roles stay minted at the provider, but the worker
 * can no longer renew, revoke or expire them until a replacement is set.
 */
export function useRevokeDynamicProviderCredential(p: ProjectRef) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (provider: string) =>
      ok(revokeDynamicProviderCredentialOp, {
        path: { org: p.org, project: p.project, provider },
      }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: providersKey(p.org, p.project) });
    },
  });
}

// ---- Lease write ops -----------------------------------------------------

/**
 * useRenewLease queues a renewal. The server flips the lease to `renewing` and
 * the worker extends the role; the response reports the queued state, not a new
 * expiry. Only an `active` lease may be renewed.
 */
export function useRenewLease(p: ProjectRef) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      environment: string;
      lease: string;
      maxTtlSeconds: number | null;
    }) =>
      parsed(renewLeaseOp, {
        path: { org: p.org, project: p.project, environment: input.environment, lease: input.lease },
        body: { max_ttl_seconds: input.maxTtlSeconds },
      }),
    onSuccess: (_lease, input) => {
      void queries.invalidateQueries({ queryKey: leasesKey(p.org, p.project, input.environment) });
    },
  });
}

/**
 * useRevokeLease queues a revocation. The worker drops the role; the response
 * reports the queued `revoking` state. Revoke is allowed from any non-terminal
 * state so a compromised workload's lease can always be torn down.
 */
export function useRevokeLease(p: ProjectRef) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: { environment: string; lease: string }) =>
      parsed(revokeLeaseOp, {
        path: { org: p.org, project: p.project, environment: input.environment, lease: input.lease },
      }),
    onSuccess: (_lease, input) => {
      void queries.invalidateQueries({ queryKey: leasesKey(p.org, p.project, input.environment) });
    },
  });
}

/**
 * useSettleLease re-triggers reconcile on an `unknown` lease — the only state it
 * accepts. The worker re-probes the provider and settles the lease to its true
 * terminal or active state.
 */
export function useSettleLease(p: ProjectRef) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: { environment: string; lease: string }) =>
      parsed(settleLeaseOp, {
        path: { org: p.org, project: p.project, environment: input.environment, lease: input.lease },
      }),
    onSuccess: (_lease, input) => {
      void queries.invalidateQueries({ queryKey: leasesKey(p.org, p.project, input.environment) });
    },
  });
}

/**
 * zLeaseMinted narrows the mint result to what the caller may keep: the
 * username and the display-once password, plus its expiry. Deliberately not the
 * whole `lease` member — that is re-read from the listing a moment later, and
 * parsing it here would let a drift in an unrelated field throw away the one
 * value nothing in the system can ever return again.
 */
const zLeaseMinted = mintLeaseOp.response.pick({
  username: true,
  password: true,
  expires_at: true,
});

export type LeaseMinted = z.infer<typeof zLeaseMinted>;

/**
 * mintLease is the display-once lease mint, and — like `mintCredential` — it is
 * deliberately NOT a `useMutation`: TanStack keeps a mutation's result cached
 * until garbage collection, and the password's whole contract is that it lives
 * in exactly one place, the ephemeral dialog that renders it once.
 */
export async function mintLease(
  p: { readonly org: string; readonly project: string; readonly environment: string },
  req: { readonly providerId: string; readonly maxTtlSeconds: number },
): Promise<LeaseMinted> {
  return parsedPick(
    mintLeaseOp,
    {
      path: { org: p.org, project: p.project, environment: p.environment },
      body: { provider_id: req.providerId, max_ttl_seconds: req.maxTtlSeconds },
    },
    { username: true, password: true, expires_at: true },
  );
}

// ---- Refusal text --------------------------------------------------------

/** withDetail appends the server's safe detail sentence when it carried one. */
function withDetail(base: string, error: unknown): string {
  if (error instanceof ApiError && error.detail !== undefined && error.detail !== '') {
    return `${base} (${error.detail})`;
  }
  return base;
}

/**
 * createProviderRefusalText names a provider-create refusal in its own
 * vocabulary. The load-bearing case is 400: the server probes the origin with
 * the credential before storing anything, so a 400 is "malformed, unreachable,
 * or the credential was refused" — the detail says which.
 */
export function createProviderRefusalText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return withDetail(
          'The server refused the provider: its origin, grant role or credential was rejected, or PostgreSQL could not be reached and authenticated with them. Nothing was created.',
          error,
        );
      case 401:
        return 'The session could not be authenticated. Reload and sign in before configuring a provider.';
      case 403:
        return 'Configuring a provider needs manage-identities on this project. Nothing was created.';
      case 404:
        return 'This project is no longer here, or you may not administer its identities. Nothing was created.';
      case 409:
        return withDetail('The server refused that as a conflict. Nothing was created.', error);
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `The provider could not be created (server error ${String(error.status)}).`;
    }
  }
  return 'The provider could not be created.';
}

/**
 * setCredentialRefusalText names a credential-replace refusal. It re-probes, so
 * 400 is the unreachable/refused-credential case, exactly as create.
 */
export function setCredentialRefusalText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return withDetail(
          'The server refused the credential: PostgreSQL could not be reached and authenticated with it. The stored credential is unchanged.',
          error,
        );
      case 401:
        return 'The session could not be authenticated. Reload and sign in before replacing the credential.';
      case 403:
        return 'Replacing the credential needs manage-identities on this project.';
      case 404:
        return 'That provider is no longer here — someone may have deleted it.';
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `The credential could not be set (server error ${String(error.status)}).`;
    }
  }
  return 'The credential could not be set.';
}

/** revokeCredentialRefusalText names a credential-revoke refusal. */
export function revokeCredentialRefusalText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 401:
        return 'The session could not be authenticated. Reload and sign in first.';
      case 403:
        return 'Revoking the credential needs manage-identities on this project.';
      case 404:
        return 'That provider is no longer here — someone may have deleted it.';
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `The credential could not be revoked (server error ${String(error.status)}).`;
    }
  }
  return 'The credential could not be revoked.';
}

/**
 * deleteProviderRefusalText names a provider-delete refusal. A 409 is the
 * live-leases guard: the operator must confirm the cascade to proceed.
 */
export function deleteProviderRefusalText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 401:
        return 'The session could not be authenticated. Reload and sign in before deleting a provider.';
      case 403:
        return 'Deleting a provider needs manage-identities on this project.';
      case 404:
        return 'That provider is no longer here — someone may have deleted it already.';
      case 409:
        return 'This provider still has live leases. Confirm the cascade to revoke them as part of the delete.';
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `The provider could not be deleted (server error ${String(error.status)}).`;
    }
  }
  return 'The provider could not be deleted.';
}

/**
 * leaseMintRefusalText names a mint refusal BEFORE the request was issued (a
 * dismissed passkey, a 403 for a missing disclosure capability). Once the mint
 * request has left, use `leaseMintFailureText` instead: a lost response may have
 * left a live role whose password is gone forever.
 */
export function leaseMintRefusalText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return withDetail('The server refused that mint request as malformed.', error);
      case 403:
        return 'The server refused this mint. A human mint needs a disclosure capability over this environment and a fresh reauthentication; a machine mint needs the project machine-reveal opt-in.';
      case 404:
        return 'That provider or environment is no longer here.';
      case 409:
        return withDetail(
          'The mint could not be completed: the provider refused it, or is not in a state that can mint. No credential was issued.',
          error,
        );
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `The mint could not be completed (server error ${String(error.status)}).`;
    }
  }
  if (error instanceof Error && error.name === 'NotAllowedError') {
    return 'The passkey prompt was dismissed or timed out. Nothing was minted.';
  }
  return 'The mint could not be completed.';
}

/**
 * leaseMintFailureText is the refusal for a mint request that WAS ISSUED. The
 * pre-commit refusals (a malformed request, authorization, a provider conflict
 * that discloses nothing, the budget) carry no ambiguity and stay the plain
 * sentence. Anything else — a dropped response, a 5xx, an unparseable body — may
 * have left a live role whose password is gone forever, so it says so.
 */
export function leaseMintFailureText(error: unknown): string {
  const base = leaseMintRefusalText(error);
  if (error instanceof ApiError && [400, 401, 403, 404, 409, 429].includes(error.status)) {
    return base;
  }
  return `${base} A lease may still have been minted: its password is not recoverable, so check the leases below and revoke anything you did not expect.`;
}

/** leaseActionRefusalText names a renew/revoke/settle refusal by verb. */
export function leaseActionRefusalText(
  verb: 'renew' | 'revoke' | 'settle',
  error: unknown,
): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 401:
        return 'The session could not be authenticated. Reload and sign in first.';
      case 403:
        return verb === 'renew'
          ? 'The server refused the renewal. Renewing re-checks read over this environment — a principal that lost it cannot renew.'
          : `The server refused to ${verb} this lease.`;
      case 404:
        return 'That lease is no longer here.';
      case 409:
        return {
          renew: 'This lease is not active, so it cannot be renewed. Reload to see its current state.',
          revoke:
            'This lease is already terminal or being revoked. Reload to see its current state.',
          settle:
            'This lease is not awaiting reconcile, so there is nothing to settle. Reload to see its current state.',
        }[verb];
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `The lease could not be ${verb === 'settle' ? 'settled' : `${verb}d`} (server error ${String(error.status)}).`;
    }
  }
  return `The lease could not be ${verb === 'settle' ? 'settled' : `${verb}d`}.`;
}
