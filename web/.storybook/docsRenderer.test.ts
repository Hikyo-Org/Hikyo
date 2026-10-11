// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest';

import { retainDocsRender } from './docsRenderer.ts';

describe('Docs renderer preserves manually explored state on globals updates', () => {
  it('retains frames and an unsaved input instead of repeating a destructive root render', async () => {
    const element = document.createElement('div');
    const context = { page: 'Projects' };
    const parameters = { docs: true };
    const render = vi.fn(async (_context: typeof context, _parameters: typeof parameters, target: HTMLElement) => {
      target.replaceChildren(document.createElement('iframe'), document.createElement('input'));
    });
    const renderer = retainDocsRender({ render, unmount: vi.fn() });
    await renderer.render(context, parameters, element);
    const frame = element.querySelector('iframe');
    const input = element.querySelector('input');
    if (frame === null || input === null) throw new Error('Docs fixture did not mount');
    input.value = 'Unsaved project draft';
    await renderer.render(context, parameters, element);
    await renderer.render(context, parameters, element);
    expect(render).toHaveBeenCalledOnce();
    expect(element.querySelector('iframe')).toBe(frame);
    expect(element.querySelector('input')).toBe(input);
    expect(input.value).toBe('Unsaved project draft');
  });

  it('delegates changed contexts, parameters, mount elements and navigation unmount', async () => {
    const context = { page: 'Projects' };
    const parameters = { docs: true };
    const first = document.createElement('div');
    const second = document.createElement('div');
    const render = vi.fn(async (_context: typeof context, _parameters: typeof parameters, _element: HTMLElement) => {});
    const unmount = vi.fn();
    const renderer = retainDocsRender({ render, unmount });
    await renderer.render(context, parameters, first);
    await renderer.render({ page: 'Projects after HMR' }, parameters, first);
    await renderer.render(context, { docs: false }, first);
    await renderer.render(context, parameters, second);
    renderer.unmount(second);
    expect(unmount).toHaveBeenCalledWith(second);
    await renderer.render(context, parameters, second);
    expect(render).toHaveBeenCalledTimes(5);
  });

  it('shares completion while mounting and retries a rejected render', async () => {
    const context = { page: 'Projects' };
    const parameters = { docs: true };
    const element = document.createElement('div');
    const render = vi.fn(async (_context: typeof context, _parameters: typeof parameters, _element: HTMLElement) => {});
    render.mockRejectedValueOnce(new Error('Docs module failed to load'));
    const renderer = retainDocsRender({ render, unmount: vi.fn() });
    const first = renderer.render(context, parameters, element);
    expect(renderer.render(context, parameters, element)).toBe(first);
    await expect(first).rejects.toThrow('Docs module failed to load');
    await renderer.render(context, parameters, element);
    expect(render).toHaveBeenCalledTimes(2);
  });

  it('does not evict a newer render when an older context fails asynchronously', async () => {
    const context = { page: 'Projects' };
    const newer = { page: 'Projects after HMR' };
    const parameters = { docs: true };
    const element = document.createElement('div');
    let failOld: (error: Error) => void = () => { throw new Error('Deferred render was not initialized'); };
    const pending = new Promise<void>((_resolve, reject) => { failOld = reject; });
    const render = vi.fn(async (_context: typeof context, _parameters: typeof parameters, _element: HTMLElement) => {});
    render.mockImplementationOnce(() => pending);
    const renderer = retainDocsRender({ render, unmount: vi.fn() });
    const old = renderer.render(context, parameters, element);
    const latest = renderer.render(newer, parameters, element);
    failOld(new Error('Old render failed'));
    await expect(old).rejects.toThrow('Old render failed');
    await latest;
    expect(renderer.render(newer, parameters, element)).toBe(latest);
    expect(render).toHaveBeenCalledTimes(2);
  });
});
