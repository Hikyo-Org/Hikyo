// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';
import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { OAuth2ProvidersPanel } from './OAuth2ProvidersPanel.tsx';

const cleanups: Array<() => Promise<void>> = [];
afterEach(async () => {
  for (const unmount of cleanups.splice(0)) await unmount();
  vi.unstubAllGlobals();
});

it('clears a refused write-only secret and keeps it outside the query caches', async () => {
  const requests: Request[] = [];
  vi.stubGlobal('fetch', vi.fn((request: Request) => {
    requests.push(request);
    return Promise.resolve(request.method === 'GET'
      ? Response.json({ providers: [] })
      : Response.json({ error: { code: 'conflict', message: 'Conflict' } }, { status: 409 }));
  }));
  const { container, client, unmount } = await renderForm(<OAuth2ProvidersPanel />);
  cleanups.push(unmount);
  await settleTask();
  const add = [...container.querySelectorAll('button')].find(b => b.textContent === 'Add GitHub provider');
  if (!add) throw new Error('missing create action');
  await act(async () => add.click());
  const secret = container.querySelector('#oauth2-secret');
  const clientID = container.querySelector('#oauth2-client');
  const form = container.querySelector('form');
  if (!(secret instanceof HTMLInputElement) || !(clientID instanceof HTMLInputElement) || !form) throw new Error('missing editor');
  await act(async () => {
    typeInto(clientID, 'client');
    typeInto(secret, 'private-fixture-secret');
  });
  await act(async () => form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })));
  await settleTask();
  const write = requests.find(r => r.method === 'PUT');
  expect(write).toBeDefined();
  expect(await write?.clone().json()).toMatchObject({ profile: 'github', issuer: 'https://github.com', client_secret: 'private-fixture-secret' });
  expect(secret.value).toBe('');
  expect(container.querySelector('[role="alert"]')?.textContent).toContain('could not be saved');
  expect(client.getMutationCache().getAll()).toHaveLength(0);
  expect(JSON.stringify(client.getQueryCache().getAll().map(q => q.state.data))).not.toContain('private-fixture-secret');
});

it('pins edits to the displayed provider version and preserves a concurrent disable on refusal', async () => {
  const requests: Request[] = [];
  const provider = {profile:'github',slug:'github',display_name:'GitHub',issuer:'https://github.com',client_id:'client',redirect_uri:'https://hikyo.test/callback',enabled:true,row_version:7};
  vi.stubGlobal('fetch', vi.fn((request: Request) => {
    requests.push(request);
    return Promise.resolve(request.method === 'GET'
      ? Response.json({providers:[provider]})
      : Response.json({error:{code:'conflict',message:'Conflict'}},{status:409}));
  }));
  const {container,unmount}=await renderForm(<OAuth2ProvidersPanel />);
  cleanups.push(unmount);await settleTask();
  const edit=[...container.querySelectorAll('button')].find(b=>b.textContent==='Reconfigure GitHub');
  if(!edit)throw new Error('missing edit action');
  await act(async()=>edit.click());
  const secret=container.querySelector('#oauth2-secret');
  const form=container.querySelector('form');
  if(!(secret instanceof HTMLInputElement)||!form)throw new Error('missing editor');
  await act(async()=>typeInto(secret,'replacement'));
  await act(async()=>form.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true})));
  await settleTask();
  const write=requests.find(r=>r.method==='PUT');
  expect(await write?.clone().json()).toMatchObject({row_version:7});
  expect(container.querySelector('[role="alert"]')?.textContent).toContain('Refresh');
  expect(secret.value).toBe('');
});
