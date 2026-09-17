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

// The argument must be one string literal. Whitespace, a line break and a
// trailing comma around it are fine; a variable, template or concatenation is
// not, and a call the literal form cannot see fails the build by name rather
// than silently missing from the export and the missing-node check.
const designCall = /\bdesign\(\s*(['"])([^'"]*)\1\s*,?\s*\)/g;
const anyDesignCall = /\bdesign\(/g;

export type DesignSource = { path: string; text: string };

export function collectDesignNodes(sources: DesignSource[]): string[] {
  const found = new Set<string>();
  for (const { path, text } of sources) {
    const literal = [...text.matchAll(designCall)];
    const every = [...text.matchAll(anyDesignCall)];
    if (every.length !== literal.length) {
      throw new Error(`${path}: ${every.length - literal.length} design() call(s) do not pass a single string literal, so the export cannot see them`);
    }
    for (const match of literal) {
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

// A node's identity in the document. `children` is otherwise unknown on
// purpose: nothing here interprets nodes, only their ids.
const penNode: z.ZodType<{ id?: string; children?: unknown[] }> = z.looseObject({
  id: z.string().optional(),
  children: z.array(z.unknown()).optional(),
});

/** Every node id that appears more than once in the document tree, sorted. */
export function findDuplicateNodeIds(penJson: string): string[] {
  const doc = z.looseObject({ children: z.array(z.unknown()).default([]) }).parse(JSON.parse(penJson));
  const seen = new Set<string>();
  const duplicates = new Set<string>();
  const walk = (nodes: unknown[]) => {
    for (const raw of nodes) {
      const node = penNode.parse(raw);
      if (node.id !== undefined) {
        if (seen.has(node.id)) duplicates.add(node.id);
        seen.add(node.id);
      }
      if (node.children !== undefined) walk(node.children);
    }
  };
  walk(doc.children);
  return [...duplicates].sort();
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

const penDocument = z.object({
  themes: z.record(z.string(), z.array(z.string())),
  variables: z.record(z.string(), penVariable).default({}),
});

export type PenSides = Map<string, { type: 'color' | 'number' | 'string'; light: string; dark: string }>;

export function parsePenVariables(penJson: string): PenSides {
  const doc = penDocument.parse(JSON.parse(penJson));
  // The reader takes the FIRST declared mode as the document default and the
  // untheme'd entry of a variable as that default's value, applying the
  // entries in order. The mirror below assumes default = Dark (the app's
  // default theme, DESIGN.md), so the declaration has to say exactly that: a
  // reordered or extra mode would render one thing while this reports another.
  if (JSON.stringify(doc.themes['Mode']) !== '["Dark","Light"]' || Object.keys(doc.themes).length !== 1) {
    throw new Error(`hikyo.pen: themes must be exactly {"Mode":["Dark","Light"]} (the first mode is the default the export renders), got ${JSON.stringify(doc.themes)}`);
  }
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
    // An explicit Dark entry beside the untheme'd default is applied after it
    // by the reader and would win in Dark mode while this mirror reported the
    // default: ambiguous, so refused. One entry per mode, no more.
    const explicitDark = v.value.filter((e) => e.theme?.Mode === 'Dark').length;
    const defaults = v.value.filter((e) => e.theme === undefined).length;
    const lights = v.value.filter((e) => e.theme?.Mode === 'Light');
    if (explicitDark > 0 || defaults > 1 || lights.length > 1) {
      throw new Error(`hikyo.pen: variable ${name} must have one untheme'd (Dark) entry and at most one Light entry, and no explicit Dark entry`);
    }
    const light = lights[0] ?? dark;
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
      const expectedFor = (value: string) => (kind === 'number' ? cssNumber(value) : value);
      // Each mode against ITS OWN css value, one message per drifting mode like
      // the colour branch: a mode that is right must not hide behind one that
      // is wrong, and a light-only css override must not be compared to dark.
      const modes: [string, string, string][] = [
        ['dark', expectedFor(expectedDark), p.dark],
        ['light', expectedFor(expectedLight), p.light],
      ];
      for (const [mode, expected, actual] of modes) {
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
    } else {
      // Only colours are seeded per mode. A non-colour that differs between
      // the themes has no supported mirror here, and seeding the dark value
      // alone would silently drop the light one, so it is refused instead.
      if (light !== dark) {
        throw new Error(`tokens.css: ${name} differs between dark (${dark}) and light (${light}); only colour tokens are mirrored per mode`);
      }
      variables[name] = kind === 'number' ? { type: 'number', value: Number(cssNumber(dark)) } : { type: 'string', value: dark };
    }
  }
  return variables;
}
