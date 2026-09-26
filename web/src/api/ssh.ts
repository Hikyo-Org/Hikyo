import {
  createSshCaOp,
  createSshProfileOp,
  deleteSshCaOp,
  deleteSshProfileOp,
  getSshTrustedKeysOp,
  issueSshCertificateOp,
  listSshCasOp,
  listSshCertificatesOp,
  listSshProfilesOp,
  retireSshCaKeyOp,
  revokeSshCertificateOp,
  rotateSshCaOp,
  updateSshProfileOp,
} from '@hikyo/operations';
import { zSshca, zSshCertificate, zSshProfile } from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, ok, parsed, parsedPick } from './client.ts';
import { useTransport } from './transport.tsx';

/**
 * SSH user certificates (#155). Everything is environment-scoped: a CA, its
 * profiles and its certificates belong to one environment.
 *
 * Two rules shape this module. A CA private key (import, rotate) and a
 * generated user private key (issue) are plaintext secrets, so those calls are
 * plain async functions, never `useMutation`: TanStack keeps a mutation's
 * variables and result in its cache until garbage collection, and neither
 * secret may outlive the dialog that holds it. Everything else carries only
 * public keys and metadata.
 */

export type SSHCA = z.infer<typeof zSshca>;
export type SSHProfile = z.infer<typeof zSshProfile>;
export type SSHCertificate = z.infer<typeof zSshCertificate>;

/** EnvironmentRef addresses one environment; every SSH object lives in one. */
export type EnvironmentRef = {
  readonly org: string;
  readonly project: string;
  readonly environment: string;
};

const casKey = (e: EnvironmentRef) => ['ssh-cas', e.org, e.project, e.environment] as const;
const profilesKey = (e: EnvironmentRef) => ['ssh-profiles', e.org, e.project, e.environment] as const;
const certificatesKey = (e: EnvironmentRef) =>
  ['ssh-certificates', e.org, e.project, e.environment] as const;

const enabled = (e: EnvironmentRef) => e.org !== '' && e.project !== '' && e.environment !== '';

export function useSSHCAs(e: EnvironmentRef): UseQueryResult<{ items: readonly SSHCA[] }> {
  const transport = useTransport();
  return useQuery({
    queryKey: casKey(e),
    queryFn: () => parsed(listSshCasOp, { path: e, ...transport }),
    enabled: enabled(e),
  });
}

export function useSSHProfiles(e: EnvironmentRef): UseQueryResult<{ items: readonly SSHProfile[] }> {
  const transport = useTransport();
  return useQuery({
    queryKey: profilesKey(e),
    queryFn: () => parsed(listSshProfilesOp, { path: e, ...transport }),
    enabled: enabled(e),
  });
}

export function useSSHCertificates(
  e: EnvironmentRef,
): UseQueryResult<{ items: readonly SSHCertificate[] }> {
  const transport = useTransport();
  return useQuery({
    queryKey: certificatesKey(e),
    queryFn: () => parsed(listSshCertificatesOp, { path: e, ...transport }),
    enabled: enabled(e),
  });
}

/**
 * useRefreshSSH re-reads every SSH listing of one environment. The secret-
 * bearing calls below have no `onSuccess`, and a call whose response was lost
 * may still have committed, so callers refresh on both paths.
 */
export function useRefreshSSH(e: EnvironmentRef): () => void {
  const queries = useQueryClient();
  return () => {
    void queries.invalidateQueries({ queryKey: casKey(e) });
    void queries.invalidateQueries({ queryKey: profilesKey(e) });
    void queries.invalidateQueries({ queryKey: certificatesKey(e) });
  };
}

/** sshTrustedKeys reads the CA's TrustedUserCAKeys content (public keys only). */
export function sshTrustedKeys(e: EnvironmentRef, ca: string): Promise<string> {
  return parsed(getSshTrustedKeysOp, { path: { ...e, sshCA: ca } });
}

/**
 * sshKRLPath is the download link for a CA's binary Key Revocation List. A
 * same-origin link, so the browser saves the bytes exactly as served.
 */
export function sshKRLPath(e: EnvironmentRef, ca: string): string {
  return `/api/v1/orgs/${encodeURIComponent(e.org)}/projects/${encodeURIComponent(e.project)}/environments/${encodeURIComponent(e.environment)}/ssh-cas/${encodeURIComponent(ca)}/krl`;
}

// ---- CAs (secret-bearing calls are plain async) --------------------------------

export type KeyAlgorithm = 'ed25519' | 'ecdsa-p256' | 'rsa-3072';

/** createSSHCA generates a CA key, or imports `privateKey` (write-only). */
export async function createSSHCA(
  e: EnvironmentRef,
  input: { name: string; algorithm?: KeyAlgorithm; privateKey?: string },
): Promise<void> {
  await parsed(createSshCaOp, {
    path: e,
    body: {
      name: input.name,
      ...(input.algorithm === undefined ? {} : { algorithm: input.algorithm }),
      ...(input.privateKey === undefined || input.privateKey === '' ? {} : { private_key: input.privateKey }),
    },
  });
}

/** rotateSSHCA moves the CA to a new key; the old one stays trusted for the overlap. */
export async function rotateSSHCA(
  e: EnvironmentRef,
  input: { ca: string; algorithm?: KeyAlgorithm; privateKey?: string; overlapSeconds: number | null },
): Promise<void> {
  await parsed(rotateSshCaOp, {
    path: { ...e, sshCA: input.ca },
    body: {
      ...(input.algorithm === undefined ? {} : { algorithm: input.algorithm }),
      ...(input.privateKey === undefined || input.privateKey === '' ? {} : { private_key: input.privateKey }),
      overlap_seconds: input.overlapSeconds,
    },
  });
}

export function useRetireSSHCAKey(e: EnvironmentRef) {
  const refresh = useRefreshSSH(e);
  return useMutation({
    mutationFn: (input: { ca: string; key: string }) =>
      parsed(retireSshCaKeyOp, { path: { ...e, sshCA: input.ca, sshCAKey: input.key } }),
    onSettled: refresh,
  });
}

export function useDeleteSSHCA(e: EnvironmentRef) {
  const refresh = useRefreshSSH(e);
  return useMutation({
    mutationFn: (ca: string) => ok(deleteSshCaOp, { path: { ...e, sshCA: ca } }),
    onSettled: refresh,
  });
}

// ---- Profiles ---------------------------------------------------------------

export type SSHProfileInput = {
  readonly caId: string;
  readonly name: string;
  readonly principals: readonly string[];
  readonly forceCommand: string;
  readonly sourceAddresses: readonly string[];
  readonly extensions: readonly SSHProfile['extensions'][number][];
  readonly keyAlgorithms: readonly KeyAlgorithm[];
  readonly defaultTtlSeconds: number;
  readonly maxTtlSeconds: number;
  readonly enabled: boolean;
  readonly requesters: readonly string[];
};

function profileBody(input: SSHProfileInput) {
  return {
    ca_id: input.caId,
    name: input.name,
    principals: [...input.principals],
    force_command: input.forceCommand,
    source_addresses: [...input.sourceAddresses],
    extensions: [...input.extensions],
    key_algorithms: [...input.keyAlgorithms],
    default_ttl_seconds: input.defaultTtlSeconds,
    max_ttl_seconds: input.maxTtlSeconds,
    enabled: input.enabled,
    requesters: [...input.requesters],
  };
}

/** useSaveSSHProfile creates (no id) or replaces (id) a profile. No secrets. */
export function useSaveSSHProfile(e: EnvironmentRef) {
  const refresh = useRefreshSSH(e);
  return useMutation({
    mutationFn: (input: { id: string | null; profile: SSHProfileInput }) =>
      input.id === null
        ? parsed(createSshProfileOp, { path: e, body: profileBody(input.profile) })
        : parsed(updateSshProfileOp, {
            path: { ...e, sshProfile: input.id },
            body: profileBody(input.profile),
          }),
    onSettled: refresh,
  });
}

export function useDeleteSSHProfile(e: EnvironmentRef) {
  const refresh = useRefreshSSH(e);
  return useMutation({
    mutationFn: (input: { id: string; revokeIssued: boolean }) =>
      parsed(deleteSshProfileOp, {
        path: { ...e, sshProfile: input.id },
        query: { revoke_issued: input.revokeIssued },
      }),
    onSettled: refresh,
  });
}

// ---- Certificates -------------------------------------------------------------

/**
 * The fields of an issue response the dialog may keep: the certificate text
 * and, for a generated key, the display-once private key. The certificate
 * record is re-read from the listing, so a drift in an unrelated field can
 * never throw away the one value nothing can return again.
 */
export type SSHIssued = {
  readonly certificate_text: string;
  readonly public_key: string;
  readonly private_key?: string | null;
};

/**
 * issueSSHCertificate is display-once for a generated key, and like every
 * display-once call here it is deliberately NOT a `useMutation`.
 */
export async function issueSSHCertificate(
  e: EnvironmentRef,
  req: {
    readonly profileId: string;
    readonly publicKey: string;
    readonly keyAlgorithm: KeyAlgorithm;
    readonly principals: readonly string[];
    readonly ttlSeconds: number | null;
  },
): Promise<SSHIssued> {
  const generated = req.publicKey.trim() === '';
  return parsedPick(
    issueSshCertificateOp,
    {
      path: e,
      body: {
        profile_id: req.profileId,
        ...(generated ? { key_algorithm: req.keyAlgorithm } : { public_key: req.publicKey.trim() }),
        ...(req.principals.length === 0 ? {} : { principals: [...req.principals] }),
        ...(req.ttlSeconds === null ? {} : { ttl_seconds: req.ttlSeconds }),
      },
    },
    { certificate_text: true, public_key: true, private_key: true },
  );
}

export function useRevokeSSHCertificate(e: EnvironmentRef) {
  const refresh = useRefreshSSH(e);
  return useMutation({
    mutationFn: (certificate: string) =>
      parsed(revokeSshCertificateOp, { path: { ...e, sshCertificate: certificate } }),
    onSettled: refresh,
  });
}

// ---- Refusal text -------------------------------------------------------------

function withDetail(base: string, error: unknown): string {
  if (error instanceof ApiError && error.detail !== undefined && error.detail !== '') {
    return `${base} (${error.detail})`;
  }
  return base;
}

/** sshRefusalText names a refusal for one SSH act, in that act's words. */
export function sshRefusalText(
  act: 'create-ca' | 'rotate' | 'retire' | 'delete-ca' | 'save-profile' | 'delete-profile' | 'revoke',
  error: unknown,
): string {
  const nothing = {
    'create-ca': 'No CA was created.',
    rotate: 'The CA was not rotated.',
    retire: 'The key was not retired.',
    'delete-ca': 'The CA was not deleted.',
    'save-profile': 'The profile was not saved.',
    'delete-profile': 'The profile was not deleted.',
    revoke: 'The certificate was not revoked.',
  }[act];
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return withDetail(`The server refused the request as invalid. ${nothing}`, error);
      case 401:
        return 'The session could not be authenticated. Reload and sign in first.';
      case 403:
      case 404:
        return act === 'revoke'
          ? `That certificate is not here, or revoking someone else's needs manage-identities on this project. ${nothing}`
          : `That is not here, or this act needs manage-identities on this project. ${nothing}`;
      case 409:
        return withDetail(
          {
            'create-ca': 'A live CA already has that name.',
            rotate: 'The CA changed underneath this request. Reload and try again.',
            retire: 'Only a retiring key can be retired. Reload to see its current state.',
            'delete-ca': 'The CA still has profiles. Delete them first.',
            'save-profile': 'A live profile already has that name, or the CA is gone.',
            'delete-profile': 'The profile changed underneath this request. Reload and try again.',
            revoke: 'The certificate changed underneath this request. Reload and try again.',
          }[act],
          error,
        );
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `${nothing} (server error ${String(error.status)})`;
    }
  }
  return nothing;
}

/**
 * sshIssueFailureText is the refusal for an issue request. Before the request
 * left (a dismissed passkey) nothing happened; once it left, a lost response
 * may have issued a certificate whose generated key is gone, so it says so.
 */
export function sshIssueFailureText(error: unknown, issued: boolean): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return withDetail('The request is outside the profile. No certificate was issued.', error);
      case 403:
        return 'The server refused this issuance: a human requester needs a fresh passkey reauthentication over this environment. No certificate was issued.';
      case 404:
        return 'That profile is not here, or you are not on its requester list. No certificate was issued.';
      case 409:
        return withDetail('The profile is disabled or its CA changed. No certificate was issued.', error);
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        break;
    }
  } else if (error instanceof Error && error.name === 'NotAllowedError') {
    return 'The passkey prompt was dismissed or timed out. No certificate was issued.';
  }
  return issued
    ? 'The issuance may have completed, but its response was lost. If a certificate appears below that you did not receive, revoke it.'
    : 'The certificate could not be issued.';
}
