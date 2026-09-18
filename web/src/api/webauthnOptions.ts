/**
 * Shared, STRICT narrowing of the server's WebAuthn option blobs.
 *
 * Both ceremonies (registration in account.ts, assertion in values.ts) hand a
 * server blob to the browser's credential API after converting its base64url
 * fields to buffers. The conversion is also the validation: a field the
 * browser API would silently coerce, or a descriptor the adapter would
 * silently drop, is an altered ceremony rather than a refused one, so every
 * parser here throws on a malformed value and preserves every supported
 * policy value verbatim (F12). One implementation, so the two ceremonies
 * cannot drift apart again.
 *
 * `prefix` names the ceremony in every error ("the enrolment options",
 * "the reauth options") so a refusal says which blob was wrong.
 */

export function fromBase64URL(value: string): ArrayBuffer {
  const padded = value.replace(/-/g, '+').replace(/_/g, '/');
  const binary = atob(padded + '='.repeat((4 - (padded.length % 4)) % 4));
  // An ArrayBuffer, not a Uint8Array view: `BufferSource` wants a view over a
  // plain ArrayBuffer and a bare `Uint8Array` is typed over `ArrayBufferLike`,
  // which admits SharedArrayBuffer. Handing back the buffer keeps the browser
  // API's own type honest without a cast.
  const buffer = new ArrayBuffer(binary.length);
  const out = new Uint8Array(buffer);
  for (let i = 0; i < binary.length; i++) {
    out[i] = binary.charCodeAt(i);
  }
  return buffer;
}

export function toBase64URL(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export function optionParsers(prefix: string) {
  function record(value: unknown, what: string): Record<string, unknown> {
    if (typeof value !== 'object' || value === null) {
      throw new Error(`${prefix} carried no ${what}`);
    }
    return { ...value };
  }

  function requiredString(value: unknown, what: string): string {
    if (typeof value !== 'string') {
      throw new Error(`${prefix} carried no ${what}`);
    }
    return value;
  }

  function publicKeyType(value: unknown, what: string): 'public-key' {
    if (value !== 'public-key') {
      throw new Error(`${prefix} carried an invalid ${what}`);
    }
    return 'public-key';
  }

  function transport(value: unknown): AuthenticatorTransport {
    if (value === 'ble' || value === 'hybrid' || value === 'internal' || value === 'nfc' || value === 'usb') {
      return value;
    }
    throw new Error(`${prefix} carried an invalid credential transport`);
  }

  /**
   * descriptors parses a credential descriptor list (`excludeCredentials`,
   * `allowCredentials`). Absent is absent; anything else is a list whose
   * every entry has an id, the public-key type and, where given, only known
   * transports, all preserved. A malformed entry refuses the whole ceremony.
   */
  function descriptors(value: unknown, what: string): PublicKeyCredentialDescriptor[] | undefined {
    if (value === undefined) {
      return undefined;
    }
    if (!Array.isArray(value)) {
      throw new Error(`${prefix} carried invalid ${what}s`);
    }
    return value.map((entry, index) => {
      const name = `${what} ${String(index + 1)}`;
      const source = record(entry, name);
      const id = requiredString(source['id'], `${name} id`);
      const transports = source['transports'];
      if (transports !== undefined && !Array.isArray(transports)) {
        throw new Error(`${prefix} carried invalid ${name} transports`);
      }
      return {
        id: fromBase64URL(id),
        type: publicKeyType(source['type'], `${name} type`),
        ...(transports === undefined ? {} : { transports: transports.map(transport) }),
      };
    });
  }

  function userVerification(value: unknown): UserVerificationRequirement | undefined {
    if (value === undefined) {
      return undefined;
    }
    if (value === 'discouraged' || value === 'preferred' || value === 'required') {
      return value;
    }
    throw new Error(`${prefix} carried an invalid user-verification policy`);
  }

  return { record, requiredString, publicKeyType, transport, descriptors, userVerification };
}
