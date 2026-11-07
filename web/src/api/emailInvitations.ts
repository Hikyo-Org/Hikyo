import {
  acceptEmailInvitationOp,
  inviteOrgMemberByEmailOp,
  listOrgEmailInvitationsOp,
  previewEmailInvitationOp,
  revokeOrgEmailInvitationOp,
} from '@hikyo/operations';
import { zEmailInvitationAcceptance, zEmailInvitationList, zEmailInvitationPreview } from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, ok, parsed, transportRefusalText } from './client.ts';
import { templatesAt, type RoleTemplateId } from './access-templates.ts';
import { commonRefusalText, statusText } from './statusText.ts';

/**
 * Inviting an existing account by its exact email (#1207, docs/spec/email-invitations.md).
 *
 * The send answers the same 202 for every address, so nothing here may word a
 * success as a match: the request records an invitation, and only an account
 * holding that address as a verified email is reached.
 */

export type EmailInvitationList = z.infer<typeof zEmailInvitationList>;
export type EmailInvitationPreview = z.infer<typeof zEmailInvitationPreview>;
export type EmailInvitationAcceptance = z.infer<typeof zEmailInvitationAcceptance>;

const listKey = (org: string) => ['org-email-invitations', org] as const;

/** The org-level templates, as the existing invitation dialog offers them. */
export const emailInvitationTemplates = () => templatesAt('org');

/** Narrows a select's string to a template id the org level admits, without a cast. */
export function emailInvitationTemplate(value: string): RoleTemplateId | null {
  return emailInvitationTemplates().find((candidate) => candidate.id === value)?.id ?? null;
}

export function useEmailInvitations(org: string, enabled: boolean): UseQueryResult<EmailInvitationList> {
  return useQuery({
    queryKey: listKey(org),
    queryFn: () => parsed(listOrgEmailInvitationsOp, { path: { org } }),
    enabled: enabled && org !== '',
  });
}

export function useRevokeEmailInvitation(org: string) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (invitation: string) => ok(revokeOrgEmailInvitationOp, { path: { org, invitation } }),
    onSettled: () => queries.invalidateQueries({ queryKey: listKey(org) }),
  });
}

/** Refreshes the pending list after a send, so the new invitation shows without a reload. */
export function useRefreshEmailInvitations(org: string) {
  const queries = useQueryClient();
  return () => queries.invalidateQueries({ queryKey: listKey(org) });
}

export async function sendEmailInvitation(
  org: string,
  input: { email: string; template: RoleTemplateId },
): Promise<void> {
  await parsed(inviteOrgMemberByEmailOp, { path: { org }, body: input });
}

export function previewEmailInvitation(token: string): Promise<EmailInvitationPreview> {
  return parsed(previewEmailInvitationOp, { body: { token } });
}

export function acceptEmailInvitation(token: string): Promise<EmailInvitationAcceptance> {
  return parsed(acceptEmailInvitationOp, { body: { token } });
}

/** The send refusal for an instance without a mailer (spec D2): a 400 naming the precondition. */
export function isMailerUnconfigured(error: unknown): boolean {
  return error instanceof ApiError && error.status === 400 && (error.detail ?? '').startsWith('mailer-unconfigured');
}

export const MAILER_UNCONFIGURED_TEXT =
  'Email invitations need a configured mail server on this instance, and it has none. Use Invite to create a new account, SCIM provisioning, or an administrator of both organisations instead.';

/** The send refusal for an instance whose public origin the operator never stated: the mailed link would point at a guess. */
export function isNoPublicOrigin(error: unknown): boolean {
  return error instanceof ApiError && error.status === 400 && (error.detail ?? '').startsWith('no-public-origin');
}

export const NO_PUBLIC_ORIGIN_TEXT =
  'Email invitations need the instance\'s public origin: its operator must set HIKYO_EXTERNAL_ORIGIN. Use Invite to create a new account, SCIM provisioning, or an administrator of both organisations instead.';

export function sendFailureText(error: unknown): string {
  if (isMailerUnconfigured(error)) return MAILER_UNCONFIGURED_TEXT;
  if (isNoPublicOrigin(error)) return NO_PUBLIC_ORIGIN_TEXT;
  return statusText(
    error,
    {
      400: (refusal) => refusal.detail ?? 'The invitation was refused: check the email address and the role template.',
      401: commonRefusalText.sessionEnded,
      403: 'Inviting members needs a second factor. Sign in again and present your passkey or a code, then retry.',
      404: 'This organisation is not available to you, or it does not exist. The two are deliberately the same answer.',
      429: commonRefusalText.requests,
    },
    transportRefusalText(error) ?? 'The invitation could not be recorded, or the answer did not match the contract.',
  );
}

export function listFailureText(error: unknown): string {
  return statusText(
    error,
    { 401: commonRefusalText.sessionEnded, 429: commonRefusalText.requests },
    transportRefusalText(error) ?? 'The pending invitations could not be listed. Reload to try again.',
  );
}

export function revokeFailureText(error: unknown): string {
  return statusText(
    error,
    {
      401: commonRefusalText.sessionEnded,
      403: 'Revoking invitations needs a second factor. Sign in again and present your passkey or a code, then retry.',
      404: 'That invitation no longer exists, or you may not manage it.',
      429: commonRefusalText.requests,
    },
    transportRefusalText(error) ?? 'The invitation could not be revoked.',
  );
}

export const INVALID_INVITATION_TEXT = 'This invitation is not valid for this account or has expired.';

/** Preview and accept share one sentence for every uniform 404. */
export function acceptanceFailureText(error: unknown): string {
  return statusText(
    error,
    {
      401: 'Your session ended. Sign in with the account the invitation was sent to, then open the link from your email again.',
      404: INVALID_INVITATION_TEXT,
      429: commonRefusalText.requests,
    },
    transportRefusalText(error) ?? 'The invitation could not be processed.',
  );
}
