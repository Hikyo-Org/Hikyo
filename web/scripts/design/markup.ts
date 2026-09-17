// Pure scanner behind `design:check`'s markup leg. No I/O here.
//
// Route markup adheres when it does not hand-write what a web/src/ui atom
// emits. The banned list is the "Done when" of issue #762; add a pattern when
// an atom lands.
//
// A site that stays raw by ruling carries a `markup-check:` comment. The
// comment sits on the offending line, or on the opener of the element that
// holds it, which can be a dozen lines above (a radiogroup's two inputs share
// one ruling). So a marker covers itself and the block it sits in: every
// following line until the first non-blank one indented less than the marker.
// That over-reaches to the end of the enclosing block rather than the end of
// the marked element, which is the price of not parsing JSX. The wording is
// the reviewer's business, not this script's: any `markup-check:` allowlists,
// so no allowlist hides in here.

export type MarkupHit = { line: number; atom: string; text: string };

/** A whole class token inside a className literal: `"notice machine__policy"` is a notice. */
const cls = (name: string) => new RegExp(`className="(?:[^"]*\\s)?${name}(?:\\s[^"]*)?"`);

const BANNED: readonly { pattern: RegExp; atom: string }[] = [
  { pattern: cls('alert'), atom: 'ui/Alert' },
  { pattern: cls('notice'), atom: 'ui/Alert tone="done"' },
  { pattern: cls('chk'), atom: 'ui/Checkbox' },
  { pattern: /type="checkbox"/, atom: 'ui/Checkbox' },
  { pattern: /type="radio"/, atom: 'ui/Radio' },
  // Single-line openers only; a `<button` split over lines is not seen.
  { pattern: /<button[^>]*className="btn/, atom: 'ui/Button' },
  { pattern: cls('ceremony'), atom: 'ui/Dialog' },
  { pattern: cls('matrix-editor'), atom: 'ui/Dialog' },
  { pattern: /className="settings-tag/, atom: 'ui/Button variant="quiet"' },
  { pattern: /className="chip/, atom: 'ui/Badge or ui/ChoiceGroup' },
  { pattern: /[🔒🔗✓✕Δ◌⋯]/u, atom: 'ui/Glyph' },
];

const MARKER = /markup-check:/;
const indent = (line: string) => line.length - line.trimStart().length;

export function scanMarkup(lines: readonly string[]): MarkupHit[] {
  const hits: MarkupHit[] = [];
  // The indent of the open marker block, or null outside one.
  let scope: number | null = null;
  for (const [i, line] of lines.entries()) {
    if (scope !== null && line.trim() !== '' && indent(line) < scope) scope = null;
    if (MARKER.test(line)) scope = indent(line);
    if (scope !== null) continue;
    for (const { pattern, atom } of BANNED) {
      if (pattern.test(line)) hits.push({ line: i + 1, atom, text: line.trim() });
    }
  }
  return hits;
}
