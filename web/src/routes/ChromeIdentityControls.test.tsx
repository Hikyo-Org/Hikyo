// @vitest-environment happy-dom
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { ChromeIdentityControls } from './ChromeIdentityControls.tsx';

describe('ChromeIdentityControls', () => {
  // vitest runs with MODE=test, so this is the production branch.
  it('renders only the read-only identity preview and its children outside prototype mode', () => {
    const container = document.createElement('div');
    container.innerHTML = renderToStaticMarkup(
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
    expect(container.textContent).not.toContain('not available');
  });
});
