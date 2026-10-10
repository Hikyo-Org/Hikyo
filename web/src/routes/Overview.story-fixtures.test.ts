// @vitest-environment happy-dom
import { zProject, zProjectList } from '@hikyo/zod';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { installAppFetch } from '../../.storybook/withApp.tsx';
import { ORG } from '../testkit/ids.ts';
import { createOverviewFixture, overviewProject } from './Overview.story-fixtures.ts';

let cleanup: (() => void) | undefined;

function deferredBody() {
  let accept: ((value: { name: string }) => void) | null = null;
  const promise = new Promise<{ name: string }>((resolve) => { accept = resolve; });
  return {
    promise,
    resolve(value: { name: string }) {
      if (accept === null) throw new Error('Deferred request body is not initialized');
      accept(value);
    },
  };
}

afterEach(() => {
  cleanup?.();
  cleanup = undefined;
});

describe('Overview journey API fixture', () => {
  it('creates through the contract and exposes the new project to both route readers', async () => {
    const fixture = createOverviewFixture();
    cleanup = await installAppFetch({ parameters: { app: { responses: fixture.responses } } });
    const path = `/api/v1/orgs/${ORG}/projects`;
    expect(zProjectList.parse(await (await fetch(path)).json()).items).toEqual([]);
    const created = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ name: 'Billing platform' }) });
    expect(created.status).toBe(201);
    const project = zProject.parse(await created.json());
    expect(project.name).toBe('Billing platform');
    const firstReader = zProjectList.parse(await (await fetch(path)).json());
    const revisitingReader = zProjectList.parse(await (await fetch(path)).json());
    expect(firstReader.items).toEqual([project]);
    expect(revisitingReader.items).toEqual([project]);
    expect(fixture.submittedNames()).toEqual(['Billing platform']);
  });

  it('rejects malformed creates and unexpected methods without changing its list', async () => {
    const fixture = createOverviewFixture();
    cleanup = await installAppFetch({ parameters: { app: { responses: fixture.responses } } });
    const path = `/api/v1/orgs/${ORG}/projects`;
    const invalid = await fetch(path, { method: 'POST', body: JSON.stringify({ name: '' }) });
    expect(invalid.status).toBe(400);
    expect((await fetch(path, { method: 'DELETE' })).status).toBe(404);
    expect(zProjectList.parse(await (await fetch(path)).json()).items).toEqual([]);
    expect(fixture.submittedNames()).toEqual([]);
  });

  it('resets a repeated story and isolates independently framed Docs examples', async () => {
    const fixture = createOverviewFixture();
    cleanup = await installAppFetch({ parameters: { app: { responses: fixture.responses } } });
    const path = `/api/v1/orgs/${ORG}/projects`;
    expect((await fetch(path, { method: 'POST', body: JSON.stringify({ name: 'Billing platform' }) })).status).toBe(201);
    fixture.reset();
    expect(zProjectList.parse(await (await fetch(path)).json()).items).toEqual([]);
    expect(fixture.submittedNames()).toEqual([]);

    const separateFrame = createOverviewFixture();
    cleanup();
    cleanup = await installAppFetch({ parameters: { app: { responses: separateFrame.responses } } });
    expect(zProjectList.parse(await (await fetch(path)).json()).items).toEqual([]);
  });

  it('does not let a retired request mutate a reset story after its body arrives', async () => {
    const fixture = createOverviewFixture();
    const post = fixture.responses.find((route) => route.method === 'POST');
    if (post?.handler === undefined) throw new Error('Overview fixture has no create handler');
    const lifetime = new AbortController();
    const body = deferredBody();
    const request = new Request(`https://storybook.invalid/api/v1/orgs/${ORG}/projects`, {
      method: 'POST', signal: lifetime.signal, body: JSON.stringify({ name: 'Retired create' }),
    });
    const readBody = vi.spyOn(request, 'json').mockReturnValue(body.promise);
    try {
      const pending = post.handler(request);
      expect(readBody).toHaveBeenCalledOnce();
      lifetime.abort(new DOMException('Story fixture retired', 'AbortError'));
      fixture.reset([overviewProject]);
      body.resolve({ name: 'Retired create' });
      await expect(pending).rejects.toMatchObject({ name: 'AbortError' });
      cleanup = await installAppFetch({ parameters: { app: { responses: fixture.responses } } });
      const list = zProjectList.parse(await (await fetch(`/api/v1/orgs/${ORG}/projects`)).json());
      expect(list.items).toEqual([overviewProject]);
      expect(fixture.submittedNames()).toEqual([]);
    } finally {
      readBody.mockRestore();
    }
  });
});
