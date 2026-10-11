// @vitest-environment happy-dom
import { MemoryRouter, Route, Routes } from 'react-router';
import { describe, expect, it, vi } from 'vitest';
import { act } from 'react';
import { Button } from '../ui/Button.tsx';
import { renderForm } from '../testkit/renderForm.tsx';
import { DemoSurface } from './DemoSurface.tsx';
import { DEMO_MATRIX_PATH } from './config.ts';
import type { SurfaceId } from '../app/navigation.ts';

describe('demo route boundary', () => {
  it('lands directly in the sample matrix', async () => {
    const { container, unmount } = await renderForm(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route path="/" element={<DemoSurface id="overview">Overview</DemoSurface>} />
          <Route path={DEMO_MATRIX_PATH} element={<h1>Sample matrix</h1>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(container.textContent).toContain('Sample matrix');
    await unmount();
  });

  it('keeps settings navigation while disabling form controls', async () => {
    const { container, unmount } = await renderForm(
      <MemoryRouter>
        <DemoSurface id="project-settings">
          <a href="#metadata">Metadata</a>
          <input aria-label="Project name" defaultValue="Sample project" />
          <button type="button">Save</button>
        </DemoSurface>
      </MemoryRouter>,
    );
    expect(container.querySelector('fieldset')?.disabled).toBe(true);
    expect(container.querySelector('a')?.textContent).toBe('Metadata');
    expect(container.textContent).toContain('Read-only in this demo');
    await unmount();
  });

  it('allows browsing actions but blocks mutation buttons and Enter submissions', async () => {
    const mutate = vi.fn();
    const browse = vi.fn();
    const { container, unmount } = await renderForm(
      <MemoryRouter>
        <DemoSurface id="machine-access">
          <form onSubmit={mutate}>
            <input defaultValue="Sample account" />
            <Button type="submit">Create account</Button>
            <Button type="button" readOnlySafe onClick={browse}>View details</Button>
          </form>
        </DemoSurface>
      </MemoryRouter>,
    );
    const submit = container.querySelector<HTMLButtonElement>('button[type="submit"]');
    const view = container.querySelector<HTMLButtonElement>('button[type="button"]');
    expect(submit?.disabled).toBe(true);
    expect(view?.disabled).toBe(false);
    await act(async () => {
      submit?.click();
      view?.click();
      container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    });
    expect(browse).toHaveBeenCalledOnce();
    expect(mutate).not.toHaveBeenCalled();
    await unmount();
  });

  it('keeps audit filter submissions active', async () => {
    const filter = vi.fn((event: React.SubmitEvent<HTMLFormElement>) => event.preventDefault());
    const { container, unmount } = await renderForm(
      <MemoryRouter>
        <DemoSurface id="audit">
          <form onSubmit={filter}><Button type="submit">Apply filter</Button></form>
        </DemoSurface>
      </MemoryRouter>,
    );
    await act(async () => { container.querySelector('form')?.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); });
    expect(filter).toHaveBeenCalledOnce();
    expect(container.querySelector('button')?.disabled).toBe(false);
    await unmount();
  });

  const unavailable: readonly { id: SurfaceId; path: string }[] = [
    { id: 'instance-config', path: '/instance/config' },
    { id: 'matrix', path: `${DEMO_MATRIX_PATH}?remote=other-instance` },
  ];
  it.each(unavailable)('does not mount an unsupported $id surface', async ({ id, path }) => {
    const { container, unmount } = await renderForm(
      <MemoryRouter initialEntries={[path]}>
        <DemoSurface id={id}><p>Unsupported surface mounted</p></DemoSurface>
      </MemoryRouter>,
    );
    expect(container.textContent).toContain('Not available in this demo');
    expect(container.textContent).not.toContain('Unsupported surface mounted');
    expect(container.querySelector('a')?.getAttribute('href')).toBe(DEMO_MATRIX_PATH);
    await unmount();
  });
});
