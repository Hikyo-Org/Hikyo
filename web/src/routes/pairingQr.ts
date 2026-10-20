import { MAX_INVITATION_LENGTH } from './pairingInvitation.ts';

export const MAX_QR_FILE_BYTES = 8 * 1_024 * 1_024;
export const MAX_QR_IMAGE_PIXELS = 16_000_000;
const MAX_SCAN_EDGE = 1_280;

/** Validate raster dimensions before asking the browser to allocate decoded
 * pixels. SVG and other formats with external resources are never accepted. */
async function validateQrImage(file: File) {
  if (file.size === 0 || file.size > MAX_QR_FILE_BYTES) throw new Error('Choose a PNG or JPEG image smaller than 8 MB.');
  const data = new DataView(await file.arrayBuffer());
  let width = 0;
  let height = 0;
  if (data.byteLength >= 24 && data.getUint32(0) === 0x89504e47 && data.getUint32(4) === 0x0d0a1a0a && data.getUint32(12) === 0x49484452) {
    width = data.getUint32(16); height = data.getUint32(20);
  } else if (data.byteLength >= 4 && data.getUint16(0) === 0xffd8) {
    let offset = 2;
    while (offset + 4 <= data.byteLength) {
      if (data.getUint8(offset++) !== 0xff) break;
      while (offset < data.byteLength && data.getUint8(offset) === 0xff) offset += 1;
      if (offset + 3 > data.byteLength) break;
      const marker = data.getUint8(offset++);
      if (marker === 0xda || marker === 0xd9) break;
      if (marker === 0x01 || (marker >= 0xd0 && marker <= 0xd7)) continue;
      const size = data.getUint16(offset);
      if (size < 2 || offset + size > data.byteLength) break;
      if ([0xc0, 0xc1, 0xc2, 0xc3, 0xc5, 0xc6, 0xc7, 0xc9, 0xca, 0xcb, 0xcd, 0xce, 0xcf].includes(marker) && size >= 8) {
        height = data.getUint16(offset + 3); width = data.getUint16(offset + 5); break;
      }
      offset += size;
    }
  }
  if (width === 0 || height === 0) throw new Error('Choose a valid PNG or JPEG QR image. You can also paste the invitation.');
  if (width * height > MAX_QR_IMAGE_PIXELS) throw new Error('This image is too large. Choose an image with at most 16 million pixels.');
}

export async function decodeQrPixels(data: Uint8ClampedArray, width: number, height: number) {
  const { default: jsQR } = await import('jsqr');
  const result = jsQR(data, width, height, { inversionAttempts: 'attemptBoth' });
  if (result === null) return null;
  if (result.data.length > MAX_INVITATION_LENGTH) throw new Error('This QR code is not a pairing invitation.');
  return result.data;
}

export function qrFrame(source: CanvasImageSource, width: number, height: number) {
  const scale = Math.min(1, MAX_SCAN_EDGE / Math.max(width, height));
  const canvas = document.createElement('canvas');
  canvas.width = Math.max(1, Math.round(width * scale));
  canvas.height = Math.max(1, Math.round(height * scale));
  const context = canvas.getContext('2d', { willReadFrequently: true });
  if (context === null) throw new Error('Image reading is unavailable. Paste the invitation instead.');
  context.drawImage(source, 0, 0, canvas.width, canvas.height);
  const pixels = context.getImageData(0, 0, canvas.width, canvas.height);
  // Release the backing canvas immediately. Only bounded pixels reach jsQR.
  canvas.width = 0; canvas.height = 0;
  return pixels;
}

export async function readQrImage(file: File, current: () => boolean) {
  await validateQrImage(file);
  if (!current()) return null;
  if (typeof createImageBitmap !== 'function') throw new Error('Image reading is unavailable in this browser. Paste the invitation instead.');
  // Decode the bounded original with its EXIF orientation, then scale from the
  // bitmap's oriented dimensions. Header dimensions can have the axes swapped.
  const bitmap = await createImageBitmap(file);
  try {
    if (!current()) return null;
    const pixels = qrFrame(bitmap, bitmap.width, bitmap.height);
    return await decodeQrPixels(pixels.data, pixels.width, pixels.height);
  } finally { bitmap.close(); }
}
