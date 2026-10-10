import { describe, expect, it } from 'vitest';

import { scanMarkup } from './markup.ts';

const lines = (text: string) => text.split('\n');

describe('scanMarkup', () => {
  it('keeps native semantic structure and specialized controls outside the shared styling policy', () => {
    expect(scanMarkup(lines(`<section><h2>Heading</h2><p>Text</p><ul><li>Item</li></ul>
      <table><tbody><tr><td>Cell</td></tr></tbody></table>
      <form><label>Name<input name="name" /></label><button className="matrix-cell">Open cell</button></form>
    </section>`))).toEqual([]);
  });

  it('flags shared button styling across multi-line, single-quoted and computed openers', () => {
    const fixtures = [
      `<button\n  className="btn"\n  type="button"\n>Action</button>`,
      `<button className='btn btn--danger'>Action</button>`,
      `<button className={cx('btn', busy && 'is-busy')}>Action</button>`,
      `<button className={busy ? 'btn--primary' : 'btn'}>Action</button>`,
      '<button className={`btn ${extra}`}>Action</button>',
    ];
    for (const fixture of fixtures) {
      expect(scanMarkup(lines(fixture)).map((hit) => ({ line: hit.line, atom: hit.atom }))).toEqual([{ line: 1, atom: 'ui/Button' }]);
    }
    expect(scanMarkup(lines(`<button className="btnish">Specialized</button>`))).toEqual([]);
    expect(scanMarkup(lines(`<button className="special">Native <span className="btn">text</span></button>`))).toEqual([]);
    expect(scanMarkup(lines(`<button\n  onClick={() => run(3 > 2)}\n  className={'btn'}\n>Action</button>`))).toHaveLength(1);
    expect(scanMarkup(lines(`<button className="special">Native</button><button aria-label="🔒" className="btn">Styled</button>`)).filter((hit) => hit.atom === 'ui/Button')).toHaveLength(1);
  });

  it('keeps a deliberate multiline native button ruling local to its element', () => {
    expect(scanMarkup(lines(`
      {/* markup-check: special native control */}
      <button
        className="btn"
      >Specialized</button>
      <button
        className="btn"
      >Sibling</button>
    `)).map((hit) => ({ line: hit.line, atom: hit.atom }))).toEqual([{ line: 6, atom: 'ui/Button' }]);
  });

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
