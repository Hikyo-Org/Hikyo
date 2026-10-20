import qrcode from 'qrcode-generator';
import { expect, it } from 'vitest';

import { readPairingInvitation } from './pairingInvitation.ts';
import { decodeQrPixels, MAX_QR_FILE_BYTES, readQrImage } from './pairingQr.ts';
import { pairingSample } from './RemotePairing.story-fixtures.ts';

it('decodes actual generated QR pixels with the portable decoder', async () => {
  const qr = qrcode(0, 'M'); qr.addData(pairingSample.bundle); qr.make();
  const quiet = 4; const scale = 5; const side = (qr.getModuleCount() + quiet * 2) * scale;
  const pixels = new Uint8ClampedArray(side * side * 4).fill(255);
  for (let row = 0; row < qr.getModuleCount(); row += 1) {
    for (let col = 0; col < qr.getModuleCount(); col += 1) {
      if (!qr.isDark(row, col)) continue;
      for (let y = 0; y < scale; y += 1) for (let x = 0; x < scale; x += 1) {
        const offset = (((row + quiet) * scale + y) * side + (col + quiet) * scale + x) * 4;
        pixels[offset] = 0; pixels[offset + 1] = 0; pixels[offset + 2] = 0;
      }
    }
  }
  expect(await decodeQrPixels(pixels, side, side)).toBe(pairingSample.bundle);
  expect(readPairingInvitation(pairingSample.bundle)?.origin).toBe(pairingSample.origin);
  expect(await decodeQrPixels(new Uint8ClampedArray(100 * 100 * 4).fill(255), 100, 100)).toBeNull();
});

it.each(['https://unrelated.example', 'not-an-invitation', 'a'.repeat(8_193), btoa('{"version":1}'), btoa(JSON.stringify({ version: 1, id: 'id', origin: 'http://unsafe.example', spki_pin: pairingSample.pin, capability: 'hik_1_hc_publicFixture', expires_at: '2099-01-01T00:00:00Z' }))])('rejects malformed or unrelated invitation input', (value) => {
  expect(readPairingInvitation(value)).toBeNull();
});

it('rejects resource-heavy and external-resource files before browser decoding', async () => {
  await expect(readQrImage(new File([new Uint8Array(MAX_QR_FILE_BYTES + 1)], 'large.png'), () => true)).rejects.toThrow('smaller than 8 MB');
  await expect(readQrImage(new File(['<svg><image href="https://untrusted.example"/></svg>'], 'external.svg'), () => true)).rejects.toThrow('valid PNG or JPEG');
  const png = new Uint8Array(24); const view = new DataView(png.buffer);
  view.setUint32(0, 0x89504e47); view.setUint32(4, 0x0d0a1a0a); view.setUint32(12, 0x49484452); view.setUint32(16, 100_000); view.setUint32(20, 100_000);
  await expect(readQrImage(new File([png], 'bomb.png'), () => true)).rejects.toThrow('16 million pixels');
});
