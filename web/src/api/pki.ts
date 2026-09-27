import {
  bindPkiProfileOp,
  createPkiIssuerOp,
  createPkiProfileOp,
  deletePkiProfileOp,
  getCertificateCrlOp,
  getPkiIssuerCrlOp,
  installPkiIssuerCertificateOp,
  issueCertificateOp,
  listCertificateProfilesOp,
  listCertificatesOp,
  listPkiIssuersOp,
  listPkiProfilesOp,
  publishPkiIssuerCrlOp,
  releasePkiIssuerHoldOp,
  renewCertificateOp,
  retirePkiIssuerOp,
  revokeCertificateOp,
  revokePkiIssuerOp,
  rotatePkiIssuerOp,
  unbindPkiProfileOp,
  updatePkiProfileOp,
} from '@hikyo/operations';
import { zCertificate, zCertificateProfile, zPkiIssuer, zPkiPolicy, zPkiProfile } from '@hikyo/zod';
import type { PkiPolicy as PkiPolicyBody } from '@hikyo/client';
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, ok, parsed, parsedPick } from './client.ts';
import { type TransportOptions, useTransport } from './transport.tsx';

// Private PKI (#154, docs/adr/pki.md). Issuers and profiles are instance
// administration (`instance-config`); certificates are environment-scoped
// (`issue-certificate` to write, `read` to inspect). No response in this
// module carries a CA private key: the server has no route that returns one.
// The single private key the SPA can ever receive is a server-generated leaf
// key, disclosed once by `issueGeneratedCertificate` below.

export type PkiIssuer = z.infer<typeof zPkiIssuer>;
export type PkiProfile = z.infer<typeof zPkiProfile>;
/** A policy as the server returned it (int64 fields parse to bigint). */
export type PkiPolicy = z.infer<typeof zPkiPolicy>;
/** A policy as a request body carries it. */
export type { PkiPolicyBody };
export type Certificate = z.infer<typeof zCertificate>;
export type CertificateProfile = z.infer<typeof zCertificateProfile>;

const issuersKey = ['instance-pki-issuers'] as const;
const profilesKey = ['instance-pki-profiles'] as const;

// ---- Issuers --------------------------------------------------------------

/** usePkiIssuers lists every CA key version, public material only. */
export function usePkiIssuers() {
  return useQuery({
    queryKey: issuersKey,
    queryFn: () => parsed(listPkiIssuersOp, {}),
  });
}

/** Wraps an issuer mutation and invalidates the issuer listing on success. */
function useIssuerMutation<Input, Result>(fn: (input: Input) => Promise<Result>) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: issuersKey });
    },
  });
}

/** The issuer kinds the browser creates. Importing an existing CA key is a
 * CLI act (`hikyo pki issuer import`): the key is read from a file or stdin
 * and never pasted into a page. */
export type IssuerDraft = {
  readonly mode: 'root' | 'intermediate';
  readonly name: string;
  readonly commonName: string;
  readonly organization: string;
  readonly keyAlgorithm: 'ecdsa-p256' | 'ecdsa-p384' | 'rsa-3072' | 'rsa-4096';
  readonly ttlDays: number;
  /** An intermediate with a parent is signed now; without one it is created
   * pending, with a CSR for an offline root to sign. */
  readonly parent: string;
  readonly crlUrl: string;
};

/** Creates an issuer from a trimmed draft, rounding its lifetime from days to
 * whole seconds, then invalidates the issuer listing on success. */
export function useCreatePkiIssuer() {
  return useIssuerMutation((draft: IssuerDraft) =>
    parsed(createPkiIssuerOp, {
      body: {
        mode: draft.mode,
        name: draft.name.trim(),
        common_name: draft.commonName.trim(),
        ...(draft.organization.trim() === '' ? {} : { organization: draft.organization.trim() }),
        key_algorithm: draft.keyAlgorithm,
        ttl_seconds: Math.round(draft.ttlDays * 86400),
        ...(draft.mode === 'intermediate' && draft.parent !== '' ? { parent: draft.parent } : {}),
        ...(draft.crlUrl.trim() === '' ? {} : { crl_distribution_url: draft.crlUrl.trim() }),
      },
    }),
  );
}

/** Rotates using the server's stored issuer settings and refreshes the issuer
 * listing on success. Imported issuers still require material through the CLI. */
export function useRotatePkiIssuer() {
  return useIssuerMutation((issuer: string) => parsed(rotatePkiIssuerOp, { path: { issuer }, body: {} }));
}

/** Installs a pending issuer's certificate and chain, invalidating the issuer
 * listing on success. */
export function useInstallPkiIssuerCertificate() {
  return useIssuerMutation((input: { issuer: string; certificatePem: string; chainPem: string }) =>
    parsed(installPkiIssuerCertificateOp, {
      path: { issuer: input.issuer },
      body: { certificate_pem: input.certificatePem, chain_pem: input.chainPem },
    }),
  );
}

/** Retires the selected key version and invalidates the issuer listing on
 * success. The server refuses retirement while live certificates remain. */
export function useRetirePkiIssuer() {
  return useIssuerMutation((input: { issuer: string; version: number }) =>
    parsed(retirePkiIssuerOp, { path: { issuer: input.issuer, version: input.version } }),
  );
}

/** Revokes the selected issuer version and its live certificates, then
 * invalidates the issuer listing on success. */
export function useRevokePkiIssuer() {
  return useIssuerMutation((input: { issuer: string; version: number }) =>
    parsed(revokePkiIssuerOp, { path: { issuer: input.issuer, version: input.version } }),
  );
}

/** Releases restore holds for all versions of a named issuer and invalidates
 * the issuer listing on success. */
export function useReleasePkiIssuerHold() {
  return useIssuerMutation((issuer: string) => parsed(releasePkiIssuerHoldOp, { path: { issuer } }));
}

/** Publishes a fresh CRL for one issuer version and invalidates the issuer
 * listing on success. */
export function usePublishPkiIssuerCrl() {
  return useIssuerMutation((input: { issuer: string; version: number }) =>
    parsed(publishPkiIssuerCrlOp, { path: { issuer: input.issuer, version: input.version } }),
  );
}

/** fetchIssuerCrl reads a version's stored CRL (public material). */
export async function fetchIssuerCrl(issuer: string, version: number): Promise<string> {
  const crl = await parsed(getPkiIssuerCrlOp, { path: { issuer, version } });
  return crl.crl_pem;
}

// ---- Profiles ---------------------------------------------------------------

/** Queries instance certificate profiles, including their policies and bindings. */
export function usePkiProfiles() {
  return useQuery({
    queryKey: profilesKey,
    queryFn: () => parsed(listPkiProfilesOp, {}),
  });
}

/** Wraps a profile mutation and invalidates the profile listing on success. */
function useProfileMutation<Input, Result>(fn: (input: Input) => Promise<Result>) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: profilesKey });
    },
  });
}

/** parsePolicy validates a pasted policy document against the contract's own
 * schema, so a typo is refused in the page before any request is sent. Returns
 * a request body with durations in seconds, or a validation message for invalid
 * JSON or schema failures; semantic policy validation remains on the server. */
export function parsePolicy(text: string): PkiPolicyBody | string {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    return 'The policy is not valid JSON.';
  }
  const result = zPkiPolicy.strict().safeParse(value);
  if (!result.success) {
    const first = result.error.issues[0];
    return `The policy does not match the profile shape${first === undefined ? '' : ` at ${first.path.join('.') || 'the top level'}: ${first.message}`}.`;
  }
  const policy = result.data;
  return {
    ...policy,
    max_ttl_seconds: Number(policy.max_ttl_seconds),
    default_ttl_seconds: Number(policy.default_ttl_seconds),
    renew_window_seconds: Number(policy.renew_window_seconds),
  };
}

/** policyText renders a returned policy as the editable JSON document, the
 * same shape `parsePolicy` reads back. */
export function policyText(policy: PkiPolicy): string {
  return JSON.stringify(policy, (_key, value: unknown) => (typeof value === 'bigint' ? Number(value) : value), 2);
}

/** Creates a profile with a trimmed name and invalidates the profile listing
 * on success. The server validates the supplied policy. */
export function useCreatePkiProfile() {
  return useProfileMutation((input: { name: string; policy: PkiPolicyBody }) =>
    parsed(createPkiProfileOp, { body: { name: input.name.trim(), policy: input.policy } }),
  );
}

/** useUpdatePkiProfile replaces a policy. The server accepts only a provable
 * narrowing; a widening, or a change it cannot prove narrows, is a 409. */
export function useUpdatePkiProfile() {
  return useProfileMutation((input: { name: string; policy: PkiPolicyBody; rowVersion: number }) =>
    parsed(updatePkiProfileOp, {
      path: { profile: input.name },
      body: { policy: input.policy, row_version: input.rowVersion },
    }),
  );
}

/** Deletes a profile and its bindings, invalidating the profile listing on success. */
export function useDeletePkiProfile() {
  return useProfileMutation((name: string) => ok(deletePkiProfileOp, { path: { profile: name } }));
}

/** Binds a profile to a project or one environment and invalidates the profile
 * listing on success. An empty environment selects the whole project. */
export function useBindPkiProfile() {
  return useProfileMutation((input: { name: string; org: string; project: string; environment: string }) =>
    parsed(bindPkiProfileOp, {
      path: { profile: input.name },
      body: {
        org_id: input.org.trim(),
        project_id: input.project.trim(),
        ...(input.environment.trim() === '' ? {} : { environment_id: input.environment.trim() }),
      },
    }),
  );
}

/** Removes a profile binding and invalidates the profile listing on success. */
export function useUnbindPkiProfile() {
  return useProfileMutation((input: { name: string; binding: string }) =>
    parsed(unbindPkiProfileOp, { path: { profile: input.name, binding: input.binding } }),
  );
}

// ---- Certificates -------------------------------------------------------------

type ProjectRef = { readonly org: string; readonly project: string };
type EnvRef = { readonly id: string; readonly name: string };

const certificatesKey = (org: string, project: string, env: string) =>
  ['pki-certificates', org, project, env] as const;
const certificateProfilesKey = (org: string, project: string, env: string) =>
  ['pki-certificate-profiles', org, project, env] as const;

export type CertificateRow = { readonly environmentId: string; readonly environmentName: string; readonly certificate: Certificate };

/**
 * useCertificates lists certificates across a project's environments, the
 * `useLeases` fan-out, with up to 500 records per environment. Pending or
 * failed queries contribute no rows unless cached data is available; the
 * combined flags indicate that the listing may be incomplete. Metadata and
 * public certificates only: a private key is never stored, so it is never listed.
 */
export function useCertificates(p: ProjectRef, environments: readonly EnvRef[]) {
  const transport = useTransport();
  return useQueries({
    queries: environments.map((env) => ({
      queryKey: certificatesKey(p.org, p.project, env.id),
      queryFn: () =>
        parsed(listCertificatesOp, {
          path: { org: p.org, project: p.project, environment: env.id },
          ...transport,
        }),
    })),
    combine: (results) => ({
      rows: environments.flatMap((env, index) =>
        (results[index]?.data?.certificates ?? []).map(
          (certificate): CertificateRow => ({ environmentId: env.id, environmentName: env.name, certificate }),
        ),
      ),
      isPending: results.some((r) => r.isPending),
      isError: results.some((r) => r.isError),
    }),
  });
}

/** useCertificateProfiles lists the profiles bound to one environment.
 * An empty environment disables the query. */
export function useCertificateProfiles(p: ProjectRef, environment: string) {
  const transport = useTransport();
  return useQuery({
    queryKey: certificateProfilesKey(p.org, p.project, environment),
    queryFn: () =>
      parsed(listCertificateProfilesOp, {
        path: { org: p.org, project: p.project, environment },
        ...transport,
      }),
    enabled: environment !== '',
  });
}

/** useRefreshCertificates returns a callback that invalidates one environment's
 * listing. Issuance callers invoke it on both success and failure because a
 * request whose response was lost may still have committed a certificate. */
export function useRefreshCertificates(p: ProjectRef): (environment: string) => void {
  const queries = useQueryClient();
  return (environment: string) => {
    void queries.invalidateQueries({ queryKey: certificatesKey(p.org, p.project, environment) });
  };
}

export type IssueDraft = {
  readonly profile: string;
  readonly commonName: string;
  readonly dnsNames: readonly string[];
  readonly ipAddresses: readonly string[];
  readonly uris: readonly string[];
  readonly ttlHours: number | null;
};

/** Builds the shared issuance fields, omitting empty names and lists. A null
 * lifetime selects the profile default; hours are rounded to whole seconds. */
function issueBody(draft: IssueDraft) {
  return {
    profile: draft.profile,
    ...(draft.commonName.trim() === '' ? {} : { common_name: draft.commonName.trim() }),
    ...(draft.dnsNames.length === 0 ? {} : { dns_names: [...draft.dnsNames] }),
    ...(draft.ipAddresses.length === 0 ? {} : { ip_addresses: [...draft.ipAddresses] }),
    ...(draft.uris.length === 0 ? {} : { uris: [...draft.uris] }),
    ...(draft.ttlHours === null ? {} : { ttl_seconds: Math.round(draft.ttlHours * 3600) }),
  };
}

/** useIssueCsrCertificate issues from a pasted CSR. Neither the CSR nor the
 * certificate is secret, so an ordinary mutation is fine. */
export function useIssueCsrCertificate(p: ProjectRef) {
  const refresh = useRefreshCertificates(p);
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { environment: string; csrPem: string; draft: IssueDraft }) =>
      parsed(issueCertificateOp, {
        path: { org: p.org, project: p.project, environment: input.environment },
        body: { ...issueBody(input.draft), csr_pem: input.csrPem },
        ...transport,
      }),
    onSettled: (_result, _error, input) => refresh(input.environment),
  });
}

/** The display-once projection of a generated-key issuance. */
export type GeneratedCertificate = { readonly certificate: Certificate; readonly privateKeyPem: string };

/**
 * issueGeneratedCertificate issues with a server-generated key. Deliberately
 * NOT a `useMutation`, for the same reason as `mintLease`: TanStack keeps a
 * mutation's result cached until garbage collection, and the private key's
 * whole contract is that it lives in exactly one place, the dialog that shows
 * it once. The caller must run the mint reauthentication ceremony first, and
 * passes its `useTransport()` so a workspace issues on the remote it shows.
 * Request and response-validation errors reject the promise; a response without
 * a private key also throws, even though the certificate may already exist.
 */
export async function issueGeneratedCertificate(
  p: ProjectRef & { readonly environment: string },
  draft: IssueDraft,
  keyAlgorithm: 'ecdsa-p256' | 'ecdsa-p384' | 'ed25519' | 'rsa-2048' | 'rsa-3072' | 'rsa-4096',
  transport: TransportOptions,
): Promise<GeneratedCertificate> {
  const result = await parsedPick(
    issueCertificateOp,
    {
      path: { org: p.org, project: p.project, environment: p.environment },
      body: { ...issueBody(draft), generate_key: true, key_algorithm: keyAlgorithm },
      ...transport,
    },
    { certificate: true, private_key_pem: true },
  );
  if (result.private_key_pem === undefined) {
    throw new Error('the server issued a certificate but returned no generated key');
  }
  return { certificate: result.certificate, privateKeyPem: result.private_key_pem };
}

/** Renews with the existing public key and invalidates the environment's
 * certificate listing on success. */
export function useRenewCertificate(p: ProjectRef) {
  const refresh = useRefreshCertificates(p);
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { environment: string; certificate: string }) =>
      parsed(renewCertificateOp, {
        path: { org: p.org, project: p.project, environment: input.environment, certificate: input.certificate },
        ...transport,
      }),
    onSuccess: (_result, input) => refresh(input.environment),
  });
}

export type RevocationReason =
  | 'unspecified'
  | 'key-compromise'
  | 'affiliation-changed'
  | 'superseded'
  | 'cessation-of-operation'
  | 'privilege-withdrawn';

/** Revokes with the selected reason and invalidates the environment's
 * certificate listing on success. */
export function useRevokeCertificate(p: ProjectRef) {
  const refresh = useRefreshCertificates(p);
  const transport = useTransport();
  return useMutation({
    mutationFn: (input: { environment: string; certificate: string; reason: RevocationReason }) =>
      parsed(revokeCertificateOp, {
        path: { org: p.org, project: p.project, environment: input.environment, certificate: input.certificate },
        body: { reason: input.reason },
        ...transport,
      }),
    onSuccess: (_result, input) => refresh(input.environment),
  });
}

/** fetchCertificateCrl reads the CRL of the certificate's issuer version. */
export async function fetchCertificateCrl(
  p: ProjectRef & { readonly environment: string },
  certificate: string,
  transport: TransportOptions,
): Promise<string> {
  const crl = await parsed(getCertificateCrlOp, {
    path: { org: p.org, project: p.project, environment: p.environment, certificate },
    ...transport,
  });
  return crl.crl_pem;
}

// ---- Refusal text ---------------------------------------------------------------

function withDetail(base: string, error: unknown): string {
  if (error instanceof ApiError && error.detail !== undefined && error.detail !== '') {
    return `${base} (${error.detail})`;
  }
  return base;
}

/** pkiAdminRefusalText names an issuer or profile refusal. */
export function pkiAdminRefusalText(error: unknown, action: string): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return withDetail(`The server refused to ${action}: the request was not valid.`, error);
      case 401:
        return 'The session could not be authenticated. Reload and sign in first.';
      case 403:
        return `To ${action} you need instance-config and a second factor. Present your authenticator in the banner above.`;
      case 404:
        return withDetail(`Could not ${action}: it is no longer here.`, error);
      case 409:
        return withDetail(`The server refused to ${action} as a conflict.`, error);
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `Could not ${action} (server error ${String(error.status)}).`;
    }
  }
  return `Could not ${action}.`;
}

/** certificateRefusalText formats an API or unknown error for a certificate
 * action. It does not determine whether issuance committed; use issueFailureText
 * for failures after an issuance request was sent. */
export function certificateRefusalText(error: unknown, action: string): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return withDetail(`The profile refused to ${action}.`, error);
      case 401:
        return 'The session could not be authenticated. Reload and sign in first.';
      case 403:
        return withDetail(`To ${action} you need issue-certificate on this environment.`, error);
      case 404:
        return `Could not ${action}: the profile is not bound here, or you may not use it.`;
      case 409:
        return withDetail(`The server refused to ${action} as a conflict.`, error);
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `Could not ${action} (server error ${String(error.status)}).`;
    }
  }
  return `Could not ${action}.`;
}

/** issueFailureText is for a failure AFTER the issue request left: the server
 * may have committed a certificate anyway. With a generated key that key is
 * now gone; with a CSR the caller still holds it, but the certificate is still
 * one they did not receive. */
export function issueFailureText(error: unknown, method: 'generated' | 'csr'): string {
  const detail =
    method === 'generated'
      ? 'A certificate may still have been issued without its key reaching you'
      : 'A certificate may still have been issued without reaching you';
  return `${certificateRefusalText(error, 'issue the certificate')} ${detail}: revoke any certificate below you did not expect.`;
}
