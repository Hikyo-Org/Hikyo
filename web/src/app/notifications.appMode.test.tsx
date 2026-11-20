// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { clearNotification, notifyUpdate, ToastViewport } from './notifications.tsx';

const shell = vi.hoisted((): { origin?: string } => ({}));
vi.mock('./shell.ts', async (importActual) => ({
  ...(await importActual<typeof import('./shell.ts')>()),
  knownShellOrigin: () => shell.origin,
}));

afterEach(() => { vi.unstubAllGlobals(); clearNotification(); delete shell.origin; });

async function hrefFor(protocol: string, href: string): Promise<string | null | undefined> {
  vi.stubGlobal('location', { protocol, origin: `${protocol}//x` });
  const view = await renderForm(<ToastViewport />);
  await act(async () => notifyUpdate('Update', href, () => undefined));
  const found = view.container.querySelector('a.toast__action')?.getAttribute('href');
  await view.unmount();
  return found;
}

describe('toast action link', () => {
  it('resolves relative links against the instance origin in the app', async () => {
    shell.origin = 'https://hikyo.example';
    expect(await hrefFor('wails:', '/orgs/a?q=1')).toBe('https://hikyo.example/orgs/a?q=1');
  });

  it('leaves absolute and protocol-relative links alone in the app', async () => {
    shell.origin = 'https://hikyo.example';
    expect(await hrefFor('wails:', 'https://github.com/r/releases')).toBe('https://github.com/r/releases');
    expect(await hrefFor('wails:', '//evil.example/x')).toBe('//evil.example/x');
  });

  it('keeps the link as given when the origin is not known yet', async () => {
    expect(await hrefFor('wails:', '/orgs/a')).toBe('/orgs/a');
  });

  it('is unchanged in a browser', async () => {
    shell.origin = 'https://hikyo.example';
    expect(await hrefFor('https:', '/orgs/a')).toBe('/orgs/a');
  });
});
