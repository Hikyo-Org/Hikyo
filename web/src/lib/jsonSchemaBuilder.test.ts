import { describe, expect, it } from 'vitest';

import {
  builderSchemaError,
  carryIds,
  changeType,
  DIALECT_2020_12,
  inferSchema,
  mentionsFieldName,
  newField,
  newNode,
  parseBuilderSchema,
  serializeBuilderSchema,
  type BuilderNode,
} from './jsonSchemaBuilder.ts';

function inferred(value: unknown): unknown {
  const result = inferSchema(value);
  if ('error' in result) throw new Error(result.error);
  const text = serializeBuilderSchema(result.root);
  return text === '' ? null : JSON.parse(text);
}

function parsed(text: string): { root: BuilderNode; dialect: boolean } {
  const result = parseBuilderSchema(text);
  if (result === null) throw new Error(`not representable: ${text}`);
  return result;
}

describe('inferSchema', () => {
  it('infers nested objects with every sampled key required', () => {
    expect(inferred({ name: 'x', port: 80, ratio: 1.5, on: true, db: { host: 'h', tags: ['a'] } })).toEqual({
      type: 'object',
      properties: {
        name: { type: 'string' },
        port: { type: 'integer' },
        ratio: { type: 'number' },
        on: { type: 'boolean' },
        db: {
          type: 'object',
          properties: { host: { type: 'string' }, tags: { type: 'array', items: { type: 'string' } } },
          required: ['host', 'tags'],
        },
      },
      required: ['name', 'port', 'ratio', 'on', 'db'],
    });
  });

  it('splits 1 from 1.5', () => {
    expect(inferred(1)).toEqual({ type: 'integer' });
    expect(inferred(1.5)).toEqual({ type: 'number' });
  });

  it('maps null to any, never type null', () => {
    expect(inferred({ a: null })).toEqual({ type: 'object', properties: { a: {} }, required: ['a'] });
  });

  it('handles root arrays and primitives, empty arrays and empty objects', () => {
    expect(inferred([])).toEqual({ type: 'array' });
    expect(inferred({})).toEqual({ type: 'object' });
    expect(inferred('x')).toEqual({ type: 'string' });
    expect(inferred(true)).toEqual({ type: 'boolean' });
    expect(inferred([1, 2])).toEqual({ type: 'array', items: { type: 'integer' } });
  });

  it('merges array elements: union of properties, required only where always present', () => {
    expect(inferred([{ a: 1, b: 'x' }, { a: 2, c: true }])).toEqual({
      type: 'array',
      items: {
        type: 'object',
        properties: { a: { type: 'integer' }, b: { type: 'string' }, c: { type: 'boolean' } },
        required: ['a'],
      },
    });
  });

  it('keeps a merged field required only if every element at every level carried it', () => {
    // The sample must satisfy its own schema: `x` is in both inner arrays, `y` in one.
    expect(inferred([[{ x: 1 }], [{ x: 1 }, { y: 1 }]])).toEqual({
      type: 'array',
      items: {
        type: 'array',
        items: { type: 'object', properties: { x: { type: 'integer' }, y: { type: 'integer' } } },
      },
    });
    expect(inferred([{ a: { b: 1 } }, { a: {} }])).toEqual({
      type: 'array',
      items: {
        type: 'object',
        properties: { a: { type: 'object', properties: { b: { type: 'integer' } } } },
        required: ['a'],
      },
    });
  });

  it('never sets a string rule or closes an object, which one sample cannot establish', () => {
    const sample = { name: 'x', tags: ['a', 'bb'], nested: { id: 'abc', list: [{ k: 'v' }] } };
    const root = inferSchemaRoot(sample);
    const nodes = (node: BuilderNode): BuilderNode[] => [node, ...node.fields.flatMap(nodes), ...(node.items === null ? [] : nodes(node.items))];
    for (const node of nodes(root)) {
      expect(node).toMatchObject({ minLength: '', maxLength: '', pattern: '', closed: false });
    }
    const text = serializeBuilderSchema(root);
    for (const keyword of ['minLength', 'maxLength', 'pattern', 'additionalProperties']) {
      expect(text).not.toContain(`"${keyword}"`);
    }
  });

  it('keeps a shape an empty array sample says nothing about', () => {
    expect(inferred([[], ['x']])).toEqual({ type: 'array', items: { type: 'array', items: { type: 'string' } } });
    expect(inferred([['x'], []])).toEqual({ type: 'array', items: { type: 'array', items: { type: 'string' } } });
    expect(inferred([[], []])).toEqual({ type: 'array', items: { type: 'array' } });
  });

  it('gives an inferred empty array the item node the view edits', () => {
    const result = inferSchema({ list: [] });
    if ('error' in result) throw new Error(result.error);
    expect(result.root.fields[0]?.items?.type).toBe('any');
  });

  it('holds the merged schema, not the raw sample, to the depth limit', () => {
    const nest = (levels: number): unknown => (levels === 0 ? 'x' : [nest(levels - 1)]);
    expect(inferSchema(['x', nest(40)])).toHaveProperty('root');
    expect(inferSchema(nest(40))).toEqual({ error: expect.stringContaining('32 levels') });
    expect(inferSchema(nest(1000))).toEqual({ error: expect.stringContaining('1000 levels') });
  });

  it('refuses a null sample rather than reading as "no schema"', () => {
    expect(inferSchema(null)).toEqual({ error: expect.stringContaining('null') });
  });

  it('refuses a sample with a blank field name', () => {
    expect(inferSchema({ '': 1 })).toEqual({ error: 'Every field needs a name.' });
  });

  it('widens integer and number, and turns any other conflict into any', () => {
    expect(inferred([1, 2.5])).toEqual({ type: 'array', items: { type: 'number' } });
    expect(inferred([1, 'x'])).toEqual({ type: 'array' });
    expect(inferred([1, null])).toEqual({ type: 'array' });
    expect(inferred([{ a: 1 }, { a: 'x' }])).toEqual({
      type: 'array',
      items: { type: 'object', properties: { a: {} }, required: ['a'] },
    });
  });

  it('never copies a sample value into the schema', () => {
    const sample = { token: 'AKIAIOSFODNN7EXAMPLE', n: 424242, list: ['s3cr3t-a', 's3cr3t-b'], deep: { k: 'zzz-value' } };
    const text = serializeBuilderSchema(inferSchemaRoot(sample));
    for (const leaked of ['AKIAIOSFODNN7EXAMPLE', '424242', 's3cr3t', 'zzz-value']) {
      expect(text).not.toContain(leaked);
    }
    for (const keyword of ['const', 'enum', 'examples', 'default', 'format', 'uniqueItems', '$id']) {
      expect(text).not.toContain(`"${keyword}"`);
    }
    expect(text).not.toContain('$schema');
  });

  it('refuses a schema over 16384 characters and over 256 nodes', () => {
    const wide = Object.fromEntries(Array.from({ length: 700 }, (_, index) => [`field_${String(index)}`, 's']));
    expect(inferSchema(wide)).toHaveProperty('error');
    const longNames = Object.fromEntries(Array.from({ length: 100 }, (_, index) => [`${'n'.repeat(200)}${String(index)}`, 1]));
    expect(inferSchema(longNames)).toHaveProperty('error');
  });

  it('keeps a __proto__ key as a property', () => {
    const value: unknown = JSON.parse('{"__proto__":{"a":1}}');
    expect(serializeBuilderSchema(inferSchemaRoot(value))).toContain('"__proto__"');
  });
});

function inferSchemaRoot(value: unknown): BuilderNode {
  const result = inferSchema(value);
  if ('error' in result) throw new Error(result.error);
  return result.root;
}

describe('serializeBuilderSchema', () => {
  it('writes keys in the order $schema, type, minLength, maxLength, pattern, properties, required, additionalProperties, items', () => {
    const root = { ...newNode('object'), closed: true, fields: [{ ...newField(), name: 'a' }] };
    expect(serializeBuilderSchema(root, true)).toBe(
      JSON.stringify(
        {
          $schema: DIALECT_2020_12,
          type: 'object',
          properties: { a: { type: 'string' } },
          required: ['a'],
          additionalProperties: false,
        },
        null,
        2,
      ),
    );
    const text = { ...newNode('string'), minLength: '1', maxLength: '8', pattern: '^a' };
    expect(Object.keys(JSON.parse(serializeBuilderSchema(text)))).toEqual(['type', 'minLength', 'maxLength', 'pattern']);
    const list = { ...newNode('array'), items: text };
    expect(Object.keys(JSON.parse(serializeBuilderSchema(list)))).toEqual(['type', 'items']);
  });

  it('writes lengths as JSON numbers and omits empty rules and an open object', () => {
    const text = { ...newNode('string'), minLength: '03', maxLength: '', pattern: '' };
    expect(JSON.parse(serializeBuilderSchema(text))).toEqual({ type: 'string', minLength: 3 });
    expect(serializeBuilderSchema(text)).toContain('"minLength": 3');
    expect(JSON.parse(serializeBuilderSchema(newNode('string')))).toEqual({ type: 'string' });
    expect(JSON.parse(serializeBuilderSchema(newNode('object')))).toEqual({ type: 'object' });
  });

  it('keeps string rules and a closed object across a type flip without writing them', () => {
    const text = { ...newNode('string'), minLength: '1', maxLength: '8', pattern: '^a' };
    const asNumber = changeType(text, 'integer');
    expect(JSON.parse(serializeBuilderSchema(asNumber))).toEqual({ type: 'integer' });
    expect(serializeBuilderSchema(changeType(asNumber, 'string'))).toBe(serializeBuilderSchema(text));
    const closed = { ...newNode('object'), closed: true };
    expect(JSON.parse(serializeBuilderSchema(changeType(closed, 'string')))).toEqual({ type: 'string' });
    expect(JSON.parse(serializeBuilderSchema(changeType(changeType(closed, 'string'), 'object')))).toEqual({
      type: 'object',
      additionalProperties: false,
    });
  });

  it('gives no schema for a bare any root, and a dialect-only schema when the dialect was declared', () => {
    expect(serializeBuilderSchema(newNode())).toBe('');
    expect(JSON.parse(serializeBuilderSchema(newNode(), true))).toEqual({ $schema: DIALECT_2020_12 });
  });

  it('omits empty required and properties, and items of any', () => {
    expect(JSON.parse(serializeBuilderSchema(newNode('object')))).toEqual({ type: 'object' });
    expect(JSON.parse(serializeBuilderSchema(changeType(newNode(), 'array')))).toEqual({ type: 'array' });
    const optional = { ...newNode('object'), fields: [{ ...newField(), name: 'a', required: false }] };
    expect(JSON.parse(serializeBuilderSchema(optional))).toEqual({
      type: 'object',
      properties: { a: { type: 'string' } },
    });
  });

  it('serializes only what the current type uses, and flipping back restores the work', () => {
    const object = { ...newNode('object'), fields: [{ ...newField(), name: 'a' }] };
    const asString = changeType(object, 'string');
    expect(JSON.parse(serializeBuilderSchema(asString))).toEqual({ type: 'string' });
    expect(serializeBuilderSchema(changeType(asString, 'object'))).toBe(serializeBuilderSchema(object));
  });

  it('round-trips a property named __proto__', () => {
    const text = '{"type":"object","properties":{"__proto__":{"type":"string"}},"required":["__proto__"]}';
    const out = serializeBuilderSchema(parsed(text).root);
    expect(out).toContain('"__proto__"');
    expect(Object.keys(JSON.parse(out).properties)).toEqual(['__proto__']);
    expect(JSON.parse(out).required).toEqual(['__proto__']);
  });
});

describe('parseBuilderSchema', () => {
  it('reads empty text as the any root', () => {
    expect(parsed('').root.type).toBe('any');
    expect(parsed('  \n').dialect).toBe(false);
  });

  it('is stable: serialize(parse(text)) equals a canonical text, and re-parsing is a fixed point', () => {
    const canonical = serializeBuilderSchema(
      inferSchemaRoot({ a: [{ b: 1 }], c: { d: 'x' }, e: 1.5 }),
    );
    const again = parsed(canonical);
    expect(serializeBuilderSchema(again.root, again.dialect)).toBe(canonical);
  });

  it.each([
    ['minLength', '{"type":"string","minLength":2}', { minLength: '2', maxLength: '', pattern: '' }],
    ['maxLength', '{"type":"string","maxLength":0}', { minLength: '', maxLength: '0', pattern: '' }],
    ['pattern', '{"type":"string","pattern":"^[a-z]+$"}', { minLength: '', maxLength: '', pattern: '^[a-z]+$' }],
    ['equal lengths', '{"type":"string","minLength":3,"maxLength":3}', { minLength: '3', maxLength: '3', pattern: '' }],
  ])('reads and writes back %s', (_label, text, rules) => {
    const { root } = parsed(text);
    expect(root).toMatchObject({ type: 'string', ...rules });
    expect(serializeBuilderSchema(root)).toBe(JSON.stringify(JSON.parse(text), null, 2));
  });

  it('reads and writes back additionalProperties false, at the root and nested', () => {
    const text = JSON.stringify(
      {
        type: 'object',
        properties: { a: { type: 'array', items: { type: 'object', additionalProperties: false } } },
        additionalProperties: false,
      },
      null,
      2,
    );
    const { root } = parsed(text);
    expect(root.closed).toBe(true);
    expect(root.fields[0]?.items?.closed).toBe(true);
    expect(serializeBuilderSchema(root)).toBe(text);
  });

  it('keeps the 2020-12 dialect and refuses any other', () => {
    const text = serializeBuilderSchema(newNode('object'), true);
    const result = parsed(text);
    expect(result.dialect).toBe(true);
    expect(serializeBuilderSchema(result.root, result.dialect)).toBe(text);
    expect(parseBuilderSchema('{"$schema":"http://json-schema.org/draft-07/schema#","type":"object"}')).toBeNull();
    expect(parseBuilderSchema('{"type":"object","properties":{"a":{"$schema":"' + DIALECT_2020_12 + '"}}}')).toBeNull();
  });

  it.each([
    ['$ref', '{"$ref":"#/$defs/a"}'],
    ['minimum', '{"type":"integer","minimum":1}'],
    ['format', '{"type":"string","format":"uri"}'],
    ['additionalProperties: true', '{"type":"object","additionalProperties":true}'],
    ['additionalProperties as a schema', '{"type":"object","additionalProperties":{"type":"string"}}'],
    ['additionalProperties on a string', '{"type":"string","additionalProperties":false}'],
    ['additionalProperties without a type', '{"additionalProperties":false}'],
    ['minLength on an object', '{"type":"object","minLength":1}'],
    ['maxLength on an array', '{"type":"array","maxLength":1}'],
    ['pattern on an integer', '{"type":"integer","pattern":"^a"}'],
    ['minLength without a type', '{"minLength":1}'],
    ['a fractional minLength', '{"type":"string","minLength":1.5}'],
    ['a negative minLength', '{"type":"string","minLength":-1}'],
    ['a string minLength', '{"type":"string","minLength":"1"}'],
    ['a maxLength past the safe integers', '{"type":"string","maxLength":9007199254740992}'],
    ['a null maxLength', '{"type":"string","maxLength":null}'],
    ['an empty pattern', '{"type":"string","pattern":""}'],
    ['a non-string pattern', '{"type":"string","pattern":1}'],
    ['a minLength above the maxLength', '{"type":"string","minLength":5,"maxLength":2}'],
    ['a nested rule that is not a length', '{"type":"object","properties":{"a":{"type":"string","maxLength":0.5}}}'],
    ['description', '{"type":"object","description":"x"}'],
    ['enum', '{"enum":["a"]}'],
    ['a nested keyword', '{"type":"object","properties":{"a":{"type":"string","format":"uri"}}}'],
    ['a boolean schema', 'true'],
    ['a boolean property schema', '{"type":"object","properties":{"a":false}}'],
    ['a type array', '{"type":["string","null"]}'],
    ['type null', '{"type":"null"}'],
    ['an unknown type', '{"type":"any"}'],
    ['properties on a string', '{"type":"string","properties":{}}'],
    ['properties without a type', '{"properties":{"a":{}}}'],
    ['required on an array', '{"type":"array","required":[]}'],
    ['items on an object', '{"type":"object","items":{}}'],
    ['a required name that is not declared', '{"type":"object","required":["a"]}'],
    ['a duplicate required name', '{"type":"object","properties":{"a":{}},"required":["a","a"]}'],
    ['a non-string required name', '{"type":"object","properties":{"a":{}},"required":[1]}'],
    ['non-object properties', '{"type":"object","properties":[]}'],
    ['a primitive root', '"x"'],
    ['an array root', '[]'],
    ['invalid JSON', '{"type":'],
  ])('refuses %s', (_label, text) => {
    expect(parseBuilderSchema(text)).toBeNull();
  });

  it('returns null for a depth bomb without throwing', () => {
    const bomb = '{"items":'.repeat(5000) + '{}' + '}'.repeat(5000);
    expect(parseBuilderSchema(bomb)).toBeNull();
    const typed = '{"type":"array","items":'.repeat(5000) + '{}' + '}'.repeat(5000);
    expect(parseBuilderSchema(typed)).toBeNull();
  });

  it('accepts exactly the 32 nodes of depth the server allows', () => {
    const nest = (levels: number): string =>
      levels === 0 ? '{}' : `{"type":"array","items":${nest(levels - 1)}}`;
    expect(parseBuilderSchema(nest(31))).not.toBeNull();
    expect(parseBuilderSchema(nest(32))).toBeNull();
  });
});

describe('builderSchemaError', () => {
  const field = (name: string): BuilderNode => ({ ...newField(), name });

  it('is null for a well-named tree', () => {
    expect(builderSchemaError({ ...newNode('object'), fields: [field('a'), field('b')] })).toBeNull();
  });

  it('flags a blank or duplicate name in the same object', () => {
    expect(builderSchemaError({ ...newNode('object'), fields: [field('a'), field('  ')] })).toMatch(/needs a name/);
    expect(builderSchemaError({ ...newNode('object'), fields: [field('a'), field('a')] })).toMatch(/unique/);
  });

  it('allows the same name at different levels and finds problems nested in objects and array items', () => {
    const inner = { ...field('a'), type: 'object' as const, fields: [field('a')] };
    expect(builderSchemaError({ ...newNode('object'), fields: [inner] })).toBeNull();
    const nested = { ...newNode('array'), items: { ...newNode('object'), fields: [field('')] } };
    expect(builderSchemaError(nested)).toMatch(/needs a name/);
  });

  it('ignores fields a different current type does not serialize', () => {
    expect(builderSchemaError({ ...newNode('string'), fields: [field('')] })).toBeNull();
  });

  it('flags a length that is not a whole number of 0 or more, and a min above the max', () => {
    const text = (minLength: string, maxLength: string): BuilderNode => ({ ...newNode('string'), minLength, maxLength });
    expect(builderSchemaError(text('0', '9'))).toBeNull();
    expect(builderSchemaError(text('4', '4'))).toBeNull();
    for (const bad of ['1.5', '-1', 'abc', ' 2', '1e3', '9007199254740992']) {
      expect(builderSchemaError(text(bad, ''))).toMatch(/whole number/);
      expect(builderSchemaError(text('', bad))).toMatch(/whole number/);
    }
    expect(builderSchemaError(text('5', '2'))).toMatch(/Min length cannot be more than max length/);
    expect(builderSchemaError(text('9007199254740991', '9007199254740990'))).toMatch(/cannot be more/);
  });

  it('reads rules nested in fields and items, but not those of a type that does not write them', () => {
    const bad = { ...newNode('string'), minLength: 'x' };
    expect(builderSchemaError({ ...newNode('object'), fields: [{ ...newField(), name: 'a', minLength: 'x' }] })).toMatch(/whole number/);
    expect(builderSchemaError({ ...newNode('array'), items: bad })).toMatch(/whole number/);
    expect(builderSchemaError({ ...bad, type: 'integer' })).toBeNull();
  });

  it('reports bounds before a name or a rule', () => {
    const chain = (levels: number): BuilderNode =>
      levels === 1 ? { ...newNode('string'), minLength: 'x' } : { ...newNode('array'), items: chain(levels - 1) };
    expect(builderSchemaError(chain(33))).toContain('32 levels');
    expect(builderSchemaError(chain(32))).toMatch(/whole number/);
  });
});

describe('carryIds', () => {
  const named = (name: string, extra: Partial<BuilderNode> = {}): BuilderNode => ({ ...newField(), name, ...extra });

  it('moves ids root to root, fields by name and array items to items, and leaves new nodes fresh', () => {
    const before: BuilderNode = {
      ...newNode('object'),
      fields: [named('a'), named('list', { type: 'array', items: named('', { type: 'object', fields: [named('deep')] }) })],
    };
    const after: BuilderNode = {
      ...newNode('object'),
      // Reordered, with one field gone and one new.
      fields: [
        named('fresh'),
        named('list', { type: 'array', items: named('', { type: 'object', fields: [named('deep'), named('other')] }) }),
        named('a'),
      ],
    };
    const carried = carryIds(before, after);
    expect(carried.id).toBe(before.id);
    const [fresh, list, a] = carried.fields;
    expect(a?.id).toBe(before.fields[0]?.id);
    expect(list?.id).toBe(before.fields[1]?.id);
    expect(fresh?.id).toBe(after.fields[0]?.id);
    const beforeItem = before.fields[1]?.items;
    expect(list?.items?.id).toBe(beforeItem?.id);
    expect(list?.items?.fields[0]?.id).toBe(beforeItem?.fields[0]?.id);
    expect(list?.items?.fields[1]?.id).toBe(after.fields[1]?.items?.fields[1]?.id);
  });

  it('changes nothing but ids, and never gives two nodes one id', () => {
    const before: BuilderNode = { ...newNode('object'), fields: [named('a', { minLength: '1' })] };
    const after: BuilderNode = { ...newNode('object'), closed: true, fields: [named('a', { minLength: '2' }), named('a')] };
    const carried = carryIds(before, after);
    expect(carried).toMatchObject({ closed: true, fields: [{ minLength: '2' }, { minLength: '' }] });
    const ids = [carried.id, ...carried.fields.map((field) => field.id)];
    expect(new Set(ids).size).toBe(ids.length);
  });

  it('copes with an items node on only one side', () => {
    const withItems = { ...newNode('array'), items: newNode('string') };
    const without = newNode('array');
    expect(carryIds(withItems, without).items).toBeNull();
    expect(carryIds(without, withItems).items?.id).toBe(withItems.items.id);
  });
});

describe('builderSchemaError bounds', () => {
  it('holds a hand-built tree to the server depth and node limits', () => {
    const chain = (levels: number): BuilderNode =>
      levels === 1 ? newNode('string') : { ...newNode('array'), items: chain(levels - 1) };
    expect(builderSchemaError(chain(32))).toBeNull();
    expect(builderSchemaError(chain(33))).toContain('32 levels');
    const many = Array.from({ length: 256 }, (_, index) => ({ ...newField(), name: `f${String(index)}` }));
    expect(builderSchemaError({ ...newNode('object'), fields: many })).toContain('256');
    expect(builderSchemaError({ ...newNode('object'), fields: many.slice(1) })).toBeNull();
  });
});

describe('builder bounds match what serializes', () => {
  it('does not count an unconstrained item the text never carries', () => {
    const emptyArrays = Object.fromEntries(Array.from({ length: 129 }, (_, index) => [`a${String(index)}`, []]));
    expect(inferSchema(emptyArrays)).toHaveProperty('root');
    const text = JSON.stringify({
      type: 'object',
      properties: Object.fromEntries(Array.from({ length: 129 }, (_, index) => [`a${String(index)}`, { type: 'array' }])),
    });
    expect(parseBuilderSchema(text)).not.toBeNull();
  });

  it('does not throw on a pasted schema far past the node limit', () => {
    const properties = Object.fromEntries(Array.from({ length: 150_000 }, (_, index) => [`f${String(index)}`, {}]));
    expect(parseBuilderSchema(JSON.stringify({ type: 'object', properties }))).toBeNull();
  });
});

describe('mentionsFieldName', () => {
  it('finds a name however it is spelled and keeps unrelated text', () => {
    expect(mentionsFieldName('{"properties":{"AKIA":{}}}', ['AKIA'])).toBe(true);
    expect(mentionsFieldName('{"properties":{"\\u0041KIA":{}}}', ['AKIA'])).toBe(true);
    expect(mentionsFieldName('{"properties":{"AKIA":{}},"properties":{}}', ['AKIA'])).toBe(true);
    expect(mentionsFieldName('{"type":"string"}', ['AKIA'])).toBe(false);
    expect(mentionsFieldName('', ['AKIA'])).toBe(false);
    expect(mentionsFieldName('{"properties":{"AKIA":{}}}', [])).toBe(false);
  });

  it('fails closed on text it cannot read', () => {
    expect(mentionsFieldName('{"properties":{"AKIA:{}}}', ['AKIA'])).toBe(true);
    // Whether stringifying this overflows depends on the engine's stack, so the
    // name in the raw text must be found before that can matter.
    const deep = `{"AKIA":1,"$comment":${'['.repeat(200_000)}${']'.repeat(200_000)}}`;
    expect(mentionsFieldName(deep, ['AKIA'])).toBe(true);
  });
});

describe('builder bounds and additionalProperties', () => {
  const many = (count: number) =>
    Array.from({ length: count }, (_, index) => ({ ...newField(), name: `f${String(index)}` }));

  it('counts the false subschema the server counts', () => {
    expect(builderSchemaError({ ...newNode('object'), closed: true, fields: many(254) })).toBeNull();
    expect(builderSchemaError({ ...newNode('object'), closed: true, fields: many(255) })).toContain('256');
    expect(builderSchemaError({ ...newNode('object'), closed: false, fields: many(255) })).toBeNull();
  });

  it('counts its depth too', () => {
    const chain = (levels: number): BuilderNode =>
      levels === 1 ? { ...newNode('object'), closed: true } : { ...newNode('array'), items: chain(levels - 1) };
    expect(builderSchemaError(chain(31))).toBeNull();
    expect(builderSchemaError(chain(32))).toContain('32 levels');
  });

  it('names the field a refusal is about', () => {
    const host = { ...newField(), name: 'host', minLength: 'x' };
    expect(builderSchemaError({ ...newNode('object'), fields: [host] })).toBe('host: Min length: Use a whole number of 0 or more.');
  });
});
