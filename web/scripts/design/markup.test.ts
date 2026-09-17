import { describe, expect, it } from 'vitest';

import { scanMarkup } from './markup.ts';

const lines = (text: string) => text.split('\n');

describe('scanMarkup', () => {
  it('flags hand-written atoms, and a class token is a whole token', () => {
    expect(scanMarkup(lines(`
      <p className="notice machine__policy" role="status">no marker</p>
      <span className="unalert">not an alert</span>
      <span className="chip chip--admin">a chip</span>
    `))).toEqual([
      { line: 2, atom: 'ui/Alert tone="done"', text: '<p className="notice machine__policy" role="status">no marker</p>' },
      { line: 4, atom: 'ui/Badge or ui/ChoiceGroup', text: '<span className="chip chip--admin">a chip</span>' },
    ]);
  });

  it('lets a marker on the line rule that line and nothing after it', () => {
    expect(scanMarkup(lines(`
      <p className="notice" role="status">{/* markup-check: not an alert */}
        ruled
      </p>
      <p className="chk">a sibling, not ruled</p>
    `))).toEqual([
      { line: 5, atom: 'ui/Checkbox', text: '<p className="chk">a sibling, not ruled</p>' },
    ]);
  });

  it('lets a standalone marker rule exactly the element that follows it', () => {
    const fixture = `
      <p className="chk">before, not ruled</p>
      {/* markup-check: rich label, two rows share the ruling */}
      <div role="radiogroup">
        <input type="radio" />

        <input type="radio" />
      </div>
      <input type="radio" />
    `;
    expect(scanMarkup(lines(fixture))).toEqual([
      { line: 2, atom: 'ui/Checkbox', text: '<p className="chk">before, not ruled</p>' },
      { line: 9, atom: 'ui/Radio', text: '<input type="radio" />' },
    ]);
    // The ruling is load-bearing: without it both rows are reported.
    expect(scanMarkup(lines(fixture.replace(/\{\/\* markup-check.*\n/, ''))).length).toBe(4);
  });

  it('follows a multi-line comment and a multi-line element to their ends', () => {
    expect(scanMarkup(lines(`
      // markup-check: the row's title disambiguates two people
      // with the same display name, so this row stays hand-written.
      return <div>{ids.map((id) => <label key={id} title={id}>
        <input type="checkbox" />
      </label>)}</div>;
    `))).toEqual([]);
  });

  it('ends a ruled element at its own indent, so a generic type cannot widen the ruling', () => {
    expect(scanMarkup(lines(`
      {/* markup-check: rich label */}
      <div role="radiogroup">
        <input type="radio" value={rows.map((r: Record<string, string>) => r.id)} />
      </div>
      <p className="chk">a sibling</p>
      <p className="alert">another sibling</p>
    `))).toEqual([
      { line: 6, atom: 'ui/Checkbox', text: '<p className="chk">a sibling</p>' },
      { line: 7, atom: 'ui/Alert', text: '<p className="alert">another sibling</p>' },
    ]);
  });

  it('keeps a multi-line opening tag inside the ruling, closing `>` and all', () => {
    expect(scanMarkup(lines(`
      {/* markup-check: rich label */}
      <div
        role="radiogroup"
      >
        <input type="radio" />
        <input type="radio" />
      </div>
      <p className="chk">a sibling</p>
    `))).toEqual([
      { line: 9, atom: 'ui/Checkbox', text: '<p className="chk">a sibling</p>' },
    ]);
  });

  it('scans the line that ends a ruling by indent: it is a sibling, not the close', () => {
    expect(scanMarkup(lines(`
      {/* markup-check: rich label */}
      <div role="radiogroup"><input type="radio" value={pick<string>(rows)} /></div>
      <p className="chk">the next line at the opener's indent</p>
    `))).toEqual([
      { line: 4, atom: 'ui/Checkbox', text: `<p className="chk">the next line at the opener's indent</p>` },
    ]);
  });

  it('reports a marker at column 0, which would rule the whole file', () => {
    expect(scanMarkup(lines(`// markup-check: everything below is fine, honest
<input type="checkbox" />`))).toEqual([
      {
        line: 1,
        note: 'a marker at column 0 rules the whole file; indent it with the element it rules',
        text: '// markup-check: everything below is fine, honest',
      },
    ]);
  });
});
