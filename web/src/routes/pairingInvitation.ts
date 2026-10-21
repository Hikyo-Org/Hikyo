import { z } from 'zod';

export const MAX_INVITATION_LENGTH = 8_192;
const base64Pin = z.string().regex(/^[A-Za-z0-9+/]{43}=$/).refine((value) => {
  try { return btoa(atob(value)) === value; } catch { return false; }
});
const invitationSchema = z.strictObject({
  version: z.literal(1),
  id: z.string().min(1).max(256),
  origin: z.string().max(2_048).refine((value) => {
    try { const url = new URL(value); return url.protocol === 'https:' && url.origin === value && url.username === '' && url.password === ''; }
    catch { return false; }
  }),
  spki_pin: base64Pin,
  capability: z.string().regex(/^hik_1_hc_[0-9A-Za-z]{7,49}$/),
  expires_at: z.iso.datetime({ offset: true }),
});

/** Navigation hints only. The local server still authenticates the invitation,
 * phrase binding, remote identity and expiry after explicit confirmation. */
export function readPairingInvitation(value: string) {
  if (value.length === 0 || value.length > MAX_INVITATION_LENGTH || !/^[A-Za-z0-9_-]+$/.test(value)) return null;
  try {
    const bytes = atob(value.replaceAll('-', '+').replaceAll('_', '/'));
    if (btoa(bytes).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '') !== value) return null;
    const text = new TextDecoder('utf-8', { fatal: true }).decode(Uint8Array.from(bytes, (char) => char.charCodeAt(0)));
    const result = invitationSchema.safeParse(JSON.parse(text));
    return result.success ? result.data : null;
  } catch { return null; }
}
