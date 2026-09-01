import { parseAllDocuments } from 'yaml';

/**
 * Pure, React-free connector layer for the browser import wizard (#496).
 *
 * #495 taught the wizard to read a local `.env` file; this module extends the
 * same client-side, review-before-transfer journey to the shipped FILE-mode
 * connectors: Kubernetes Secret manifests, the pinned Infisical export, and the
 * Vault/OpenBao JSON Lines capture. Each turns foreign bytes into a normalized
 * record set that feeds the shared classify/collision/apply flow unchanged.
 *
 * Everything here mirrors the Go connectors the CLI drives
 * (`internal/importer/{k8s,infisical,vault,names,importer}.go`) so the browser
 * preview matches the plan the CLI would author for the same choices (ADR
 * `docs/adr/import-paths.md`; acceptance criterion "equivalent browser and CLI
 * choices produce semantically identical plans and results"). The server
 * re-validates every byte on `value.import`; this mirror exists so the operator
 * reviews an accurate preview, never so a mismatch can slip a write.
 *
 * Two invariants carried over verbatim from the Go framework:
 *
 *   - EVERY FOREIGN BYTE IS SECRET UNTIL CLASSIFIED. No refusal here carries
 *     source content — messages name keys, paths, bounds and codes, never a
 *     value fragment or a parser's echo. Foreign NAMES are rendered only through
 *     `safeName` (control bytes escaped, length-capped), the single safe
 *     renderer, because the ADR requires errors to name keys.
 *   - WORK IS BOUNDED. File size, record count, per-value size and name length
 *     are checked while parsing; exceeding one fails loud, naming the bound.
 *
 * SOPS (its only mode needs decryption keys) and every live mode (ambient
 * kubeconfig / Vault client conventions) stay on the CLI: the import-paths ADR
 * rejects both server-side importers and a browser credential prompt, so those
 * connectors are surfaced in the picker but routed to the CLI with guidance.
 */

/** The file-mode connectors the browser can complete. `dotenv` stays in the
 * wizard's own `import-state` module; these three are this file's subject. */
export type FileConnector = 'k8s' | 'infisical' | 'vault';

// The uniform bounds, mirroring the one block at the top of
// `internal/importer/importer.go`. Values match main; a refusal names the bound.
/** Per-file cap (`importer.MaxFileBytes`); exported so the wizard can refuse an
 * oversized selection by `file.size` BEFORE reading it into memory. */
export const MAX_FILE_BYTES = 10 << 20;
const MAX_DECODED_BYTES = 50 << 20;
const MAX_RECORDS = 50000;
const MAX_VALUE_BYTES = 65536;
const MAX_KEY_NAME_BYTES = 128;
const MAX_DEPTH = 32;
/** How much of a foreign NAME any message renders, after escaping. */
const MAX_SHOWN_NAME_BYTES = 128;

/** KeyName grammar (`schema.CheckKeyName`): ASCII upper-snake, ≤128 bytes. */
const KEY_NAME = /^[A-Z_][A-Z0-9_]*$/;

/** One normalized leaf: a target key under a folder, with its source name for
 * the rename surface. `value` is the exact bytes phase 2 will write. */
export type SourceEntry = {
  readonly key: string;
  readonly sourceName: string;
  readonly value: string;
  readonly folderPath: string;
};

/** A surfaced rename (source name → canonical KEY); nothing is renamed unseen. */
export type SourceRename = { readonly from: string; readonly to: string };

/** The connector's result: a set of entries plus what it skipped and renamed,
 * OR an all-or-nothing refusal naming keys/paths/bounds (never content). */
export type SourceParse =
  | {
      readonly ok: true;
      readonly entries: readonly SourceEntry[];
      readonly renames: readonly SourceRename[];
      readonly skipped: readonly string[];
    }
  | { readonly ok: false; readonly reason: string };

/** A pre-mapping record, as a connector `Read` emits it. `binary` marks a value
 * whose decoded bytes are not UTF-8 (a K8s Secret can hold arbitrary bytes);
 * `byteLength` is the exact decoded size the per-value bound checks. */
type SourceRecord = {
  readonly folder: readonly string[];
  readonly sourceName: string;
  readonly value: string;
  readonly byteLength: number;
  readonly binary: boolean;
};

/** A refusal, thrown by the connectors and caught at the boundary. Carries only
 * the content-free message the ADR permits. */
class Refusal extends Error {}

function refuse(message: string): never {
  throw new Refusal(message);
}

/** UTF-8 byte length — Go's `len(string)`, which the value/name bounds use. */
const ENCODER = new TextEncoder();
function byteLength(value: string): number {
  return ENCODER.encode(value).length;
}

/**
 * The run budget, mirroring `importer.Budget`: it charges EVERY leaf a connector
 * decodes — record count and decoded bytes — while parsing, so resource
 * exhaustion is impossible before a result exists and a skipped leaf still costs
 * its slot (`b.Record`/`b.Bytes` in the Go connectors). Exceeding a bound fails
 * loud, naming it.
 */
type Budget = { record: () => void; bytes: (count: number) => void };

function newBudget(): Budget {
  let records = 0;
  let decoded = 0;
  return {
    record() {
      records += 1;
      if (records > MAX_RECORDS) {
        refuse(`the source holds more than the ${MAX_RECORDS}-record cap`);
      }
    },
    bytes(count) {
      decoded += count;
      if (decoded > MAX_DECODED_BYTES) {
        refuse(`the source decodes to more than the ${MAX_DECODED_BYTES}-byte cap`);
      }
    },
  };
}

/**
 * safeName is THE ONLY way foreign text is rendered by this module — the mirror
 * of Go's `quoteName`. Control bytes, DEL and the quote/backslash are escaped so
 * a hostile source name (literally `"\x1b[2J\x1b]0;pwned\x07"`) cannot smuggle
 * terminal escapes or markup into a message; the length cap runs after escaping,
 * so the budget is on what is shown. React escapes the DOM, but a refusal string
 * also reaches logs and copy, so the escaping is load-bearing, not tidy.
 */
export function safeName(value: string): string {
  let out = '"';
  for (const char of value) {
    const code = char.codePointAt(0) ?? 0;
    if (char === '"') {
      out += '\\"';
    } else if (char === '\\') {
      out += '\\\\';
    } else if (char === '\n') {
      out += '\\n';
    } else if (char === '\t') {
      out += '\\t';
    } else if (char === '\r') {
      out += '\\r';
    } else if (code < 0x20 || code === 0x7f) {
      out += `\\x${code.toString(16).padStart(2, '0')}`;
    } else if (code > 0x7f) {
      // Non-ASCII is escaped rather than passed through, matching Go %q's
      // treatment of foreign runes.
      out += code > 0xffff
        ? `\\U${code.toString(16).padStart(8, '0')}`
        : `\\u${code.toString(16).padStart(4, '0')}`;
    } else {
      out += char;
    }
  }
  out += '"';
  return out.length <= MAX_SHOWN_NAME_BYTES ? out : `${out.slice(0, MAX_SHOWN_NAME_BYTES)}..."`;
}

function recordPath(record: SourceRecord): string {
  if (record.folder.length === 0) {
    return safeName(record.sourceName);
  }
  return safeName([...record.folder, record.sourceName].join('/'));
}

/**
 * transformName maps a source name onto the canonical grammar, mirroring
 * `importer.TransformName`. The asymmetry is the whole design: a name already
 * valid is preserved byte-for-byte (a transform on a valid name is a silent
 * rename); an invalid name goes through ONE documented transform; anything that
 * transform cannot resolve is a hard stop requiring an explicit rename.
 *
 * The documented transform, in full: lowercase ASCII → uppercase; `-`, `.`, `/`,
 * `\` → `_`; a leading digit takes one leading `_`. Everything else — a space,
 * `=`, `:`, any non-ASCII — is unmappable.
 */
export function transformName(source: string): { readonly target: string } | { readonly error: true } {
  if (KEY_NAME.test(source) && byteLength(source) <= MAX_KEY_NAME_BYTES) {
    return { target: source };
  }
  if (source === '') {
    return { error: true };
  }
  if (byteLength(source) > MAX_KEY_NAME_BYTES) {
    return { error: true };
  }
  let out = '';
  const chars = [...source];
  for (let i = 0; i < chars.length; i += 1) {
    const char = chars[i] ?? '';
    const code = char.codePointAt(0) ?? 0;
    if (char >= 'a' && char <= 'z') {
      out += char.toUpperCase();
    } else if ((char >= 'A' && char <= 'Z') || char === '_') {
      out += char;
    } else if (char >= '0' && char <= '9') {
      // A leading digit cannot open a name; one underscore is the documented,
      // deterministic, surfaced resolution.
      out += i === 0 ? `_${char}` : char;
    } else if (char === '-' || char === '.' || char === '/' || char === '\\') {
      out += '_';
    } else {
      // Space, `=`, `:`, any non-ASCII rune (`code > 0x7f`): outside the mapping.
      void code;
      return { error: true };
    }
  }
  return KEY_NAME.test(out) ? { target: out } : { error: true };
}

// ── Lossless JSON ──────────────────────────────────────────────────────────
//
// Native `JSON.parse` mangles numbers: `9007199254740993` becomes `…992`, and a
// long decimal loses precision. The Vault connector's non-string leaves become
// `json`-typed values through a canonical serialization the CLI pins with
// exact-number fixtures, so a value that round-trips through `JSON.parse` would
// diverge from the CLI and be stored wrong. This tiny parser keeps numbers as
// their source literal, rejects duplicate object members (the CLI's
// `rejectDuplicateMembers`) and rejects trailing content, so one line of the
// capture is exactly one record.

/** A number kept as its exact source literal. A class (not a `{__num}` object)
 * so it can never be confused with a genuine JSON object that has a `__num`
 * member. */
class JsonNumber {
  constructor(readonly literal: string) {}
}
type JsonValue = string | boolean | null | JsonNumber | JsonValue[] | { [key: string]: JsonValue };

function isJsonNumber(value: unknown): value is JsonNumber {
  return value instanceof JsonNumber;
}

/** A JSON field that is present and non-null but not a string — what Go's typed
 * `json.Unmarshal` into a `string`/`*string` field refuses. Absent or null is
 * allowed (Go leaves the zero value / nil). */
function mistyped(field: JsonValue | undefined): boolean {
  return field !== undefined && field !== null && typeof field !== 'string';
}

function isJsonObject(value: unknown): value is { [key: string]: JsonValue } {
  return typeof value === 'object' && value !== null && !Array.isArray(value) && !(value instanceof JsonNumber);
}

function parseJsonLossless(text: string): JsonValue {
  const parser = new JsonParser(text);
  const value = parser.parseValue();
  parser.skipWhitespace();
  if (!parser.atEnd()) {
    refuse('the JSON carries trailing content after its value');
  }
  return value;
}

class JsonParser {
  private i = 0;
  private depth = 0;
  constructor(private readonly s: string) {}

  atEnd(): boolean {
    return this.i >= this.s.length;
  }

  skipWhitespace(): void {
    while (this.i < this.s.length && ' \t\n\r'.includes(this.s[this.i] ?? '')) {
      this.i += 1;
    }
  }

  parseValue(): JsonValue {
    this.skipWhitespace();
    const char = this.s[this.i];
    // Bound nesting the way `importer.normalizeTree` does (depth 32). Without
    // this a deeply nested leaf would recurse until the JS stack overflows — an
    // uncaught error, not a loud refusal at the bound.
    if (char === '{' || char === '[') {
      this.depth += 1;
      if (this.depth > MAX_DEPTH) {
        refuse(`the JSON nests deeper than the ${MAX_DEPTH}-level depth bound`);
      }
      const value = char === '{' ? this.parseObject() : this.parseArray();
      this.depth -= 1;
      return value;
    }
    if (char === '"') return this.parseString();
    if (char === '-' || (char !== undefined && char >= '0' && char <= '9')) return this.parseNumber();
    if (this.s.startsWith('true', this.i)) return this.take('true', true);
    if (this.s.startsWith('false', this.i)) return this.take('false', false);
    if (this.s.startsWith('null', this.i)) return this.take('null', null);
    return refuse('the JSON is malformed');
  }

  private take<T extends JsonValue>(literal: string, value: T): T {
    this.i += literal.length;
    return value;
  }

  private parseObject(): { [key: string]: JsonValue } {
    this.i += 1;
    // A null-prototype object: a member literally named `__proto__` becomes an
    // ordinary own key rather than mutating the prototype (prototype pollution),
    // and it then shows up in `Object.keys` for the unknown-field checks.
    const out: { [key: string]: JsonValue } = Object.create(null);
    // Duplicate detection is case-insensitive, matching Go's `foldJSONMember`:
    // `encoding/json` matches struct fields case-insensitively, so `{"a":1,"A":2}`
    // is a last-value-wins ambiguity the CLI refuses, and the browser must too.
    const seen = new Set<string>();
    this.skipWhitespace();
    if (this.s[this.i] === '}') {
      this.i += 1;
      return out;
    }
    for (;;) {
      this.skipWhitespace();
      if (this.s[this.i] !== '"') refuse('the JSON object is malformed');
      const key = this.parseString();
      const folded = key.toLowerCase();
      if (seen.has(folded)) {
        refuse(`a JSON object declares the member ${safeName(key)} more than once`);
      }
      seen.add(folded);
      this.skipWhitespace();
      if (this.s[this.i] !== ':') refuse('the JSON object is malformed');
      this.i += 1;
      out[key] = this.parseValue();
      this.skipWhitespace();
      const next = this.s[this.i];
      if (next === ',') {
        this.i += 1;
        continue;
      }
      if (next === '}') {
        this.i += 1;
        return out;
      }
      return refuse('the JSON object is malformed');
    }
  }

  private parseArray(): JsonValue[] {
    this.i += 1;
    const out: JsonValue[] = [];
    this.skipWhitespace();
    if (this.s[this.i] === ']') {
      this.i += 1;
      return out;
    }
    for (;;) {
      out.push(this.parseValue());
      this.skipWhitespace();
      const next = this.s[this.i];
      if (next === ',') {
        this.i += 1;
        continue;
      }
      if (next === ']') {
        this.i += 1;
        return out;
      }
      return refuse('the JSON array is malformed');
    }
  }

  private parseString(): string {
    this.i += 1;
    let out = '';
    while (this.i < this.s.length) {
      const char = this.s[this.i] ?? '';
      if (char === '"') {
        this.i += 1;
        return JsonParser.replaceLoneSurrogates(out);
      }
      if (char === '\\') {
        const esc = this.s[this.i + 1] ?? '';
        if (esc === 'u') {
          const hex = this.s.slice(this.i + 2, this.i + 6);
          if (!/^[0-9a-fA-F]{4}$/.test(hex)) refuse('the JSON string has a bad \\u escape');
          out += String.fromCharCode(parseInt(hex, 16));
          this.i += 6;
          continue;
        }
        const simple: Record<string, string> = {
          '"': '"', '\\': '\\', '/': '/', b: '\b', f: '\f', n: '\n', r: '\r', t: '\t',
        };
        const decoded = simple[esc];
        if (decoded === undefined) refuse('the JSON string has a bad escape');
        out += decoded;
        this.i += 2;
        continue;
      }
      // A literal control byte inside a string is invalid JSON; the Go decoder
      // rejects it, so an unescaped tab or newline must not be accepted here.
      if ((char.codePointAt(0) ?? 0) < 0x20) {
        refuse('the JSON string holds an unescaped control character');
      }
      out += char;
      this.i += 1;
    }
    return refuse('the JSON string is unterminated');
  }

  // Go's `encoding/json` substitutes U+FFFD for an unpaired UTF-16 surrogate
  // (from a lone `\uD800`-class escape), so a structured value's canonical JSON
  // matches the CLI byte-for-byte. A valid surrogate PAIR is left intact.
  private static replaceLoneSurrogates(value: string): string {
    return value.replace(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g, '\uFFFD');
  }

  private parseNumber(): JsonNumber {
    const start = this.i;
    if (this.s[this.i] === '-') this.i += 1;
    while (this.i < this.s.length && /[0-9]/.test(this.s[this.i] ?? '')) this.i += 1;
    if (this.s[this.i] === '.') {
      this.i += 1;
      while (this.i < this.s.length && /[0-9]/.test(this.s[this.i] ?? '')) this.i += 1;
    }
    if (this.s[this.i] === 'e' || this.s[this.i] === 'E') {
      this.i += 1;
      if (this.s[this.i] === '+' || this.s[this.i] === '-') this.i += 1;
      while (this.i < this.s.length && /[0-9]/.test(this.s[this.i] ?? '')) this.i += 1;
    }
    const literal = this.s.slice(start, this.i);
    // Strict JSON number grammar: no leading zero (`01`), a fraction and an
    // exponent each require at least one digit. The Go decoder rejects the rest.
    if (!/^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$/.test(literal)) {
      refuse('the JSON number is malformed');
    }
    return new JsonNumber(literal);
  }
}

/**
 * canonicalJson is THE deterministic conversion for non-scalar leaves, mirroring
 * `importer.canonicalJSON`: object members ordered by key, HTML escaping OFF
 * (`<`, `>`, `&` survive — JS `JSON.stringify` already leaves them), no
 * indentation, no trailing newline, and number literals preserved exactly. A
 * SOPS array and a Vault object serialize identically through it.
 */
function canonicalJson(value: JsonValue): string {
  if (isJsonNumber(value)) {
    return value.literal;
  }
  if (typeof value === 'string') {
    return encodeJsonString(value);
  }
  if (value === null || typeof value === 'boolean') {
    return JSON.stringify(value);
  }
  if (Array.isArray(value)) {
    return `[${value.map(canonicalJson).join(',')}]`;
  }
  // Go's `encoding/json` sorts map keys by UTF-8 byte order, which equals
  // Unicode code-point order; JS `<` sorts by UTF-16 code units, which disagrees
  // above U+FFFF. Compare by code point so a structured value serializes exactly
  // as the CLI does.
  const members = Object.entries(value).sort(([a], [b]) => compareCodePoints(a, b));
  return `{${members.map(([key, child]) => `${encodeJsonString(key)}:${canonicalJson(child)}`).join(',')}}`;
}

/** JSON string encoding matching Go's encoder with HTML escaping off: `<`, `>`,
 * `&` survive, but the line separators U+2028/U+2029 are escaped (Go always
 * escapes them; JS `JSON.stringify` does not). */
function encodeJsonString(value: string): string {
  return JSON.stringify(value).replace(/\u2028/g, '\\u2028').replace(/\u2029/g, '\\u2029');
}

/** Compares two strings by Unicode code point (== UTF-8 byte order). */
function compareCodePoints(a: string, b: string): number {
  const ca = [...a];
  const cb = [...b];
  for (let i = 0; i < ca.length && i < cb.length; i += 1) {
    const pa = ca[i]?.codePointAt(0) ?? 0;
    const pb = cb[i]?.codePointAt(0) ?? 0;
    if (pa !== pb) return pa - pb;
  }
  return ca.length - cb.length;
}

// ── Kubernetes Secret manifests ─────────────────────────────────────────────

/**
 * Parses one or more Kubernetes Secret manifests (YAML or JSON, multi-document
 * `---` streams included), mirroring `k8s.go`'s file mode. `data` is
 * base64-decoded, then `stringData` is overlaid and STRINGDATA WINS (Kubernetes'
 * own admission merge). One Secret → one folder named after it. A wrong `kind`,
 * a duplicate key within one mapping, and a missing name are refused; the
 * decoded value's UTF-8/NUL and size checks run uniformly afterwards.
 */
function readK8s(text: string, budget: Budget): SourceRecord[] {
  // `uniqueKeys` refuses a mapping that declares a key twice; the lib's default
  // `maxAliasCount` (100) caps YAML alias expansion so a billion-laughs bomb
  // fails loud rather than in the allocator.
  const documents = parseAllDocuments(text, { uniqueKeys: true });
  const records: SourceRecord[] = [];
  const names: string[] = [];
  documents.forEach((doc, index) => {
    const where = `document ${index}`;
    if (doc.errors.length > 0) {
      const duplicate = doc.errors.find((error) => error.code === 'DUPLICATE_KEY');
      if (duplicate !== undefined) {
        refuse(`a key is declared more than once in one mapping (${where})`);
      }
      const alias = doc.errors.find((error) => /alias/i.test(error.message));
      if (alias !== undefined) {
        refuse(`the ${where} expands beyond the alias/decoded-size bound`);
      }
      refuse(`the ${where} is not parseable as YAML or JSON`);
    }
    let object: unknown;
    try {
      object = doc.toJS();
    } catch (caught) {
      if (caught instanceof Error && /alias/i.test(caught.message)) {
        refuse(`the ${where} expands beyond the alias/decoded-size bound`);
      }
      refuse(`the ${where} is not parseable as YAML or JSON`);
    }
    // An empty document — a trailing `---` — is skipped, as `kubectl` emits them.
    if (object === null || object === undefined) {
      return;
    }
    if (!isObject(object)) {
      refuse(`the ${where} is not shaped like a Kubernetes Secret manifest`);
    }
    const manifest = object;
    const kind = manifest.kind;
    if (kind !== 'Secret') {
      // The refused value is NOT echoed: `kind` is a foreign field a document
      // can fill with a token or an escape sequence. Naming the field says all.
      refuse(`the ${where}'s \`kind\` is not \`Secret\`; this connector reads Kubernetes Secret manifests only`);
    }
    const metadata = isObject(manifest.metadata) ? manifest.metadata : {};
    const name = typeof metadata.name === 'string' ? metadata.name : '';
    if (name === '') {
      refuse(`the Secret in ${where} carries no metadata.name; one Secret maps onto one folder named after it`);
    }
    names.push(name);
    const merged = new Map<string, { value: string; byteLength: number; binary: boolean }>();
    const data = manifest.data;
    if (data !== undefined && data !== null) {
      if (!isObject(data)) {
        refuse(`the Secret ${safeName(name)} in ${where} is not shaped like a Kubernetes Secret manifest`);
      }
      for (const dataKey of Object.keys(data).sort()) {
        const encoded = data[dataKey];
        if (typeof encoded !== 'string') {
          refuse(`the Secret ${safeName(name)} \`data\` entry ${safeName(dataKey)} is not a base64 string`);
        }
        const decoded = decodeK8sData(name, dataKey, encoded);
        // Charge the DECODED bytes, matching Go's `b.Bytes(len(raw))` after
        // base64 decoding — the file cap alone does not bound base64 expansion.
        budget.bytes(decoded.byteLength);
        merged.set(dataKey, decoded);
      }
    }
    const stringData = manifest.stringData;
    if (stringData !== undefined && stringData !== null) {
      if (!isObject(stringData)) {
        refuse(`the Secret ${safeName(name)} in ${where} is not shaped like a Kubernetes Secret manifest`);
      }
      for (const stringKey of Object.keys(stringData).sort()) {
        const raw = stringData[stringKey];
        if (typeof raw !== 'string') {
          refuse(`the Secret ${safeName(name)} \`stringData\` entry ${safeName(stringKey)} is not a string`);
        }
        budget.bytes(byteLength(raw));
        // stringData wins, silently and correctly — the admission merge.
        merged.set(stringKey, { value: raw, byteLength: byteLength(raw), binary: raw.includes('\u0000') });
      }
    }
    for (const key of [...merged.keys()].sort()) {
      const leaf = merged.get(key);
      if (leaf === undefined) continue;
      budget.record();
      records.push({ folder: [name], sourceName: key, value: leaf.value, byteLength: leaf.byteLength, binary: leaf.binary });
    }
  });
  if (records.length === 0) {
    refuse('the file holds no Kubernetes Secret manifest with any entry');
  }
  return records;
}

/** Decodes one `data` value from base64 and classifies its bytes. A K8s Secret
 * can hold arbitrary bytes; a value that is not UTF-8 text (or carries NUL) is
 * marked binary and refused by name in the uniform value check. */
function decodeK8sData(
  secret: string,
  key: string,
  encoded: string,
): { value: string; byteLength: number; binary: boolean } {
  // Go's `base64.StdEncoding.DecodeString` is strict on the alphabet, requires
  // correct `=` padding, and a length that is a multiple of 4 — but it DOES skip
  // `\r`/`\n` (a YAML block scalar can wrap base64 across lines). `atob` is
  // otherwise too forgiving (accepts unpadded input, ignores all whitespace), so
  // strip only CR/LF, then validate strictly, or the browser would decode bytes
  // the CLI refuses (and vice versa).
  const cleaned = encoded.replace(/[\r\n]/g, '');
  if (!/^[A-Za-z0-9+/]*={0,2}$/.test(cleaned) || cleaned.length % 4 !== 0) {
    refuse(`the Secret ${safeName(secret)} \`data\` entry ${safeName(key)} is not valid base64`);
  }
  let bytes: Uint8Array;
  try {
    const binaryString = atob(cleaned);
    bytes = Uint8Array.from(binaryString, (char) => char.charCodeAt(0));
  } catch {
    refuse(`the Secret ${safeName(secret)} \`data\` entry ${safeName(key)} is not valid base64`);
  }
  let value = '';
  let binary = bytes.includes(0);
  try {
    value = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
  } catch {
    binary = true;
  }
  return { value, byteLength: bytes.length, binary };
}

// ── Infisical pinned export ─────────────────────────────────────────────────

const INFISICAL_MINIMUM_VERSION = 'v0.43.0';

/**
 * Parses the pinned Infisical export (`infisical export --format=json`),
 * mirroring `infisical.go`. A JSON array of `{key,value,type,secretPath,_id}`;
 * `secretPath` maps onto a folder chain. A flat object is a dotenv export
 * wearing JSON and is routed to the `.env` path; an entry without `secretPath`
 * (no folder provenance) or without `type` (personal overrides already resolved)
 * is refused; `type: "personal"` entries are skipped and listed by name.
 */
function readInfisical(text: string, budget: Budget): { records: SourceRecord[]; skipped: string[] } {
  const trimmed = text.trim();
  if (trimmed === '') {
    refuse('the Infisical export is empty');
  }
  if (trimmed[0] !== '[') {
    refuse(
      'this is not the pinned structured export (a JSON array): a flattened export carries no folder or ' +
        'override provenance — export it with `infisical export --format=json --env <slug> --path <path>` (' +
        `${INFISICAL_MINIMUM_VERSION} or newer), or route the flattened form through the .env import instead`,
    );
  }
  const parsed = parseJsonLossless(trimmed);
  if (!Array.isArray(parsed)) {
    refuse('the export is not the pinned JSON array of secret entries');
  }
  if (parsed.length === 0) {
    refuse('the export holds no secrets');
  }
  const records: SourceRecord[] = [];
  const skipped: string[] = [];
  parsed.forEach((raw, index) => {
    const where = `entry ${index}`;
    if (!isJsonObject(raw)) {
      refuse(`the ${where} is not a secret object`);
    }
    const entry = raw;
    // Mirror Go's typed `json.Unmarshal`: a field present with the wrong JSON
    // type refuses the WHOLE export (before mapping and before the personal-skip
    // branch), never coerces. Coercing a non-string `value` to `""` — the
    // pre-fix behaviour — declared and imported an empty secret where the CLI
    // refuses the file.
    if (mistyped(entry.key) || mistyped(entry.value) || mistyped(entry.type) ||
        mistyped(entry.secretPath) || mistyped(entry['_id'])) {
      refuse(`the ${where} is not the pinned JSON array of secret entries`);
    }
    // Charge every entry before the personal-skip branch (Go's `b.Record` per
    // entry): a skipped override still costs its record slot.
    budget.record();
    const key = typeof entry.key === 'string' ? entry.key : '';
    if (key === '') {
      refuse(`the ${where} carries no \`key\``);
    }
    const named = `key ${safeName(key)}`;
    if (!('secretPath' in entry) || typeof entry.secretPath !== 'string') {
      refuse(`the ${named} carries no \`secretPath\`: this export has no folder provenance and cannot be mapped`);
    }
    if (!('type' in entry) || typeof entry.type !== 'string') {
      refuse(
        `the ${named} carries no \`type\`: personal overrides are indistinguishable from shared secrets, ` +
          'so this export has already resolved them into values',
      );
    }
    const type = entry.type;
    if (type === 'personal') {
      skipped.push(safeName(key));
      return;
    }
    if (type !== 'shared') {
      // `type` is a foreign enum-shaped field; its value is not echoed.
      refuse(`the ${named}'s \`type\` is neither \`shared\` nor \`personal\``);
    }
    const value = typeof entry.value === 'string' ? entry.value : '';
    budget.bytes(byteLength(value));
    const folder = infisicalFolder(named, entry.secretPath);
    if (folder.length > MAX_DEPTH) {
      refuse(`the ${named} folder path exceeds the ${MAX_DEPTH}-level depth bound`);
    }
    records.push({ folder, sourceName: key, value, byteLength: byteLength(value), binary: value.includes('\u0000') });
  });
  if (records.length === 0) {
    refuse('every entry in the export is a personal override; there is no shared secret to import');
  }
  skipped.sort();
  return { records, skipped };
}

/** Maps `secretPath` onto a folder chain; `/` is the root and maps onto none. */
function infisicalFolder(where: string, path: string): string[] {
  if (!path.startsWith('/')) {
    refuse(`the ${where} \`secretPath\` is not absolute; the pinned export spells folder paths from the root`);
  }
  return path.split('/').filter((segment) => segment !== '');
}

// ── Vault / OpenBao JSON Lines capture ──────────────────────────────────────

/**
 * Parses the pinned Vault/OpenBao JSON Lines capture, mirroring `vault.go`'s
 * file mode. One object per line: `{path,mount,engine_version,secret_version?,
 * deleted,destroyed,data}`. Path segments below the common prefix become a
 * folder chain; non-string field values become `json` leaves through the
 * canonical serialization; deleted/destroyed records are skipped by name. One
 * capture file must describe one mount and one engine version.
 */
function readVault(text: string, budget: Budget): { records: SourceRecord[]; skipped: string[] } {
  type Capture = {
    path: string;
    mount: string;
    engineVersion: number;
    secretVersion: number | null;
    deleted: boolean;
    destroyed: boolean;
    data: { [key: string]: JsonValue };
  };
  const captures: Capture[] = [];
  const seen = new Set<string>();
  const lines = text.split('\n');
  for (let index = 0; index < lines.length; index += 1) {
    const raw = (lines[index] ?? '').trim();
    if (raw === '') continue;
    const where = `line ${index + 1}`;
    const parsed = parseJsonLossless(raw);
    if (!isJsonObject(parsed)) {
      refuse(
        `the ${where} is not one pinned Vault/OpenBao capture record; see ` +
          'docs/handoff/69-import-live-connectors.md#vaultopenbao-capture-recipe',
      );
    }
    const capture = validateVaultCapture(parsed, where);
    const identity = `${capture.mount}\u0000${capture.path}`;
    if (seen.has(identity)) {
      refuse(`the capture file declares this mount and canonical secret path more than once (${where})`);
    }
    seen.add(identity);
    if (pathSegments(capture.path).length > MAX_DEPTH) {
      refuse(`the ${where} secret path exceeds the ${MAX_DEPTH}-level depth bound`);
    }
    // Charge every record now (Go charges in this first pass): a deleted/
    // destroyed record costs one slot, a live one costs a slot per data field —
    // so a capture of 50 000 deleted records cannot walk past the record cap.
    if (capture.deleted || capture.destroyed) {
      budget.record();
    } else {
      for (const _field of Object.keys(capture.data)) {
        void _field;
        budget.record();
      }
    }
    captures.push(capture);
  }
  if (captures.length === 0) {
    refuse('the file holds no Vault/OpenBao JSON Lines capture record');
  }
  captures.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  const mount = captures[0]?.mount ?? '';
  const version = captures[0]?.engineVersion ?? 0;
  for (const capture of captures) {
    if (capture.mount !== mount || capture.engineVersion !== version) {
      refuse('one capture file must describe one mount and one KV engine version');
    }
  }
  const prefix = commonPathPrefix(captures.map((capture) => pathSegments(capture.path)));
  const records: SourceRecord[] = [];
  const skipped: string[] = [];
  for (const capture of captures) {
    if (capture.deleted || capture.destroyed) {
      skipped.push(safeName(capture.path));
      continue;
    }
    const folder = pathSegments(stripPrefix(capture.path, prefix));
    for (const [name, field] of Object.entries(capture.data).sort(([a], [b]) => compareCodePoints(a, b))) {
      const value = typeof field === 'string' ? field : canonicalJson(field);
      budget.bytes(byteLength(value));
      records.push({ folder, sourceName: name, value, byteLength: byteLength(value), binary: value.includes('\u0000') });
    }
  }
  if (records.length === 0) {
    refuse('the capture holds no current Vault/OpenBao KV field');
  }
  return { records, skipped };
}

function validateVaultCapture(
  raw: Record<string, JsonValue>,
  where: string,
): {
  path: string;
  mount: string;
  engineVersion: number;
  secretVersion: number | null;
  deleted: boolean;
  destroyed: boolean;
  data: { [key: string]: JsonValue };
} {
  const known = new Set(['path', 'mount', 'engine_version', 'secret_version', 'deleted', 'destroyed', 'data']);
  for (const field of Object.keys(raw)) {
    if (!known.has(field)) {
      refuse(`the ${where} carries an unexpected field ${safeName(field)}`);
    }
  }
  const missing: string[] = [];
  if (!('deleted' in raw) || typeof raw.deleted !== 'boolean') missing.push('deleted');
  if (!('destroyed' in raw) || typeof raw.destroyed !== 'boolean') missing.push('destroyed');
  // `isJsonObject`, not the generic `isObject`: a `JsonNumber` is a class
  // instance and would pass `isObject`, letting `"data":1` slip through (Go's
  // typed decode into a map refuses the whole file).
  if (!('data' in raw) || !isJsonObject(raw.data)) missing.push('data');
  if (missing.length > 0) {
    refuse(`the ${where} capture record omits required ${missing.join(', ')}`);
  }
  const mount = typeof raw.mount === 'string' ? raw.mount : '';
  if (mount === '' || mount.includes('/')) {
    refuse(`the ${where} capture record carries no single mount name`);
  }
  const path = typeof raw.path === 'string' ? raw.path : '';
  if (!canonicalSourcePath(path)) {
    refuse(`the ${where} capture record carries no canonical secret path`);
  }
  // engine_version and secret_version are Go `int` fields: a fractional or
  // exponent literal (`1.5`, `2e0`) cannot unmarshal into `int` and is refused.
  const engineVersion = isJsonNumber(raw.engine_version) ? asInteger(raw.engine_version) : null;
  if (engineVersion !== 1 && engineVersion !== 2) {
    refuse(`the ${where} capture record's engine_version is neither 1 nor 2`);
  }
  const secretVersion = isJsonNumber(raw.secret_version) ? asInteger(raw.secret_version) : null;
  if (engineVersion === 1 && 'secret_version' in raw) {
    refuse(`a KV v1 capture record carries a v2 secret_version (${where})`);
  }
  if (engineVersion === 2 && (secretVersion === null || secretVersion < 1)) {
    refuse(`a KV v2 capture record carries no positive secret_version (${where})`);
  }
  const data = isJsonObject(raw.data) ? raw.data : {};
  const deleted = raw.deleted === true;
  const destroyed = raw.destroyed === true;
  if (!deleted && !destroyed && Object.keys(data).length === 0) {
    refuse(`a current capture record carries no data fields (${where})`);
  }
  return { path, mount, engineVersion, secretVersion, deleted, destroyed, data };
}

const INT64_MIN = -9223372036854775808n;
const INT64_MAX = 9223372036854775807n;

/** A JSON number as a Go `int`: the exact integer, or null if the literal is
 * fractional, has an exponent, or overflows signed 64-bit — all of which
 * `json.Unmarshal` into `int` refuses. */
function asInteger(number: JsonNumber): number | null {
  if (!/^-?[0-9]+$/.test(number.literal)) {
    return null;
  }
  const magnitude = BigInt(number.literal);
  if (magnitude < INT64_MIN || magnitude > INT64_MAX) {
    return null;
  }
  return Number(magnitude);
}

function pathSegments(value: string): string[] {
  const trimmed = value.replace(/^\/+/, '').replace(/\/+$/, '');
  return trimmed === '' ? [] : trimmed.split('/');
}

function canonicalSourcePath(value: string): boolean {
  const segments = pathSegments(value);
  if (segments.length === 0 || segments.join('/') !== value) {
    return false;
  }
  return segments.every((segment) => segment !== '' && segment !== '.' && segment !== '..');
}

function commonPathPrefix(paths: string[][]): string {
  if (paths.length === 0) return '';
  let prefix = [...(paths[0] ?? [])];
  for (const candidate of paths.slice(1)) {
    let i = 0;
    while (i < prefix.length && i < candidate.length && prefix[i] === candidate[i]) i += 1;
    prefix = prefix.slice(0, i);
  }
  return prefix.join('/');
}

function stripPrefix(path: string, prefix: string): string {
  return prefix !== '' && path.startsWith(prefix) ? path.slice(prefix.length) : path;
}

// ── Mapping, bounds, and the boundary ───────────────────────────────────────

/** Validates one target folder path, mirroring `targetFolderPath`: no empty or
 * reserved segment, no surrounding whitespace, no separator inside a segment. */
function targetFolderPath(folder: readonly string[]): string {
  for (const segment of folder) {
    if (segment === '' || segment === '.' || segment === '..' || segment.trim() !== segment || segment.includes('/')) {
      refuse(`a folder path segment in ${safeName(folder.join('/'))} is empty, reserved, or malformed`);
    }
  }
  return folder.join('/');
}

/**
 * mapRecords runs the rename transform and the post-transform collision check
 * over every record, mirroring `plan.go`'s `mapRecords`. Every offender is
 * collected before refusing — an unmappable or colliding name is one fix in one
 * edit, not one run each. `k8s` collapses a single-Secret folder onto the
 * environment root; other connectors carry their folder chain.
 */
function mapRecords(
  connector: FileConnector,
  records: readonly SourceRecord[],
): { entries: SourceEntry[]; renames: SourceRename[] } {
  const rootCollapse = connector === 'k8s' && singleSourceFolder(records);
  const rows: SourceEntry[] = [];
  const renames: SourceRename[] = [];
  const unmappable: string[] = [];
  const collisions: string[] = [];
  const origin = new Map<string, string>();
  for (const record of records) {
    const sourcePath = recordPath(record);
    const transformed = transformName(record.sourceName);
    if ('error' in transformed) {
      unmappable.push(sourcePath);
      continue;
    }
    const target = transformed.target;
    if (target !== record.sourceName) {
      renames.push({ from: record.sourceName, to: target });
    }
    const prior = origin.get(target);
    if (prior !== undefined) {
      collisions.push(`${prior} and ${sourcePath} both map onto ${safeName(target)}`);
      continue;
    }
    origin.set(target, sourcePath);
    const folderPath = rootCollapse ? '' : targetFolderPath(record.folder);
    rows.push({ key: target, sourceName: record.sourceName, value: record.value, folderPath });
  }
  if (unmappable.length > 0) {
    unmappable.sort();
    refuse(
      `${unmappable.length} source name(s) fall outside the documented transform; rename each explicitly at ` +
        `the source or import through the CLI: ${unmappable.join(', ')}`,
    );
  }
  if (collisions.length > 0) {
    collisions.sort();
    refuse(`${collisions.length} post-transform collision(s); resolve each with an explicit rename: ${collisions.join('; ')}`);
  }
  rows.sort((a, b) => (a.key < b.key ? -1 : a.key > b.key ? 1 : 0));
  renames.sort((a, b) => (a.from < b.from ? -1 : a.from > b.from ? 1 : 0));
  return { entries: rows, renames };
}

function singleSourceFolder(records: readonly SourceRecord[]): boolean {
  const seen = new Set<string>();
  for (const record of records) {
    seen.add(record.folder.join('/'));
    if (seen.size > 1) return false;
  }
  return seen.size === 1;
}

/** The uniform per-value bound and UTF-8/NUL rule, mirroring `validateResult`:
 * both name every offending key at once. */
function validateRecords(records: readonly SourceRecord[]): void {
  if (records.length > MAX_RECORDS) {
    refuse(`the source holds ${records.length} records, over the ${MAX_RECORDS}-record cap`);
  }
  const oversized: string[] = [];
  const binary: string[] = [];
  for (const record of records) {
    if (record.byteLength > MAX_VALUE_BYTES) {
      oversized.push(recordPath(record));
    } else if (record.binary) {
      binary.push(recordPath(record));
    }
  }
  if (oversized.length > 0) {
    refuse(`${oversized.length} value(s) exceed the ${MAX_VALUE_BYTES}-byte per-value cap: ${oversized.join(', ')}`);
  }
  if (binary.length > 0) {
    refuse(
      `${binary.length} value(s) are not UTF-8 text (invalid encoding or a NUL byte); Hikyo values are UTF-8 text: ` +
        binary.join(', '),
    );
  }
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/**
 * parseSource is the boundary: it runs the named connector over the file text
 * and returns the normalized entries, renames and skips — or a single
 * content-free refusal. Infisical requires its source env slug; the others take
 * none. The order of checks mirrors the CLI: file size → parse → per-value
 * bounds → name mapping and collisions.
 */
export function parseSource(
  connector: FileConnector,
  text: string,
  options: { readonly envSlug?: string } = {},
): SourceParse {
  try {
    if (byteLength(text) > MAX_FILE_BYTES) {
      refuse(`the file exceeds the ${MAX_FILE_BYTES}-byte per-file cap`);
    }
    const budget = newBudget();
    let records: SourceRecord[];
    let skipped: string[] = [];
    if (connector === 'k8s') {
      records = readK8s(text, budget);
    } else if (connector === 'infisical') {
      if (options.envSlug === undefined || options.envSlug === '') {
        refuse('an Infisical import names its source slice: choose the source environment slug');
      }
      const result = readInfisical(text, budget);
      records = result.records;
      skipped = result.skipped;
    } else {
      const result = readVault(text, budget);
      records = result.records;
      skipped = result.skipped;
    }
    validateRecords(records);
    const { entries, renames } = mapRecords(connector, records);
    return { ok: true, entries, renames, skipped };
  } catch (caught) {
    if (caught instanceof Refusal) {
      return { ok: false, reason: caught.message };
    }
    throw caught;
  }
}
