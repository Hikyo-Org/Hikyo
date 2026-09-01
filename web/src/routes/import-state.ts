import type { ValueOccurrence } from '@hikyo/client';

/**
 * Pure, React-free logic for the browser dotenv import wizard (#495).
 *
 * The file is parsed in the browser and never leaves it until the reviewed
 * phase-2 write. Everything here is deterministic and unit-tested so the risky
 * parts — the dotenv grammar, the type suggestion, the collision/trim buckets —
 * match the Go importer the CLI drives (`internal/dotenv`, `internal/importer`).
 * The server re-validates every one of these; the browser mirror exists so the
 * operator reviews an accurate preview, never so a mismatch can slip a write.
 */

/** A single KEY=value assignment, with its 1-based source line for the preview. */
export type ParsedEntry = { readonly key: string; readonly value: string; readonly line: number };

/** A line the strict grammar refused, named for the preview — never its value. */
export type ParseError = { readonly line: number; readonly reason: string };

export type ParseResult = {
  readonly entries: readonly ParsedEntry[];
  readonly errors: readonly ParseError[];
};

/** The primitive types the wizard can declare a new key as. `enum` is excluded:
 * it needs member authoring the wizard does not gather and the suggester never
 * proposes it — declare enum keys through the matrix `+ New key` form. */
export type PrimitiveType = 'string' | 'integer' | 'boolean' | 'url' | 'json';

/** KeyName contract (openapi `KeyName`): ASCII upper-snake, ≤128 code points. */
const KEY_NAME = /^[A-Z_][A-Z0-9_]*$/;
const MAX_KEY_NAME = 128;

/** schema.Normalize is `strings.TrimSpace`; a value that changes under it is the
 * importer's trim offender and must be acknowledged before it is written. */
export function normalizeValue(value: string): string {
  return value.trim();
}

export function needsTrim(value: string): boolean {
  return value !== normalizeValue(value);
}

/**
 * parseDotenv mirrors `internal/dotenv.Parse`'s grammar exactly, with one
 * deliberate difference: Parse fails loud on the first bad line, but a review
 * wizard collects EVERY invalid entry so the operator sees them all at once.
 * Valid entries are still returned; the strict server parse remains the gate.
 */
export function parseDotenv(text: string): ParseResult {
  const entries: ParsedEntry[] = [];
  const errors: ParseError[] = [];
  const firstSeen = new Map<string, number>();

  const lines = text.split('\n');
  for (let index = 0; index < lines.length; index += 1) {
    const lineNumber = index + 1;
    // CRLF tolerance: a trailing \r is stripped like the Go reader does.
    const raw = lines[index]?.replace(/\r$/, '') ?? '';
    const trimmedLeft = raw.replace(/^[ \t]+/, '');
    // A blank line, or a comment (only when `#` is the first non-space char).
    if (trimmedLeft === '' || trimmedLeft.startsWith('#')) {
      continue;
    }
    const assignment = cutExportPrefix(trimmedLeft);
    const equals = assignment.indexOf('=');
    if (equals === -1) {
      errors.push({ line: lineNumber, reason: 'not a KEY=value assignment' });
      continue;
    }
    const key = assignment.slice(0, equals).replace(/[ \t]+$/, '');
    if (key.length > MAX_KEY_NAME) {
      errors.push({ line: lineNumber, reason: 'key name is longer than 128 characters' });
      continue;
    }
    if (!KEY_NAME.test(key)) {
      errors.push({
        line: lineNumber,
        reason: 'key name is not upper-snake (`[A-Z_][A-Z0-9_]*`)',
      });
      continue;
    }
    const firstLine = firstSeen.get(key);
    if (firstLine !== undefined) {
      errors.push({
        line: lineNumber,
        reason: `key ${key} is already assigned on line ${String(firstLine)}`,
      });
      continue;
    }
    const parsedValue = parseValue(assignment.slice(equals + 1).replace(/^[ \t]+/, ''));
    if ('error' in parsedValue) {
      errors.push({ line: lineNumber, reason: parsedValue.error });
      continue;
    }
    firstSeen.set(key, lineNumber);
    entries.push({ key, value: parsedValue.value, line: lineNumber });
  }
  return { entries, errors };
}

/** `export ` / `export\t` prefix is stripped; `exportKEY=` (no space) is not the
 * keyword and passes through untouched. */
function cutExportPrefix(line: string): string {
  if (line.startsWith('export ') || line.startsWith('export\t')) {
    return line.slice('export'.length).replace(/^[ \t]+/, '');
  }
  return line;
}

type ValueResult = { readonly value: string } | { readonly error: string };

function parseValue(rest: string): ValueResult {
  if (rest === '') {
    return { value: '' };
  }
  if (rest.startsWith('"')) {
    return parseDoubleQuoted(rest);
  }
  if (rest.startsWith("'")) {
    return parseSingleQuoted(rest);
  }
  // Unquoted: the rest of the line with trailing blanks trimmed. A `#` here is
  // part of the value, not an inline comment.
  return { value: rest.replace(/[ \t]+$/, '') };
}

function parseDoubleQuoted(rest: string): ValueResult {
  let out = '';
  let index = 1;
  while (index < rest.length) {
    const char = rest[index];
    if (char === '\\') {
      const next = rest[index + 1];
      if (next === undefined) {
        return { error: 'dangling backslash in a double-quoted value' };
      }
      const decoded = DOUBLE_ESCAPES[next];
      if (decoded === undefined) {
        // The offending character comes from the value, so it is never echoed
        // into a diagnostic the preview renders — only the class of error is.
        return { error: 'unknown escape in a double-quoted value' };
      }
      out += decoded;
      index += 2;
      continue;
    }
    if (char === '"') {
      return onlyTrailingBlank(rest.slice(index + 1))
        ? { value: out }
        : { error: 'unexpected content after the closing quote' };
    }
    out += char;
    index += 1;
  }
  return { error: 'unterminated double-quoted value' };
}

const DOUBLE_ESCAPES: Record<string, string> = {
  n: '\n',
  r: '\r',
  t: '\t',
  '\\': '\\',
  '"': '"',
  "'": "'",
};

function parseSingleQuoted(rest: string): ValueResult {
  const close = rest.indexOf("'", 1);
  if (close === -1) {
    return { error: 'unterminated single-quoted value' };
  }
  return onlyTrailingBlank(rest.slice(close + 1))
    ? { value: rest.slice(1, close) }
    : { error: 'unexpected content after the closing quote' };
}

function onlyTrailingBlank(tail: string): boolean {
  return /^[ \t]*$/.test(tail);
}

const INT64_MIN = -9223372036854775808n;
const INT64_MAX = 9223372036854775807n;
const INTEGER = /^-?[0-9]+$/;

/**
 * suggestType mirrors `importer.SuggestType`: a type is suggested only when
 * EVERY value satisfies it, checked in the fixed order boolean → integer → json,
 * falling back to string (also the floor for an empty set). The suggestion is
 * only ever displayed — it lands only on an explicit accept.
 */
export function suggestType(values: readonly string[]): PrimitiveType {
  if (values.length === 0) {
    return 'string';
  }
  if (values.every(isBoolean)) {
    return 'boolean';
  }
  if (values.every(isInteger)) {
    return 'integer';
  }
  if (values.every(isJSONObjectOrArray)) {
    return 'json';
  }
  return 'string';
}

/** Boolean is the canonical `true`/`false` only — `1`, `yes`, `TRUE` are not
 * coerced (schema TypeBoolean). */
function isBoolean(value: string): boolean {
  return value === 'true' || value === 'false';
}

/** Integer is `-?[0-9]+` within signed 64-bit (schema TypeInteger). */
function isInteger(value: string): boolean {
  if (!INTEGER.test(value)) {
    return false;
  }
  const magnitude = BigInt(value);
  return magnitude >= INT64_MIN && magnitude <= INT64_MAX;
}

/** JSON suggestion fires only for a single well-formed object or array; scalar
 * JSON (`4`, `true`, `"x"`) belongs to integer/boolean/string. */
function isJSONObjectOrArray(value: string): boolean {
  const trimmed = normalizeValue(value);
  if (trimmed === '' || (trimmed[0] !== '{' && trimmed[0] !== '[')) {
    return false;
  }
  let doc: unknown;
  try {
    doc = JSON.parse(value);
  } catch {
    return false;
  }
  return typeof doc === 'object' && doc !== null;
}

/** A key's state for one environment, as phase 1 (`listValueOccurrences`)
 * observed it, keyed by name for the wizard's lookups. */
export type OccurrenceIndex = ReadonlyMap<string, ValueOccurrence>;

export function indexOccurrences(items: readonly ValueOccurrence[]): OccurrenceIndex {
  return new Map(items.map((item) => [item.name, item]));
}

/** The buckets one environment's import falls into, for the review preview and
 * for building the phase-2 request. Mirrors `importer.planEnvironment`. */
export type EnvironmentPlan = {
  /** Undeclared keys — a declaration must land before phase 2 accepts them. */
  readonly newKeys: readonly string[];
  /** Keys phase 2 will write: declared and either absent or an accepted overwrite. */
  readonly imported: readonly string[];
  /** Keys already `set` that no overwrite named — phase 2 skips them by name. */
  readonly skipped: readonly string[];
  /** Keys already `set` — the overwrite opt-in candidates (superset of chosen). */
  readonly collisions: readonly string[];
};

export function planEnvironment(
  entries: readonly ParsedEntry[],
  occurrences: OccurrenceIndex,
  overwrite: ReadonlySet<string>,
): EnvironmentPlan {
  const newKeys: string[] = [];
  const imported: string[] = [];
  const skipped: string[] = [];
  const collisions: string[] = [];
  for (const entry of entries) {
    const occurrence = occurrences.get(entry.key);
    if (occurrence === undefined || !occurrence.declared) {
      newKeys.push(entry.key);
      imported.push(entry.key);
      continue;
    }
    if (occurrence.set) {
      collisions.push(entry.key);
      if (overwrite.has(entry.key)) {
        imported.push(entry.key);
      } else {
        skipped.push(entry.key);
      }
      continue;
    }
    imported.push(entry.key);
  }
  return { newKeys, imported, skipped, collisions };
}
