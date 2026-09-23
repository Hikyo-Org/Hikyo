import {
  deleteInstanceRegistrationPolicyOp,
  deleteOrgRegistrationPolicyOp,
  getInstanceRegistrationPolicyOp,
  getOrgRegistrationPolicyOp,
  putInstanceRegistrationPolicyOp,
  putOrgRegistrationPolicyOp,
} from '@hikyo/operations';
import type { RegistrationPolicy, RegistrationPolicyPutRequest } from '@hikyo/client';
import { zProviderRef, zRoleTemplate } from '@hikyo/zod';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import { z } from 'zod';

import { ApiError, ok, parsed, transportRefusalText } from './client.ts';

export type { RegistrationPolicy, RegistrationPolicyPutRequest };

/**
 * The registration policy, as the Members surface sees it (#606, #579 as
 * amended; locked prototype social-signin iteration 2).
 *
 * One policy per scope: an organisation, or the instance. Absent means
 * registration is closed. The read carries the policy's live state (the
 * standing delegation and the preconditions are re-checked on every read), so
 * the panel renders what the server evaluated, never a client guess.
 *
 * The wire shape keeps its conditional members optional (a template only on
 * an org landing, a cap and a count only on a fresh-org one, a cause only
 * when inactive). `policyView` parses those couplings, so a response that
 * breaks one fails loudly here instead of rendering a default.
 *
 * Writes carry fresh proof (a TOTP code, or the password where no factor is
 * enrolled) and are plain async calls, never a mutation cache: the proof is
 * component-owned plaintext for exactly as long as the dialog asking for it.
 */
export type RegistrationScope = { readonly kind: 'org'; readonly org: string } | { readonly kind: 'instance' };

/** The provider kinds a sign-up entry can name. */
export type EntryKind = 'oidc' | 'oauth2';

const zEntryKind = z.enum(['oidc', 'oauth2']);

const zEntryView = z
  .object({
    provider: zProviderRef.extend({ kind: zEntryKind }),
    display_name: z.string().optional(),
    claim: z.string().optional(),
    values: z.array(z.string()).optional(),
  })
  .transform((entry, context) => {
    if ((entry.claim === undefined) !== (entry.values === undefined)) {
      context.addIssue({ code: 'custom', message: 'an allowlist claim and its values arrive together' });
      return z.NEVER;
    }
    return {
      provider: entry.provider,
      name: entry.display_name ?? entry.provider.slug,
      allow: entry.claim === undefined || entry.values === undefined ? null : { claim: entry.claim, values: entry.values },
    };
  });

const zLandingView = z.discriminatedUnion('kind', [
  z.object({ kind: z.literal('org-template'), template: zRoleTemplate }),
  z.object({ kind: z.literal('none') }),
  z.object({ kind: z.literal('fresh-org'), cap: z.number().int().positive() }),
]);

const zStateView = z.discriminatedUnion('state', [
  z.object({ state: z.literal('active') }),
  z.object({
    state: z.literal('inactive'),
    inactive_cause: z.enum(['authority-lost', 'authority-unassigned', 'precondition']),
    inactive_precondition: z.string().optional(),
  }),
]);

const zPolicyView = z
  .object({
    id: z.string(),
    external: z.array(zEntryView),
    local: z.object({ domains: z.array(z.string()) }).optional(),
    landing: zLandingView,
    authority_principal_id: z.string(),
    fresh_org_count: z.number().int().nonnegative().optional(),
    state: z.enum(['active', 'inactive']),
    inactive_cause: z.enum(['authority-lost', 'authority-unassigned', 'precondition']).optional(),
    inactive_precondition: z.string().optional(),
  })
  .transform((policy, context) => {
    const state = zStateView.safeParse(policy);
    if (!state.success) {
      context.addIssue({ code: 'custom', message: 'an inactive policy names its cause' });
      return z.NEVER;
    }
    if (policy.landing.kind === 'fresh-org' && policy.fresh_org_count === undefined) {
      context.addIssue({ code: 'custom', message: 'a fresh-org policy reports its live org count' });
      return z.NEVER;
    }
    return {
      id: policy.id,
      external: policy.external,
      local: policy.local ?? null,
      landing: policy.landing,
      authority: policy.authority_principal_id,
      mintedOrgs: policy.fresh_org_count ?? null,
      state: state.data,
    };
  });

/** A policy as the panel renders it, its wire couplings proven. */
export type PolicyView = z.infer<typeof zPolicyView>;
export type EntryView = PolicyView['external'][number];

export function policyView(policy: RegistrationPolicy): PolicyView {
  return zPolicyView.parse(policy);
}

export function registrationKey(scope: RegistrationScope) {
  return scope.kind === 'instance'
    ? (['registration-policy', 'instance'] as const)
    : (['registration-policy', 'org', scope.org] as const);
}

/** The policy, or null when the scope has none (closed). */
export function useRegistrationPolicy(
  scope: RegistrationScope,
  enabled: boolean,
): UseQueryResult<PolicyView | null> {
  return useQuery({
    queryKey: registrationKey(scope),
    queryFn: async () => {
      try {
        const policy = scope.kind === 'instance'
          ? await parsed(getInstanceRegistrationPolicyOp, {})
          : await parsed(getOrgRegistrationPolicyOp, { path: { org: scope.org } });
        return policyView(policy);
      } catch (error) {
        // The panel renders only for a manage-members holder, for whom a 404
        // is "no policy here": registration is closed.
        if (error instanceof ApiError && error.status === 404) return null;
        throw error;
      }
    },
    enabled: enabled && (scope.kind === 'instance' || scope.org !== ''),
  });
}

export async function putRegistrationPolicy(
  scope: RegistrationScope,
  body: RegistrationPolicyPutRequest,
): Promise<PolicyView> {
  const saved = scope.kind === 'instance'
    ? await parsed(putInstanceRegistrationPolicyOp, { body })
    : await parsed(putOrgRegistrationPolicyOp, { path: { org: scope.org }, body });
  return policyView(saved);
}

export async function deleteRegistrationPolicy(scope: RegistrationScope, proof: string): Promise<void> {
  const body = { proof };
  return scope.kind === 'instance'
    ? ok(deleteInstanceRegistrationPolicyOp, { body })
    : ok(deleteOrgRegistrationPolicyOp, { path: { org: scope.org }, body });
}

/** The PUT body that re-saves a policy exactly as it stands (the caller becomes its authority). */
export function resaveBody(policy: PolicyView, proof: string): RegistrationPolicyPutRequest {
  return {
    external: policy.external.map((entry) => ({
      provider: entry.provider,
      ...(entry.allow === null ? {} : { claim: entry.allow.claim, values: entry.allow.values }),
    })),
    ...(policy.local === null ? {} : { local: { domains: policy.local.domains } }),
    landing: policy.landing,
    proof,
  };
}

/** The requirement each admitted method places on a signer (#598). */
export function requirementLine(kind: EntryKind | 'local'): string {
  switch (kind) {
    case 'oidc':
      return 'The ID token must carry email plus email_verified or xms_edov as boolean true.';
    case 'oauth2':
      return 'The provider must report a primary, verified address.';
    case 'local':
      return 'The address is proven by a mailed link before any account exists. Plain-text mail, valid 24 hours.';
  }
}

/** The org's own sign-up door, to hand to people who should join it. */
export function signupLink(origin: string, org: string): string {
  return `${origin}/signup?org=${encodeURIComponent(org)}`;
}

/** A precondition detail's provider, `<kind>:<slug>`, when it names one. */
export function preconditionProvider(detail: string): string | null {
  const match = /^[a-z-]+: ([a-z0-9]+:.+)$/.exec(detail);
  return match?.[1] ?? null;
}

/** The sentence for a named precondition (write-time 400 detail or use-time cause). */
export function preconditionText(detail: string): string {
  const provider = preconditionProvider(detail);
  const slug = provider === null ? '' : provider.slice(provider.indexOf(':') + 1);
  const name = provider === null ? detail : detail.slice(0, detail.indexOf(':'));
  switch (name) {
    case 'no-public-origin':
      return 'Registration needs an explicit public origin: set HIKYO_EXTERNAL_ORIGIN on the instance.';
    case 'mailer-unconfigured':
      return 'The email entry needs a configured mailer (HIKYO_MAIL_*). Configure it on the instance, or leave this entry off.';
    case 'cap-zero':
      return 'A cap of at least 1 is required for this landing.';
    case 'template-not-org-applicable':
      return 'That role template cannot be applied at organisation scope.';
    case 'provider-missing-email-scope':
      return `${slug}: this provider row does not request the email scope, so it cannot assert a verified address. Add the scope on the provider, then admit it.`;
    case 'provider-disabled':
      return `${slug} is disabled. Enable it, or leave it off.`;
    case 'provider-missing':
      return `The provider this entry named (${slug}) no longer exists. Edit the policy to remove it.`;
    case 'provider-kind-unsupported':
      return `${provider ?? detail}: this provider kind cannot admit sign-ups yet.`;
    default:
      return detail;
  }
}

/** The remedy the panel shows for an inactive policy; the cause never reaches the public page. */
export function inactiveText(state: Extract<PolicyView['state'], { state: 'inactive' }>, authority: string): string {
  switch (state.inactive_cause) {
    case 'authority-lost':
      return `${authority} no longer holds the grant this policy hands out. Sign-ups are refused. Re-save to become the authority.`;
    case 'authority-unassigned':
      return 'This policy has no authority yet. Sign-ups are refused. Re-save to become the authority.';
    case 'precondition':
      if (state.inactive_precondition === undefined) {
        throw new Error('a precondition cause arrived without naming the precondition');
      }
      return `Sign-ups are refused: ${preconditionText(state.inactive_precondition)}`;
  }
}

/**
 * registrationFailureText voices a refused write. A 400 names the failing
 * item; 401 on a write is the proof (a session that ended is voiced the
 * same way, since the next step, entering proof again, is the same).
 */
export function registrationFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400:
        return error.detail === undefined
          ? 'The policy was refused. Check each entry and try again.'
          : preconditionText(error.detail);
      case 401:
        return 'That proof was not accepted. Enter a fresh authenticator code, or your password if you have no authenticator.';
      case 403:
        return 'This needs a second factor, or a grant you do not hold (a new organisation per sign-up needs org.create).';
      case 404:
        return 'There is no registration policy here any more. Reload.';
      case 409:
        return 'The policy changed underneath you. Reload before saving again.';
      case 429:
        return 'Too many attempts. Wait a moment, then try again.';
      default:
        return 'The server could not save the policy. Try again.';
    }
  }
  return transportRefusalText(error) ?? 'The server could not be reached. Try again.';
}
