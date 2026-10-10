import { describe, expect, it } from 'vitest';

import { storybookOutputDirectory } from './build-options.ts';

describe('immutable Storybook output selection', () => {
  it('tracks the installed Storybook output flag forms while preserving its default', () => {
    expect(storybookOutputDirectory([])).toBe('storybook-static');
    expect(storybookOutputDirectory(['--quiet', '-o', '.artifacts/after'])).toBe('.artifacts/after');
    expect(storybookOutputDirectory(['--output-dir', '/tmp/immutable-storybook'])).toBe('/tmp/immutable-storybook');
    expect(storybookOutputDirectory(['--output-dir=.artifacts/after'])).toBe('.artifacts/after');
    expect(() => storybookOutputDirectory(['-o'])).toThrow('requires an output directory');
    expect(() => storybookOutputDirectory(['--output-dir', '--quiet'])).toThrow('requires an output directory');
    expect(() => storybookOutputDirectory(['--output-dir='])).toThrow('requires an output directory');
  });
});
