/** RFC 4122 version 4 identifier.
 *
 * `crypto.randomUUID` is a secure-context-only API: on a plain-http LAN
 * address it is simply absent, and calling it — at module load or at runtime —
 * throws and takes the whole SPA down. LAN testing over http is a supported
 * way to run this UI, so every identifier goes through here instead.
 * `crypto.getRandomValues` is available in insecure contexts and is the same
 * CSPRNG; it is never substituted with `Math.random`, and a crypto-less
 * environment is a hard error rather than a weak identifier. */
export function uuid(): string {
  if (typeof crypto === 'undefined' || typeof crypto.getRandomValues !== 'function') {
    throw new Error('uuid() needs Web Crypto: neither crypto.randomUUID nor crypto.getRandomValues is available in this environment.');
  }
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  const hex = Array.from(bytes, (byte, index) => {
    // Byte 6 carries the version nibble (4), byte 8 the variant bits (10xx).
    const tagged = index === 6 ? (byte & 0x0f) | 0x40 : index === 8 ? (byte & 0x3f) | 0x80 : byte;
    return tagged.toString(16).padStart(2, '0');
  }).join('');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
