// @vitest-environment happy-dom
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { ReadOnlyContext } from '../ui/ReadOnlyContext.ts';
import { ChromeIdentityControls, type ChromeIdentityActions } from './ChromeIdentityControls.tsx';

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
