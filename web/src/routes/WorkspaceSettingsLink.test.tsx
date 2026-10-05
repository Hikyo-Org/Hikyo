// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter, useLocation } from 'react-router';
import { createClient } from '@hikyo/runtime-core';
import { expect, it } from 'vitest';
import { WorkspaceContextProvider } from '../api/transport.tsx';
import { renderForm } from '../testkit/renderForm.tsx';
import { WorkspaceSettingsLink } from './WorkspaceSettingsLink.tsx';

const path = '/orgs/org_same/projects/project_same/settings#project-policy';
function Location() { return <output>{useLocation().pathname}</output>; }
it('keeps local settings on the router', async () => {
  const view = await renderForm(<MemoryRouter><WorkspaceSettingsLink path={path}>Policy</WorkspaceSettingsLink><Location /></MemoryRouter>);
  const link = view.container.querySelector('a');
  expect(link?.getAttribute('href')).toBe(path);
  await act(async () => link?.dispatchEvent(new MouseEvent('click', { bubbles: true, button: 0 })));
  expect(view.container.querySelector('output')?.textContent).toBe(path.split('#')[0]);
  await view.unmount();
});
it('leaves the home router for the remote origin even when project IDs match', async () => {
  const view = await renderForm(<MemoryRouter><WorkspaceContextProvider value={{ origin: 'https://remote.example', remote: 'other', client: createClient() }}><WorkspaceSettingsLink path={path}>Policy</WorkspaceSettingsLink></WorkspaceContextProvider><Location /></MemoryRouter>);
  expect(view.container.querySelector('a')?.href).toBe(`https://remote.example${path}`);
  expect(view.container.querySelector('a')?.href).not.toContain('remote=');
  expect(view.container.querySelector('output')?.textContent).toBe('/');
  await view.unmount();
});
