// Pure scanner behind `design:check`'s adherence leg. No I/O here.
//
// A stylesheet adheres to the design system when every size it states comes
// from a token in src/styles/tokens.css. This finds the declarations that do
// not: a literal pixel value on a property a token family covers. Hairlines
// (1px) and the 2px focus ring are DESIGN.md constants, not drift, and any
// value inside var() or a token definition is the token itself.

/** Properties a token family covers: heights and widths, type, spacing, shape, shadow. */
const COVERED = new Set([
  'font-size',
  'letter-spacing',
  'min-height',
  'height',
  'max-height',
  'min-width',
  'width',
  'max-width',
  'gap',
  'row-gap',
  'column-gap',
  'padding',
  'padding-top',
  'padding-right',
  'padding-bottom',
  'padding-left',
  'padding-inline',
  'padding-block',
  'margin',
  'margin-top',
  'margin-right',
  'margin-bottom',
  'margin-left',
  'margin-inline',
  'margin-block',
  'border-radius',
  'box-shadow',
  'z-index',
]);

const EXEMPT = new Set(['0', '1px', '2px', '-1px', '-2px', 'auto', 'none', '100%']);

export type RawSize = { line: number; property: string; value: string };

/** Every covered declaration whose value carries a literal px (or, for z-index, a literal integer). */
export function scanRawSizes(css: string): RawSize[] {
  const out: RawSize[] = [];
  // Blank out comments (keeping newlines) so a documented literal does not count.
  const stripped = css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '));
  // A declaration: property, colon, value, then `;` or the block's `}`. The
  // lookahead is what keeps selector pseudo-classes (`a:not(...) {`) out.
  const declaration = /([a-z-]+)\s*:\s*([^;{}]+)(?=[;}])/g;
  for (const m of stripped.matchAll(declaration)) {
    const property = m[1] ?? '';
    const value = (m[2] ?? '').trim();
    if (property.startsWith('--') || !COVERED.has(property)) continue;
    const withoutVars = value.replace(/var\([^)]*\)/g, '');
    const parts = withoutVars.split(/\s+/).filter((part) => part !== '');
    const literal =
      property === 'z-index'
        ? parts.some((part) => /^-?\d+$/.test(part))
        : parts.some((part) => /-?\d*\.?\d+px/.test(part) && !EXEMPT.has(part));
    if (literal && !parts.every((part) => EXEMPT.has(part))) {
      const line = stripped.slice(0, m.index).split('\n').length;
      out.push({ line, property, value });
    }
  }
  return out;
}
