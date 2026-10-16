import {
  getDeveloperCredentialPolicyOp,
  listProjectDeveloperCredentialsOp,
  revokeProjectDeveloperCredentialOp,
  setDeveloperCredentialPolicyOp,
} from '@hikyo/operations';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { ApiError, ok, parsed, transportRefusalText } from './client.ts';
import type { ProjectRef } from './identities.ts';
import { SessionChangedError } from './sessionEpoch.ts';
import { commonRefusalText, statusText } from './statusText.ts';

const projectKey = (ref: ProjectRef) => ['project-developer-credentials', ref.org, ref.project];
const policyKey = ['developer-credential-policy'];

/** Incident-response metadata only. Personal mint/list/revoke remains CLI-only. */
export function useProjectDeveloperCredentials(ref: ProjectRef) {
  return useQuery({
    queryKey: projectKey(ref),
    queryFn: ({ signal }) => parsed(listProjectDeveloperCredentialsOp, { path: ref, signal }),
    enabled: ref.org !== '' && ref.project !== '',
    retry: false,
  });
}

export function useRevokeProjectDeveloperCredential(ref: ProjectRef) {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (credential: string) => ok(revokeProjectDeveloperCredentialOp, { path: { ...ref, credential } }),
    // A refusal can mean access was narrowed. Re-read rather than leaving a
    // previously authorized listing and revoke control on screen.
    onSuccess: () => queries.invalidateQueries({ queryKey: projectKey(ref) }),
    onError: (error) => {
      if (error instanceof ApiError && (error.status === 403 || error.status === 404)) {
        return queries.invalidateQueries({ queryKey: projectKey(ref) });
      }
    },
  });
}

export function useDeveloperCredentialPolicy() {
  return useQuery({
    queryKey: policyKey,
    queryFn: ({ signal }) => parsed(getDeveloperCredentialPolicyOp, { signal }),
    retry: false,
  });
}

export function useSetDeveloperCredentialPolicy() {
  const queries = useQueryClient();
  return useMutation({
    mutationFn: (seconds: number) => parsed(setDeveloperCredentialPolicyOp, { body: { max_lifetime_seconds: seconds } }),
    onSuccess: async () => {
      await Promise.all([
        queries.invalidateQueries({ queryKey: policyKey }),
        queries.invalidateQueries({ queryKey: ['project-developer-credentials'] }),
      ]);
    },
    onError: (error) => {
      if (error instanceof ApiError && (error.status === 403 || error.status === 404)) {
        return queries.invalidateQueries({ queryKey: policyKey });
      }
    },
  });
}

/** Never echo a server error body into administration screens. */
export function developerCredentialFailureText(error: unknown): string {
  if (error instanceof SessionChangedError) return 'The browser session changed. Refresh after sign-in completes before retrying.';
  const generic = transportRefusalText(error);
  return statusText(error, {
    400: 'The request was refused. The lifetime must be a whole number from 1 to 28,800 seconds.',
    401: commonRefusalText.sessionEnded,
    403: 'This session lacks the required permission or second-factor assurance. Check your access; if the security banner asks for verification, verify this session before retrying.',
    404: 'Developer credential administration is not disclosed to this session. The scope may be unavailable or your permission may have changed.',
    409: 'The state changed. Refresh the current metadata and try again.',
    429: commonRefusalText.requests,
  }, `${generic ?? 'Developer credential administration was refused.'} Refresh the current metadata before retrying a change; its outcome may be unconfirmed.`);
}
