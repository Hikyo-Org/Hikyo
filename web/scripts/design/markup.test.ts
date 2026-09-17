import { describe, expect, it } from 'vitest';

import { scanMarkup } from './markup.ts';

describe('scanMarkup', () => {
  it('flags hand-written atoms and honours a marker over the block it sits in', () => {
    const lines = [
      'function Panel() {',
      '  return (',
      '    <p className="alert" role="alert">raw</p>',
      '    <p className="notice machine__policy">{/* markup-check: skin */}',
      '      <input type="checkbox" />',
      '    </p>',
      '    {/* markup-check: rich label, two rows share the ruling */}',
      '    <div role="radiogroup">',
      '      <input type="radio" />',
      '',
      '      <input type="radio" />',
      '    </div>',
      '  );',
      '}',
      '<input type="radio" />',
    ];
    expect(scanMarkup(lines)).toEqual([
      { line: 3, atom: 'ui/Alert', text: '<p className="alert" role="alert">raw</p>' },
      { line: 15, atom: 'ui/Radio', text: '<input type="radio" />' },
    ]);
  });
});
