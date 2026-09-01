import {
  deleteOidcProviderOp,
  listOidcProvidersOp,
  putOidcProviderOp,
} from '@hikyo/operations';
import { zOidcProvider, zOidcProviderList } from '@hikyo/zod';
import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseQueryResult,
} from '@tanstack/react-query';
import { z } from 'zod';

import { ApiError, ok, parsed } from './client.ts';

/**
 * OIDC provider administration (#499), riding the instance provider API
 * (`/api/v1/instance/oidc-providers`) exactly as it is — no contract change.
 *
 * The server returns only a coarse refusal CODE and never a per-field message
 * (errors.go: it "never writes anything derived from the cause beyond the code
 * itself"), so every field-level distinction the acceptance criteria ask for is
 * drawn HERE, before submit:
 *
 *  - the slug is validated against the contract's own pattern;
 *  - the issuer is IMMUTABLE after create (A3), so a reconfigure that changes it
 *    is refused as a field error client-side rather than sent to a 400 that
 *    could not name which field it meant;
 *  - the JIT and assurance policies are parsed as JSON objects before they ride.
 *
 * The one thing the WebUI cannot hold and must not pretend to: the client secret
 * is envelope-encrypted and NEVER returned. Every PUT re-seals whatever secret
 * it carries (service/oidc.go seals `in.ClientSecret` unconditionally on both
 * create and reconfigure), so there is no "keep the old secret" path — the
 * editor field is always blank and always required, and "replacement cannot
 * silently retain an old value" falls out of that contract.
 */

export type OidcProvider = z.infer<typeof zOidcProvider>;
export type OidcProviderList = z.infer<typeof zOidcProviderList>;

const oidcProvidersKey = ['instance-oidc-providers'] as const;

export function useOidcProviders(): UseQueryResult<OidcProviderList> {
  return useQuery({
    queryKey: oidcProvidersKey,
    queryFn: () => parsed(listOidcProvidersOp, {}),
    retry: false,
  });
}

/** The editor's own value shape, mapped to the wire body at the boundary. */
export type OidcProviderInput = {
  readonly displayName: string;
  readonly issuer: string;
  readonly clientId: string;
  readonly clientSecret: string;
  readonly scopes: string;
  readonly jitPolicy: string | null;
  readonly assurancePolicy: string | null;
  readonly enabled: boolean;
};

export function usePutOidcProvider() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (variables: { slug: string; input: OidcProviderInput }) =>
      parsed(putOidcProviderOp, {
        path: { slug: variables.slug },
        body: {
          display_name: variables.input.displayName,
          issuer: variables.input.issuer,
          client_id: variables.input.clientId,
          client_secret: variables.input.clientSecret,
          scopes: variables.input.scopes,
          jit_policy: variables.input.jitPolicy,
          assurance_policy: variables.input.assurancePolicy,
          enabled: variables.input.enabled,
        },
      }),
    onSuccess: () => queries.invalidateQueries({ queryKey: oidcProvidersKey }),
  });
}

export function useDeleteOidcProvider(onDeleted?: () => void) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (variables: { slug: string }) =>
      ok(deleteOidcProviderOp, { path: { slug: variables.slug } }),
    // The list invalidation unmounts the deleted row. Run the durable parent
    // feedback first; a per-call callback is not guaranteed to survive it.
    onSuccess: async () => {
      onDeleted?.();
      await queries.invalidateQueries({ queryKey: oidcProvidersKey });
    },
  });
}

// --- client-side validation -------------------------------------------------

export type FieldValidation =
  | { readonly ok: true }
  | { readonly ok: false; readonly field: OidcProviderField; readonly message: string };

export type OidcProviderField =
  | 'slug'
  | 'display_name'
  | 'issuer'
  | 'client_id'
  | 'client_secret'
  | 'scopes'
  | 'jit_policy'
  | 'assurance_policy';

/** The contract's own slug pattern (ProviderSlugPath), enforced before submit. */
const SLUG_PATTERN = /^[a-z0-9][a-z0-9-]{0,63}$/;

export function validateSlug(slug: string): FieldValidation {
  if (!SLUG_PATTERN.test(slug)) {
    return {
      ok: false,
      field: 'slug',
      message:
        'The slug must be 1–64 characters of lowercase letters, digits, or hyphens, and start with a letter or digit.',
    };
  }
  return { ok: true };
}

type PolicyValidation =
  | { readonly ok: true; readonly value: string | null }
  | { readonly ok: false; readonly message: string };

/**
 * validatePolicyJson parses an optional policy field as a JSON OBJECT.
 *
 * Empty is a real, distinct value — the absent policy — and is returned as
 * `null`, not an empty string, because the wire's "absent" is `null`. Anything
 * present must be a JSON object: `JSON.parse` is routed through Zod (never cast)
 * and arrays and primitives are refused, because the server's policies are
 * objects and a bare array here would only reach it to be refused with no field
 * name attached.
 */
export function validatePolicyJson(text: string, label: string): PolicyValidation {
  const trimmed = text.trim();
  if (trimmed === '') {
    return { ok: true, value: null };
  }
  let raw: unknown;
  try {
    raw = JSON.parse(trimmed);
  } catch {
    return { ok: false, message: `${label} must be valid JSON.` };
  }
  if (Array.isArray(raw) || !z.looseObject({}).safeParse(raw).success) {
    return { ok: false, message: `${label} must be a JSON object.` };
  }
  return { ok: true, value: trimmed };
}

export type OidcProviderValidated =
  | { readonly ok: true; readonly slug: string; readonly input: OidcProviderInput }
  | { readonly ok: false; readonly field: OidcProviderField; readonly message: string };

/** The raw editor fields, before validation binds them to a wire body. */
export type OidcProviderDraft = {
  readonly slug: string;
  readonly displayName: string;
  readonly issuer: string;
  readonly clientId: string;
  readonly clientSecret: string;
  readonly scopes: string;
  readonly jitPolicy: string;
  readonly assurancePolicy: string;
  readonly enabled: boolean;
};

/**
 * validateProviderDraft turns editor fields into a wire-ready input or the first
 * offending field. `original` is the provider being reconfigured, or null for a
 * create: on a reconfigure the issuer is immutable (A3), so a changed issuer is
 * the one field error the server would otherwise answer as an unattributed 400.
 *
 * `existing` is the currently-loaded provider list. It lets the editor refuse an
 * enabled-issuer collision here, with a named field, rather than let it reach
 * the database's `oidc_providers_issuer_enabled` unique index — a raw
 * constraint the server does not translate, so it would otherwise surface as an
 * opaque 500.
 */
export function validateProviderDraft(
  draft: OidcProviderDraft,
  original: OidcProvider | null,
  existing: readonly OidcProvider[],
): OidcProviderValidated {
  if (original === null) {
    const slug = validateSlug(draft.slug);
    if (!slug.ok) {
      return slug;
    }
  }
  if (draft.displayName.trim() === '') {
    return { ok: false, field: 'display_name', message: 'A display name is required.' };
  }
  if (draft.issuer.trim() === '') {
    return { ok: false, field: 'issuer', message: 'An issuer URL is required.' };
  }
  if (original !== null && draft.issuer !== original.issuer) {
    return {
      ok: false,
      field: 'issuer',
      message:
        'The issuer is immutable after create — every linked identity is keyed by it. To change it, delete this provider and create a new one.',
    };
  }
  if (draft.clientId.trim() === '') {
    return { ok: false, field: 'client_id', message: 'A client ID is required.' };
  }
  if (draft.clientSecret === '') {
    return {
      ok: false,
      field: 'client_secret',
      message:
        'The client secret is write-only and is never returned, so it must be entered on every save — including when only disabling.',
    };
  }
  if (draft.scopes.trim() === '') {
    return { ok: false, field: 'scopes', message: 'At least one scope is required (for example openid).' };
  }
  if (draft.enabled) {
    const keepSlug = original === null ? draft.slug : original.slug;
    const clash = existing.find(
      (candidate) =>
        candidate.enabled && candidate.issuer === draft.issuer && candidate.slug !== keepSlug,
    );
    if (clash !== undefined) {
      return {
        ok: false,
        field: 'issuer',
        message: `Another enabled provider (${clash.display_name}) already uses this issuer. At most one enabled provider is allowed per issuer — disable or delete it first.`,
      };
    }
  }
  const jit = validatePolicyJson(draft.jitPolicy, 'The JIT policy');
  if (!jit.ok) {
    return { ok: false, field: 'jit_policy', message: jit.message };
  }
  const assurance = validatePolicyJson(draft.assurancePolicy, 'The assurance policy');
  if (!assurance.ok) {
    return { ok: false, field: 'assurance_policy', message: assurance.message };
  }
  return {
    ok: true,
    slug: original === null ? draft.slug : original.slug,
    input: {
      displayName: draft.displayName,
      issuer: draft.issuer,
      clientId: draft.clientId,
      clientSecret: draft.clientSecret,
      scopes: draft.scopes,
      jitPolicy: jit.value,
      assurancePolicy: assurance.value,
      enabled: draft.enabled,
    },
  };
}

// --- refusal text -----------------------------------------------------------

export type OidcProviderOperation =
  | 'list-oidc-providers'
  | 'save-oidc-provider'
  | 'delete-oidc-provider';

/**
 * oidcProviderRefusalText maps a refusal to one honest sentence.
 *
 * The server cannot tell a discovery failure from a slug already in use — both
 * are a bare `bad_request` — so the 400 sentence names both possibilities
 * rather than guessing one. The distinguishable failures (a bad slug, a changed
 * issuer, malformed policy JSON) never reach here: they are refused as field
 * errors before submit.
 */
export function oidcProviderRefusalText(
  error: unknown,
  operation: OidcProviderOperation,
): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return (
          error.detail ??
          'The server refused this provider. If the issuer is new, its OpenID configuration must be reachable and its discovered issuer must match exactly; if the slug is already in use, choose another.'
        );
      case 401:
        return 'Your session ended. Sign in again to continue.';
      case 403:
        return 'You are not permitted to administer identity providers — that needs instance-config, which is MFA-mandatory. Present your second factor.';
      case 404:
        return operation === 'list-oidc-providers'
          ? 'The identity-provider directory is not disclosed to this session.'
          : 'This identity provider is unavailable or does not exist.';
      case 409:
        return 'This identity provider changed underneath you. Reload the provider list before retrying.';
      case 429:
        return 'Too many attempts right now. Wait a moment and try again.';
    }
  }
  return operation === 'delete-oidc-provider'
    ? 'The server failed; whether the provider was deleted is unknown — reload to check.'
    : 'The server failed; whether the change applied is unknown — reload to check.';
}
