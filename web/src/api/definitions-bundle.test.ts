// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from './client.ts';
import {
  applyBundle,
  bundleRefusalText,
  checkBundle,
  definitionsExportPath,
  readDefinitionsFile,
  type DefinitionsPlan,
} from './definitions-bundle.ts';

const empty = { format_version: 1, environments: [], key_groups: [], keys: [] };
const diff = { creates: [], updates: [], renames: [], deletes: [] };
const plan: DefinitionsPlan = {
  id: 'pln_123e4567-e89b-12d3-a456-426614174000',
  digest: 'a'.repeat(64),
  current_revision: 2n,
  additive: true,
  expires_at: '2030-01-01T00:00:00Z',
  protected_environments: [],
  deletions_present: false,
  reveal_required: [],
  diff: {
    environments: diff,
    key_groups: diff,
    keys: diff,
    key_deletions: [],
    env_deletions: [],
    reveal_required: [],
  },
};
const scope = { org: 'org-1', project: 'project-1' };
afterEach(() => vi.unstubAllGlobals());

describe('definitions bundle boundary', () => {
  it('parses canonical JSON and retains a based revision as a JSON-safe integer', async () => {
    const file = new File([JSON.stringify({ ...empty, base_revision: 17 })], 'definitions.json');
    const parsed = await readDefinitionsFile(file);
    expect(parsed.base_revision).toBe(17);
    expect(() => JSON.stringify(parsed)).not.toThrow();
  });
  it('preserves integer rules in direct and union declarations as JSON numbers', async () => {
    const key = {
      name: 'PORT',
      folder_path: '',
      classification: 'config',
      description: '',
      deprecated: false,
      deprecation_note: '',
      group: '',
      required_in: { mode: 'none', environments: [] },
      forbidden_in: { mode: 'none', environments: [] },
    };
    const bundle = await readDefinitionsFile(
      new File(
        [
          JSON.stringify({
            ...empty,
            keys: [
              {
                ...key,
                declaration: {
                  rule: { type: 'integer', min: -12, max: 65535 },
                },
              },
              {
                ...key,
                name: 'OTHER',
                declaration: {
                  any_of: [{ type: 'integer', min: 1, max: 10 }, { type: 'string' }],
                },
              },
            ],
          }),
        ],
        'definitions.json',
      ),
    );
    expect(bundle.keys[0]?.declaration.rule?.max).toBe(65535);
    expect(bundle.keys[1]?.declaration.any_of?.[0]?.min).toBe(1);
    expect(() => JSON.stringify(bundle)).not.toThrow();
  });
  it('refuses misspelled nested fields instead of silently changing the submitted definitions', async () => {
    const file = new File(
      [
        JSON.stringify({
          ...empty,
          environments: [{ name: 'preview', unexpected: 'not dropped' }],
        }),
      ],
      'definitions.json',
    );
    await expect(readDefinitionsFile(file)).rejects.toThrow('valid JSON definitions bundle');
  });
  it('refuses duplicate fields and unsafe integers rather than changing file meaning', async () => {
    await expect(
      readDefinitionsFile(
        new File(
          ['{"format_version":1,"format_version":1,"environments":[],"key_groups":[],"keys":[]}'],
          'duplicate.json',
        ),
      ),
    ).rejects.toThrow('duplicate object fields');
    await expect(
      readDefinitionsFile(
        new File(
          [JSON.stringify({ ...empty, base_revision: Number.MAX_SAFE_INTEGER + 1 })],
          'unsafe.json',
        ),
      ),
    ).rejects.toThrow('exact JSON range');
  });
  it('rejects malformed, unsupported, extra-field and oversized files', async () => {
    for (const body of [
      'not JSON',
      JSON.stringify({ ...empty, format_version: 2 }),
      JSON.stringify({ ...empty, extra: true }),
    ]) {
      await expect(readDefinitionsFile(new File([body], 'bad.json'))).rejects.toThrow(
        'valid JSON definitions bundle',
      );
    }
    await expect(
      readDefinitionsFile(new File(['x'.repeat(1_048_577)], 'large.json')),
    ).rejects.toThrow('1 MiB');
  });
  it('rejects numeric coercion in revisions and direct or union bounds', async () => {
    const key = {
      name: 'PORT',
      folder_path: '',
      classification: 'config',
      description: '',
      deprecated: false,
      deprecation_note: '',
      group: '',
      required_in: { mode: 'none', environments: [] },
      forbidden_in: { mode: 'none', environments: [] },
    };
    for (const value of [true, false, '', '12', [], [12], {}, null, 1.5]) {
      const bundles = [
        { ...empty, base_revision: value },
        ...['min', 'max'].flatMap((bound) => {
          const rule = { type: 'integer', [bound]: value };
          return [
            { ...empty, keys: [{ ...key, declaration: { rule } }] },
            { ...empty, keys: [{ ...key, declaration: { any_of: [rule, { type: 'string' }] } }] },
          ];
        }),
      ];
      for (const bundle of bundles) {
        await expect(
          readDefinitionsFile(new File([JSON.stringify(bundle)], 'malformed.json')),
        ).rejects.toThrow('valid JSON definitions bundle');
      }
    }
  });
  it('never puts bundle content into the export URL', () => {
    expect(definitionsExportPath({ org: 'org/with space', project: 'project?x' })).toBe(
      '/api/v1/orgs/org%2Fwith%20space/projects/project%3Fx/definitions/export',
    );
  });
  it('rechecks Git governance immediately before apply and sends no write when it changed', async () => {
    const request = vi.fn(() =>
      Promise.resolve(
        new Response(JSON.stringify({ definitions_source: 'git' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    vi.stubGlobal('fetch', request);
    await expect(applyBundle(scope, plan, false, {}, new AbortController().signal)).rejects.toThrow(
      'Git-managed',
    );
    expect(request).toHaveBeenCalledTimes(1);
    const first = request.mock.calls[0];
    expect(first).toBeDefined();
  });
  it('pins digest and explicit deletion consent in apply and excludes file data', async () => {
    const seen: Request[] = [];
    vi.stubGlobal('fetch', async (request: Request) => {
      seen.push(request);
      return new Response(
        JSON.stringify(
          request.method === 'GET'
            ? { definitions_source: 'db' }
            : {
                revision: 3,
                published: ['production'],
                plan_id: 'pln_123e4567-e89b-12d3-a456-426614174000',
              },
        ),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );
    });
    await applyBundle(scope, plan, true, {}, new AbortController().signal);
    expect(seen).toHaveLength(2);
    expect(await seen[1]?.text()).toBe(
      JSON.stringify({
        digest: plan.digest,
        allow_delete: true,
        acknowledgements: [],
      }),
    );
  });
  it('shows refused and stale operations as actionable text', () => {
    expect(bundleRefusalText(new ApiError(404, 'missing'))).toContain(
      'publish on every affected environment',
    );
    expect(bundleRefusalText(new ApiError(409, 'stale'))).toContain('reconcile');
    expect(bundleRefusalText(new ApiError(409, 'stale', 'plan expired; re-plan'))).toBe(
      'plan expired; re-plan',
    );
  });
  it('sends a checked bundle only in the request body', async () => {
    const seen: Request[] = [];
    vi.stubGlobal('fetch', async (request: Request) => {
      seen.push(request);
      return new Response(
        JSON.stringify({
          state: 'equal',
          current_revision: 0,
          differences: {
            environments: diff,
            key_groups: diff,
            keys: diff,
            reveal_required: [],
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );
    });
    const bundle = await readDefinitionsFile(new File([JSON.stringify(empty)], 'definitions.json'));
    await checkBundle(scope, bundle, {}, new AbortController().signal);
    expect(seen[0]?.url).toBe(
      'http://localhost:3000/api/v1/orgs/org-1/projects/project-1/definitions/check',
    );
    expect(await seen[0]?.text()).toBe(JSON.stringify(bundle));
  });
});
