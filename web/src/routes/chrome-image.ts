/**
 * Chrome identity images: what the picker accepts and what is uploaded.
 *
 * The server stores PNG, JPEG and WebP only and never sees SVG. An SVG pick is
 * rasterised here, in the browser, onto a fixed transparent canvas and sent as
 * PNG. The conversion loads the SVG through an `<img>` from a blob URL, which
 * is why the CSP's `img-src` admits `blob:` (never `data:`).
 */

export const MAX_CHROME_IMAGE_BYTES = 256 * 1024;
export const MAX_SVG_SOURCE_BYTES = 1024 * 1024;
export const SVG_RASTER_SIZE = 512;
export const CHROME_IMAGE_ACCEPT = 'image/png,image/jpeg,image/webp,image/svg+xml';

const STORED_TYPES: readonly string[] = ['image/png', 'image/jpeg', 'image/webp'];

export type PreparedImage =
  | { readonly ok: true; readonly blob: Blob; readonly contentType: string }
  | { readonly ok: false; readonly message: string };

type SvgSource =
  | { readonly ok: true; readonly markup: string; readonly width: number; readonly height: number }
  | { readonly ok: false; readonly message: string };

export type SvgRasteriser = (source: {
  readonly markup: string;
  readonly width: number;
  readonly height: number;
}) => Promise<Blob>;

const tooLarge = `The image is larger than ${String(MAX_CHROME_IMAGE_BYTES / 1024)} KiB. Choose a smaller file.`;

function positive(value: number): boolean {
  return Number.isFinite(value) && value > 0;
}

/**
 * normaliseSvg validates the markup and pins an explicit pixel size from the
 * viewBox, because an SVG without one rasterises at an engine-chosen size (or
 * not at all).
 */
export function normaliseSvg(text: string): SvgSource {
  const document = new DOMParser().parseFromString(text, 'image/svg+xml');
  const root = document.documentElement;
  if (document.getElementsByTagName('parsererror').length > 0 || root.localName !== 'svg') {
    return { ok: false, message: 'This file is not a valid SVG image.' };
  }
  const box = (root.getAttribute('viewBox') ?? '').trim().split(/[\s,]+/).map(Number);
  const viewWidth = box[2] ?? Number.NaN;
  const viewHeight = box[3] ?? Number.NaN;
  const width = positive(viewWidth) && box.length === 4 ? viewWidth : Number.parseFloat(root.getAttribute('width') ?? '');
  const height = positive(viewHeight) && box.length === 4 ? viewHeight : Number.parseFloat(root.getAttribute('height') ?? '');
  if (!positive(width) || !positive(height)) {
    return { ok: false, message: 'This SVG has no viewBox or size, so it cannot be converted.' };
  }
  root.setAttribute('width', String(width));
  root.setAttribute('height', String(height));
  return { ok: true, markup: new XMLSerializer().serializeToString(root), width, height };
}

/** rasteriseSvg draws the SVG contained on a transparent square canvas and returns PNG. */
export const rasteriseSvg: SvgRasteriser = async ({ markup, width, height }) => {
  const url = URL.createObjectURL(new Blob([markup], { type: 'image/svg+xml' }));
  try {
    const image = new Image();
    await new Promise<void>((resolve, reject) => {
      image.addEventListener('load', () => resolve());
      image.addEventListener('error', () => reject(new Error('The SVG could not be rendered.')));
      image.src = url;
    });
    const canvas = document.createElement('canvas');
    canvas.width = SVG_RASTER_SIZE;
    canvas.height = SVG_RASTER_SIZE;
    const context = canvas.getContext('2d');
    if (context === null) throw new Error('This browser cannot convert SVG images.');
    const scale = Math.min(SVG_RASTER_SIZE / width, SVG_RASTER_SIZE / height);
    const drawnWidth = width * scale;
    const drawnHeight = height * scale;
    context.drawImage(
      image,
      (SVG_RASTER_SIZE - drawnWidth) / 2,
      (SVG_RASTER_SIZE - drawnHeight) / 2,
      drawnWidth,
      drawnHeight,
    );
    // A tainted canvas throws here rather than at drawImage.
    return await new Promise<Blob>((resolve, reject) => {
      try {
        canvas.toBlob((blob) => {
          if (blob === null) reject(new Error('The SVG could not be converted to PNG.'));
          else resolve(blob);
        }, 'image/png');
      } catch {
        reject(new Error('The SVG could not be converted to PNG.'));
      }
    });
  } finally {
    URL.revokeObjectURL(url);
  }
};

/**
 * prepareChromeImage turns a picked file into the bytes to upload, or a
 * message to show. Size is checked before anything is read where the browser
 * already knows it.
 */
export async function prepareChromeImage(
  file: File,
  rasterise: SvgRasteriser = rasteriseSvg,
): Promise<PreparedImage> {
  if (file.type === 'image/svg+xml') {
    if (file.size > MAX_SVG_SOURCE_BYTES) {
      return { ok: false, message: 'The SVG is larger than 1 MiB. Choose a smaller file.' };
    }
    const source = normaliseSvg(await file.text());
    if (!source.ok) return source;
    let png: Blob;
    try {
      png = await rasterise(source);
    } catch (error) {
      return { ok: false, message: error instanceof Error ? error.message : 'The SVG could not be converted to PNG.' };
    }
    if (png.size > MAX_CHROME_IMAGE_BYTES) {
      return { ok: false, message: `The converted PNG is larger than ${String(MAX_CHROME_IMAGE_BYTES / 1024)} KiB. Simplify the SVG.` };
    }
    return { ok: true, blob: png, contentType: 'image/png' };
  }
  if (!STORED_TYPES.includes(file.type)) {
    return { ok: false, message: 'Choose a PNG, JPEG, WebP or SVG image.' };
  }
  if (file.size > MAX_CHROME_IMAGE_BYTES) return { ok: false, message: tooLarge };
  return { ok: true, blob: file, contentType: file.type };
}
