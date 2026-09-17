import { describe, expect, it } from 'vitest';

import { design } from './design.ts';

describe('design()', () => {
  it('emits a relative image URL that resolves under any deployment base', () => {
    const { url } = design('Button/Primary');
    expect(url).toBe('design/Button--Primary.png');
    // Published under /storybook/ (docs.yml) and served at the root in dev.
    expect(new URL(url, 'https://hikyo.app/storybook/iframe.html?id=ui-button--primary').pathname).toBe('/storybook/design/Button--Primary.png');
    expect(new URL(url, 'https://hikyo.app/storybook/?path=/story/ui-button--primary').pathname).toBe('/storybook/design/Button--Primary.png');
    expect(new URL(url, 'http://localhost:6006/iframe.html').pathname).toBe('/design/Button--Primary.png');
  });
  it('keeps the node name for the panel title', () => {
    expect(design('Members/Empty state')).toEqual({ type: 'image', url: 'design/Members--Empty state.png', name: 'Members/Empty state' });
  });
  it('rejects a malformed path', () => {
    expect(() => design('Button')).toThrow(/must be Title\/Variant/);
  });
});
