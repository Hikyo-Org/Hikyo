import {
  listOauth2ProvidersOp,
  putOauth2ProviderOp,
  deleteOauth2ProviderOp,
} from '@hikyo/operations';
import { zOauth2Provider, zOauth2ProviderInput } from '@hikyo/zod';
import { useQuery } from '@tanstack/react-query';
import type { z } from 'zod';
import { ok, parsed } from './client.ts';

export type OAuth2Provider = z.infer<typeof zOauth2Provider>;
export type OAuth2ProviderInput = z.infer<typeof zOauth2ProviderInput>;
export function useOAuth2Providers() {
  return useQuery({
    queryKey: ['instance-oauth2-providers'],
    queryFn: () => parsed(listOauth2ProvidersOp, {}),
  });
}
// Secrets stay in the one-shot request, outside React Query's mutation cache.
export function putOAuth2Provider(slug: string, body: OAuth2ProviderInput) {
  return parsed(putOauth2ProviderOp, { path: { slug }, body });
}
export function deleteOAuth2Provider(slug: string) {
  return ok(deleteOauth2ProviderOp, { path: { slug } });
}
