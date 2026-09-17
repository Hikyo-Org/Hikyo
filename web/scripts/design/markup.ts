// Pure scanner behind `design:check`'s markup leg. No I/O here.
//
// Route markup adheres when it does not hand-write what a web/src/ui atom
// emits. The banned list is the "Done when" of issue #762; add a pattern when
// an atom lands.
//
// A site that stays raw by ruling carries a `markup-check:` comment, and the
// comment rules exactly one thing:
//
//   - on a line that also holds markup, it rules that line;
//   - alone on its line (a `{/* */}`, `/* */` or `//` comment), it rules the
//     single element that starts on the next non-blank line, through that
//     element's close, which is how one ruling covers a radiogroup's two
//     inputs.
//
// It never reaches a sibling: the element after the ruled one is checked
// again. A marker at column 0 would rule a whole module, so it is itself a
// failure. The wording is the reviewer's business, not this script's: any
// `markup-check:` allowlists what it rules, so no allowlist hides in here.
//
// Two limits worth knowing. Only double-quoted `className="..."` literals are
// scanned, so a computed `className={...}` is invisible. And a ruled element
// is followed by tag depth, which a generic type argument or a `<` comparison
// inflates, so the ruling is also bounded by indentation: it ends at the first
// non-blank line at or below the opener's own indent. That closing line is
// still pattern-scanned, because it is a sibling the ruling never covered.

export type MarkupHit = { line: number; text: string } & (
  | { atom: string; note?: undefined }
  | { atom?: undefined; note: string }
);

/** A whole class token inside a className literal: `"notice machine__policy"` is a notice. */
const cls = (name: string) => new RegExp(`className="(?:[^"]*\\s)?${name}(?:[\\s-][^"]*)?"`);

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
  { pattern: cls('settings-tag'), atom: 'ui/Button variant="quiet"' },
  { pattern: cls('chip'), atom: 'ui/Badge or ui/ChoiceGroup' },
  { pattern: /[🔒🔗✓✕Δ◌⋯]/u, atom: 'ui/Glyph' },
];

const MARKER = /markup-check:/;
/** The marker owns the line: nothing but a comment opens it. */
const ALONE = /^(?:\{\s*)?(?:\/\/|\/\*)/;
const indent = (line: string) => line.length - line.trimStart().length;
const count = (line: string, pattern: RegExp) => line.match(pattern)?.length ?? 0;

/** Tags opened minus tags closed on one line, so an element can be followed to its end. */
const depthDelta = (line: string) =>
  count(line, /<[A-Za-z]/g) - count(line, /<\//g) - count(line, /\/>/g);

type State =
  | { kind: 'open' }
  | { kind: 'comment'; line: boolean }
  | { kind: 'awaiting' }
  | { kind: 'element'; depth: number; opener: number };

export function scanMarkup(lines: readonly string[]): MarkupHit[] {
  const hits: MarkupHit[] = [];
  let state: State = { kind: 'open' };
  for (const [i, raw] of lines.entries()) {
    const line = raw;
    const hit = (atom: string) => hits.push({ line: i + 1, atom, text: line.trim() });

    if (MARKER.test(line)) {
      if (indent(line) === 0) {
        hits.push({
          line: i + 1,
          text: line.trim(),
          note: 'a marker at column 0 rules the whole file; indent it with the element it rules',
        });
      }
      const alone = ALONE.test(line.trim());
      const openBlock = line.includes('/*') && !line.includes('*/');
      state = alone
        ? openBlock || line.trim().startsWith('//')
          ? { kind: 'comment', line: !openBlock }
          : { kind: 'awaiting' }
        : { kind: 'open' };
      continue;
    }

    if (state.kind === 'comment') {
      // A `//` run ends at the first line that does not continue it; a `/* */`
      // block ends on its closing line.
      if (state.line) {
        if (line.trim().startsWith('//')) continue;
        state = { kind: 'awaiting' };
      } else {
        if (line.includes('*/')) state = { kind: 'awaiting' };
        continue;
      }
    }

    if (state.kind === 'awaiting') {
      if (line.trim() === '') continue;
      const depth = depthDelta(line);
      state = depth > 0 ? { kind: 'element', depth, opener: indent(line) } : { kind: 'open' };
      continue;
    }

    if (state.kind === 'element') {
      const depth: number = state.depth + depthDelta(line);
      // The tag count reaching zero is the element's own closing tag: over, and
      // the line belongs to the ruled element, so it is not scanned.
      if (depth <= 0) {
        state = { kind: 'open' };
        continue;
      }
      // Back at or below the opener's indent with tags still nominally open is
      // an inflated count, not a nesting: the element ended and THIS line is a
      // sibling, so it falls through to the patterns like any unruled line.
      if (line.trim() === '' || indent(line) > state.opener) {
        state = { kind: 'element', depth, opener: state.opener };
        continue;
      }
      state = { kind: 'open' };
    }

    for (const { pattern, atom } of BANNED) if (pattern.test(line)) hit(atom);
  }
  return hits;
}
