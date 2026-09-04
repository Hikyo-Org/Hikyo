import {
  createFederationIssuerOp,
  deleteFederationIssuerOp,
  listFederationIssuersOp,
  updateFederationIssuerOp,
} from '@hikyo/operations';
import { zFederationIssuerList } from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, ok, parsed } from './client.ts';

/**
 * The federation-issuer surface, instance-scoped and MFA-mandatory
 * (`instance-config@instance`). It is deliberately NOT the project machine
 * surface: an issuer is one external authority the whole instance trusts, and
 * an org- or project-scoped issuer would let a tenant admit a new external
 * identity provider the instance never reviewed.
 *
 * Two rules the ADRs put here rather than in the component:
 *
 *  - **`static_jwks` never round-trips.** The read shape has no such member —
 *    the document is configuration an operator supplied and can re-supply — so
 *    the editor field is always blank and, under `static` mode, always
 *    re-entered. There is no keep-the-old-document path, and there cannot be
 *    one that silently retains a key set nobody rotates.
 *  - **Delete is fail-closed on every naming binding, live OR historical.** The
 *    server refuses a delete while any binding names the issuer, revoked
 *    included: erasing the issuer a past binding trusted erases what it trusted.
 *    The `live_bindings` count carries that same census so an operator sees the
 *    blast radius before attempting a delete.
 */

export type FederationIssuer = z.infer<typeof zFederationIssuerList>['items'][number];
export type FederationIssuerType = FederationIssuer['issuer_type'];
export type FederationJwksMode = FederationIssuer['jwks_mode'];

const issuersKey = ['federation-issuers'] as const;

export function useFederationIssuers() {
  return useQuery({
    queryKey: issuersKey,
    queryFn: () => parsed(listFederationIssuersOp, {}),
  });
}

/**
 * useCreateFederationIssuer configures one issuer. `static_jwks` is sent IFF
 * the mode is `static`: the wire schema refuses additional properties, so a
 * document carried under `discovery` would be a 400, and a document dropped
 * under `static` is the key set nobody rotates.
 */
export function useCreateFederationIssuer() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      issuer: string;
      issuerType: FederationIssuerType;
      jwksMode: FederationJwksMode;
      staticJwks?: string;
      refusedAudiences: readonly string[];
    }) =>
      parsed(createFederationIssuerOp, {
        body: {
          issuer: input.issuer,
          issuer_type: input.issuerType,
          jwks_mode: input.jwksMode,
          ...(input.jwksMode === 'static' && input.staticJwks !== undefined
            ? { static_jwks: input.staticJwks }
            : {}),
          refused_audiences: [...input.refusedAudiences],
        },
      }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: issuersKey });
    },
  });
}

/**
 * useUpdateFederationIssuer moves the MUTABLE half only — the JWKS source and
 * the refused audiences. The issuer string and platform type are absent on
 * purpose: changing either is a replacement, not an edit, and the request
 * schema has no member for them.
 */
export function useUpdateFederationIssuer() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      id: string;
      jwksMode: FederationJwksMode;
      staticJwks?: string;
      refusedAudiences: readonly string[];
    }) =>
      parsed(updateFederationIssuerOp, {
        path: { issuer: input.id },
        body: {
          jwks_mode: input.jwksMode,
          ...(input.jwksMode === 'static' && input.staticJwks !== undefined
            ? { static_jwks: input.staticJwks }
            : {}),
          refused_audiences: [...input.refusedAudiences],
        },
      }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: issuersKey });
    },
  });
}

/**
 * useDeleteFederationIssuer removes one configuration. The server refuses it
 * with a 409 while any binding names the issuer — live or historical — so the
 * caller shows the `live_bindings` census first and revokes those bindings
 * before the delete can land.
 */
export function useDeleteFederationIssuer() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (id: string) =>
      ok(deleteFederationIssuerOp, {
        path: { issuer: id },
      }),
    onSuccess: () => {
      void queries.invalidateQueries({ queryKey: issuersKey });
    },
  });
}

/**
 * issuerCreateRefusalText names a create refusal in the create verb's OWN
 * vocabulary — every status, never delegating. A 409 is a duplicate issuer
 * (matched byte-exact); a 403 is the instance-config second factor, never a
 * disclosure capability; a 404 is the authorization mask.
 */
export function issuerCreateRefusalText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return 'The server refused that issuer. Check the https URL, the JWKS document under static mode, and that every refused audience is a non-empty single line. Nothing was configured.';
      case 401:
        return 'The session could not be authenticated. Reload and sign in before configuring an issuer.';
      case 403:
        return 'Configuring a federation issuer is instance-config work and needs a second factor. Present your authenticator code or passkey in the banner above. Nothing was configured.';
      case 404:
        return 'You may not administer this instance’s configuration. Nothing was configured.';
      case 409:
        return 'An issuer with that exact URL is already configured. The match is byte-exact, so a trailing slash is a different issuer. Nothing was configured.';
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `The issuer could not be configured (server error ${String(error.status)}).`;
    }
  }
  return 'The issuer could not be configured.';
}

/**
 * issuerUpdateRefusalText names an update refusal. It has no 409: the issuer
 * string and platform type cannot move, so there is no duplicate to conflict
 * with. A 404 means the issuer was deleted underneath the edit.
 */
export function issuerUpdateRefusalText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return 'The server refused that change. Check the JWKS document under static mode and that every refused audience is a non-empty single line. Nothing was changed.';
      case 401:
        return 'The session could not be authenticated. Reload and sign in before editing an issuer.';
      case 403:
        return 'Editing a federation issuer is instance-config work and needs a second factor. Present your authenticator code or passkey in the banner above. Nothing was changed.';
      case 404:
        return 'That issuer is absent, or not disclosed to this session — the two are the same uniform response. Nothing was changed.';
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `The issuer could not be changed (server error ${String(error.status)}).`;
    }
  }
  return 'The issuer could not be changed.';
}

/**
 * issuerDeleteRefusalText names a delete refusal. The 409 is the load-bearing
 * one: the delete is refused while any binding names the issuer, live or
 * revoked, because erasing the issuer a past binding trusted erases what it
 * trusted. The operator revokes those bindings first.
 */
export function issuerDeleteRefusalText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 401:
        return 'The session could not be authenticated. Reload and sign in before deleting an issuer.';
      case 403:
        return 'Deleting a federation issuer is instance-config work and needs a second factor. Present your authenticator code or passkey in the banner above.';
      case 404:
        return 'That issuer is absent, or not disclosed to this session — the two are the same uniform response.';
      case 409:
        return 'This issuer has bindings naming it — live or revoked — so it cannot be deleted. That binding history is append-only and never reaches zero once an issuer has been used, because erasing the issuer would erase what those bindings trusted.';
      case 429:
        return 'Too many requests right now. Wait a moment and try again.';
      default:
        return `The issuer could not be deleted (server error ${String(error.status)}).`;
    }
  }
  return 'The issuer could not be deleted.';
}

/**
 * issuerFieldRefusal is the create/update form's client-side gate, refused HERE
 * rather than as a 400 so an operator meets each rule as a form. It mirrors the
 * server's `checkIssuerRequest` byte-for-byte: an https URL with a host and no
 * userinfo, query or fragment, and at least one non-empty single-line refused
 * audience. The issuer is only validated when supplied (create), since update
 * cannot move it.
 */
export function issuerFieldRefusal(input: {
  issuer?: string;
  jwksMode: FederationJwksMode;
  staticJwks: string;
  refusedAudiences: readonly string[];
}): string | null {
  if (input.issuer !== undefined) {
    if (!isHttpsIssuer(input.issuer)) {
      return 'The issuer must be an https URL with a host and nothing else — no user, no query, no fragment. Discovery and JWKS are fetched from it, so http would rest the instance’s whole federation trust on the network path. Nothing was saved.';
    }
  }
  const audiences = input.refusedAudiences.filter((a) => a !== '');
  if (audiences.length === 0) {
    return 'At least one refused audience is required: the default-audience rule turns on the instance knowing what the default is, and it is not derivable. Nothing was saved.';
  }
  if (audiences.some((a) => /[\n\r]/.test(a))) {
    return 'A refused audience may not contain a line break — newline is the storage separator, so one value would split into two. Nothing was saved.';
  }
  if (input.jwksMode === 'static' && input.staticJwks.trim() === '') {
    return 'Static mode requires the JWKS document: it is the key set this instance verifies against, and there is no discovery endpoint to fetch it from. Nothing was saved.';
  }
  return null;
}

/** isHttpsIssuer mirrors the server's OIDC issuer grammar exactly. */
export function isHttpsIssuer(issuer: string): boolean {
  let url: URL;
  try {
    url = new URL(issuer);
  } catch {
    return false;
  }
  return (
    url.protocol === 'https:' &&
    url.host !== '' &&
    url.username === '' &&
    url.password === '' &&
    url.search === '' &&
    url.hash === ''
  );
}
