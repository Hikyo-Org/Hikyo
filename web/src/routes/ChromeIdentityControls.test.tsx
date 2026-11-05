// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ReadOnlyContext } from '../ui/ReadOnlyContext.ts';
import type { PreparedImage } from './chrome-image.ts';
import { ChromeIdentityControls, type ChromeIdentityActions } from './ChromeIdentityControls.tsx';

const prepare = vi.hoisted((): { fn: ((file: File) => Promise<PreparedImage>) | undefined } => ({ fn: undefined }));
vi.mock('./chrome-image.ts', async (importOriginal) => {
  const real = await importOriginal<typeof import('./chrome-image.ts')>();
  return { ...real, prepareChromeImage: (file: File) => (prepare.fn ?? real.prepareChromeImage)(file) };
});

const actions: ChromeIdentityActions = {
  setIdentity: () => Promise.resolve(),
  setImage: () => Promise.resolve(),
  clearImage: () => Promise.resolve(),
};

function render(node: React.ReactElement): HTMLElement {
  const container = document.createElement('div');
  container.innerHTML = renderToStaticMarkup(node);
  return container;
}

describe('ChromeIdentityControls', () => {
  // vitest runs with MODE=test, so this is the production branch.
  it('renders only the read-only identity preview and its children without actions', () => {
    const container = render(
      <ChromeIdentityControls identityId="prj_1" name="Payments" kind="project">
        <input aria-label="Name" />
      </ChromeIdentityControls>,
    );
    expect(container.querySelector('.identity-controls__preview')?.textContent).toBe('PA');
    expect(container.querySelector('input[aria-label="Name"]')).not.toBeNull();
    expect(container.querySelectorAll('button')).toHaveLength(0);
    expect(container.textContent).not.toContain('not available');
    expect(container.querySelector('input[type="range"]')).toBeNull();
    expect(container.querySelector('input[type="file"]')).toBeNull();
    expect(container.querySelector('[disabled], [aria-disabled]')).toBeNull();
  });

  it('stays read-only under ReadOnlyContext even with actions', () => {
    const container = render(
      <ReadOnlyContext.Provider value>
        <ChromeIdentityControls
          identityId="prj_1"
          name="Payments"
          kind="project"
          identity={{ hue: 195 }}
          actions={actions}
        />
      </ReadOnlyContext.Provider>,
    );
    expect(container.querySelectorAll('button')).toHaveLength(0);
    expect(container.querySelector('input')).toBeNull();
  });

  it.each(['org', 'project'] as const)('is editable for a %s with named controls', (kind) => {
    const container = render(
      <ChromeIdentityControls
        identityId="x"
        name="Payments"
        kind={kind}
        identity={{ hue: 280, mark: 'PO' }}
        actions={actions}
      />,
    );
    expect(container.querySelector('.identity-controls__preview')?.textContent).toBe('PO');
    expect(container.querySelectorAll('.identity-hue')).toHaveLength(6);
    expect(container.querySelector('.identity-hue[aria-pressed="true"]')?.getAttribute('aria-label')).toBe('Hue 280');
    expect(container.querySelector('input[type="range"]')).not.toBeNull();
    const text = container.querySelector<HTMLInputElement>('input[type="text"]');
    expect(text?.value).toBe('PO');
    expect(container.querySelector(`label[for="${text?.id ?? ''}"]`)).not.toBeNull();
    expect(container.querySelector('input[type="file"]')?.getAttribute('accept')).toBe(
      'image/png,image/jpeg,image/webp,image/svg+xml',
    );
    expect(container.textContent).toContain(
      kind === 'org'
        ? 'PNG, JPEG, WebP or SVG, replaces the monogram'
        : 'PNG, JPEG, WebP or SVG, replaces the content',
    );
    expect(container.textContent).toContain('converted to PNG');
    for (const button of container.querySelectorAll('button')) {
      expect(button.getAttribute('aria-label') ?? button.textContent).not.toBe('');
    }
  });

  it('offers image removal only when an image exists', () => {
    const withImage = render(
      <ChromeIdentityControls
        identityId="x"
        name="Payments"
        kind="org"
        identity={{ hue: 195, image_version: 'abc' }}
        imageUrl={(version) => `/img?v=${version}`}
        actions={actions}
      />,
    );
    expect(withImage.querySelector('button[aria-label="remove image"]')).not.toBeNull();
    const without = render(
      <ChromeIdentityControls identityId="x" name="Payments" kind="org" identity={{ hue: 195 }} actions={actions} />,
    );
    expect(without.querySelector('button[aria-label="remove image"]')).toBeNull();
  });
});

describe('ChromeIdentityControls saving', () => {
  type Saved = { hue?: number; mark?: string };
  let root: Root | undefined;
  let host: HTMLElement | undefined;

  afterEach(() => {
    act(() => root?.unmount());
    host?.remove();
    root = undefined;
  });

  function mount(identity: { hue: number; mark?: string | null }, setIdentity: (next: Saved) => Promise<void>) {
    host = document.createElement('div');
    document.body.append(host);
    root = createRoot(host);
    const draw = (current: { hue: number; mark?: string | null }) => {
      act(() =>
        root?.render(
          <ChromeIdentityControls
            identityId="x"
            name="Payments"
            kind="org"
            identity={current}
            actions={{ ...actions, setIdentity }}
          />,
        ),
      );
    };
    draw(identity);
    return draw;
  }
  const preset = (hue: number) => {
    const button = host?.querySelector<HTMLButtonElement>(`button[aria-label="Hue ${String(hue)}"]`);
    if (!button) throw new Error(`no preset ${String(hue)}`);
    return button;
  };
  const pressedHue = () => host?.querySelector('.identity-hue[aria-pressed="true"]')?.getAttribute('aria-label');
  const markInput = () => {
    const input = host?.querySelector<HTMLInputElement>('input[type="text"]');
    if (!input) throw new Error('no mark input');
    return input;
  };
  function type(input: HTMLInputElement, value: string) {
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
    setter?.call(input, value);
    input.dispatchEvent(new Event('input', { bubbles: true }));
  }
  function deferred() {
    let resolve = () => {};
    let reject: (reason: Error) => void = () => {};
    const promise = new Promise<void>((res, rej) => {
      resolve = res;
      reject = rej;
    });
    return { promise, resolve, reject };
  }

  it('keeps showing a choice until the server holds it', async () => {
    const first = deferred();
    const setIdentity = vi.fn<(next: Saved) => Promise<void>>(() => first.promise);
    const redraw = mount({ hue: 195 }, setIdentity);
    await act(async () => preset(280).click());
    expect(setIdentity).toHaveBeenCalledExactlyOnceWith({ hue: 280 });
    expect(pressedHue()).toBe('Hue 280');
    redraw({ hue: 280 });
    await act(async () => {
      first.resolve();
      await first.promise;
    });
    expect(pressedHue()).toBe('Hue 280');
  });

  it('leaves no draft behind when a failed save is followed by the server value', async () => {
    const first = deferred();
    const setIdentity = vi
      .fn<(next: Saved) => Promise<void>>()
      .mockReturnValueOnce(first.promise)
      .mockResolvedValue(undefined);
    const redraw = mount({ hue: 195 }, setIdentity);
    await act(async () => preset(280).click());
    await act(async () => preset(195).click());
    await act(async () => {
      first.reject(new Error('refused'));
      await first.promise.catch(() => undefined);
    });
    expect(setIdentity).toHaveBeenCalledTimes(2);
    // Another admin's change must show: no stale 195 draft may mask it.
    redraw({ hue: 60 });
    expect(pressedHue()).toBe('Hue 60');
  });

  it('reverts to the server state when the save fails', async () => {
    const setIdentity = vi.fn(() => Promise.reject(new Error('refused')));
    mount({ hue: 195 }, setIdentity);
    await act(async () => preset(280).click());
    expect(pressedHue()).toBe('Hue 195');
  });

  it('keeps an edit made after the save that failed', async () => {
    const first = deferred();
    const setIdentity = vi.fn<(next: Saved) => Promise<void>>(() => first.promise);
    mount({ hue: 195 }, setIdentity);
    await act(async () => preset(280).click());
    act(() => type(markInput(), 'AB'));
    await act(async () => {
      first.reject(new Error('refused'));
      await first.promise.catch(() => undefined);
    });
    expect(pressedHue()).toBe('Hue 195');
    expect(markInput().value).toBe('AB');
  });

  it('does not lose a mark saved just before a hue', async () => {
    const first = deferred();
    const setIdentity = vi.fn<(next: Saved) => Promise<void>>(() => first.promise);
    const redraw = mount({ hue: 195 }, setIdentity);
    act(() => type(markInput(), 'AB'));
    await act(async () => {
      markInput().focus();
      markInput().blur();
    });
    expect(setIdentity).toHaveBeenLastCalledWith({ mark: 'AB' });
    await act(async () => preset(280).click());
    // Serialised: the second save waits for the first and carries only the hue,
    // so it cannot overwrite a mark another admin saved in between.
    expect(setIdentity).toHaveBeenCalledTimes(1);
    await act(async () => {
      first.resolve();
      await first.promise;
    });
    expect(setIdentity).toHaveBeenLastCalledWith({ hue: 280 });
    redraw({ hue: 280, mark: 'AB' });
    expect(pressedHue()).toBe('Hue 280');
    expect(markInput().value).toBe('AB');
  });

  it('coalesces changes made during a save to the latest', async () => {
    const first = deferred();
    const setIdentity = vi.fn<(next: Saved) => Promise<void>>(() => first.promise);
    mount({ hue: 195 }, setIdentity);
    await act(async () => preset(280).click());
    await act(async () => preset(60).click());
    await act(async () => preset(195).click());
    expect(setIdentity).toHaveBeenCalledTimes(1);
    await act(async () => {
      first.resolve();
      await first.promise;
    });
    expect(setIdentity.mock.calls.map(([next]) => next.hue)).toEqual([280, 195]);
  });

  it('saves the slider once, on its native change', async () => {
    const setIdentity = vi.fn(() => Promise.resolve());
    mount({ hue: 195 }, setIdentity);
    const range = host?.querySelector<HTMLInputElement>('input[type="range"]');
    if (!range) throw new Error('no range');
    act(() => type(range, '100'));
    expect(setIdentity).not.toHaveBeenCalled();
    await act(async () => range.dispatchEvent(new Event('change')));
    expect(setIdentity).toHaveBeenCalledExactlyOnceWith({ hue: 100 });
  });
});

describe('ChromeIdentityControls image uploads', () => {
  afterEach(() => {
    prepare.fn = undefined;
  });

  it('uploads in selection order when a later file prepares first', async () => {
    let finishSvg: (image: PreparedImage) => void = () => {};
    const svgPng = new Blob(['svg-png']);
    const png = new Blob(['png']);
    prepare.fn = (file) =>
      file.name === 'slow.svg'
        ? new Promise<PreparedImage>((resolve) => {
            finishSvg = resolve;
          })
        : Promise.resolve({ ok: true, blob: png, contentType: 'image/png' });
    const uploads: Blob[] = [];
    const setImage = vi.fn(({ blob }: { blob: Blob }) => {
      uploads.push(blob);
      return Promise.resolve();
    });
    const host = document.createElement('div');
    document.body.append(host);
    const root = createRoot(host);
    act(() =>
      root.render(
        <ChromeIdentityControls identityId="x" name="Payments" kind="org" identity={{ hue: 195 }} actions={{ ...actions, setImage }} />,
      ),
    );
    const input = host.querySelector<HTMLInputElement>('input[type="file"]');
    if (!input) throw new Error('no file input');
    const pick = async (file: File) => {
      Object.defineProperty(input, 'files', { configurable: true, value: [file] });
      await act(async () => input.dispatchEvent(new Event('change', { bubbles: true })));
    };
    await pick(new File(['<svg/>'], 'slow.svg', { type: 'image/svg+xml' }));
    await pick(new File(['png'], 'fast.png', { type: 'image/png' }));
    // The PNG is ready, but it must wait for the SVG picked before it.
    expect(setImage).not.toHaveBeenCalled();
    await act(async () => finishSvg({ ok: true, blob: svgPng, contentType: 'image/png' }));
    expect(uploads).toEqual([svgPng, png]);
    act(() => root.unmount());
    host.remove();
  });
});
