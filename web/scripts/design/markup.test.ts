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

  it('reports a marker at column 0, which would rule the whole file', () => {
    expect(scanMarkup(lines(`// markup-check: everything below is fine, honest
<input type="checkbox" />`))).toEqual([
      { line: 1, atom: 'an indented marker: at column 0 it would rule the whole file', text: '// markup-check: everything below is fine, honest' },
    ]);
  });
});
