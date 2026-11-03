import { zJSONText } from './jsonText.ts';

/**
 * The no-code JSON Schema builder's model: a tree over the four keywords it can
 * show (`type`, `properties`, `required`, `items`). The schema TEXT stays the
 * source of truth in the form; the tree is a view that is rebuilt from it, so
 * any schema outside this vocabulary is refused here and stays raw JSON.
 */
export const BUILDER_TYPES = ['any', 'string', 'integer', 'number', 'boolean', 'object', 'array'] as const;
export type BuilderType = (typeof BUILDER_TYPES)[number];

export type BuilderNode = {
  readonly id: string;
  /** The property name; blank for a root or an array's item node. */
  readonly name: string;
  readonly required: boolean;
  readonly type: BuilderType;
  /** Kept when the type changes, so flipping back loses no work; only serialized for `object`. */
  readonly fields: readonly BuilderNode[];
  /** Kept when the type changes; only serialized for `array`. */
  readonly items: BuilderNode | null;
};

export const DIALECT_2020_12 = 'https://json-schema.org/draft/2020-12/schema';

// The server's bounds (internal/schema MaxJSONSchemaDepth, MaxJSONSchemaSubschemas)
// and the form's own text cap.
const MAX_DEPTH = 32;
const MAX_SUBSCHEMAS = 256;
export const MAX_SCHEMA_LENGTH = 16384;

const BUILDER_KEYWORDS = new Set(['type', 'properties', 'required', 'items']);

let lastId = 0;

export function newNode(type: BuilderType = 'any'): BuilderNode {
  lastId += 1;
  return { id: `schema-node-${String(lastId)}`, name: '', required: false, type, fields: [], items: null };
}

/** A fresh, required, text-typed field with no name yet. */
export function newField(): BuilderNode {
  return { ...newNode('string'), required: true };
}

export function changeType(node: BuilderNode, type: BuilderType): BuilderNode {
  return { ...node, type, items: type === 'array' && node.items === null ? newNode() : node.items };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/** The nodes the current type actually serializes. */
function children(node: BuilderNode): readonly BuilderNode[] {
  if (node.type === 'object') return node.fields;
  return node.type === 'array' && node.items !== null && node.items.type !== 'any' ? [node.items] : [];
}

function countNodes(node: BuilderNode): number {
  return children(node).reduce((total, child) => total + countNodes(child), 1);
}

function depthOf(node: BuilderNode): number {
  return 1 + children(node).reduce((deepest, child) => Math.max(deepest, depthOf(child)), 0);
}

// `depth` starts at 0 where the server's count starts at 1.
function tooDeep(value: unknown, depth: number): boolean {
  if (depth >= MAX_DEPTH) return true;
  if (Array.isArray(value)) return value.some((element) => tooDeep(element, depth + 1));
  return isRecord(value) && Object.values(value).some((member) => tooDeep(member, depth + 1));
}

/** Two shapes seen in one array become one: same type merges, integer and number widen, the rest is `any`. */
function merge(a: BuilderNode, b: BuilderNode): BuilderNode {
  if (a.type !== b.type) {
    const pair = [a.type, b.type];
    return newNode(pair.includes('integer') && pair.includes('number') ? 'number' : 'any');
  }
  if (a.type === 'object') {
    const inB = new Map(b.fields.map((field) => [field.name, field]));
    const inA = new Set(a.fields.map((field) => field.name));
    // A field is required only if every element so far carried it.
    const fields = [
      ...a.fields.map((field) => {
        const other = inB.get(field.name);
        return other === undefined
          ? { ...field, required: false }
          : { ...merge(field, other), name: field.name, required: field.required && other.required };
      }),
      ...b.fields.filter((field) => !inA.has(field.name)).map((field) => ({ ...field, required: false })),
    ];
    return { ...a, fields };
  }
  if (a.type === 'array') return { ...a, items: merge(a.items ?? newNode(), b.items ?? newNode()) };
  return a;
}

// Only shapes are read from the sample, never a value: the schema is exported to Git.
function infer(value: unknown): BuilderNode {
  if (typeof value === 'string') return newNode('string');
  if (typeof value === 'number') return newNode(Number.isInteger(value) ? 'integer' : 'number');
  if (typeof value === 'boolean') return newNode('boolean');
  if (Array.isArray(value)) {
    const shapes = value.map(infer);
    return { ...newNode('array'), items: shapes.length === 0 ? newNode() : shapes.reduce(merge) };
  }
  if (isRecord(value)) {
    return {
      ...newNode('object'),
      fields: Object.entries(value).map(([name, member]) => ({ ...infer(member), name, required: true })),
    };
  }
  return newNode();
}

/** Every field name in the part of the tree that serializes. */
export function fieldNames(node: BuilderNode): readonly string[] {
  const own = node.type === 'object' ? node.fields.map((field) => field.name) : [];
  return [...own, ...children(node).flatMap(fieldNames)];
}

/**
 * Whether schema text may hold one of these field names. Reads both spellings, so
 * an escape or a duplicate key cannot hide a name, and answers yes for text it
 * cannot read: this guards what a secret key exports, so it fails closed. A name
 * that is also a keyword, such as `type`, reads as present.
 */
export function mentionsFieldName(text: string, names: readonly string[]): boolean {
  if (names.length === 0 || text.trim() === '') return false;
  const quoted = names.map((name) => JSON.stringify(name));
  if (quoted.some((name) => text.includes(name))) return true;
  const parsed = zJSONText.safeParse(text);
  if (!parsed.success) return true;
  try {
    const canonical = JSON.stringify(parsed.data);
    return quoted.some((name) => canonical.includes(name));
  } catch {
    return true; // Deeper than this engine can stringify.
  }
}

/** A schema inferred from one sample value, or why it cannot be inferred. */
export function inferSchema(value: unknown): { readonly root: BuilderNode } | { readonly error: string } {
  if (tooDeep(value, 0)) {
    return { error: `This value is nested more than ${String(MAX_DEPTH)} levels deep, which a schema cannot describe.` };
  }
  const root = infer(value);
  // `any` serializes to no schema at all, which would read as clearing one.
  if (root.type === 'any') return { error: 'A null value has no shape to infer a schema from.' };
  const problem = builderSchemaError(root);
  if (problem !== null) return { error: problem };
  if (serializeBuilderSchema(root).length > MAX_SCHEMA_LENGTH) {
    return { error: `The schema for this value would exceed ${String(MAX_SCHEMA_LENGTH)} characters.` };
  }
  return { root };
}

function walk(schema: unknown, depth: number): BuilderNode | null {
  if (depth >= MAX_DEPTH || !isRecord(schema)) return null;
  if (Object.keys(schema).some((keyword) => !BUILDER_KEYWORDS.has(keyword))) return null;
  const { type: rawType, properties, required, items } = schema;
  const type = rawType === undefined ? 'any' : BUILDER_TYPES.find((candidate) => candidate !== 'any' && candidate === rawType);
  if (type === undefined) return null;
  if ((properties !== undefined || required !== undefined) && type !== 'object') return null;
  if (items !== undefined && type !== 'array') return null;

  const node = newNode(type);
  if (type === 'array') {
    const item = items === undefined ? newNode() : walk(items, depth + 1);
    return item === null ? null : { ...node, items: item };
  }
  if (type !== 'object') return node;

  const members = properties === undefined ? {} : properties;
  if (!isRecord(members)) return null;
  const fields: BuilderNode[] = [];
  for (const [name, member] of Object.entries(members)) {
    const child = walk(member, depth + 1);
    if (child === null) return null;
    fields.push({ ...child, name });
  }
  const declared = new Set(fields.map((field) => field.name));
  const requiredNames = new Set<string>();
  if (required !== undefined) {
    if (!Array.isArray(required)) return null;
    for (const name of required) {
      if (typeof name !== 'string' || requiredNames.has(name) || !declared.has(name)) return null;
      requiredNames.add(name);
    }
  }
  return { ...node, fields: fields.map((field) => ({ ...field, required: requiredNames.has(field.name) })) };
}

/**
 * The tree for a schema text, or null when the text is not exactly something the
 * builder can show. Strict on purpose: the builder must never silently drop a
 * keyword it does not know, and it only returns a tree builderSchemaError
 * accepts, so a clean tree is what the component can always rebuild to. Runs
 * during render on whatever is typed, so it never throws and its recursion is
 * capped.
 */
export function parseBuilderSchema(text: string): { readonly root: BuilderNode; readonly dialect: boolean } | null {
  if (text.trim() === '') return { root: newNode(), dialect: false };
  const parsed = zJSONText.safeParse(text);
  if (!parsed.success) return null;
  let body = parsed.data;
  let dialect = false;
  if (isRecord(body) && Object.hasOwn(body, '$schema')) {
    if (body['$schema'] !== DIALECT_2020_12) return null;
    dialect = true;
    body = Object.fromEntries(Object.entries(body).filter(([keyword]) => keyword !== '$schema'));
  }
  const root = walk(body, 0);
  return root === null || builderSchemaError(root) !== null ? null : { root, dialect };
}

function toSchema(node: BuilderNode): Record<string, unknown> {
  const entries: [string, unknown][] = [];
  if (node.type !== 'any') entries.push(['type', node.type]);
  if (node.type === 'object') {
    // `fromEntries`, not `obj[name] =`: a property named `__proto__` must stay a property.
    if (node.fields.length > 0) {
      entries.push(['properties', Object.fromEntries(node.fields.map((field) => [field.name, toSchema(field)]))]);
    }
    const required = node.fields.filter((field) => field.required).map((field) => field.name);
    if (required.length > 0) entries.push(['required', required]);
  }
  if (node.type === 'array' && node.items !== null && node.items.type !== 'any') {
    entries.push(['items', toSchema(node.items)]);
  }
  return Object.fromEntries(entries);
}

/** The schema text for a tree; `''` (no schema) for a bare `any` root with no dialect. */
export function serializeBuilderSchema(root: BuilderNode, dialect = false): string {
  const body = toSchema(root);
  if (!dialect && Object.keys(body).length === 0) return '';
  return JSON.stringify(dialect ? { $schema: DIALECT_2020_12, ...body } : body, null, 2);
}

/** Why one field's name is unusable among its siblings, or null. */
export function fieldNameProblem(siblings: readonly BuilderNode[], field: BuilderNode): string | null {
  if (field.name.trim() === '') return 'Every field needs a name.';
  if (siblings.some((other) => other.id !== field.id && other.name === field.name)) {
    return `Field names must be unique: ${field.name} is used twice.`;
  }
  return null;
}

/** The first reason the server or the text could not hold this tree: its bounds, then any field name. */
export function builderSchemaError(root: BuilderNode): string | null {
  if (depthOf(root) > MAX_DEPTH) return `A schema nests at most ${String(MAX_DEPTH)} levels.`;
  if (countNodes(root) > MAX_SUBSCHEMAS) return `A schema holds at most ${String(MAX_SUBSCHEMAS)} fields and items.`;
  return firstNameProblem(root);
}

function firstNameProblem(root: BuilderNode): string | null {
  const own = root.type === 'object' ? root.fields.map((field) => fieldNameProblem(root.fields, field)) : [];
  const found = own.find((problem) => problem !== null);
  if (found !== undefined) return found;
  for (const child of children(root)) {
    const problem = firstNameProblem(child);
    if (problem !== null) return problem;
  }
  return null;
}
