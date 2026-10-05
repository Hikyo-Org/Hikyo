import assert from 'node:assert/strict';
import { existsSync } from 'node:fs';
import { registerHooks } from 'node:module';
import { spawnSync } from 'node:child_process';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

import { zRevisionBefore } from './generated/zod.gen.ts';
import { z } from 'zod';
import BigNumber from 'bignumber.js';
import { parseJson } from './parseJson.ts';

// The generator emits bundler-style relative imports. Resolve only this
// package's source tree for the Node execution of the actual HTTP/SSE runtime.
registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier.startsWith('.') && context.parentURL?.startsWith(new URL('./', import.meta.url).href)) {
      const url = new URL(specifier, context.parentURL);
      for (const candidate of [url.href + '.ts', url.href + '/index.ts']) {
        if (existsSync(fileURLToPath(candidate))) return nextResolve(candidate, context);
      }
    }
    return nextResolve(specifier, context);
  },
});

const { createClient } = await import('./generated/client/client.gen.ts');
const { createSseClient } = await import('./generated/core/serverSentEvents.gen.ts');

test('wire integers retain exact digits without changing ordinary numeric fields', () => {
  assert.deepEqual(parseJson('{"rows":[9007199254740993,-9223372036854775808,9223372036854775807],"count":42,"fraction":1.25,"label":"9007199254740993"}'), {
    rows: [9007199254740993n, -9223372036854775808n, 9223372036854775807n],
    count: 42,
    fraction: 1.25,
    label: '9007199254740993',
  });
  assert.equal(parseJson('9007199254740991'), Number.MAX_SAFE_INTEGER);
  assert.equal(parseJson('-9007199254740991'), Number.MIN_SAFE_INTEGER);
  assert.equal(parseJson('9007199254740993.0'), 9007199254740993n);
  assert.equal(parseJson('9.007199254740993e15'), 9007199254740993n);
  assert.throws(() => parseJson('{"revision":'), SyntaxError);
});

for (const revision of ['9007199254740993', '9223372036854775807']) {
  test(`generated HTTP client passes exact ${revision} to generated Zod validator`, async () => {
    const client = createClient({
      baseUrl: 'https://fixture.example',
      fetch: async () => new Response(revision, { headers: { 'Content-Type': 'application/json' } }),
    });
    const response = await client.get({
      url: '/revision',
      responseValidator: async value => { assert.equal(zRevisionBefore.parse(value), BigInt(revision)); },
    });
    assert.equal(zRevisionBefore.parse(response.data), BigInt(revision));
  });

  test(`generated SSE client passes exact ${revision} to generated Zod validator`, async () => {
    const response = createSseClient({
      url: 'https://fixture.example/events',
      fetch: async () => new Response(`data: ${revision}\n\n`, { headers: { 'Content-Type': 'text/event-stream' } }),
      responseValidator: async value => { assert.equal(zRevisionBefore.parse(value), BigInt(revision)); },
    });
    for await (const value of response.stream) {
      assert.equal(zRevisionBefore.parse(value), BigInt(revision));
      return;
    }
    assert.fail('SSE did not emit the revision');
  });
}

test('generated HTTP error decoding also retains integer evidence', async () => {
  const client = createClient({
    baseUrl: 'https://fixture.example',
    fetch: async () => new Response('{"generation":9007199254740993}', { status: 409 }),
  });
  const response = await client.get({ url: '/revision' });
  assert.deepEqual(response.error, { generation: 9007199254740993n });
});

test('generated Zod still refuses values outside int64 after lossless decoding', () => {
  assert.throws(() => zRevisionBefore.parse(parseJson('9223372036854775808')));
});

test('wire object keys cannot fabricate inherited required properties', async () => {
  const schema = z.object({ revision: z.bigint() }).strict();
  for (const key of ['__proto__', '__\\u0070roto__', 'constructor']) {
    const wire = `{"${key}":{"revision":9007199254740993}}`;
    const decoded = parseJson(wire);
    assert.equal(schema.safeParse(decoded).success, false);
    const ownKey = key === '__\\u0070roto__' ? '__proto__' : key;
    assert.deepEqual(decoded, { [ownKey]: { revision: 9007199254740993n } });
    assert.equal(Object.getPrototypeOf(decoded), Object.prototype);
    const client = createClient({ baseUrl: 'https://fixture.example', fetch: async () => new Response(wire, { headers: { 'Content-Type': 'application/json' } }) });
    const response = await client.get({ url: '/revision' });
    assert.deepEqual(response.data, decoded);
    assert.equal(schema.safeParse(response.data).success, false);
    const sse = createSseClient({ url: 'https://fixture.example/events', fetch: async () => new Response(`data: ${wire}\n\n`) });
    for await (const data of sse.stream) {
      assert.equal(schema.safeParse(data).success, false);
      assert.deepEqual(data, decoded);
      break;
    }
  }
  assert.deepEqual(parseJson('{"__proto__":42}'), { ['__proto__']: 42 });
});

test('decoder refuses malformed JSON and rounded fractional integers', () => {
  for (const wire of ['.1', '{"count":.1}', '01', '1.', '\u000b1', '{"count":1,}', '1.000000000000000000001', '1e-999', '-1e-999', '1.1e-999', '1e-10000001', '1e-100000000', '-1e-100000000', '1e999']) {
    assert.throws(() => parseJson(wire));
  }
  assert.equal(parseJson('1.234567890123456789'), 1.2345678901234567);
  assert.equal(parseJson('9e18'), 9000000000000000000n);
  assert.equal(parseJson('0e-100000000'), 0);
  assert.deepEqual(parseJson('{"count":1,"count":2}'), { count: 2 });
  assert.deepEqual(parseJson('{"_isBigNumber":true,"name":"literal metadata"}'), { _isBigNumber: true, name: 'literal metadata' });
});

test('unrelated decimal configuration cannot change integer decoding', () => {
  const config = BigNumber.config();
  try {
    BigNumber.config({ RANGE: [-1, 1] });
    assert.equal(parseJson('9007199254740993'), 9007199254740993n);
    assert.equal(parseJson('0.001'), 0.001);
  } finally {
    BigNumber.config(config);
  }
});

test('older-browser ponyfill preserves exact tokens and native object/syntax semantics', () => {
  // Import the decoder afresh in an isolated process without native rawJSON.
  // This executes the browser fallback, rather than only modern Node's parser.
  const script = `
    import assert from 'node:assert/strict';
    Object.defineProperty(JSON, 'rawJSON', { value: undefined, configurable: true });
    const { parseJson } = await import(${JSON.stringify(new URL('./parseJson.ts', import.meta.url).href)});
    assert.equal(parseJson('9007199254740993'), 9007199254740993n);
    assert.equal(parseJson('9e18'), 9000000000000000000n);
    const value = parseJson('{"__proto__":{"revision":9007199254740993},"count":42,"text":"123"}');
    assert.equal(Object.getPrototypeOf(value), Object.prototype);
    assert.equal(Object.hasOwn(value, 'revision'), false);
    assert.equal(Object.hasOwn(value, '__proto__'), true);
    assert.equal(value.__proto__.revision, 9007199254740993n);
    assert.equal(value.count, 42);
    assert.equal(value.text, '123');
    for (const text of ['.1', '01', '1.', '1e-999', '-1e-999', '1.1e-999', '1e-100000000', '{1:2}', '{true:1}', '{null:1}', '{false:1}', '{1e2:3}']) assert.throws(() => parseJson(text));
    assert.equal(JSON.rawJSON, undefined);
  `;
  const result = spawnSync(process.execPath, ['--experimental-strip-types', '--input-type=module', '-e', script], { encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
});
