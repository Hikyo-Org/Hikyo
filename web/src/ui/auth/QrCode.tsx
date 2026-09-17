import qrcode from 'qrcode-generator';
import { useMemo } from 'react';

/**
 * QrCode renders `value` as a scannable QR built as inline SVG. Inline and not
 * an `<img src="data:…">` because the CSP's `img-src 'self'` forbids data-URL
 * images. The modules are one `<path>`, painted black on white regardless of
 * theme (a scanner needs the contrast); `.totp-qr` carries
 * `forced-color-adjust: none` so the OS cannot repaint it unscannable.
 *
 * Moved here from routes/AccountSecurity.tsx for the enrolment step; the
 * route keeps its copy until the migration swaps it for this one.
 */
export function QrCode({ value, title }: { value: string; title: string }) {
  const { path, count } = useMemo(() => {
    const qr = qrcode(0, 'M');
    qr.addData(value);
    qr.make();
    const modules = qr.getModuleCount();
    let d = '';
    for (let row = 0; row < modules; row += 1) {
      for (let col = 0; col < modules; col += 1) {
        if (qr.isDark(row, col)) {
          d += `M${String(col)} ${String(row)}h1v1h-1z`;
        }
      }
    }
    return { path: d, count: modules };
  }, [value]);

  const quiet = 4;
  const box = count + quiet * 2;
  return (
    <svg
      className="totp-qr"
      viewBox={`0 0 ${String(box)} ${String(box)}`}
      width="176"
      height="176"
      role="img"
      aria-label={title}
      shapeRendering="crispEdges"
    >
      <rect width={box} height={box} fill="#ffffff" />
      <path d={path} transform={`translate(${String(quiet)} ${String(quiet)})`} fill="#000000" />
    </svg>
  );
}
