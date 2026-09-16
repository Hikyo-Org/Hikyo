// Pure helpers behind the design scripts. No I/O here, so they are unit-testable.
// Node 26 runs .ts directly (type stripping), so no build step and no .mjs shim.
import { formatHex, parse } from 'culori';
import { z } from 'zod';

export const NODE_PATH = /^[A-Za-z0-9]+( [A-Za-z0-9]+)*(\/[A-Za-z0-9]+( [A-Za-z0-9]+)*)+$/;

/** Tokens that are derived (color-mix), motion, or font stacks: not mirrored in the design file. */
export const MIRRORED_SKIP = new Set(['--ease', '--dur', '--font-ui', '--font-mono']);

export function slugFor(node: string): string {
  return node.replaceAll('/', '--');
}

// The argument must be a string literal: a variable or template expression is invisible here.
const designCall = /design\((['"])([^'"]*)\1\)/g;

export function collectDesignNodes(sources: string[]): string[] {
  const found = new Set<string>();
  for (const source of sources) {
    for (const match of source.matchAll(designCall)) {
      const node = match[2];
      // The group is not optional, so this only ever satisfies the type checker.
      if (node === undefined) throw new Error('design(): regex matched without a capture group');
      if (!NODE_PATH.test(node)) {
        throw new Error(`design(): "${node}" must be Title/Variant: letters, digits and single spaces, at least two segments`);
      }
      found.add(node);
    }
  }
  return [...found].sort();
}

const queryNode = z.object({ id: z.string(), name: z.string() });

export function parseQueryOutput(json: string): { id: string; name: string }[] {
  // The non-strict object already strips the CLI's extra keys.
  return z.array(queryNode).parse(JSON.parse(json));
}

const declaration = /(--[a-z0-9-]+)\s*:\s*([^;]+);/g;

function block(css: string, header: string): string {
  const start = css.indexOf(header);
  if (start === -1) throw new Error(`tokens.css: block "${header}" not found`);
  const end = css.indexOf('}', start);
  return css.slice(start + header.length, end);
}

function isMirrored(name: string, value: string): boolean {
  return !MIRRORED_SKIP.has(name) && !value.startsWith('color-mix(');
}

export type TokenSides = { dark: Map<string, string>; light: Map<string, string> };

export function parseTokensCss(css: string): TokenSides {
  const read = (text: string) => {
    const map = new Map<string, string>();
    for (const m of text.matchAll(declaration)) {
      const name = m[1];
      const raw = m[2];
      // Neither group is optional, so this only ever satisfies the type checker.
      if (name === undefined || raw === undefined) throw new Error('tokens.css: declaration matched without a capture group');
      const value = raw.trim();
      if (isMirrored(name, value)) map.set(name, value);
    }
    return map;
  };
  const dark = read(block(css, ':root {'));
  const overrides = read(block(css, ":root[data-theme='light'] {"));
  // tokens.css states light twice: once under prefers-color-scheme for people
  // who never chose, once under the explicit opt-in. Only the second is used
  // below, so the first has to be proven identical or the mirror silently
  // tracks whichever half was edited last.
  const media = read(block(css, ":root:not([data-theme='dark']) {"));
  const differing = [...new Set([...media.keys(), ...overrides.keys()])].filter((k) => media.get(k) !== overrides.get(k)).sort();
  if (differing.length > 0) {
    throw new Error(`tokens.css: the prefers-color-scheme light block and the [data-theme='light'] block differ on ${differing.join(', ')}`);
  }
  const light = new Map(dark);
  for (const [k, v] of overrides) light.set(k, v);
  return { dark, light };
}

const penValue = z.object({
  value: z.union([z.string(), z.number()]),
  theme: z.object({ Mode: z.enum(['Light', 'Dark']) }).optional(),
});
const penVariable = z.object({
  type: z.enum(['color', 'number', 'string']),
  value: z.union([z.string(), z.number(), z.array(penValue)]),
});

/** The shape of one entry in a .pen document's `variables` block. */
export type PenVariable = z.infer<typeof penVariable>;

const penDocument = z.object({ variables: z.record(z.string(), penVariable).default({}) });

export type PenSides = Map<string, { type: 'color' | 'number' | 'string'; light: string; dark: string }>;

export function parsePenVariables(penJson: string): PenSides {
  const doc = penDocument.parse(JSON.parse(penJson));
  const out: PenSides = new Map();
  for (const [name, v] of Object.entries(doc.variables)) {
    if (!Array.isArray(v.value)) {
      out.set(name, { type: v.type, light: String(v.value), dark: String(v.value) });
      continue;
    }
    // The untheme'd entry IS the default mode, and the default mode is Dark: the
    // app's default theme is dark (DESIGN.md) and the headless renderer exports
    // whichever mode is the default, so the design file has to agree.
    const dark = v.value.find((e) => e.theme === undefined);
    if (!dark) throw new Error(`hikyo.pen: variable ${name} has no default (Dark) value`);
    const light = v.value.find((e) => e.theme?.Mode === 'Light') ?? dark;
    out.set(name, { type: v.type, light: String(light.value), dark: String(dark.value) });
  }
  return out;
}

function cssKind(value: string): 'color' | 'number' | 'string' {
  if (value.startsWith('oklch(')) return 'color';
  if (/^-?\d+(\.\d+)?px$/.test(value)) return 'number';
  return 'string';
}

function cssNumber(value: string): string {
  return value.replace(/px$/, '');
}

/** The .pen stores sRGB hex. Convert the CSS side the same way the seed does, then compare strings: deterministic, no tolerance knob. */
function cssHex(value: string): string {
  const parsed = parse(value);
  if (!parsed) throw new Error(`tokens.css: unparseable colour ${value}`);
  return formatHex(parsed).toLowerCase();
}

export function compareTokens(css: TokenSides, pen: PenSides): string[] {
  const errors: string[] = [];
  for (const [name, expectedDark] of css.dark) {
    const p = pen.get(name);
    if (!p) {
      errors.push(`${name}: missing in hikyo.pen`);
      continue;
    }
    const expectedLight = css.light.get(name);
    // The light map is dark plus overrides, so this only ever satisfies the type checker.
    if (expectedLight === undefined) throw new Error(`tokens.css: light map lacks ${name}`);
    const kind = cssKind(expectedDark);
    if (p.type !== kind) {
      errors.push(`${name}: expected type ${kind} (tokens.css), got ${p.type} (hikyo.pen)`);
      continue;
    }
    if (kind === 'color') {
      const check = (mode: string, expected: string, actual: string) => {
        if (cssHex(expected) !== actual.toLowerCase()) {
          errors.push(`${name} (${mode}): expected ${expected} = ${cssHex(expected)} (tokens.css), got ${actual} (hikyo.pen)`);
        }
      };
      check('dark', expectedDark, p.dark);
      check('light', expectedLight, p.light);
    } else {
      const expected = kind === 'number' ? cssNumber(expectedDark) : expectedDark;
      // One message per drifting mode, like the colour branch: a mode that is right must not hide behind one that is wrong.
      const modes: [string, string][] = [
        ['dark', p.dark],
        ['light', p.light],
      ];
      for (const [mode, actual] of modes) {
        if (actual !== expected) errors.push(`${name}: expected ${expected} (tokens.css), got ${actual} (hikyo.pen, ${mode})`);
      }
    }
  }
  for (const name of pen.keys()) {
    if (!css.dark.has(name)) errors.push(`${name}: present in hikyo.pen, absent from tokens.css`);
  }
  return errors;
}

/** Build the `variables` block of a .pen document from parsed tokens.css. Colours become hex, which is what the app stores. */
export function cssToPenVariables(css: TokenSides): Record<string, PenVariable> {
  const variables: Record<string, PenVariable> = {};
  for (const [name, dark] of css.dark) {
    const light = css.light.get(name);
    // The light map is dark plus overrides, so this only ever satisfies the type checker.
    if (light === undefined) throw new Error(`tokens.css: light map lacks ${name}`);
    const kind = cssKind(dark);
    if (kind === 'color') {
      variables[name] = {
        type: 'color',
        // Dark first and untheme'd: it is the default mode (see parsePenVariables).
        value: [{ value: cssHex(dark) }, { value: cssHex(light), theme: { Mode: 'Light' } }],
      };
    } else if (kind === 'number') {
      variables[name] = { type: 'number', value: Number(cssNumber(dark)) };
    } else {
      variables[name] = { type: 'string', value: dark };
    }
  }
  return variables;
}
