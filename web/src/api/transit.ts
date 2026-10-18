import type {
  ConfigureTransitKeyRequest, CreateTransitKeyRequest, TransitDataKeyRequest,
  TransitDecryptRequest, TransitEncryptRequest, TransitHmacRequest, TransitHmacVerifyRequest,
  TransitLifecycleRequest, TransitSignRequest, TransitVerifyRequest,
} from '@hikyo/client';
import {
  changeTransitKeyStateOp, configureTransitKeyOp, createTransitKeyOp, listTransitKeysOp,
  rotateTransitKeyOp, showTransitKeyOp, transitDataKeyOp, transitDecryptOp,
  transitEncryptOp, transitHmacOp, transitRewrapOp, transitSignOp, transitVerifyHmacOp,
  transitVerifyOp, trimTransitKeyOp,
} from '@hikyo/operations';
import type {
  zTransitCiphertextResult, zTransitDataKeyResult, zTransitDecryptResult, zTransitHmacResult,
  zTransitKey, zTransitKeyVersion, zTransitSignatureResult, zTransitVerifyResult,
} from '@hikyo/zod';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { z } from 'zod';

import { ApiError, parsed, parsedPick, transportRefusalText } from './client.ts';
import { useMetadataQuery } from './metadataQuery.ts';
import { assertSessionEpoch, captureSessionEpoch, SessionChangedError } from './sessionEpoch.ts';
import { type TransportOptions, useTransport, useWorkspaceContext } from './transport.tsx';

export type {
  ConfigureTransitKeyRequest, CreateTransitKeyRequest, TransitDataKeyRequest,
  TransitDecryptRequest, TransitEncryptRequest, TransitHmacRequest, TransitHmacVerifyRequest,
  TransitSignRequest, TransitVerifyRequest,
};
export type TransitKeyStateRequest = TransitLifecycleRequest;
export type TransitKey = z.infer<typeof zTransitKey>;
export type TransitKeyVersion = z.infer<typeof zTransitKeyVersion>;
export type EnvRef = { readonly org: string; readonly project: string; readonly environment: string };

const listKey = (origin: string, ref: EnvRef | null) =>
  ['transit-keys', ref?.org ?? '', ref?.project ?? '', ref?.environment ?? '', origin];
const detailPrefix = (origin: string, ref: EnvRef | null) =>
  ['transit-key', ref?.org ?? '', ref?.project ?? '', ref?.environment ?? '', origin];

function requireRef(ref: EnvRef | null): EnvRef {
  if (ref === null) throw new Error('Choose a Transit environment first.');
  return ref;
}

/** Metadata alone enters the query cache. Authoritative denials retire it. */
export function useTransitKeys(ref: EnvRef | null) {
  const transport = useTransport();
  const origin = useWorkspaceContext()?.origin ?? '';
  return useMetadataQuery(listKey(origin, ref),
    (signal) => parsed(listTransitKeysOp, { path: requireRef(ref), signal, ...transport }), ref !== null);
}

export function useTransitKey(ref: EnvRef | null, key: string | null) {
  const transport = useTransport();
  const origin = useWorkspaceContext()?.origin ?? '';
  return useMetadataQuery([...detailPrefix(origin, ref), key ?? ''], (signal) => {
    if (key === null || key === '') throw new Error('Choose a Transit key first.');
    return parsed(showTransitKeyOp, { path: { ...requireRef(ref), transit_key: key }, signal, ...transport });
  }, ref !== null && key !== null && key !== '');
}

/** Refresh every detail because lifecycle changes can alter several versions. */
export function useRefreshTransit(ref: EnvRef | null) {
  const queries = useQueryClient();
  const origin = useWorkspaceContext()?.origin ?? '';
  return () => {
    void queries.invalidateQueries({ queryKey: listKey(origin, ref) });
    void queries.invalidateQueries({ queryKey: detailPrefix(origin, ref) });
  };
}

/** A lost response can hide a committed change. Reconcile in the originating
 * session, even if navigation has unmounted the requesting form. */
function useTransitMutation<Input, Result>(ref: EnvRef | null, request: (input: Input) => Promise<Result>) {
  const refresh = useRefreshTransit(ref);
  return useMutation({
    mutationFn: async (input: Input) => {
      const epoch = captureSessionEpoch();
      try {
        return await request(input);
      } finally {
        refreshInSession(epoch, refresh);
      }
    },
  });
}

function refreshInSession(epoch: number, refresh: () => void): void {
  try {
    assertSessionEpoch(epoch);
  } catch (error) {
    if (error instanceof SessionChangedError) return;
    throw error;
  }
  refresh();
}

export function useCreateTransitKey(ref: EnvRef | null) {
  const transport = useTransport();
  return useTransitMutation(ref, (body: CreateTransitKeyRequest) =>
    parsed(createTransitKeyOp, { path: requireRef(ref), body, ...transport }));
}

export function useConfigureTransitKey(ref: EnvRef | null) {
  const transport = useTransport();
  return useTransitMutation(ref, (input: { key: string; body: ConfigureTransitKeyRequest }) =>
    parsed(configureTransitKeyOp, { path: { ...requireRef(ref), transit_key: input.key }, body: input.body, ...transport }));
}

export function useRotateTransitKey(ref: EnvRef | null) {
  const transport = useTransport();
  return useTransitMutation(ref, (key: string) =>
    parsed(rotateTransitKeyOp, { path: { ...requireRef(ref), transit_key: key }, ...transport }));
}

export function useChangeTransitKeyState(ref: EnvRef | null) {
  const transport = useTransport();
  return useTransitMutation(ref, (input: { key: string; body: TransitKeyStateRequest }) =>
    parsed(changeTransitKeyStateOp, { path: { ...requireRef(ref), transit_key: input.key }, body: input.body, ...transport }));
}

/** Trim is a bodyless POST: its deletion boundary is the stored decrypt floor. */
export function useTrimTransitKey(ref: EnvRef | null) {
  const transport = useTransport();
  return useTransitMutation(ref, (key: string) =>
    parsed(trimTransitKeyOp, { path: { ...requireRef(ref), transit_key: key }, ...transport }));
}

export type TransitInput =
  | { operation: 'encrypt'; body: TransitEncryptRequest }
  | { operation: 'decrypt' | 'rewrap'; body: TransitDecryptRequest }
  | { operation: 'datakey'; body: TransitDataKeyRequest }
  | { operation: 'sign'; body: TransitSignRequest }
  | { operation: 'verify'; body: TransitVerifyRequest }
  | { operation: 'hmac'; body: TransitHmacRequest }
  | { operation: 'hmac-verify'; body: TransitHmacVerifyRequest };
export type TransitOperation = TransitInput['operation'];
export type TransitResult =
  | { operation: 'encrypt' | 'rewrap'; output: z.infer<typeof zTransitCiphertextResult> }
  | { operation: 'decrypt'; output: z.infer<typeof zTransitDecryptResult> }
  | { operation: 'datakey'; output: z.infer<typeof zTransitDataKeyResult> }
  | { operation: 'sign'; output: z.infer<typeof zTransitSignatureResult> }
  | { operation: 'verify' | 'hmac-verify'; output: z.infer<typeof zTransitVerifyResult> }
  | { operation: 'hmac'; output: z.infer<typeof zTransitHmacResult> };
export type TransitOperationInput = TransitInput;
export type TransitOperationResult = TransitResult;

/** Sensitive inputs and results stay in the caller's ephemeral ownership.
 * This function must never become a query or a cached mutation. */
export async function runTransit(
  ref: EnvRef, key: string, input: TransitInput, transport: TransportOptions, signal?: AbortSignal,
): Promise<TransitResult> {
  const options = { path: { ...ref, transit_key: key }, ...transport, ...(signal === undefined ? {} : { signal }) };
  switch (input.operation) {
    case 'encrypt':
      return { operation: input.operation, output: await parsedPick(transitEncryptOp, { ...options, body: input.body }, { ciphertext: true, key_version: true }) };
    case 'decrypt':
      return { operation: input.operation, output: await parsedPick(transitDecryptOp, { ...options, body: input.body }, { plaintext: true, key_version: true }) };
    case 'rewrap':
      return { operation: input.operation, output: await parsedPick(transitRewrapOp, { ...options, body: input.body }, { ciphertext: true, key_version: true }) };
    case 'datakey': {
      if (input.body.plaintext !== true) {
        return { operation: input.operation, output: await parsedPick(transitDataKeyOp,
          { ...options, body: input.body }, { ciphertext: true, key_version: true }) };
      }
      const output = await parsedPick(transitDataKeyOp, { ...options, body: input.body }, { ciphertext: true, key_version: true, plaintext: true });
      if (output.plaintext == null) {
        throw new Error('The server returned no plaintext data key.');
      }
      return { operation: input.operation, output };
    }
    case 'sign':
      return { operation: input.operation, output: await parsedPick(transitSignOp, { ...options, body: input.body }, { signature: true, key_version: true }) };
    case 'verify':
      return { operation: input.operation, output: await parsedPick(transitVerifyOp, { ...options, body: input.body }, { valid: true, key_version: true }) };
    case 'hmac':
      return { operation: input.operation, output: await parsedPick(transitHmacOp, { ...options, body: input.body }, { mac: true, key_version: true }) };
    case 'hmac-verify':
      return { operation: input.operation, output: await parsedPick(transitVerifyHmacOp, { ...options, body: input.body }, { valid: true, key_version: true }) };
  }
}

/** Do not render server details, Zod issues or thrown messages: requests and
 * invalid responses may contain the plaintext this operation handles. */
export function transitRefusalText(error: Error | null): string {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 400: return 'The Transit request was refused. Check the input, context and key version.';
      case 401: return 'Your session ended. Sign in again to continue.';
      case 403: return 'Transit access was refused. Check your environment permissions, caller grant and second factor.';
      case 404: return 'This Transit key is no longer available in the selected environment.';
      case 409: return 'The key state or policy refused this operation. Refresh the key and review its versions.';
      case 429: return 'Too many requests right now. Wait a moment and try again.';
    }
  }
  return transportRefusalText(error) ?? 'The Transit request could not be completed.';
}
