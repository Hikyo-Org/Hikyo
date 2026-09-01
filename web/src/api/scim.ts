import {
  createScimBindingOp,
  createScimMappingOp,
  deleteScimBindingOp,
  deleteScimMappingOp,
  getScimBindingOp,
  listScimBindingsOp,
  listScimCredentialsOp,
  listScimDirectoryGroupsOp,
  listScimDirectoryUsersOp,
  listScimMappingsOp,
  mintScimCredentialOp,
  revokeScimCredentialOp,
  updateScimMappingOp,
} from '@hikyo/operations';
import {
  zScimAttention,
  zScimBinding,
  zScimBindingList,
  zScimBlastWarning,
  zScimCredential,
  zScimCredentialList,
  zScimDirectoryGroup,
  zScimDirectoryGroupList,
  zScimDirectoryUser,
  zScimDirectoryUserList,
  zScimMapping,
  zScimMappingList,
  zScimMappingResult,
  zScimMintResult,
} from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import { useState } from 'react';
import type { z } from 'zod';

import { ApiError, ok, parsed, parsedPick } from './client.ts';

export type ScimBinding = z.infer<typeof zScimBinding>;
export type ScimBindingList = z.infer<typeof zScimBindingList>;
export type ScimAttention = z.infer<typeof zScimAttention>;
export type ScimMapping = z.infer<typeof zScimMapping>;
export type ScimMappingList = z.infer<typeof zScimMappingList>;
export type ScimMappingResult = z.infer<typeof zScimMappingResult>;
export type ScimBlastWarning = z.infer<typeof zScimBlastWarning>;
export type ScimCredential = z.infer<typeof zScimCredential>;
export type ScimCredentialList = z.infer<typeof zScimCredentialList>;
export type ScimMintResult = z.infer<typeof zScimMintResult>;
export type ScimDirectoryUser = z.infer<typeof zScimDirectoryUser>;
export type ScimDirectoryUserList = z.infer<typeof zScimDirectoryUserList>;
export type ScimDirectoryGroup = z.infer<typeof zScimDirectoryGroup>;
export type ScimDirectoryGroupList = z.infer<typeof zScimDirectoryGroupList>;

/**
 * The minted credential exactly as the display-once dialog holds it: the token
 * and the two facts framing it, and nothing that could pull the plaintext back
 * out of the list read. `parsedPick` narrows the mint response to these three
 * so unrelated metadata drift cannot hide the one irretrievable member.
 */
export type MintedScimCredential = Pick<ScimMintResult, 'credential' | 'token' | 'rotated'>;

// Query keys are addressed by (org, binding) so switching binding never reads
// another binding's cached mappings, credentials or directory. The bindings
// list keys on the org alone.
export const scimBindingsKey = (org: string) => ['scim-bindings', org] as const;
export const scimBindingKey = (org: string, binding: string) =>
  ['scim-binding', org, binding] as const;
export const scimMappingsKey = (org: string, binding: string) =>
  ['scim-mappings', org, binding] as const;
export const scimCredentialsKey = (org: string, binding: string) =>
  ['scim-credentials', org, binding] as const;
export const scimDirectoryUsersKey = (org: string, binding: string) =>
  ['scim-directory-users', org, binding] as const;
export const scimDirectoryGroupsKey = (org: string, binding: string) =>
  ['scim-directory-groups', org, binding] as const;

export function useScimBindings(org: string): UseQueryResult<ScimBindingList> {
  return useQuery({
    queryKey: scimBindingsKey(org),
    queryFn: () => parsed(listScimBindingsOp, { path: { org } }),
    retry: false,
  });
}

/**
 * useScimBinding reads ONE binding with its live attention states. Enabled only
 * when a binding is selected — the surface renders the list until one is.
 */
export function useScimBinding(org: string, binding: string): UseQueryResult<ScimBinding> {
  return useQuery({
    queryKey: scimBindingKey(org, binding),
    queryFn: () => parsed(getScimBindingOp, { path: { org, binding } }),
    enabled: binding !== '',
    retry: false,
  });
}

export type CreateScimBindingInput = {
  readonly providerKind: 'oidc' | 'saml';
  readonly providerSlug: string;
  readonly subjectSource: string;
};

/**
 * useCreateScimBinding creates the org's binding for one provider. The subject
 * source and provider are immutable after creation, so the form is the only
 * place they are chosen. A concurrent create resolves to one row; the loser is
 * a 409, surfaced rather than swallowed.
 */
export function useCreateScimBinding(org: string) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateScimBindingInput) =>
      parsed(createScimBindingOp, {
        path: { org },
        body: {
          provider_kind: input.providerKind,
          provider_slug: input.providerSlug,
          subject_source: input.subjectSource,
        },
      }),
    onSuccess: () => queries.invalidateQueries({ queryKey: scimBindingsKey(org) }),
  });
}

/**
 * useDeleteScimBinding runs the atomic teardown. It refreshes on SETTLE, not
 * just success: a teardown whose response never arrived may still have
 * committed, so the list must re-read to show the truth either way.
 */
export function useDeleteScimBinding(org: string) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (binding: string) => ok(deleteScimBindingOp, { path: { org, binding } }),
    onSettled: () => queries.invalidateQueries({ queryKey: scimBindingsKey(org) }),
  });
}

export function useScimMappings(org: string, binding: string): UseQueryResult<ScimMappingList> {
  return useQuery({
    queryKey: scimMappingsKey(org, binding),
    queryFn: () => parsed(listScimMappingsOp, { path: { org, binding } }),
    enabled: binding !== '',
    retry: false,
  });
}

export type ScimMappingInput = {
  readonly groupId: string;
  readonly template: string;
  readonly projectId?: string;
  readonly environmentId?: string;
};

function mappingBody(input: ScimMappingInput) {
  return {
    group_id: input.groupId,
    template: input.template,
    ...(input.projectId === undefined ? {} : { project_id: input.projectId }),
    ...(input.environmentId === undefined ? {} : { environment_id: input.environmentId }),
  };
}

// A mapping mutation may change grants for members of the named group, so it
// refreshes the mapping table AND the directory the flags live on.
function invalidateMappingScope(
  queries: ReturnType<typeof useQueryClient>,
  org: string,
  binding: string,
) {
  void queries.invalidateQueries({ queryKey: scimMappingsKey(org, binding) });
  void queries.invalidateQueries({ queryKey: scimDirectoryUsersKey(org, binding) });
}

/**
 * useCreateScimMapping adds a row and grants the group's current members in the
 * same transaction. The server-authored consequence language rides the RESULT
 * (`warnings`), never a client guess, so the caller renders `warnings` verbatim
 * after the commit returns.
 */
export function useCreateScimMapping(org: string, binding: string) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: ScimMappingInput): Promise<ScimMappingResult> =>
      parsed(createScimMappingOp, { path: { org, binding }, body: mappingBody(input) }),
    onSettled: () => invalidateMappingScope(queries, org, binding),
  });
}

/**
 * useUpdateScimMapping retargets an existing row's template. The row keeps its
 * id, so widening creates the newly covered origins and narrowing releases the
 * rest in one transaction — the result reports both.
 */
export function useUpdateScimMapping(org: string, binding: string) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: ScimMappingInput): Promise<ScimMappingResult> =>
      parsed(updateScimMappingOp, { path: { org, binding }, body: mappingBody(input) }),
    onSettled: () => invalidateMappingScope(queries, org, binding),
  });
}

export type DeleteScimMappingInput = {
  readonly group: string;
  readonly project?: string;
  readonly environment?: string;
};

/**
 * useDeleteScimMapping releases every origin the addressed row held. The row is
 * addressed by its (group, scope) triple as query parameters, and the 200 body
 * reports the origin count released, so this is a body operation, not a bodyless
 * one.
 */
export function useDeleteScimMapping(org: string, binding: string) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: DeleteScimMappingInput): Promise<ScimMappingResult> =>
      parsed(deleteScimMappingOp, {
        path: { org, binding },
        query: {
          group: input.group,
          ...(input.project === undefined ? {} : { project: input.project }),
          ...(input.environment === undefined ? {} : { environment: input.environment }),
        },
      }),
    onSettled: () => invalidateMappingScope(queries, org, binding),
  });
}

export function useScimCredentials(
  org: string,
  binding: string,
): UseQueryResult<ScimCredentialList> {
  return useQuery({
    queryKey: scimCredentialsKey(org, binding),
    queryFn: () => parsed(listScimCredentialsOp, { path: { org, binding } }),
    enabled: binding !== '',
    retry: false,
  });
}

export type MintScimCredentialInput = {
  /** A fresh reauthentication proof: a TOTP code, or the password if unfactored. */
  readonly proof: string;
  readonly indefinite?: boolean;
};

/**
 * useMintScimCredential returns the display-once token WITHOUT a TanStack
 * mutation on purpose: a mutation caches its `data`, and this token exists in
 * the mint response and nowhere else, ever. So it flows straight back to the
 * caller and touches no query or mutation cache — the same discipline the
 * connection-credential mint follows (#498).
 */
export function useMintScimCredential(org: string, binding: string) {
  const queries = useQueryClient();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const mint = async (input: MintScimCredentialInput): Promise<MintedScimCredential> => {
    setPending(true);
    setError(null);
    try {
      const minted = await parsedPick(
        mintScimCredentialOp,
        {
          path: { org, binding },
          body: {
            proof: input.proof,
            ...(input.indefinite === undefined ? {} : { indefinite: input.indefinite }),
          },
        },
        { credential: true, token: true, rotated: true },
      );
      // Disclose FIRST, refresh the inventory second and un-awaited: the token
      // is irretrievable, so it must reach the guarded dialog the instant the
      // POST returns, never after a list refetch the operator could interrupt.
      void queries.invalidateQueries({ queryKey: scimCredentialsKey(org, binding) });
      return minted;
    } catch (caught) {
      // A mint whose response never arrived may still have committed, so the
      // inventory is refreshed on the failure path too: a lost-but-live
      // credential must at least become visible enough to revoke.
      void queries.invalidateQueries({ queryKey: scimCredentialsKey(org, binding) });
      setError(caught);
      throw caught;
    } finally {
      setPending(false);
    }
  };
  return { mint, pending, error };
}

/**
 * useRevokeScimCredential marks the credential dead; it bites at the next wire
 * request. A double revoke is a 409 — refreshing on SETTLE flips the row to
 * revoked in exactly that case rather than leaving one that reads live and
 * cannot be revoked again.
 */
export function useRevokeScimCredential(org: string, binding: string) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => ok(revokeScimCredentialOp, { path: { org, binding, id } }),
    onSettled: () => queries.invalidateQueries({ queryKey: scimCredentialsKey(org, binding) }),
  });
}

export function useScimDirectoryUsers(
  org: string,
  binding: string,
): UseQueryResult<ScimDirectoryUserList> {
  return useQuery({
    queryKey: scimDirectoryUsersKey(org, binding),
    queryFn: () => parsed(listScimDirectoryUsersOp, { path: { org, binding } }),
    enabled: binding !== '',
    retry: false,
  });
}

export function useScimDirectoryGroups(
  org: string,
  binding: string,
): UseQueryResult<ScimDirectoryGroupList> {
  return useQuery({
    queryKey: scimDirectoryGroupsKey(org, binding),
    queryFn: () => parsed(listScimDirectoryGroupsOp, { path: { org, binding } }),
    enabled: binding !== '',
    retry: false,
  });
}

// --- refusals ---------------------------------------------------------------
//
// Every SCIM administration route is `manage-members@org` and MFA-mandatory, so
// on this surface a 403 IS the second-factor refusal (the same pin members and
// grants rely on) and a 404 is the uniform "not available OR does not exist"
// answer, never an oracle for which the two it is.

function commonScimFailureText(error: ApiError): string | null {
  switch (error.status) {
    case 401:
      return 'Your session ended. Sign in again to continue.';
    case 403:
      return 'Administering SCIM needs a second factor. Sign in again and present your passkey or a code, then retry.';
    case 404:
      return 'This is not available to you, or it does not exist. The two are deliberately the same answer.';
    case 429:
      return 'Too many attempts right now. Wait a moment and try again.';
    default:
      return null;
  }
}

/** scimReadFailureText names a failed read without inventing a cause. */
export function scimReadFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    const common = commonScimFailureText(error);
    if (common !== null) {
      return common;
    }
    return `The server failed while reading (${error.status}). Reload to try again.`;
  }
  return 'The SCIM surface could not be reached, or it answered something this client does not understand. Reload to try again.';
}

/** scimMutationFailureText names a failed create/update/delete. */
export function scimMutationFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    const common = commonScimFailureText(error);
    if (common !== null) {
      return common;
    }
    switch (error.status) {
      case 400:
        return error.detail ?? 'That request was refused: it did not meet the contract for this operation.';
      case 409:
        return (
          error.detail ??
          'Refused: this conflicts with the current state — reload to see what changed, then retry.'
        );
      default:
        return `The server failed (${error.status}); whether the change applied is unknown — reload to check.`;
    }
  }
  return 'The SCIM surface could not be reached, or it answered something this client does not understand. Whether the change applied is unknown — reload to check.';
}

/**
 * scimMintFailureText names a failed mint. A 400 here is the proof or the
 * indefinite opt-in refused by name: the token was never issued, so the message
 * says exactly that rather than the mutation family's "unknown whether applied".
 */
export function scimMintFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    const common = commonScimFailureText(error);
    if (common !== null) {
      return common;
    }
    switch (error.status) {
      case 400:
        return (
          error.detail ??
          'The mint was refused: the reauthentication proof was not accepted, or an indefinite credential is not allowed on this instance. No credential was issued.'
        );
      case 409:
        return error.detail ?? 'Refused: reload to see the binding’s current credentials, then retry.';
      default:
        return `The mint failed (${error.status}). No credential was issued; try again shortly.`;
    }
  }
  return 'The mint could not be completed: the server could not be reached, or it answered something this client does not understand. No credential was issued.';
}
