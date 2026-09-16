import { describe, expect, it } from 'vitest';

import {
  collectDesignNodes,
  compareTokens,
  cssToPenVariables,
  parsePenVariables,
  parseQueryOutput,
  parseTokensCss,
  slugFor,
} from './lib.ts';

const css = `
:root {
  --bg: oklch(0.19 0.012 220);
  --accent-soft: color-mix(in oklch, var(--accent) 14%, transparent);
  --radius-control: 4px;
  --font-ui: 'Instrument Sans Variable', system-ui;
  --ease: cubic-bezier(0.25, 1, 0.5, 1);
  --dur: 180ms;
}
:root[data-theme='light'] {
  color-scheme: light;
  --bg: oklch(0.965 0.008 200);
}
`;

const pen = JSON.stringify({
  version: '2.8',
  children: [],
  themes: { Mode: ['Light', 'Dark'] },
  variables: {
    '--bg': {
      type: 'color',
      value: [{ value: '#f4f4f2' }, { value: '#22272d', theme: { Mode: 'Dark' } }],
    },
    '--radius-control': { type: 'number', value: 4 },
  },
});

describe('slugFor', () => {
  it('replaces slashes', () => {
    expect(slugFor('Button/Primary')).toBe('Button--Primary');
  });
});

describe('collectDesignNodes', () => {
  it('finds design() calls, dedupes, sorts', () => {
    const a = `parameters: { design: design('Button/Primary') }`;
    const b = `design('Badge/Danger')\n design('Button/Primary')`;
    expect(collectDesignNodes([a, b])).toEqual(['Badge/Danger', 'Button/Primary']);
  });
  it('rejects a malformed path', () => {
    expect(() => collectDesignNodes([`design('Button')`])).toThrow(/Button/);
  });
});

describe('parseQueryOutput', () => {
  it('parses the CLI json', () => {
    const out = JSON.stringify([{ id: 'T3Um0', name: 'Button/Primary', type: 'COMPONENT', x: 1, y: 2, width: 3, height: 4 }]);
    expect(parseQueryOutput(out)).toEqual([{ id: 'T3Um0', name: 'Button/Primary' }]);
  });
});

describe('parseTokensCss', () => {
  it('splits dark defaults and light overrides, skipping derived tokens', () => {
    const t = parseTokensCss(css);
    expect(t.dark.get('--bg')).toBe('oklch(0.19 0.012 220)');
    expect(t.light.get('--bg')).toBe('oklch(0.965 0.008 200)');
    expect(t.dark.get('--radius-control')).toBe('4px');
    expect(t.dark.has('--accent-soft')).toBe(false);
    expect(t.dark.has('--font-ui')).toBe(false);
    expect(t.dark.has('--ease')).toBe(false);
    expect(t.dark.has('--dur')).toBe(false);
  });
});

describe('parsePenVariables', () => {
  it('reads per-mode values', () => {
    const v = parsePenVariables(pen);
    expect(v.get('--bg')).toEqual({ type: 'color', light: '#f4f4f2', dark: '#22272d' });
    expect(v.get('--radius-control')).toEqual({ type: 'number', light: '4', dark: '4' });
  });
});

describe('compareTokens', () => {
  it('is clean when pen mirrors css', () => {
    const t = parseTokensCss(css);
    const v = parsePenVariables(JSON.stringify({
      version: '2.8', children: [], themes: { Mode: ['Light', 'Dark'] },
      variables: cssToPenVariables(t),
    }));
    expect(compareTokens(t, v)).toEqual([]);
  });
  it('reports a drifted colour with the css value as expected', () => {
    const t = parseTokensCss(css);
    const v = parsePenVariables(pen);
    const errors = compareTokens(t, v);
    expect(errors.some((e) => e.includes('--bg') && e.includes('oklch(0.19 0.012 220)'))).toBe(true);
  });
  it('reports a missing token', () => {
    const t = parseTokensCss(css);
    const v = parsePenVariables(JSON.stringify({ version: '2.8', children: [], themes: { Mode: ['Light', 'Dark'] }, variables: {} }));
    expect(compareTokens(t, v)).toContain('--bg: missing in hikyo.pen');
  });
  it('reports a number mismatch exactly', () => {
    const t = parseTokensCss(css);
    const v = parsePenVariables(JSON.stringify({
      version: '2.8', children: [], themes: { Mode: ['Light', 'Dark'] },
      variables: { ...cssToPenVariables(t), '--radius-control': { type: 'number', value: 5 } },
    }));
    expect(compareTokens(t, v)).toContain('--radius-control: expected 4 (tokens.css), got 5 (hikyo.pen)');
  });
});
