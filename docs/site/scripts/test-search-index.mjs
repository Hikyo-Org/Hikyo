import assert from 'node:assert/strict';
import { test } from 'node:test';
import { loader } from 'fumadocs-core/source';
import { createFromSource } from 'fumadocs-core/search/server';
import { staticClient } from 'fumadocs-core/search/client/orama-static';
import { docsSearchOptions } from '../src/lib/search-options.ts';

test('offline index omits unused field sorting while preserving complete ranked search results', async () => {
  const source = loader({
    baseUrl: '/docs',
    source: {
      files: [{
        type: 'page',
        path: 'credentials.mdx',
        data: {
          title: 'Credential rotation',
          description: 'Replace provider credentials without restoring revoked authority.',
          structuredData: {
            headings: [{ id: 'rotate', content: 'Rotate deployment credentials' }],
            contents: Array.from({ length: 30 }, (_, section) => ({
              heading: 'rotate',
              content: `Section${section} covers isolated provider credential rotation and artifact admission, with transparent source-bound request handling. Complete matching snippets remain available offline.`,
            })),
          },
        },
      }, {
        type: 'page',
        path: 'recovery.mdx',
        data: {
          title: 'Isolated recovery',
          structuredData: {
            headings: [{ id: 'restore', content: 'Restore credential evidence' }],
            contents: [{ heading: 'restore', content: 'A recovery rehearsal preserves original credential and rotation evidence.' }],
          },
        },
      }],
    },
  });
  const baseline = createFromSource(source);
  const compact = createFromSource(source, docsSearchOptions);
  const [originalResponse, compactResponse] = await Promise.all([baseline.staticGET(), compact.staticGET()]);
  const [originalBytes, compactBytes] = await Promise.all([
    originalResponse.clone().arrayBuffer(), compactResponse.clone().arrayBuffer(),
  ]);
  assert.ok(compactBytes.byteLength < originalBytes.byteLength * 0.85,
    'unused field-sort indexes must not duplicate the complete searchable text');

  const responses = new Map([
    ['https://docs.invalid/original-search.json', originalResponse],
    ['https://docs.invalid/compact-search.json', compactResponse],
  ]);
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (input) => {
    const response = responses.get(String(input));
    assert.ok(response, 'static search must load the expected complete exported index');
    return response.clone();
  };
  try {
    const originalClient = staticClient({ from: 'https://docs.invalid/original-search.json' });
    const compactClient = staticClient({ from: 'https://docs.invalid/compact-search.json' });
    for (const query of ['rotation', 'Section29', 'deployment', 'isolated', 'credential evidence', 'unmatchedword', '']) {
      const expected = await baseline.search(query);
      if (query !== 'unmatchedword') assert.ok(expected.length > 0, `fixture query ${query} must exercise real matches`);
      assert.deepEqual(await compact.search(query), expected, `server search changed for ${query}`);
      assert.deepEqual(await originalClient.search(query), expected, `baseline static client differs for ${query}`);
      assert.deepEqual(await compactClient.search(query), expected, `offline static search changed for ${query}`);
    }
  } finally {
    globalThis.fetch = originalFetch;
  }
});
