import { describe, expect, it } from 'vitest';

import { scanRawSizes } from './adherence.ts';

describe('scanRawSizes', () => {
  it('flags literal sizes on covered properties and nothing else', () => {
    const css = `
      .a { min-height: 44px; }
      .b { min-height: var(--control); }
      .c { border: 1px solid var(--line); }
      .d { gap: 12px 16px; }
      .e { padding: 0 var(--space-3); }
      .f { font-size: 13px; }
      .g { outline-offset: 2px; }
      .h { border-radius: 4px; }
      .i { z-index: 30; }
      .j { z-index: var(--z-drawer); }
      .k { box-shadow: 0 14px 42px oklch(0 0 0 / 45%); }
      .l { width: min(520px, 100%); }
      .m { margin: 0; }
      .n { padding: 1px 6px; }
    `;
    const found = scanRawSizes(css).map((r) => `${r.property}=${r.value}`);
    expect(found).toEqual([
      'min-height=44px',
      'gap=12px 16px',
      'font-size=13px',
      'border-radius=4px',
      'z-index=30',
      'box-shadow=0 14px 42px oklch(0 0 0 / 45%)',
      'width=min(520px, 100%)',
      'padding=1px 6px',
    ]);
  });

  it('ignores token definitions and comments', () => {
    const css = `
      :root { --control: 36px; }
      /* height: 44px; */
      .a { height: var(--control); /* was 44px */ }
    `;
    expect(scanRawSizes(css)).toEqual([]);
  });

  it('reports the line number', () => {
    const css = 'x {\n  color: red;\n  gap: 8px;\n}';
    expect(scanRawSizes(css)).toEqual([{ line: 3, property: 'gap', value: '8px' }]);
  });
});
