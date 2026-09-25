import { z } from 'zod';

/**
 * The way in this browser last used, so the staged entry can badge that row
 * "Last used" (a returning person picks the same method almost every time).
 *
 * Not a secret and not session state: a method name, never a credential, an
 * identifier or a token, kept in localStorage beside the theme choice. It is
 * written when a leg is started with a credential the server accepted (the
 * password answered, the passkey asserted) or when a provider round-trip
 * begins, since the redirect leaves no later moment. Absent or malformed
 * storage reads as nothing, the same as a first visit.
 */
export type LastSignIn =
  | { kind: 'password' }
  | { kind: 'passkey' }
  | { kind: 'provider'; providerKind: string; slug: string };

const STORAGE_KEY = 'hikyo.last-sign-in';

// A provider is stored as `provider:<kind>:<slug>`, since a slug is unique per
// kind only. Kinds are a closed server enum and slugs DNS-style, so neither
// holds a colon; a value missing either segment (an older `provider:<slug>`
// included) reads as a first visit.
const zSegment = z.string().regex(/^[^:]+$/);
const zStored = z.union([
  z.literal('password'),
  z.literal('passkey'),
  z.templateLiteral(['provider:', zSegment, ':', zSegment]),
]);

// Best-effort like the theme choice (app/theme.ts): a hardened browser or a
// private mode can throw on ANY storage access, and a badge is not worth a
// crashed sign-in page.
export function readLastSignIn(): LastSignIn | null {
  let raw: string | null;
  try {
    raw = globalThis.localStorage?.getItem(STORAGE_KEY) ?? null;
  } catch {
    return null;
  }
  const stored = zStored.safeParse(raw);
  if (!stored.success) return null;
  if (stored.data === 'password' || stored.data === 'passkey') return { kind: stored.data };
  const [, providerKind, slug] = stored.data.split(':');
  if (providerKind === undefined || slug === undefined) return null;
  return { kind: 'provider', providerKind, slug };
}

export function rememberLastSignIn(method: LastSignIn): void {
  try {
    globalThis.localStorage?.setItem(
      STORAGE_KEY,
      method.kind === 'provider' ? `provider:${method.providerKind}:${method.slug}` : method.kind,
    );
  } catch {
    // Storage refused the write; the next visit simply shows no badge.
  }
}

/** Whether a row is the one badged: same kind, and for a provider the same provider kind and slug. */
export function isLastSignIn(last: LastSignIn | null, method: LastSignIn): boolean {
  if (last === null || last.kind !== method.kind) return false;
  return (
    method.kind !== 'provider' ||
    (last.kind === 'provider' && last.providerKind === method.providerKind && last.slug === method.slug)
  );
}
