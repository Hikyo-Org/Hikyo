import { describe, expect, it } from 'vitest';

import { parseSource, transformName, safeName, type SourceParse } from './import-sources.ts';

/**
 * Parity tests for the browser file-mode connectors (#496). The fixtures are
 * the exact byte content the Go connectors pin in
 * `internal/importer/testdata/*`; the assertions mirror what those connectors
 * map or refuse, so a divergence between this preview and the CLI shows up here
 * (acceptance criterion "equivalent browser and CLI choices produce
 * semantically identical plans and results").
 */

function ok(result: SourceParse): Extract<SourceParse, { ok: true }> {
  if (!result.ok) {
    throw new Error(`expected ok, got refusal: ${result.reason}`);
  }
  return result;
}

function refusal(result: SourceParse): string {
  if (result.ok) {
    throw new Error('expected a refusal, got ok');
  }
  return result.reason;
}

describe('transformName', () => {
  it('preserves an already-valid name byte-for-byte', () => {
    expect(transformName('DB_URL')).toEqual({ target: 'DB_URL' });
  });

  it('uppercases lowercase ASCII and maps -, ., /, \\ to underscore', () => {
    expect(transformName('db-host')).toEqual({ target: 'DB_HOST' });
    expect(transformName('db.host')).toEqual({ target: 'DB_HOST' });
    expect(transformName('a/b')).toEqual({ target: 'A_B' });
  });

  it('prefixes a leading digit with one underscore', () => {
    expect(transformName('9lives')).toEqual({ target: '_9LIVES' });
  });

  it('hard-stops on a byte outside the documented transform', () => {
    expect(transformName('has space')).toEqual({ error: true });
    expect(transformName('a=b')).toEqual({ error: true });
    expect(transformName('café')).toEqual({ error: true });
    expect(transformName('')).toEqual({ error: true });
  });
});

describe('safeName', () => {
  it('escapes control bytes so a hostile name cannot inject terminal escapes', () => {
    const rendered = safeName('A\x1b[2J\x1b]0;pwned\x07B');
    expect(rendered).not.toContain('\x1b');
    expect(rendered).not.toContain('\x07');
    expect(rendered).toContain('\\x1b');
  });

  it('escapes non-ASCII runes and caps the shown length', () => {
    expect(safeName('caf\u00e9')).toBe('"caf\\u00e9"');
  });
});

describe('k8s connector', () => {
  it('maps a single Secret onto the environment root (no folder)', () => {
    const { entries, renames, skipped } = ok(
      parseSource('k8s', 'apiVersion: v1\nkind: Secret\nmetadata:\n  name: solo\n  resourceVersion: "1"\ndata:\n  ALPHA: YWxwaGE=\n  BETA: YmV0YQ==\n'),
    );
    expect(entries).toEqual([
      { key: 'ALPHA', sourceName: 'ALPHA', value: 'alpha', folderPath: '' },
      { key: 'BETA', sourceName: 'BETA', value: 'beta', folderPath: '' },
    ]);
    expect(renames).toEqual([]);
    expect(skipped).toEqual([]);
  });

  it('folders multiple Secrets, overlays stringData, and surfaces renames', () => {
    const manifest = [
      'apiVersion: v1',
      'kind: Secret',
      'metadata:',
      '  name: app-db',
      '  namespace: prod',
      '  resourceVersion: "8821"',
      'type: Opaque',
      'data:',
      '  DB_PASSWORD: czNjcjN0',
      '  db-host: ZGIuaW50ZXJuYWw=',
      '  DB_PORT: NTQzMg==',
      'stringData:',
      '  DB_PASSWORD: overlaid-wins',
      '---',
      'apiVersion: v1',
      'kind: Secret',
      'metadata:',
      '  name: app-api',
      '  namespace: prod',
      '  resourceVersion: "8822"',
      'data:',
      '  API_KEY: c2tfbGl2ZV9hYmM=',
      '---',
      '',
    ].join('\n');
    const { entries, renames } = ok(parseSource('k8s', manifest));
    expect(entries).toEqual([
      { key: 'API_KEY', sourceName: 'API_KEY', value: 'sk_live_abc', folderPath: 'app-api' },
      { key: 'DB_HOST', sourceName: 'db-host', value: 'db.internal', folderPath: 'app-db' },
      // stringData wins over data for DB_PASSWORD (admission merge).
      { key: 'DB_PASSWORD', sourceName: 'DB_PASSWORD', value: 'overlaid-wins', folderPath: 'app-db' },
      { key: 'DB_PORT', sourceName: 'DB_PORT', value: '5432', folderPath: 'app-db' },
    ]);
    expect(renames).toEqual([{ from: 'db-host', to: 'DB_HOST' }]);
  });

  it('refuses a duplicate key within one Secret', () => {
    expect(refusal(parseSource('k8s', 'kind: Secret\nmetadata:\n  name: dup\ndata:\n  A: eA==\n  A: eQ==\n'))).toMatch(
      /more than once/,
    );
  });

  it('refuses a post-transform collision', () => {
    expect(
      refusal(parseSource('k8s', 'kind: Secret\nmetadata:\n  name: coll\ndata:\n  db-host: eA==\n  db.host: eQ==\n')),
    ).toMatch(/collision/);
  });

  it('refuses a binary (non-UTF-8) value by name', () => {
    const reason = refusal(parseSource('k8s', 'kind: Secret\nmetadata:\n  name: bin\ndata:\n  BLOB: AAECAw==\n'));
    expect(reason).toMatch(/not UTF-8/);
    expect(reason).toContain('BLOB');
  });

  it('refuses a wrong kind without echoing the field value', () => {
    const reason = refusal(
      parseSource('k8s', '{"kind": "\\u001b[2J\\u001b]0;pwned\\u0007 sk_live_KINDLEAK", "metadata": {"name": "x"}, "data": {"A": "eA=="}}'),
    );
    expect(reason).toMatch(/`kind` is not `Secret`/);
    expect(reason).not.toContain('sk_live_KINDLEAK');
  });

  it('refuses unpadded/whitespace base64 the strict Go decoder would reject', () => {
    // `YQ` is `atob`-decodable to "a" but is not valid StdEncoding (no padding).
    expect(refusal(parseSource('k8s', 'kind: Secret\nmetadata:\n  name: s\ndata:\n  TOKEN: YQ\n'))).toMatch(/valid base64/);
  });

  it('accepts base64 wrapped across lines (Go skips CR/LF)', () => {
    // A JSON manifest whose data value carries an embedded newline, as a YAML
    // block scalar would; Go's decoder skips it, so the browser must too.
    const { entries } = ok(parseSource('k8s', '{"kind":"Secret","metadata":{"name":"s"},"data":{"TOKEN":"YQ==\\n"}}'));
    expect(entries[0]?.value).toBe('a');
  });

  it('refuses an unmappable name', () => {
    expect(refusal(parseSource('k8s', 'kind: Secret\nmetadata:\n  name: bad\ndata:\n  "has space": eA==\n'))).toMatch(
      /outside the documented transform/,
    );
  });

  it('preserves a value the write-time trim would alter (acknowledged later)', () => {
    // CERT decodes to "-----BEGIN-----\n"; the connector keeps it verbatim, the
    // wizard's trim acknowledgement handles it.
    const { entries } = ok(parseSource('k8s', 'kind: Secret\nmetadata:\n  name: trim\ndata:\n  CERT: LS0tLUJFR0lOLS0tLQo=\n'));
    expect(entries[0]?.value).toBe('----BEGIN----\n');
  });
});

describe('infisical connector', () => {
  const exportJson = JSON.stringify([
    { key: 'DB_URL', workspace: 'ws_1', value: 'postgres://x', type: 'shared', _id: 'sec_1', secretPath: '/db' },
    { key: 'api-key', workspace: 'ws_1', value: 'sk_live_x', type: 'shared', _id: 'sec_2', secretPath: '/db' },
    { key: 'MY_OVERRIDE', workspace: 'ws_1', value: 'personal', type: 'personal', _id: 'sec_3', secretPath: '/db' },
  ]);

  it('maps shared secrets to folders, skips personal overrides, surfaces renames', () => {
    const { entries, renames, skipped } = ok(parseSource('infisical', exportJson, { envSlug: 'prod' }));
    expect(entries).toEqual([
      { key: 'API_KEY', sourceName: 'api-key', value: 'sk_live_x', folderPath: 'db' },
      { key: 'DB_URL', sourceName: 'DB_URL', value: 'postgres://x', folderPath: 'db' },
    ]);
    expect(renames).toEqual([{ from: 'api-key', to: 'API_KEY' }]);
    // Skipped names are rendered through safeName (quoted, escaped, capped).
    expect(skipped).toEqual(['"MY_OVERRIDE"']);
  });

  it('requires the source env slug', () => {
    expect(refusal(parseSource('infisical', exportJson))).toMatch(/source environment slug/);
  });

  it('refuses a flat (dotenv-shaped) export and points at the .env path', () => {
    expect(refusal(parseSource('infisical', '{"DB_URL":"postgres://x","API_KEY":"sk_live_x"}', { envSlug: 'prod' }))).toMatch(
      /\.env import/,
    );
  });

  it('refuses an entry without secretPath (no folder provenance)', () => {
    expect(
      refusal(parseSource('infisical', '[{"key":"DB_URL","value":"postgres://x","type":"shared"}]', { envSlug: 'prod' })),
    ).toMatch(/secretPath/);
  });

  it('refuses an entry without type (personal overrides resolved)', () => {
    expect(
      refusal(parseSource('infisical', '[{"key":"DB_URL","value":"postgres://x","secretPath":"/"}]', { envSlug: 'prod' })),
    ).toMatch(/`type`/);
  });

  it('refuses a case-variant duplicate member (fold-collision, matching the CLI)', () => {
    expect(
      refusal(parseSource('infisical', '[{"key":"DB_URL","Key":"other","value":"x","type":"shared","secretPath":"/"}]', { envSlug: 'prod' })),
    ).toMatch(/more than once/);
  });

  it('refuses a non-string value instead of coercing it to an empty secret', () => {
    expect(
      refusal(parseSource('infisical', '[{"key":"API_KEY","value":123,"type":"shared","secretPath":"/"}]', { envSlug: 'prod' })),
    ).toMatch(/pinned JSON array/);
  });

  it('refuses a non-string value even on a personal (skipped) entry', () => {
    expect(
      refusal(parseSource('infisical', '[{"key":"X","value":123,"type":"personal","secretPath":"/"}]', { envSlug: 'prod' })),
    ).toMatch(/pinned JSON array/);
  });

  it('refuses a hostile type without echoing its value', () => {
    const reason = refusal(
      parseSource('infisical', '[{"key": "DB_URL", "value": "x", "type": "\\u001b[2J\\u001b]0;pwned\\u0007 sk_live_TYPELEAK", "secretPath": "/"}]', {
        envSlug: 'prod',
      }),
    );
    expect(reason).toMatch(/neither `shared` nor `personal`/);
    expect(reason).not.toContain('sk_live_TYPELEAK');
  });
});

describe('vault/openbao connector', () => {
  it('strips the common prefix to folders, skips deleted, canonicalizes json leaves', () => {
    const capture = [
      '{"path":"apps/db/main","mount":"secret","engine_version":2,"secret_version":4,"deleted":false,"destroyed":false,"data":{"DB_URL":"postgres://fixture","OPTIONS":{"pool":5,"ssl":true}}}',
      '{"path":"apps/old","mount":"secret","engine_version":2,"secret_version":8,"deleted":true,"destroyed":false,"data":{}}',
      '{"path":"apps/top","mount":"secret","engine_version":2,"secret_version":2,"deleted":false,"destroyed":false,"data":{"API_KEY":"top-secret"}}',
    ].join('\n');
    const { entries, skipped } = ok(parseSource('vault', capture));
    expect(entries).toEqual([
      { key: 'API_KEY', sourceName: 'API_KEY', value: 'top-secret', folderPath: 'top' },
      { key: 'DB_URL', sourceName: 'DB_URL', value: 'postgres://fixture', folderPath: 'db/main' },
      // Non-string leaf → canonical JSON (sorted keys, no spaces), type json.
      { key: 'OPTIONS', sourceName: 'OPTIONS', value: '{"pool":5,"ssl":true}', folderPath: 'db/main' },
    ]);
    expect(skipped).toEqual(['"apps/old"']);
  });

  it('preserves number literals exactly (no JSON.parse precision loss)', () => {
    const capture =
      '{"path":"apps/numbers","mount":"secret","engine_version":1,"deleted":false,"destroyed":false,"data":{"DECIMAL":0.12345678901234567890123456789,"LARGE_INTEGER":9007199254740993}}';
    const { entries } = ok(parseSource('vault', capture));
    expect(entries).toEqual([
      { key: 'DECIMAL', sourceName: 'DECIMAL', value: '0.12345678901234567890123456789', folderPath: '' },
      { key: 'LARGE_INTEGER', sourceName: 'LARGE_INTEGER', value: '9007199254740993', folderPath: '' },
    ]);
  });

  it('refuses a mixed mount or engine version in one file', () => {
    const capture = [
      '{"path":"a","mount":"secret","engine_version":1,"deleted":false,"destroyed":false,"data":{"X":"1"}}',
      '{"path":"b","mount":"secret","engine_version":2,"secret_version":1,"deleted":false,"destroyed":false,"data":{"Y":"2"}}',
    ].join('\n');
    expect(refusal(parseSource('vault', capture))).toMatch(/one mount and one KV engine version/);
  });

  it('refuses a duplicate mount+path record', () => {
    const capture = [
      '{"path":"a","mount":"secret","engine_version":1,"deleted":false,"destroyed":false,"data":{"X":"1"}}',
      '{"path":"a","mount":"secret","engine_version":1,"deleted":false,"destroyed":false,"data":{"Y":"2"}}',
    ].join('\n');
    expect(refusal(parseSource('vault', capture))).toMatch(/more than once/);
  });

  it('refuses a bare kv-get response (unexpected field) with a pointer to the recipe', () => {
    expect(refusal(parseSource('vault', '{"request_id":"abc","data":{"X":"1"}}'))).toMatch(/unexpected field|capture record/);
  });

  it('refuses a non-object data field (a JSON number is not a map)', () => {
    const capture = '{"path":"a","mount":"secret","engine_version":1,"deleted":true,"destroyed":false,"data":1}';
    expect(refusal(parseSource('vault', capture))).toMatch(/omits required data/);
  });

  it('refuses a secret_version beyond signed 64-bit range', () => {
    const capture = '{"path":"a","mount":"secret","engine_version":2,"secret_version":9223372036854775808,"deleted":false,"destroyed":false,"data":{"X":"1"}}';
    expect(refusal(parseSource('vault', capture))).toMatch(/positive secret_version/);
  });

  it('substitutes U+FFFD for a lone surrogate, matching Go encoding/json', () => {
    const capture = '{"path":"a","mount":"secret","engine_version":1,"deleted":false,"destroyed":false,"data":{"OPTS":{"n":"\\ud800"}}}';
    const { entries } = ok(parseSource('vault', capture));
    expect(entries[0]?.value).toBe('{"n":"\uFFFD"}');
  });

  it('refuses a non-integer secret_version (Go int field)', () => {
    const capture = '{"path":"a","mount":"secret","engine_version":2,"secret_version":1.5,"deleted":false,"destroyed":false,"data":{"X":"1"}}';
    expect(refusal(parseSource('vault', capture))).toMatch(/positive secret_version/);
  });

  it('refuses a leading-zero number literal (strict JSON)', () => {
    const capture = '{"path":"a","mount":"secret","engine_version":01,"deleted":false,"destroyed":false,"data":{"X":"1"}}';
    expect(refusal(parseSource('vault', capture))).toMatch(/malformed|engine_version/);
  });

  it('refuses an unescaped control character inside a JSON string', () => {
    const capture = '{"path":"a","mount":"secret","engine_version":1,"deleted":false,"destroyed":false,"data":{"X":"a\tb"}}';
    expect(refusal(parseSource('vault', capture))).toMatch(/control character/);
  });

  it('does not let a __proto__ member pollute validation', () => {
    // `__proto__` is an own key on a null-prototype object → an unexpected field.
    const capture = '{"__proto__":{"deleted":false,"destroyed":false,"data":{"X":"1"}},"path":"a","mount":"secret","engine_version":1,"deleted":false,"destroyed":false,"data":{"X":"1"}}';
    expect(refusal(parseSource('vault', capture))).toMatch(/unexpected field/);
  });

  it('refuses a deeply nested leaf at the depth bound instead of overflowing the stack', () => {
    // A leaf nested far past depth 32; a bounded refusal, never a RangeError.
    const nested = `${'['.repeat(200)}1${']'.repeat(200)}`;
    const capture = `{"path":"a","mount":"secret","engine_version":1,"deleted":false,"destroyed":false,"data":{"DEEP":${nested}}}`;
    expect(refusal(parseSource('vault', capture))).toMatch(/depth bound/);
  });
});
