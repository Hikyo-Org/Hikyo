import { describe, expect, it } from 'vitest';

import { requestOptions } from './values.ts';
import { optionParsers } from './webauthnOptions.ts';

const bytes = (source: BufferSource): number[] =>
  Array.from(
    source instanceof ArrayBuffer
      ? new Uint8Array(source)
      : new Uint8Array(source.buffer, source.byteOffset, source.byteLength),
  );

describe('optionParsers', () => {
  const parsers = optionParsers('the test options');

  it('preserves every descriptor field, transports included', () => {
    const parsed = parsers.descriptors(
      [{ id: 'AQID', type: 'public-key', transports: ['usb', 'hybrid'] }, { id: 'BAUG', type: 'public-key' }],
      'allowed credential',
    );
    expect(parsed).toHaveLength(2);
    expect(bytes(parsed?.[0]?.id ?? new ArrayBuffer())).toEqual([1, 2, 3]);
    expect(parsed?.[0]?.transports).toEqual(['usb', 'hybrid']);
    expect(parsed?.[1]?.transports).toBeUndefined();
  });

  it('refuses a malformed descriptor instead of filtering it away', () => {
    expect(() => parsers.descriptors([{ type: 'public-key' }], 'allowed credential')).toThrow(
      /the test options carried no allowed credential 1 id/,
    );
    expect(() => parsers.descriptors([{ id: 'AQID', type: 'public-key', transports: ['carrier-pigeon'] }], 'allowed credential')).toThrow(
      /invalid credential transport/,
    );
    expect(() => parsers.descriptors([{ id: 'AQID', type: 'not-a-key' }], 'allowed credential')).toThrow(
      /invalid allowed credential 1 type/,
    );
    expect(() => parsers.descriptors('nope', 'allowed credential')).toThrow(/invalid allowed credentials/);
  });

  it('keeps every supported user-verification policy and refuses an unknown one', () => {
    expect(parsers.userVerification(undefined)).toBeUndefined();
    expect(parsers.userVerification('discouraged')).toBe('discouraged');
    expect(parsers.userVerification('preferred')).toBe('preferred');
    expect(parsers.userVerification('required')).toBe('required');
    expect(() => parsers.userVerification('sometimes')).toThrow(/invalid user-verification policy/);
  });
});

describe('requestOptions', () => {
  it('converts the assertion blob without altering its policy', () => {
    const request = requestOptions({
      publicKey: {
        challenge: 'AQID',
        rpId: 'example.test',
        timeout: 60_000,
        userVerification: 'discouraged',
        allowCredentials: [{ id: 'BAUG', type: 'public-key', transports: ['internal'] }],
      },
    });
    expect(bytes(request.challenge)).toEqual([1, 2, 3]);
    expect(request.rpId).toBe('example.test');
    expect(request.timeout).toBe(60_000);
    expect(request.userVerification).toBe('discouraged');
    expect(request.allowCredentials?.[0]?.transports).toEqual(['internal']);
    expect(bytes(request.allowCredentials?.[0]?.id ?? new ArrayBuffer())).toEqual([4, 5, 6]);
  });

  it('refuses a malformed allowed credential instead of narrowing the ceremony', () => {
    expect(() =>
      requestOptions({ challenge: 'AQID', allowCredentials: [{ type: 'public-key' }] }),
    ).toThrow(/the reauth options carried no allowed credential 1 id/);
    expect(() => requestOptions({ allowCredentials: [] })).toThrow(/the reauth options carried no challenge/);
  });
});
