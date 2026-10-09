// @vitest-environment happy-dom
import { act } from 'react';
import { MemoryRouter } from 'react-router';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { deferred, revealWindow } from '../testkit/ceremony.ts';
import { renderForm, settle } from '../testkit/renderForm.tsx';
import { MatrixRowEditor } from './MatrixRowEditor.tsx';
import { Values } from './Values.tsx';
const mocks = vi.hoisted(() => ({ one: vi.fn(), all: vi.fn() }));
vi.mock('../api/values.ts', async (importActual) => ({
  ...await importActual<typeof import('../api/values.ts')>(),
  fetchRevealWindow: vi.fn(async () => revealWindow(true)),
  useRevealWindow: () => ({ data: revealWindow(true) }),
  useRevealOne: () => ({ mutateAsync: mocks.one }),
  useRevealAll: () => ({ mutateAsync: mocks.all }),
  useValues: () => ({ data: { items: [{ key_id: 'key-a', name: 'PASSWORD', classification: 'secret', set: true }] }, isError: false }),
  useEnvironments: () => ({ data: { items: [] } }),
}));
const record: Parameters<typeof MatrixRowEditor>[0]['keyRecord'] = {
  id: 'key-a', org_id: 'org-a', project_id: 'project-a', name: 'PASSWORD',
  folder_path: '', classification: 'secret', description: '', deprecated: false,
  deprecation_note: '', declaration: { rule: { type: 'string', allow_empty: true } },
  presence: { required_in: { mode: 'none' }, forbidden_in: { mode: 'none' } },
  group_id: '', created_at: '2026-10-09T00:00:00Z',
};
const revealed = { key_id: 'key-a', name: 'PASSWORD', value: 'focus-test-secret' };
function click(container: HTMLElement, label: string) {
  const button = [...container.querySelectorAll('button')].find(b => b.textContent === label || b.getAttribute('aria-label') === label);
  if (!button) throw new Error(`Missing ${label}`);
  button.click();
}
async function renderSurface(surface: string) {
  return renderForm(<MemoryRouter>{surface === 'Values' ? <Values /> :
    <MatrixRowEditor refData={{ org: 'org-a', project: 'project-a' }}
      keyRecord={record} environmentId="env-a"
      rows={[{ environmentId: 'env-a', environment: { id: 'env-a', org_id: 'org-a', project_id: 'project-a', name: 'development', display_order: 0, created_at: record.created_at }, protected: false, degraded: false,
        cell: { key_id: 'key-a', name: 'PASSWORD', classification: 'secret', set: true, revealed: false },
        signal: undefined, draftPreview: undefined, problems: [] }]}
      busy={false} mutationError={null} onClose={vi.fn()} onCopy={vi.fn()}
      onApply={async () => {}} />}</MemoryRouter>);
}
function loseFocus(event: string) {
  if (event === 'blur') window.dispatchEvent(new Event('blur'));
  else {
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
    document.dispatchEvent(new Event('visibilitychange'));
  }
}
function regainFocus() {
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  window.dispatchEvent(new Event('focus'));
  document.dispatchEvent(new Event('visibilitychange'));
}
beforeEach(() => {
  mocks.one.mockReset().mockResolvedValue(revealed);
  mocks.all.mockReset().mockResolvedValue({ items: [{ ...revealed, classification: 'secret' }] });
  vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
});
afterEach(() => vi.restoreAllMocks());
for (const surface of ['Values', 'Matrix editor']) {
  describe(`${surface} focus remasking`, () => {
    for (const event of ['blur', 'visibilitychange']) {
      it(`clears a revealed secret immediately on ${event} and permits a fresh reveal on return`, async () => {
        const view = await renderSurface(surface);
        vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 45_000);
        await act(async () => click(view.container, 'Reveal PASSWORD'));
        await settle();
        expect(view.container.textContent).toContain(revealed.value);
        const countdown = view.container.querySelector(surface === 'Values'
          ? '.values__countdown' : 'small[aria-hidden="true"]');
        expect(countdown?.textContent).toBe('re-masks in 10s');
        await act(async () => loseFocus(event));
        expect(view.container.textContent).not.toContain(revealed.value);
        await act(async () => regainFocus());
        expect(view.container.textContent).not.toContain(revealed.value);
        await act(async () => click(view.container, 'Reveal PASSWORD'));
        await settle();
        expect(view.container.textContent).toContain(revealed.value);
        await view.unmount();
      });
      it(`rejects a pending reveal after ${event}, even after focus returns`, async () => {
        const pending = deferred<typeof revealed>();
        mocks.one.mockReturnValueOnce(pending.promise);
        const view = await renderSurface(surface);
        await act(async () => click(view.container, 'Reveal PASSWORD'));
        await settle();
        expect(mocks.one).toHaveBeenCalledOnce();
        await act(async () => loseFocus(event));
        await act(async () => regainFocus());
        await act(async () => pending.resolve(revealed));
        await settle();
        expect(view.container.textContent).not.toContain(revealed.value);
        await view.unmount();
      });
    }
    it('removes its focus listeners on unmount', async () => {
      const addWindow = vi.spyOn(window, 'addEventListener');
      const addDocument = vi.spyOn(document, 'addEventListener');
      const removeWindow = vi.spyOn(window, 'removeEventListener');
      const removeDocument = vi.spyOn(document, 'removeEventListener');
      const view = await renderSurface(surface);
      const blur = addWindow.mock.calls.find(([event]) => event === 'blur');
      const visibility = addDocument.mock.calls.find(([event]) => event === 'visibilitychange');
      expect(blur).toBeDefined();
      expect(visibility).toBeDefined();
      await view.unmount();
      expect(removeWindow).toHaveBeenCalledWith('blur', blur?.[1]);
      expect(removeDocument).toHaveBeenCalledWith('visibilitychange', visibility?.[1]);
    });
  });
}
it('clears Values bulk disclosure and rejects a pending bulk reveal', async () => {
  const view = await renderSurface('Values');
  await act(async () => click(view.container, 'Reveal every secret'));
  await settle();
  expect(view.container.textContent).toContain(revealed.value);
  await act(async () => loseFocus('blur'));
  expect(view.container.textContent).not.toContain(revealed.value);
  const pending = deferred<{ items: (typeof revealed & { classification: 'secret' })[] }>();
  mocks.all.mockReturnValueOnce(pending.promise);
  await act(async () => click(view.container, 'Reveal every secret'));
  await settle();
  await act(async () => loseFocus('visibilitychange'));
  await act(async () => regainFocus());
  await act(async () => pending.resolve({ items: [{ ...revealed, classification: 'secret' }] }));
  await settle();
  expect(view.container.textContent).not.toContain(revealed.value);
  await view.unmount();
});
