import { adoptInstanceConfigOp, applyInstanceConfigOp, getInstanceConfigOp, previewInstanceConfigAdoptionOp, reauthPasskeyFinishOp, reauthPasskeyStartOp, reauthTotpOp, testInstanceConfigMailOp } from '@hikyo/operations';
import type { zInstanceConfigStatus, zSelfConfigReauthIntent } from '@hikyo/zod';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { z } from 'zod';
import { ApiError, parsed } from './client.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { notifySuccess } from '../app/notifications.tsx';
import { forgetWorkspace } from './workspace.ts';
import { useTransport, useWorkspaceContext, type TransportOptions } from './transport.tsx';
import { assertSessionEpoch, captureSessionEpoch } from './sessionEpoch.ts';
import { requestOptions, toBase64URL } from './values.ts';

export type SelfConfigStatus = z.infer<typeof zInstanceConfigStatus>;
export type SelfConfigIntent = z.infer<typeof zSelfConfigReauthIntent>;
const configKey = ['self-config'];

export function useSelfConfig(enabled = true) {
  const transport = useTransport();
  return useQuery({ queryKey: configKey, queryFn: () => parsed(getInstanceConfigOp, { ...transport }), enabled, refetchInterval: (query) => query.state.status === 'error' ? false : 2000, retry: false });
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
  if (error instanceof ApiError) {
    switch (error.status) {
      case 401: return 'Your session ended. Reconnect to this instance.';
      case 403: return 'This action needs instance administration, project access and fresh reauthentication on this owner.';
      case 404: return 'Configuration is not disclosed to this session, or this owner does not support managed configuration.';
      case 409: return 'The selected revision or generation changed. Refresh, review the current state and try again.';
      case 503: return 'This node is catching up with the committed configuration. Retry after refreshing status.';
      case 429: return 'The owner is limiting requests. Wait before trying again.';
      case 400: return 'The owner refused this configuration or authorization. Check the selected revision and try again.';
    }
  }
  return 'The owner could not confirm the result. Refresh status before retrying.';
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
