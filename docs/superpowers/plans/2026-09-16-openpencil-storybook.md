# OpenPencil ↔ Storybook Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Link every Storybook story to a node in an in-repo OpenPencil design file, render that node next to the story, open it in the app with one click, and fail CI when design tokens drift from `tokens.css`.

**Architecture:** One design file `web/design/hikyo.pen` (JSON) mirrors `web/src/styles/tokens.css` as variables. Stories declare `parameters.design = design('Title/Variant')`. A build script resolves those names with the headless `openpencil` CLI and exports PNGs into a gitignored dir that `@storybook/addon-designs` shows. A manager toolbar button opens the node through a dev-only Vite middleware (interim) or an `openpencil://` link (production, upstream plan). A token check script parses both sides and compares in OKLCH.

**Tech Stack:** Storybook 10.6 (react-vite), Vitest 4, Zod 4, `@open-pencil/cli` 0.14.0, `@open-pencil/mcp` 0.14.0, `@storybook/addon-designs` 11.1.4, `culori`.

**Spec:** `docs/superpowers/specs/2026-09-16-openpencil-storybook-design.md`

## Global Constraints

- Every commit DCO signed: `git commit -s`. Commit trailer `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- No `as` casts. No `z.any`/`z.unknown` where the shape is known. All `JSON.parse` goes through Zod.
- Fail fast: unresolved design node, malformed parameter, token drift, unreachable font provider = exit 1 with the offending name printed. No fallbacks.
- No em-dash in any user-visible text or docs prose.
- Design file path is fixed: `web/design/hikyo.pen`. Exports go to `web/design/exports/` (gitignored, served at `/design/`).
- Node names are `Title/Variant` paths; slug = path with `/` replaced by `--`.
- The OpenPencil RPC bearer token never reaches browser code.
- Pinned deps: `@open-pencil/cli@0.14.0`, `@open-pencil/mcp@0.14.0`, `@storybook/addon-designs@11.1.4`, `culori@4.x` (pin the exact version `pnpm add` resolves).
- Working directory for all `pnpm` commands: `web/`. First run needs `pnpm --dir clients/ts install --frozen-lockfile && pnpm --dir web install --frozen-lockfile` (the worktree has no `node_modules`).
- Scripts are `.ts` run directly by Node 26 (`.nvmrc` = 26.7.0, native type stripping): `node scripts/design/export.ts`. Only erasable syntax (no enums, no parameter properties). Tests live in `web/scripts/design/*.test.ts` under the `unit` vitest project (Task 1 adds the include).
- Format/lint: repo uses oxfmt/oxlint via CI; run `pnpm --dir web run typecheck` after each TypeScript task.

---

## File structure

| Path | Responsibility |
| --- | --- |
| `web/design/hikyo.pen` | The design document. Variables mirror tokens.css. Committed. |
| `web/design/exports/` | Generated PNGs. Gitignored. |
| `web/.storybook/design.ts` | `design(node)` helper: validates the name path, returns the addon-designs config. |
| `web/.storybook/openpencil-addon.tsx` | Manager toolbar button "Open in OpenPencil". |
| `web/.storybook/openpencil-middleware.ts` | Vite middleware + RPC client (dev only). Token stays here. |
| `web/.storybook/main.ts` | Registers addon-designs, staticDirs, managerEntries, viteFinal. |
| `web/scripts/design/lib.ts` | Pure functions: collect nodes from stories, slug, parse CLI output, parse tokens.css, parse .pen variables, compare. |
| `web/scripts/design/export.ts` | CLI entry: resolve + export PNGs. |
| `web/scripts/design/tokens-check.ts` | CLI entry: drift check. |
| `web/scripts/design/tokens-seed.ts` | CLI entry: write/refresh the `variables` block of hikyo.pen from tokens.css. |
| `web/scripts/design/lib.test.ts` | Unit tests for lib.ts. |
| `web/.storybook/openpencil-middleware.test.ts` | Middleware tests with fake discovery + stub RPC. |
| `.claude/skills/design-loop/SKILL.md` | Agent workflow. |
| `DESIGN.md` | New "Design source" section. |
| `docs/handoff/openpencil-storybook.md` | Handoff doc. |

---

### Task 1: Dependencies, gitignore, vitest include

**Files:**
- Modify: `web/package.json`
- Modify: `.gitignore`
- Modify: `web/vite.config.ts:74`
- Modify: `web/tsconfig.json:27`

**Interfaces:**
- Produces: `pnpm exec openpencil` on PATH inside `web/`; `web/scripts/design/*.test.ts` picked up by `pnpm run test`.

- [ ] **Step 1: Install deps**

```bash
cd web
pnpm add -D @open-pencil/cli@0.14.0 @open-pencil/mcp@0.14.0 @storybook/addon-designs@11.1.4 culori
pnpm add -D @types/culori
pnpm exec openpencil --version
```
Expected: prints `0.14.0`. If `pnpm add` picks a caret range, edit `package.json` to the exact version.

- [ ] **Step 2: Gitignore exports**

Append to `.gitignore` after line 63 (`internal/webui/dist/`):
```
web/design/exports/*
!web/design/exports/.gitkeep
```
Then `mkdir -p web/design/exports && touch web/design/exports/.gitkeep`. The `ci.yml` storybook job runs `test-storybook` without `design:export`, and `staticDirs` must point at an existing directory.

- [ ] **Step 3: Vitest include and tsconfig**

`web/vite.config.ts` line 74, replace the include array with:
```ts
          include: ['src/**/*.test.ts', 'src/**/*.test.tsx', 'e2e/**/*.test.ts', 'scripts/**/*.test.ts', '.storybook/**/*.test.ts'],
```
`web/tsconfig.json` line 27, add `"scripts"` to the include array.

- [ ] **Step 4: Verify**

```bash
pnpm run typecheck && pnpm run test
```
Expected: both pass (no new tests yet).

- [ ] **Step 5: Commit**

```bash
git add web/package.json web/pnpm-lock.yaml .gitignore web/design/exports/.gitkeep web/vite.config.ts web/tsconfig.json
git commit -s -m "build(web): add OpenPencil CLI/MCP, addon-designs, culori

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: `lib.ts` pure functions (TDD)

**Files:**
- Create: `web/scripts/design/lib.ts`
- Create: `web/scripts/design/lib.test.ts`

**Interfaces:**
- Produces:
  - `NODE_PATH = /^[A-Za-z0-9][A-Za-z0-9 ]*(\/[A-Za-z0-9][A-Za-z0-9 ]*)+$/`
  - `slugFor(node: string): string`
  - `collectDesignNodes(sources: string[]): string[]` (unique, sorted)
  - `parseQueryOutput(json: string): { id: string; name: string }[]` (Zod)
  - `parseTokensCss(css: string): { dark: Map<string,string>; light: Map<string,string> }`
  - `parsePenVariables(penJson: string): Map<string, { light: string; dark: string; type: 'color'|'number'|'string' }>` (Zod)
  - `compareTokens(css, pen): string[]` (empty = clean; colours compared as lowercase hex, the form the .pen stores)
  - `MIRRORED_SKIP: Set<string>`
  - `cssToPenVariables(css): Record<string, PenVariable>` for the seed script

- [ ] **Step 1: Write failing tests**

```ts
// web/scripts/design/lib.test.ts
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
```

- [ ] **Step 2: Run, expect failure**

```bash
pnpm run test -- scripts/design
```
Expected: FAIL, cannot resolve `./lib.ts`.

- [ ] **Step 3: Implement**

```ts
// web/scripts/design/lib.ts
// Pure helpers behind the design scripts. No I/O here, so they are unit-testable.
// Node 26 runs .ts directly (type stripping), so no build step and no .mjs shim.
import { formatHex, parse } from 'culori';
import { z } from 'zod';

export const NODE_PATH = /^[A-Za-z0-9][A-Za-z0-9 ]*(\/[A-Za-z0-9][A-Za-z0-9 ]*)+$/;

/** Tokens that are derived (color-mix), motion, or font stacks: not mirrored in the design file. */
export const MIRRORED_SKIP = new Set(['--ease', '--dur', '--font-ui', '--font-mono']);

export function slugFor(node: string): string {
  return node.replaceAll('/', '--');
}

const designCall = /design\('([^']*)'\)/g;

export function collectDesignNodes(sources: string[]): string[] {
  const found = new Set();
  for (const source of sources) {
    for (const match of source.matchAll(designCall)) {
      const node = match[1];
      if (!NODE_PATH.test(node)) throw new Error(`design(): "${node}" is not a Title/Variant node path`);
      found.add(node);
    }
  }
  return [...found].sort();
}

const queryNode = z.object({ id: z.string(), name: z.string() });

export function parseQueryOutput(json: string): { id: string; name: string }[] {
  return z.array(queryNode).parse(JSON.parse(json)).map(({ id, name }) => ({ id, name }));
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
      const value = m[2].trim();
      if (isMirrored(m[1], value)) map.set(m[1], value);
    }
    return map;
  };
  const dark = read(block(css, ':root {'));
  const overrides = read(block(css, ":root[data-theme='light'] {"));
  const light = new Map(dark);
  for (const [k, v] of overrides) light.set(k, v);
  return { dark, light };
}

const penValue = z.object({ value: z.union([z.string(), z.number()]), theme: z.object({ Mode: z.enum(['Light', 'Dark']) }).optional() });
const penVariable = z.object({ type: z.enum(['color', 'number', 'string']), value: z.union([z.string(), z.number(), z.array(penValue)]) });
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
    const light = v.value.find((e) => e.theme === undefined || e.theme.Mode === 'Light');
    const dark = v.value.find((e) => e.theme?.Mode === 'Dark') ?? light;
    if (!light) throw new Error(`hikyo.pen: variable ${name} has no Light value`);
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
    const expectedLight = css.light.get(name) ?? expectedDark;
    const kind = cssKind(expectedDark);
    if (kind === 'color') {
      for (const [mode, expected, actual] of [['dark', expectedDark, p.dark], ['light', expectedLight, p.light]]) {
        if (cssHex(expected) !== actual.toLowerCase()) {
          errors.push(`${name} (${mode}): expected ${expected} = ${cssHex(expected)} (tokens.css), got ${actual} (hikyo.pen)`);
        }
      }
    } else {
      const expected = kind === 'number' ? cssNumber(expectedDark) : expectedDark;
      if (p.light !== expected || p.dark !== expected) {
        errors.push(`${name}: expected ${expected} (tokens.css), got ${p.dark} (hikyo.pen)`);
      }
    }
  }
  for (const name of pen.keys()) {
    if (!css.dark.has(name)) errors.push(`${name}: present in hikyo.pen, absent from tokens.css`);
  }
  return errors;
}

/** Build the `variables` block of a .pen document from parsed tokens.css. Colours become hex, which is what the app stores. */
type PenVariable = z.infer<typeof penVariable>;

export function cssToPenVariables(css: TokenSides): Record<string, PenVariable> {
  const variables: Record<string, PenVariable> = {};
  for (const [name, dark] of css.dark) {
    const light = css.light.get(name) ?? dark;
    const kind = cssKind(dark);
    if (kind === 'color') {
      variables[name] = {
        type: 'color',
        value: [{ value: formatHex(parse(light)) }, { value: formatHex(parse(dark)), theme: { Mode: 'Dark' } }],
      };
    } else if (kind === 'number') {
      variables[name] = { type: 'number', value: Number(cssNumber(dark)) };
    } else {
      variables[name] = { type: 'string', value: dark };
    }
  }
  return variables;
}
```

- [ ] **Step 4: Run, expect pass**

```bash
pnpm run test -- scripts/design
```
Expected: PASS, 9 tests.

- [ ] **Step 5: Commit**

```bash
git add web/scripts/design/lib.ts web/scripts/design/lib.test.ts
git commit -s -m "feat(web): design-script helpers, node collection and token comparison

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: Seed `hikyo.pen` from tokens.css

**Files:**
- Create: `web/scripts/design/tokens-seed.ts`
- Create: `web/design/hikyo.pen`
- Modify: `web/package.json` scripts

**Interfaces:**
- Consumes: `parseTokensCss`, `cssToPenVariables` from Task 2.
- Produces: `web/design/hikyo.pen` with a `variables` block; `pnpm run design:seed`.

- [ ] **Step 1: Write the seed script**

```ts
// web/scripts/design/tokens-seed.ts
// Writes or refreshes the `variables` block of hikyo.pen from tokens.css.
// Direction is always CSS → design. Everything else in the .pen is preserved.
import { readFile, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { z } from 'zod';

import { cssToPenVariables, parseTokensCss } from './lib.ts';

const here = (p: string) => resolve(import.meta.dirname, '../..', p);
const PEN = here('design/hikyo.pen');
const CSS = here('src/styles/tokens.css');

const penShell = z.object({ version: z.string(), children: z.array(z.unknown()), themes: z.record(z.string(), z.array(z.string())) }).passthrough();

const css = parseTokensCss(await readFile(CSS, 'utf8'));
let doc: z.infer<typeof penShell> & { variables?: unknown };
try {
  doc = penShell.parse(JSON.parse(await readFile(PEN, 'utf8')));
} catch (error) {
  if (!(error instanceof Error && 'code' in error && error.code === 'ENOENT')) throw error;
  doc = { version: '2.8', children: [], themes: { Mode: ['Light', 'Dark'] } };
}
doc.themes = { ...doc.themes, Mode: ['Light', 'Dark'] };
doc.variables = cssToPenVariables(css);
await writeFile(PEN, `${JSON.stringify(doc, null, 2)}\n`);
console.log(`hikyo.pen: ${Object.keys(doc.variables).length} variables written from tokens.css`);
```

Note: `z.array(z.unknown())` for `children` is deliberate: the seed script does not interpret nodes, it round-trips them untouched. That is the one sanctioned use.

- [ ] **Step 2: Add script and run**

`web/package.json` scripts, add:
```json
    "design:seed": "node scripts/design/tokens-seed.ts",
```
```bash
pnpm run design:seed
pnpm exec openpencil variables design/hikyo.pen --json | head -20
```
Expected: log line with the variable count (about 22); CLI lists `--bg` etc. with `Light`/`Dark` modes.

- [ ] **Step 3: Confirm the app opens it (needs the app running; if not, note it in the handoff and continue)**

MCP: `open_file` with `path: "web/design/hikyo.pen"`, then `list_variables`. Expected: same names as tokens.css.

- [ ] **Step 4: Commit**

```bash
git add web/scripts/design/tokens-seed.ts web/design/hikyo.pen web/package.json
git commit -s -m "feat(design): seed hikyo.pen variables from tokens.css

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: Token drift check

**Files:**
- Create: `web/scripts/design/tokens-check.ts`
- Modify: `web/package.json` scripts

**Interfaces:**
- Consumes: `parseTokensCss`, `parsePenVariables`, `compareTokens`.
- Produces: `pnpm run design:check`, exit 1 with one line per drift.

- [ ] **Step 1: Write the script**

```ts
// web/scripts/design/tokens-check.ts
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import { compareTokens, parsePenVariables, parseTokensCss } from './lib.ts';

const here = (p: string) => resolve(import.meta.dirname, '../..', p);

const css = parseTokensCss(await readFile(here('src/styles/tokens.css'), 'utf8'));
const pen = parsePenVariables(await readFile(here('design/hikyo.pen'), 'utf8'));
const errors = compareTokens(css, pen);
if (errors.length > 0) {
  console.error('Design tokens drifted from tokens.css. Fix the CSS first, then run `pnpm run design:seed`.');
  for (const e of errors) console.error(`  ${e}`);
  process.exit(1);
}
console.log(`design tokens: ${pen.size} variables match tokens.css`);
```

- [ ] **Step 2: Add script, run clean, then prove it fails**

`web/package.json` scripts, add:
```json
    "design:check": "node scripts/design/tokens-check.ts",
```
```bash
pnpm run design:check
sed -i '' 's/"#[0-9a-f]\{6\}"/"#ff0000"/' design/hikyo.pen   # corrupt the first colour
pnpm run design:check; echo "exit=$?"
git checkout design/hikyo.pen
```
Expected: first run prints the match line. Second run exits 1 and names the variable with the tokens.css value as expected.

- [ ] **Step 3: Commit**

```bash
git add web/scripts/design/tokens-check.ts web/package.json
git commit -s -m "feat(design): token drift check between tokens.css and hikyo.pen

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: `design()` helper and addon-designs registration

**Files:**
- Create: `web/.storybook/design.ts`
- Modify: `web/.storybook/main.ts`

**Interfaces:**
- Produces: `design(node: string): { type: 'image'; url: string; name: string }`.

- [ ] **Step 1: Helper**

```ts
// web/.storybook/design.ts
// The one way a story points at its design. The node is a `Title/Variant`
// name path inside web/design/hikyo.pen; scripts/design/export.ts turns it
// into /design/<slug>.png at build time, so authors never write a URL.
const NODE_PATH = /^[A-Za-z0-9][A-Za-z0-9 ]*(\/[A-Za-z0-9][A-Za-z0-9 ]*)+$/;

export function design(node: string) {
  if (!NODE_PATH.test(node)) throw new Error(`design(): "${node}" is not a Title/Variant node path`);
  return { type: 'image' as const, url: `/design/${node.replaceAll('/', '--')}.png`, name: node };
}
```

- [ ] **Step 2: main.ts**

Replace `web/.storybook/main.ts` with:
```ts
import { fileURLToPath } from 'node:url';

import type { StorybookConfig } from '@storybook/react-vite';

const config: StorybookConfig = {
  stories: ['../src/**/*.stories.@(js|jsx|mjs|ts|tsx)'],
  addons: [
    '@storybook/addon-vitest',
    '@storybook/addon-a11y',
    '@storybook/addon-docs',
    '@storybook/addon-mcp',
    '@storybook/addon-designs',
  ],
  framework: '@storybook/react-vite',
  // Design exports rendered by scripts/design/export.ts; gitignored.
  staticDirs: [{ from: '../design/exports', to: '/design' }],
  // Zero-telemetry ADR: no phone-home from local or CI builds.
  core: { disableTelemetry: true },
};
export default config;
```

- [ ] **Step 3: Verify**

```bash
pnpm run typecheck
pnpm run build-storybook
```
Expected: build succeeds; `storybook-static/` contains the addon-designs manager chunk.

- [ ] **Step 4: Commit**

```bash
git add web/.storybook/design.ts web/.storybook/main.ts
git commit -s -m "feat(storybook): design() parameter helper and addon-designs

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: Export script wired into Storybook scripts

**Files:**
- Create: `web/scripts/design/export.ts`
- Modify: `web/package.json` scripts

**Interfaces:**
- Consumes: `collectDesignNodes`, `parseQueryOutput`, `slugFor`.
- Produces: `pnpm run design:export` (check + export); `storybook` and `build-storybook` run it first.

- [ ] **Step 1: Script**

```ts
// web/scripts/design/export.ts
// For every design('Title/Variant') in a story, render that node of
// hikyo.pen to design/exports/<slug>.png with the headless CLI. Unresolved
// node = exit 1: a story must not point at a design that no longer exists.
import { execFile } from 'node:child_process';
import { glob, mkdir, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { promisify } from 'node:util';

import { collectDesignNodes, parseQueryOutput, slugFor } from './lib.ts';

const run = promisify(execFile);
const here = (p: string) => resolve(import.meta.dirname, '../..', p);
const PEN = here('design/hikyo.pen');
const OUT = here('design/exports');
const CLI = here('node_modules/.bin/openpencil');

const files: string[] = [];
for await (const f of glob('src/**/*.stories.tsx', { cwd: here('.') })) files.push(here(f));
const nodes = collectDesignNodes(await Promise.all(files.map((f) => readFile(f, 'utf8'))));
await mkdir(OUT, { recursive: true });

const failures: string[] = [];
for (const node of nodes) {
  const escaped = node.replaceAll("'", "\\'");
  const { stdout } = await run(CLI, ['query', PEN, `//*[@name='${escaped}']`, '--json']);
  const matches = parseQueryOutput(stdout).filter((m) => m.name === node);
  if (matches.length !== 1) {
    failures.push(`${node}: ${matches.length === 0 ? 'not found' : `${matches.length} nodes share this name`} in hikyo.pen`);
    continue;
  }
  const out = resolve(OUT, `${slugFor(node)}.png`);
  await run(CLI, ['export', PEN, '--node', matches[0].id, '-s', '2', '-o', out]);
  console.log(`design: ${node} → ${out}`);
}
if (failures.length > 0) {
  console.error('Design export failed:');
  for (const f of failures) console.error(`  ${f}`);
  process.exit(1);
}
console.log(`design: ${nodes.length} node(s) exported`);
```

- [ ] **Step 2: Scripts**

`web/package.json` scripts, change:
```json
    "design:export": "pnpm run design:check && node scripts/design/export.ts",
    "storybook": "pnpm run design:export && storybook dev -p 6006",
    "build-storybook": "pnpm run design:export && storybook build",
```

- [ ] **Step 3: Verify the empty case and the failure case**

```bash
pnpm run design:export          # no stories link yet: "0 node(s) exported"
printf "export const x = design('Ghost/Nope');\n" > src/ui/_tmp.stories.tsx
pnpm run design:export; echo "exit=$?"
rm src/ui/_tmp.stories.tsx
```
Expected: second run exits 1 with `Ghost/Nope: not found in hikyo.pen`.

- [ ] **Step 4: Commit**

```bash
git add web/scripts/design/export.ts web/package.json
git commit -s -m "feat(storybook): render linked design nodes at build time

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: Dev middleware (interim), TDD

**Files:**
- Create: `web/.storybook/openpencil-middleware.ts`
- Create: `web/.storybook/openpencil-middleware.test.ts`
- Modify: `web/.storybook/main.ts`

**Interfaces:**
- Produces:
  - `openInOpenPencil(node: string, deps: { discovery: () => Promise<DiscoveryInfo | null>; rpc: (info, command, args) => Promise<unknown>; penPath: string }): Promise<{ status: 200 | 404 | 503; message: string }>`
  - `openPencilMiddleware(opts: { penPath: string; allowedOrigin: string }): Connect.NextHandleFunction`
  - `openPencilPlugin(): Plugin` (Vite)

- [ ] **Step 1: Failing tests**

```ts
// web/.storybook/openpencil-middleware.test.ts
import { describe, expect, it, vi } from 'vitest';

import { openInOpenPencil } from './openpencil-middleware.ts';

const info = { pid: 1, socketPath: null, httpPort: 7600, authRequired: true, authToken: 'secret', version: '0.14.0', startedAt: '' };

describe('openInOpenPencil', () => {
  it('opens the file, selects the node, zooms', async () => {
    const rpc = vi.fn(async (_i, command: string, args: Record<string, unknown>) => {
      if (command === 'tool' && args.name === 'find_nodes') return { nodes: [{ id: 'T3Um0', name: 'Button/Primary' }] };
      return {};
    });
    const r = await openInOpenPencil('Button/Primary', { discovery: async () => info, rpc, penPath: '/repo/web/design/hikyo.pen' });
    expect(r.status).toBe(200);
    expect(rpc.mock.calls.map((c) => c[1])).toEqual(['open_file', 'tool', 'tool', 'tool']);
    expect(rpc.mock.calls[0][2]).toEqual({ path: '/repo/web/design/hikyo.pen' });
    expect(rpc.mock.calls[2][2]).toEqual({ name: 'select_nodes', args: { ids: ['T3Um0'] } });
    expect(rpc.mock.calls[3][2]).toEqual({ name: 'viewport_zoom_to_fit', args: { ids: ['T3Um0'] } });
  });
  it('503 when the app is not running', async () => {
    const r = await openInOpenPencil('Button/Primary', { discovery: async () => null, rpc: vi.fn(), penPath: '/x.pen' });
    expect(r.status).toBe(503);
    expect(r.message).toMatch(/OpenPencil is not running/);
  });
  it('404 when the node is missing, without leaking the token', async () => {
    const rpc = vi.fn(async () => ({ nodes: [] }));
    const r = await openInOpenPencil('Button/Nope', { discovery: async () => info, rpc, penPath: '/x.pen' });
    expect(r.status).toBe(404);
    expect(JSON.stringify(r)).not.toContain('secret');
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
pnpm run test -- openpencil-middleware
```
Expected: FAIL, module not found.

- [ ] **Step 3: Implement**

```ts
// web/.storybook/openpencil-middleware.ts
// Dev-only bridge from the Storybook toolbar to the running OpenPencil app.
// The RPC bearer token is read from the discovery file here, in Node, and
// never sent to the browser: dev servers bind 0.0.0.0, so anything in the
// bundle is readable on the LAN. Interim until the openpencil:// scheme
// ships upstream (design-tooling ADR, point 5).
import { request as httpRequest } from 'node:http';
import type { IncomingMessage, ServerResponse } from 'node:http';

import { readDiscoveryFile, type DiscoveryInfo } from '@open-pencil/mcp/discovery';
import type { Plugin } from 'vite';
import { z } from 'zod';

type Rpc = (info: DiscoveryInfo, command: string, args: Record<string, unknown>) => Promise<unknown>;

const foundNodes = z.object({ nodes: z.array(z.object({ id: z.string(), name: z.string() })) });
const body = z.object({ node: z.string().regex(/^[A-Za-z0-9][A-Za-z0-9 ]*(\/[A-Za-z0-9][A-Za-z0-9 ]*)+$/) });

export async function openInOpenPencil(
  node: string,
  deps: { discovery: () => Promise<DiscoveryInfo | null>; rpc: Rpc; penPath: string },
): Promise<{ status: 200 | 404 | 503; message: string }> {
  const info = await deps.discovery();
  if (!info) return { status: 503, message: 'OpenPencil is not running. Start the app, then click again.' };
  await deps.rpc(info, 'open_file', { path: deps.penPath });
  const found = foundNodes.parse(await deps.rpc(info, 'tool', { name: 'find_nodes', args: { name: node } }));
  const match = found.nodes.find((n) => n.name === node);
  if (!match) return { status: 404, message: `Node "${node}" not found in hikyo.pen.` };
  await deps.rpc(info, 'tool', { name: 'select_nodes', args: { ids: [match.id] } });
  await deps.rpc(info, 'tool', { name: 'viewport_zoom_to_fit', args: { ids: [match.id] } });
  return { status: 200, message: `Opened ${node}` };
}

const rpc: Rpc = (info, command, args) =>
  new Promise((resolve, reject) => {
    const payload = JSON.stringify({ command, args });
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (info.authToken) headers.Authorization = `Bearer ${info.authToken}`;
    const target = info.socketPath ? { socketPath: info.socketPath } : { host: '127.0.0.1', port: info.httpPort };
    const req = httpRequest({ ...target, path: '/rpc', method: 'POST', headers }, (res) => {
      let data = '';
      res.on('data', (c: Buffer) => { data += c; });
      res.on('end', () => {
        if (res.statusCode !== 200) return reject(new Error(`OpenPencil RPC ${command}: HTTP ${res.statusCode}`));
        resolve(JSON.parse(data));
      });
    });
    req.on('error', reject);
    req.end(payload);
  });

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve) => {
    let data = '';
    req.on('data', (c: Buffer) => { data += c; });
    req.on('end', () => resolve(data));
  });
}

export function openPencilMiddleware(opts: { penPath: string; allowedOrigin: string }) {
  return async (req: IncomingMessage, res: ServerResponse) => {
    const send = (status: number, message: string) => {
      res.statusCode = status;
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify({ message }));
    };
    if (req.method !== 'POST') return send(405, 'POST only');
    if (req.headers.origin !== opts.allowedOrigin) return send(403, 'Origin not allowed');
    const parsed = body.safeParse(JSON.parse(await readBody(req)));
    if (!parsed.success) return send(400, 'Body must be { node: "Title/Variant" }');
    try {
      const result = await openInOpenPencil(parsed.data.node, { discovery: readDiscoveryFile, rpc, penPath: opts.penPath });
      send(result.status, result.message);
    } catch (error) {
      send(502, error instanceof Error ? error.message : 'OpenPencil RPC failed');
    }
  };
}

export function openPencilPlugin(penPath: string): Plugin {
  return {
    name: 'hikyo-openpencil-open',
    apply: 'serve',
    configureServer(server) {
      server.httpServer?.once('listening', () => {
        const address = server.httpServer?.address();
        const port = typeof address === 'object' && address ? address.port : 6006;
        server.middlewares.use('/__openpencil/open', openPencilMiddleware({ penPath, allowedOrigin: `http://localhost:${port}` }));
      });
    },
  };
}
```
Note on `JSON.parse(data)` inside `rpc`: the caller parses with Zod (`foundNodes`) or ignores the result, so the raw parse feeds a schema; that satisfies the parse-don't-cast rule. `find_nodes` takes `{ name: string }` (case-insensitive substring) and returns `{ count, nodes: [{ id, name, … }] }` (verified in `packages/core/src/tools/read/nodes.ts`); hence the exact-name filter after the call. `select_nodes` and `viewport_zoom_to_fit` both take `{ ids: string[] }`.

`allowedOrigin`: Storybook prints its URL as `http://localhost:6006`; when testing from a LAN device the button is expected to be refused (403), which is the point.

- [ ] **Step 4: Register in main.ts**

The button ships in every build (spec §3); only the middleware is dev-only. Add to the config object in `web/.storybook/main.ts`:
```ts
  managerEntries: (entries = []) => [...entries, fileURLToPath(new URL('./openpencil-addon.tsx', import.meta.url))],
  viteFinal: async (config, { configType }) => {
    if (configType !== 'DEVELOPMENT') return config;
    const { openPencilPlugin } = await import('./openpencil-middleware.ts');
    const penPath = fileURLToPath(new URL('../design/hikyo.pen', import.meta.url));
    return { ...config, plugins: [...(config.plugins ?? []), openPencilPlugin(penPath)] };
  },
```
The addon file is created in Task 8; for this task's verification create `web/.storybook/openpencil-addon.tsx` containing only `export {};` and let Task 8 fill it.

- [ ] **Step 5: Run tests, typecheck, manual check**

```bash
pnpm run test -- openpencil-middleware
pnpm run typecheck
pnpm run storybook &
curl -s -X POST -H 'Origin: http://localhost:6006' -H 'Content-Type: application/json' -d '{"node":"Button/Primary"}' http://localhost:6006/__openpencil/open
curl -s -X POST -H 'Origin: http://evil' -d '{}' http://localhost:6006/__openpencil/open
kill %1
```
Expected: tests pass; first curl returns 503 (app not running) or 404/200 (running); second returns 403.

- [ ] **Step 6: Commit**

```bash
git add web/.storybook/openpencil-middleware.ts web/.storybook/openpencil-middleware.test.ts web/.storybook/openpencil-addon.tsx web/.storybook/main.ts
git commit -s -m "feat(storybook): dev middleware to open a design node in OpenPencil

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 8: Manager toolbar button

**Files:**
- Modify: `web/.storybook/openpencil-addon.tsx` (created empty in Task 7)

**Interfaces:**
- Consumes: `parameters.design.name` set by `design()`; dev endpoint `/__openpencil/open`.

- [ ] **Step 1: Implement**

```tsx
// web/.storybook/openpencil-addon.tsx
// "Open in OpenPencil" toolbar button. Tries the dev middleware first; on
// 404/405 (static build, or middleware removed after the upstream scheme
// ships) it falls back to the openpencil:// link, which the OS routes to the app.
import React from 'react';
import { addons, types, useParameter, useStorybookApi } from 'storybook/manager-api';
import { IconButton } from 'storybook/internal/components';
import { z } from 'zod';

const ADDON_ID = 'hikyo/openpencil';
const reply = z.object({ message: z.string() });
const MIN_APP_VERSION = '0.15.0';
const PEN_FILE = 'web/design/hikyo.pen';

function schemeUrl(node: string) {
  return `openpencil://open?file=${encodeURIComponent(PEN_FILE)}&node=${encodeURIComponent(node)}`;
}

function OpenButton() {
  const design = useParameter<{ name?: string } | undefined>('design');
  const api = useStorybookApi();
  const node = design?.name;
  if (!node) return null;
  const onClick = async () => {
    try {
      const res = await fetch('/__openpencil/open', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ node }),
      });
      if (res.status === 404 || res.status === 405) {
        window.location.assign(schemeUrl(node));
        return;
      }
      const { message } = reply.parse(await res.json());
      api.addNotification({ id: ADDON_ID, content: { headline: message }, duration: 4000 });
    } catch {
      window.location.assign(schemeUrl(node));
    }
  };
  return (
    <IconButton key={ADDON_ID} title={`Open ${node} in OpenPencil (needs OpenPencil ≥ ${MIN_APP_VERSION} installed)`} onClick={onClick}>
      ✎
    </IconButton>
  );
}

addons.register(ADDON_ID, () => {
  addons.add(`${ADDON_ID}/tool`, {
    type: types.TOOL,
    title: 'Open in OpenPencil',
    match: ({ viewMode }) => viewMode === 'story' || viewMode === 'docs',
    render: () => <OpenButton />,
  });
});
```
`useParameter<T>` is a generic read, not a cast; the only untrusted input (the fetch body) goes through Zod.

- [ ] **Step 2: Verify in dev**

```bash
pnpm run typecheck
pnpm run storybook
```
Open http://localhost:6006, pick any story: no button yet (no story has `design`). Task 9 adds the pilot and the button appears.

- [ ] **Step 3: Commit**

```bash
git add web/.storybook/openpencil-addon.tsx
git commit -s -m "feat(storybook): Open in OpenPencil toolbar button

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 9: Button pilot, design nodes + story links

**Files:**
- Modify: `web/design/hikyo.pen` (nodes added through the app or MCP)
- Modify: `web/src/ui/Button.stories.tsx`

**Interfaces:**
- Consumes: `design()` from Task 5; variables from Task 3.
- Produces: nodes `Button/Secondary`, `Button/Primary`, `Button/Disabled`, `Button/Icon` in hikyo.pen.

- [ ] **Step 1: Bootstrap Button nodes (app running, MCP session)**

With OpenPencil open on `web/design/hikyo.pen` (`open_file`), for each variant call `create_shape` (frame, auto-layout, padding 12 24, radius bound to `--radius-control`, fill bound to `--accent` for Primary and `--bg-raise` for Secondary via `bind_variable`), then `set_text` with the label in Instrument Sans 500 14px, `rename_node` to `Button/<Variant>`, `node_to_component`. Disabled: opacity 0.5. Icon: 36×36 with `☾`. Then `save_file` with `path: "web/design/hikyo.pen"`.

Alternative without the app: write a minimal `web/design/bootstrap/button.html` using the real `.btn` markup and concatenate `src/styles/tokens.css` and `src/styles/app.css` into `design/bootstrap/button.css` (`--css` is accepted once) and run `pnpm exec openpencil import design/bootstrap/button.html --css design/bootstrap/button.css -o design/button.pen`, then copy its `children` into `hikyo.pen` and rename the frames. Delete `design/bootstrap/` and `design/button.pen` afterwards; they are scaffolding.

- [ ] **Step 2: Link the stories**

In `web/src/ui/Button.stories.tsx` add the import and parameters:
```ts
import { design } from '../../.storybook/design.ts';
```
```ts
export const Secondary: Story = { parameters: { design: design('Button/Secondary') } };
export const Primary: Story = { args: { variant: 'primary' }, parameters: { design: design('Button/Primary') } };
export const Disabled: Story = { args: { disabled: true }, parameters: { design: design('Button/Disabled') } };
export const Icon: Story = {
  args: { icon: true, 'aria-label': 'Toggle theme', children: '☾' },
  parameters: { design: design('Button/Icon') },
};
```

- [ ] **Step 3: Run the whole loop**

```bash
pnpm run design:export
ls design/exports
pnpm run test-storybook
pnpm run storybook
```
Expected: four PNGs; story tests pass; in the browser the Button stories show a "Design" panel with the image, and the ✎ toolbar button opens the node in the app (status toast "Opened Button/Primary"). Screenshot the Design panel for the PR.

- [ ] **Step 4: Prove the drift check bites**

Change `--radius-control` to `5px` in `tokens.css`, run `pnpm run design:check`, confirm exit 1 naming `--radius-control`, revert.

- [ ] **Step 5: Commit**

```bash
git add web/design/hikyo.pen web/src/ui/Button.stories.tsx
git commit -s -m "feat(design): Button pilot linked between hikyo.pen and Storybook

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 10: Skill, DESIGN.md, handoff, follow-up issue

**Files:**
- Create: `.claude/skills/design-loop/SKILL.md`
- Modify: `DESIGN.md` (new section after "Theme")
- Create: `docs/handoff/openpencil-storybook.md`

- [ ] **Step 1: Skill**

```markdown
---
name: design-loop
description: Design an atom or screen in OpenPencil, implement it with a Storybook story, ship it. Use when asked to design, mock up, or add a component or screen, or to link a story to its design.
---

# Design loop (OpenPencil ↔ Storybook)

Source of truth for tokens: `web/src/styles/tokens.css`. Design file:
`web/design/hikyo.pen` (JSON, committed). Both MCPs are installed:
`open-pencil` (app must be running with the file open) and Storybook's
`addon-mcp` (dev server on 6006).

## 1. Design
- `open_file` `web/design/hikyo.pen`. Never `new_document`.
- Build the node with `create_shape` / `set_layout` / `set_text`. Bind every
  fill, stroke, and radius to a variable with `bind_variable`; raw values are
  a review finding.
- Name it `Title/Variant` (`rename_node`) and make it a component
  (`node_to_component`). Screens follow the same rule (`Members/Empty`).
- `save_file` to the same path.

## 2. Implement
- `get_codegen_prompt`, then `get_jsx` for the node.
- Write the component in `web/src/ui/` (atom) or `web/src/routes/` (screen)
  using the same custom properties.
- Story: `parameters: { design: design('Title/Variant') }` with
  `import { design } from '../../.storybook/design.ts'`.

## 3. Verify
- `pnpm --dir web run design:export` (runs the token check first).
- Open Storybook, compare the Design panel with the rendered story, flip the
  theme toolbar for light and dark.
- ✎ in the toolbar opens the node in the app.

## 4. Ship
- Normal PR. The `.pen` diff is reviewed like code.

## Token changes
Edit `tokens.css` and `DESIGN.md`, then `pnpm --dir web run design:seed`.
Never edit variables in the app; the check compares hex for hex and fails the build.

## Bootstrapping from an existing component
Save the story's rendered HTML, then
`cat src/styles/tokens.css src/styles/app.css > /tmp/hikyo.css && pnpm --dir web exec openpencil import that.html --css /tmp/hikyo.css -o design/tmp.pen`,
copy its `children` into `hikyo.pen`, rename, delete `tmp.pen`.
```

- [ ] **Step 2: DESIGN.md section**

Insert after the "Theme" section:
```markdown
## Design source

Designs live in `web/design/hikyo.pen` (OpenPencil, JSON, committed). Its variables mirror `web/src/styles/tokens.css` name for name; `pnpm --dir web run design:check` fails the Storybook build on drift, and `design:seed` refreshes the file from the CSS. A story links to its node with `parameters.design = design('Title/Variant')`; the Storybook build renders that node into the Design panel and the ✎ toolbar button opens it in the app. Decision record: the design-tooling ADR.
```

- [ ] **Step 3: Handoff**

```markdown
# Handoff: OpenPencil ↔ Storybook design loop

Spec: docs/superpowers/specs/2026-09-16-openpencil-storybook-design.md
Plan: docs/superpowers/plans/2026-09-16-openpencil-storybook.md
ADR: docs/adr/design-tooling.md

## Done
- hikyo.pen seeded from tokens.css; drift check in `design:export`.
- addon-designs panel fed by headless CLI export at build time.
- ✎ toolbar button: dev middleware now, `openpencil://` link fallback.
- Button pilot linked.

## Open
- Upstream: `openpencil://open?file=&node=` URL scheme
  (docs/superpowers/plans/2026-09-16-openpencil-url-scheme.md). Once in a
  tagged release: bump MIN_APP_VERSION in `.storybook/openpencil-addon.tsx`,
  delete `.storybook/openpencil-middleware.ts` + its test + the `viteFinal`
  block, drop `@open-pencil/mcp` from devDependencies. Issue: #<n>.
- Other 43 stories are unlinked by design; link as they are touched.
```

- [ ] **Step 4: Open the follow-up issue and fill `#<n>`**

```bash
gh issue create --title "Remove Storybook OpenPencil dev middleware once openpencil:// ships" --body-file - <<'EOF'
Interim bridge from docs/superpowers/specs/2026-09-16-openpencil-storybook-design.md §3.
Removal condition: upstream URL scheme in a tagged OpenPencil release.
Steps: bump MIN_APP_VERSION in web/.storybook/openpencil-addon.tsx; delete web/.storybook/openpencil-middleware.ts and its test and the viteFinal block in main.ts; drop @open-pencil/mcp from web devDependencies.
EOF
```

- [ ] **Step 5: Commit**

```bash
git add .claude/skills/design-loop/SKILL.md DESIGN.md docs/handoff/openpencil-storybook.md
git commit -s -m "docs(design): design-loop skill, DESIGN.md design source, handoff

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 11: Graph refresh, PR

- [ ] **Step 1:** `graphify update .` and commit `graphify-out/` if it changed.
- [ ] **Step 2:** `pnpm --dir web run typecheck && pnpm --dir web run test && pnpm --dir web run build-storybook`.
- [ ] **Step 3:** Push, open PR with the Design panel screenshot, link it with `link_pull_request`. Body ends with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
- [ ] **Step 4:** Adversarial review per WORKSTYLE (cross-model gate applies), then stop at the human merge gate.
