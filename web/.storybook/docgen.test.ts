import { describe, expect, it } from 'vitest';
import type { Plugin } from 'vite';

import { generatedClient, scopeDocgen } from './docgen.ts';

describe('generated API files do not require React prop extraction', () => {
  it('excludes the generated client while retaining UI, routes, fixtures and authored API owners', () => {
    expect(generatedClient.test('/repo/clients/ts/src/generated/zod.gen.ts')).toBe(true);
    for (const path of ['/repo/web/src/ui/Button.tsx', '/repo/web/src/routes/Overview.stories.tsx', '/repo/web/src/testkit/storybookPki.ts', '/repo/web/src/api/client.ts']) {
      expect(generatedClient.test(path)).toBe(false);
    }
  });

  it('retains the docgen handler and other plugins, including nested asynchronous entries', async () => {
    const handler = () => null;
    const docgen: Plugin = { name: 'storybook:react-docgen-plugin', enforce: 'pre', transform: handler };
    const unrelated: Plugin = { name: 'other', transform: handler };
    expect(await scopeDocgen(docgen)).toEqual({ ...docgen, transform: { filter: { id: { exclude: generatedClient } }, handler } });
    expect(await scopeDocgen([Promise.resolve(unrelated), false])).toEqual([unrelated, false]);
  });

  it('requires review when the installed docgen hook contract changes', async () => {
    await expect(scopeDocgen({ name: 'storybook:react-docgen-plugin', transform: { handler: () => null } })).rejects.toThrow('contract changed');
  });
});
