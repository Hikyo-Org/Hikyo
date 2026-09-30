import { statusText } from './statusText.ts';
import { adoptInstanceConfigOp, applyInstanceConfigOp, getInstanceConfigOp, previewInstanceConfigAdoptionOp, reauthPasskeyFinishOp, reauthPasskeyStartOp, reauthTotpOp, testInstanceConfigMailOp } from '@hikyo/operations';
import type { zInstanceConfigStatus, zSelfConfigReauthIntent } from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { z } from 'zod';
import { parsed } from './client.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { notifySuccess } from '../app/notifications.tsx';
import { forgetWorkspace } from './workspace.ts';
import { useTransport, useWorkspaceContext, type TransportOptions } from './transport.tsx';
import { assertSessionEpoch, captureSessionEpoch } from './sessionEpoch.ts';
import { requestOptions, toBase64URL } from './values.ts';

export type SelfConfigStatus = z.infer<typeof zInstanceConfigStatus>;
export type SelfConfigIntent = z.infer<typeof zSelfConfigReauthIntent>;
const configKey = ['self-config'];

/** The Hikyo system organisation and project ids, from the self-configuration binding. */
export type SystemScope = { readonly org: string; readonly project: string };

/**
 * The surfaces the protected system profile refuses outright (permission-model
 * ADR, 2026-09-06 amendment: machine consumers, adapters, SCIM). The sidebar
 * omits them in the system scope and their routes answer a deep link with the
 * reason; both read this one list.
 */
export const SYSTEM_SCOPE_REFUSED_SURFACES = ['machine-access', 'adapters', 'scim'] as const;
export type SystemScopeSurface = (typeof SYSTEM_SCOPE_REFUSED_SURFACES)[number];

export function useSelfConfig(enabled = true) {
  const transport = useTransport();
  return useQuery({ queryKey: configKey, queryFn: () => parsed(getInstanceConfigOp, { ...transport }), enabled, refetchInterval: (query) => query.state.status === 'error' ? false : 2000, retry: false });
}

/**
 * The Hikyo system organisation and project, or null while the binding is
 * unknown (not adopted, not disclosed to this session, or not yet read).
 * Shares the self-config query so a page that polls it keeps this fresh; on
 * its own it re-reads once a minute rather than every two seconds. Disabled
 * for anyone but an instance operator: the status read needs
 * `instance-config` and records its denial, and the protected hierarchy is
 * hidden from everyone else anyway.
 */
export function useSystemScope(enabled: boolean): { readonly pending: boolean; readonly scope: SystemScope | null } {
  const transport = useTransport();
  const query = useQuery({ queryKey: configKey, queryFn: () => parsed(getInstanceConfigOp, { ...transport }), enabled, staleTime: 60_000, retry: false });
  const binding = query.data?.binding;
  return { pending: enabled && query.isPending, scope: binding === undefined || binding === null ? null : { org: binding.org_id, project: binding.project_id } };
}

export function useSelfConfigActions() {
  const transport = useTransport();
  const queries = useQueryClient();
  const auth = useAuth();
  const workspace = useWorkspaceContext();
  const refresh = () => queries.invalidateQueries({ queryKey: configKey });
  const preview = useMutation({ mutationFn: () => parsed(previewInstanceConfigAdoptionOp, { ...transport }) });
  const adopt = useMutation({ mutationFn: (body: { preview_token: string; idempotency_key: string }) => parsed(adoptInstanceConfigOp, { body, ...transport }), onSuccess: async () => { notifySuccess('Configuration adopted. Sign in again to use your new project access.'); if (workspace === null) await auth.refreshSession(); else forgetWorkspace(workspace.origin); }, onError: refresh });
  const apply = useMutation({ mutationFn: (body: { revision: bigint; expected_generation: bigint; schema_version: number; idempotency_key: string; confirm_restored_credentials: boolean; prepare_only?: boolean; restore_deployment?: boolean; plan_digest?: string }) => parsed(applyInstanceConfigOp, { body: { ...body, revision: wireInteger(body.revision), expected_generation: wireInteger(body.expected_generation) }, ...transport }), onSettled: refresh });
  const test = useMutation({ mutationFn: (body: { revision: bigint; expected_generation: bigint; schema_version: number; to: string }) => parsed(testInstanceConfigMailOp, { body: { ...body, revision: wireInteger(body.revision), expected_generation: wireInteger(body.expected_generation) }, ...transport }) });
  return { preview, adopt, apply, test };
}

export function selfConfigFailure(error: Error): string {
  return statusText(
    error,
    {
      401: 'Your session ended. Reconnect to this instance.',
      403: 'This action needs instance administration, project access and fresh reauthentication on this owner.',
      404: 'Configuration is not disclosed to this session, or this owner does not support managed configuration.',
      409: 'The selected revision or generation changed. Refresh, review the current state and try again.',
      503: 'This node is catching up with the committed configuration. Retry after refreshing status.',
      429: 'The owner is limiting requests. Wait before trying again.',
      400: 'The owner refused this configuration or authorization. Check the selected revision and try again.',
    },
    'The owner could not confirm the result. Refresh status before retrying.',
  );
}

export function revisionNumber(value: string): bigint | null {
  if (!/^[1-9][0-9]*$/.test(value)) return null;
  const revision = BigInt(value);
  return revision <= 9223372036854775807n ? revision : null;
}

/** Reuses the existing factor routes, carrying the complete selected decision. */
export async function reauthenticateSelfConfig(intent: SelfConfigIntent, factor: { kind: 'totp'; code: string } | { kind: 'passkey' }, transport: TransportOptions = {}) {
  const epoch = captureSessionEpoch();
  const target = { ...intent, revision: wireInteger(intent.revision), expected_generation: wireInteger(intent.expected_generation) };
  if (factor.kind === 'totp') {
    const result = await parsed(reauthTotpOp, { body: { purpose: 'self-config', self_config: target, code: factor.code }, ...transport });
    assertSessionEpoch(epoch);
    return result;
  }
  const options = await parsed(reauthPasskeyStartOp, { body: { operation: 'self-config', environment_id: `instance:${intent.owner_instance_id}`, key_ids: [], self_config: target }, ...transport });
  const assertion = await navigator.credentials.get({ publicKey: requestOptions(options) });
  assertSessionEpoch(epoch);
  if (!(assertion instanceof PublicKeyCredential) || !(assertion.response instanceof AuthenticatorAssertionResponse)) throw new Error('The passkey did not return an assertion.');
  const response = assertion.response;
  const result = await parsed(reauthPasskeyFinishOp, { body: { id: assertion.id, rawId: toBase64URL(assertion.rawId), type: assertion.type, response: { clientDataJSON: toBase64URL(response.clientDataJSON), authenticatorData: toBase64URL(response.authenticatorData), signature: toBase64URL(response.signature), userHandle: response.userHandle === null ? null : toBase64URL(response.userHandle) } }, ...transport });
  assertSessionEpoch(epoch);
  return result;
}

function wireInteger(value: bigint): number {
  if (value < 0n || value > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('This revision or generation is outside the browser request range. Use the CLI.');
  return Number(value);
}
