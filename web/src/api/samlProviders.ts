import {
  compromiseRetireSamlSpKeyOp,
  deleteSamlProviderOp,
  listSamlProvidersOp,
  listSamlSpKeysOp,
  patchSamlProviderOp,
  putSamlProviderOp,
  refreshSamlProviderMetadataOp,
  retireSamlSpKeyOp,
  rotateSamlSpKeyOp,
} from '@hikyo/operations';
import type { SamlMetadataSource, SamlProviderPatch } from '@hikyo/client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { useSensitiveMutation } from './sensitiveMutation.ts';
import { ApiError, ok, parsed } from './client.ts';

/**
 * The SAML administrative surface (#500): provider inventory and lifecycle plus
 * SP signing-key rotation and retirement, riding the locked instance-config API
 * exactly as it is. No route is added, these are panels on `instance-admin`,
 * so there is no second place a provider is administered.
 *
 * The write-only rule the contract already enforces is preserved here: metadata
 * XML is only ever an INPUT. `SamlProvider` never returns the document, so it is
 * never rendered back, and no mutation here echoes it into feedback.
 */

/** Matches `ProviderSlugPath`: lowercase, digit or hyphen, no leading hyphen, ≤64. */
export const SAML_SLUG_PATTERN = /^[a-z0-9][a-z0-9-]{0,63}$/;

const samlProvidersKey = ['instance-saml-providers'] as const;
const samlSpKeysKey = ['instance-saml-sp-keys'] as const;

export function useSamlProviders() {
  return useQuery({
    queryKey: samlProvidersKey,
    queryFn: () => parsed(listSamlProvidersOp, {}),
  });
}

export function useSamlSpKeys() {
  return useQuery({
    queryKey: samlSpKeysKey,
    queryFn: () => parsed(listSamlSpKeysOp, {}),
  });
}

/**
 * The create/reconfigure body. `metadata_document` and `metadata_url` are
 * mutually exclusive by source, enforced client-side before the request so a
 * file-backed provider never carries an unused URL and vice versa. The
 * `confirmed_*` arrays carry the metadata trust-diff acknowledgement on a rerun.
 */
export type SamlProviderInputDraft = {
  readonly slug: string;
  readonly displayName: string;
  readonly entityId: string;
  readonly metadataSource: SamlMetadataSource;
  readonly metadataDocument: string;
  readonly metadataUrl: string;
  readonly assurancePolicy: readonly string[] | null;
  readonly allowEmailNameid: boolean;
  readonly forceSignRequests: boolean;
  readonly enabled: boolean;
  readonly confirmedFingerprints?: readonly string[];
  readonly confirmedEndpoints?: readonly string[];
};

export function usePutSamlProvider() {
  const queryClient = useQueryClient();
  return useSensitiveMutation({
    // The write-only document is owned by the current editor, never a cache.
    mutationFn: (draft: SamlProviderInputDraft) =>
      parsed(putSamlProviderOp, {
        path: { slug: draft.slug },
        body: {
          display_name: draft.displayName.trim(),
          entity_id: draft.entityId.trim(),
          metadata_source: draft.metadataSource,
          metadata_document: draft.metadataSource === 'file' ? draft.metadataDocument : null,
          metadata_url: draft.metadataSource === 'url' ? draft.metadataUrl.trim() : null,
          assurance_policy: draft.assurancePolicy === null ? null : [...draft.assurancePolicy],
          allow_email_nameid: draft.allowEmailNameid,
          force_sign_requests: draft.forceSignRequests,
          enabled: draft.enabled,
          ...(draft.confirmedFingerprints ? { confirmed_fingerprints: [...draft.confirmedFingerprints] } : {}),
          ...(draft.confirmedEndpoints ? { confirmed_endpoints: [...draft.confirmedEndpoints] } : {}),
        },
      }),
    onSuccess: (result) => {
      if (result.applied) void queryClient.invalidateQueries({ queryKey: samlProvidersKey });
    },
  });
}

export function usePatchSamlProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ slug, patch }: { slug: string; patch: SamlProviderPatch }) =>
      parsed(patchSamlProviderOp, { path: { slug }, body: patch }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: samlProvidersKey }),
  });
}

export type SamlMetadataRefreshDraft = {
  readonly slug: string;
  readonly metadataDocument: string | null;
  readonly confirmedFingerprints?: readonly string[];
  readonly confirmedEndpoints?: readonly string[];
};

export function useRefreshSamlMetadata() {
  const queryClient = useQueryClient();
  return useSensitiveMutation({
    // See usePutSamlProvider: keep the replacement document out of the cache.
    mutationFn: (draft: SamlMetadataRefreshDraft) =>
      parsed(refreshSamlProviderMetadataOp, {
        path: { slug: draft.slug },
        body: {
          ...(draft.metadataDocument === null ? {} : { metadata_document: draft.metadataDocument }),
          ...(draft.confirmedFingerprints ? { confirmed_fingerprints: [...draft.confirmedFingerprints] } : {}),
          ...(draft.confirmedEndpoints ? { confirmed_endpoints: [...draft.confirmedEndpoints] } : {}),
        },
      }),
    onSuccess: (result) => {
      if (result.applied) void queryClient.invalidateQueries({ queryKey: samlProvidersKey });
    },
  });
}

export function useDeleteSamlProvider() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (slug: string) => ok(deleteSamlProviderOp, { path: { slug } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: samlProvidersKey }),
  });
}

export function useRotateSamlSpKey() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => parsed(rotateSamlSpKeyOp, {}),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: samlSpKeysKey }),
  });
}

export function useRetireSamlSpKey() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (fingerprint: string) => ok(retireSamlSpKeyOp, { path: { fingerprint } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: samlSpKeysKey }),
  });
}

export function useCompromiseRetireSamlSpKey() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (fingerprint: string) =>
      parsed(compromiseRetireSamlSpKeyOp, { path: { fingerprint } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: samlSpKeysKey }),
  });
}

/**
 * Client-side validation, because the server returns only a coarse code for
 * everything but a 400 detail: naming the wrong field to the operator has to
 * happen here. Deliberately no stricter than the contract, the slug pattern is
 * the exact `ProviderSlugPath` one, trailing hyphen and all.
 */
export function samlProviderInputErrors(draft: {
  readonly creating: boolean;
  readonly slug: string;
  readonly displayName: string;
  readonly entityId: string;
  readonly metadataSource: SamlMetadataSource;
  readonly metadataDocument: string;
  readonly metadataUrl: string;
}): string[] {
  const errors: string[] = [];
  // The slug is chosen at create and is fixed route data thereafter, so it is
  // only validated while creating.
  if (draft.creating && !SAML_SLUG_PATTERN.test(draft.slug)) {
    errors.push('Slug must be lowercase letters, digits or hyphens, and start with a letter or digit.');
  }
  if (draft.displayName.trim() === '') errors.push('A display name is required.');
  if (draft.entityId.trim() === '') errors.push('An entity ID is required.');
  if (draft.metadataSource === 'file') {
    if (draft.metadataDocument.trim() === '') {
      errors.push('Paste the IdP metadata XML for a file-backed provider.');
    }
  } else if (draft.metadataUrl.trim() === '') {
    errors.push('A metadata URL is required for a URL-backed provider.');
  } else if (!/^https:\/\//i.test(draft.metadataUrl.trim())) {
    errors.push('The metadata URL must be an https:// address.');
  }
  return errors;
}

export type SamlAction =
  | 'list'
  | 'save-provider'
  | 'update-provider'
  | 'refresh-metadata'
  | 'delete-provider'
  | 'rotate-key'
  | 'retire-key'
  | 'compromise-retire-key';

/** Map each refusal to a sentence, keyed to the action that hit it. */
export function samlFailureText(error: unknown, action: SamlAction): string {
  if (!(error instanceof ApiError)) {
    return 'The server failed; whether the change applied is unknown: reload to check.';
  }
  switch (error.status) {
    case 400:
      return error.detail ?? invalidText(action);
    case 401:
      return 'Your session ended. Sign in again to continue.';
    case 403:
      // A 403 on an instance-config operation is uniform: either the session's
      // assurance is inadequate for this MFA-mandatory operation, or the
      // principal does not hold the capability. The contract does not
      // distinguish them, so the copy must not claim it is only step-up.
      return `${samlAction(action)} needs a second factor and this authority. If you hold it, present your authenticator code or passkey in the banner above, then try again.`;
    case 404:
      return 'This is not disclosed to this session.';
    case 409:
      // Only a 400 carries a caller-safe detail; a 409 never does, so never
      // render one.
      return conflictText(action);
    case 429:
      return 'Too many attempts right now. Wait a moment and try again.';
    default:
      return 'The server failed; whether the change applied is unknown: reload to check.';
  }
}

function samlAction(action: SamlAction): string {
  switch (action) {
    case 'list':
      return 'Listing SAML providers';
    case 'save-provider':
      return 'Configuring a SAML provider';
    case 'update-provider':
      return 'Updating a SAML provider';
    case 'refresh-metadata':
      return 'Refreshing provider metadata';
    case 'delete-provider':
      return 'Removing a SAML provider';
    case 'rotate-key':
      return 'Rotating the SP signing key';
    case 'retire-key':
      return 'Retiring an SP signing key';
    case 'compromise-retire-key':
      return 'Compromise-retiring the active SP key';
  }
}

function invalidText(action: SamlAction): string {
  switch (action) {
    case 'save-provider':
    case 'update-provider':
    case 'refresh-metadata':
      return 'The provider metadata or policy was rejected. Check the entity ID, the metadata source, and that any trust changes were confirmed.';
    default:
      return 'The request was rejected.';
  }
}

function conflictText(action: SamlAction): string {
  switch (action) {
    case 'save-provider':
    case 'update-provider':
    case 'refresh-metadata':
      return 'The provider changed under you, or a trust change needs explicit confirmation. Reload and reapply.';
    case 'retire-key':
      return 'This key is the active signing key. Rotate first so request-signing continuity is explicit, then retire the key it leaves behind.';
    case 'rotate-key':
    case 'compromise-retire-key':
      return 'The signing-key set changed under you. Reload the key inventory and try again.';
    default:
      return 'The current state refuses this change. Reload and try again.';
  }
}
