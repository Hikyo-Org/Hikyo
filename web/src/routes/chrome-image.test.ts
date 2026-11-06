// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest';

import { MAX_CHROME_IMAGE_BYTES, normaliseSvg, prepareChromeImage } from './chrome-image.ts';

const svg = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 20"><rect width="40" height="20"/></svg>';

function file(parts: BlobPart[], type: string): File {
  return new File(parts, 'x', { type });
}

describe('normaliseSvg', () => {
  it('pins an explicit size from the viewBox', () => {
    const result = normaliseSvg(svg);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect([result.width, result.height]).toEqual([40, 20]);
    expect(result.markup).toContain('width="40"');
    expect(result.markup).toContain('height="20"');
  });

  it('refuses malformed markup and a missing size', () => {
    expect(normaliseSvg('<svg').ok).toBe(false);
    expect(normaliseSvg('<html></html>').ok).toBe(false);
    expect(normaliseSvg('<svg xmlns="http://www.w3.org/2000/svg"></svg>').ok).toBe(false);
  });

  it('sizes from width and height only in px when there is no viewBox', () => {
    const sized = (width: string, height: string) =>
      normaliseSvg(`<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}"/>`);
    expect(sized('24', '32px')).toMatchObject({ ok: true, width: 24, height: 32 });
    for (const [width, height] of [['2in', '2in'], ['50%', '50%'], ['10em', '10'], ['10px5', '10'], ['-4', '4']]) {
      expect(sized(width ?? '', height ?? '').ok, `${String(width)} x ${String(height)}`).toBe(false);
    }
  });
});

describe('prepareChromeImage', () => {
  it('passes a stored type through unchanged', async () => {
    const png = file([new Uint8Array(10)], 'image/png');
    const result = await prepareChromeImage(png);
    expect(result).toMatchObject({ ok: true, contentType: 'image/png' });
  });

  it('refuses an over-limit raster, an unknown type and an oversized SVG', async () => {
    const big = file([new Uint8Array(MAX_CHROME_IMAGE_BYTES + 1)], 'image/webp');
    expect((await prepareChromeImage(big)).ok).toBe(false);
    expect((await prepareChromeImage(file(['x'], 'image/gif'))).ok).toBe(false);
    const huge = file([new Uint8Array(1024 * 1024 + 1)], 'image/svg+xml');
    expect((await prepareChromeImage(huge)).ok).toBe(false);
  });

  it('converts an SVG through the rasteriser and uploads PNG', async () => {
    const rasterise = vi.fn(() => Promise.resolve(new Blob([new Uint8Array(5)], { type: 'image/png' })));
    const result = await prepareChromeImage(file([svg], 'image/svg+xml'), rasterise);
    expect(result).toMatchObject({ ok: true, contentType: 'image/png' });
    expect(rasterise).toHaveBeenCalledWith(expect.objectContaining({ width: 40, height: 20 }));
  });

  it('shows a refusal when conversion fails or yields an oversized PNG', async () => {
    const failing = await prepareChromeImage(file([svg], 'image/svg+xml'), () => Promise.reject(new Error('tainted')));
    expect(failing).toEqual({ ok: false, message: 'tainted' });
    const bloated = await prepareChromeImage(file([svg], 'image/svg+xml'), () =>
      Promise.resolve(new Blob([new Uint8Array(MAX_CHROME_IMAGE_BYTES + 1)])),
    );
    expect(bloated.ok).toBe(false);
  });
});
