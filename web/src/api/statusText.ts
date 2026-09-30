import { ApiError } from './client.ts';

type StatusMessage = string | ((error: ApiError) => string);

/** Keep feature wording and safe details while sharing status dispatch. */
export function statusText(
  error: unknown,
  messages: Readonly<Partial<Record<number, StatusMessage>>>,
  fallback: string,
  serverFallback: StatusMessage = fallback,
): string {
  if (!(error instanceof ApiError)) return fallback;
  const message = messages[error.status] ?? serverFallback;
  return typeof message === 'function' ? message(error) : message;
}

/** Shared wording only; each feature still chooses which statuses use it. */
export const commonRefusalText = {
  sessionEnded: 'Your session ended. Sign in again to continue.',
  unauthenticated: 'The session could not be authenticated. Reload and sign in first.',
  requests: 'Too many requests right now. Wait a moment and try again.',
  attempts: 'Too many attempts right now. Wait a moment and try again.',
};
