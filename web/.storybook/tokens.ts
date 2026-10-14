/** sRGB of any CSS colour the browser understands, via a 1px canvas. */
function rgba(css: string): [number, number, number, number] {
  const canvas = document.createElement('canvas');
  canvas.width = 1;
  canvas.height = 1;
  const context = canvas.getContext('2d', { willReadFrequently: true });
  if (context === null) throw new Error('no 2d context');
  context.fillStyle = css;
  context.fillRect(0, 0, 1, 1);
  const [r = 0, g = 0, b = 0, a = 0] = context.getImageData(0, 0, 1, 1).data;
  return [r, g, b, a / 255];
}

function luminance([r, g, b]: readonly number[]): number {
  const lin = (c = 0) => {
    const v = c / 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

/** WCAG contrast of an element's ink on its own fill, the fill composited over the page. */
export function inkContrast(element: Element): number {
  const style = getComputedStyle(element);
  const page = rgba(getComputedStyle(document.body).backgroundColor);
  const fill = rgba(style.backgroundColor);
  const ink = rgba(style.color);
  const over = (top: number[], alpha: number) => top.slice(0, 3).map((c, i) => c * alpha + (page[i] ?? 0) * (1 - alpha));
  const bg = over(fill, fill[3]);
  const fg = over(ink, ink[3]);
  const [hi, lo] = [luminance(fg), luminance(bg)].sort((x, y) => y - x);
  return ((hi ?? 0) + 0.05) / ((lo ?? 0) + 0.05);
}

const css = (token: string) => (token.startsWith('--') ? `var(${token})` : token);

/** A colour token (or any colour expression) resolved the way a computed `color` is. */
export function tokenColor(token: string): string {
  const probe = document.createElement('span');
  probe.style.color = css(token);
  document.body.append(probe);
  const value = getComputedStyle(probe).color;
  probe.remove();
  return value;
}

/** A length token resolved to pixels on this pointer and viewport. */
export function tokenPx(token: string): number {
  const probe = document.createElement('div');
  probe.style.height = css(token);
  document.body.append(probe);
  const value = probe.getBoundingClientRect().height;
  probe.remove();
  return value;
}
