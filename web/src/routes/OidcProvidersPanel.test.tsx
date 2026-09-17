// @vitest-environment happy-dom
import { act } from 'react';
import { afterEach, expect, it, vi } from 'vitest';

import { renderForm, settleTask, typeInto } from '../testkit/renderForm.tsx';
import { OidcProvidersPanel } from './OidcProvidersPanel.tsx';

const cleanups: Array<() => Promise<void>> = [];

afterEach(async () => {
  for (const unmount of cleanups.splice(0)) await unmount();
  vi.unstubAllGlobals();
});

/** Find a labelled control the way the ui atoms associate it: `label[for]`. */
function control(container: HTMLElement, label: string): HTMLInputElement {
  const found = [...container.querySelectorAll('label')].find((l) => l.textContent === label);
  if (found === undefined) throw new Error(`no label "${label}"`);
  const target = container.querySelector(`#${CSS.escape(found.htmlFor)}`);
  if (!(target instanceof HTMLInputElement)) throw new Error(`no control for "${label}"`);
  return target;
}

async function click(container: HTMLElement, name: string): Promise<void> {
  const button = [...container.querySelectorAll('button')].find((b) => b.textContent === name);
  if (button === undefined) throw new Error(`no button "${name}"`);
  await act(async () => button.dispatchEvent(new MouseEvent('click', { bubbles: true })));
  await settleTask();
}

it('replaces a shown server refusal with the field error on an invalid re-submit', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn((request: RequestInfo | URL) => {
      const method = request instanceof Request ? request.method : 'GET';
      if (method === 'GET') return Promise.resolve(Response.json({ providers: [] }));
      return Promise.resolve(
        Response.json(
          { error: { code: 'bad_request', message: 'Bad request', detail: 'The issuer did not resolve.' } },
          { status: 400 },
        ),
      );
    }),
  );

  const { container, unmount } = await renderForm(<OidcProvidersPanel />);
  cleanups.push(unmount);
  await settleTask();

  await click(container, '+ add identity provider');
  await act(async () => {
    typeInto(control(container, 'Slug'), 'example');
    typeInto(control(container, 'Display name'), 'Example');
    typeInto(control(container, 'Issuer URL'), 'https://idp.example');
    typeInto(control(container, 'Client ID'), 'client');
    typeInto(control(container, 'Client secret'), 'secret');
    typeInto(control(container, 'Scopes'), 'openid');
  });
  await click(container, 'Configure provider');

  // The whole-save refusal is form-level, and the write-only secret is blanked.
  expect(container.querySelector('[role="alert"]')?.textContent).toContain(
    'The issuer did not resolve.',
  );

  // Re-submitting is now invalid (the secret must be re-entered). That refusal
  // names one control, so it replaces the stale server sentence rather than
  // stacking with it: two refusals at once name no single cause.
  await click(container, 'Configure provider');
  const alerts = [...container.querySelectorAll('[role="alert"]')];
  expect(alerts).toHaveLength(1);
  expect(alerts[0]?.className).toContain('field__error');
  expect(alerts[0]?.textContent).toContain('The client secret is write-only');
  expect(control(container, 'Client secret').getAttribute('aria-invalid')).toBe('true');
});
