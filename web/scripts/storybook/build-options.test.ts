import { describe, expect, it } from 'vitest';

import { storybookIndexDirectory, storybookOutputDirectory } from './build-options.ts';

describe('immutable Storybook output selection', () => {
  it('accepts the Docs directory with or without a package-manager separator', () => {
    expect(storybookIndexDirectory([])).toBe('storybook-static');
    expect(storybookIndexDirectory(['--'])).toBe('storybook-static');
    expect(storybookIndexDirectory(['.artifacts/after'])).toBe('.artifacts/after');
    expect(storybookIndexDirectory(['--', '.artifacts/after'])).toBe('.artifacts/after');
    expect(() => storybookIndexDirectory(['first', 'second'])).toThrow('one build directory');
  });
  it('tracks the installed Storybook output flag forms while preserving its default', () => {
    expect(storybookOutputDirectory([])).toBe('storybook-static');
    expect(storybookOutputDirectory(['--quiet', '-o', '.artifacts/after'])).toBe('.artifacts/after');
    expect(storybookOutputDirectory(['-o.artifacts/attached-output'])).toBe('.artifacts/attached-output');
    expect(storybookOutputDirectory(['--output-dir=first', '-osecond'])).toBe('second');
    expect(storybookOutputDirectory(['--output-dir', '/tmp/immutable-storybook'])).toBe('/tmp/immutable-storybook');
    expect(storybookOutputDirectory(['--output-dir=.artifacts/after'])).toBe('.artifacts/after');
    expect(() => storybookOutputDirectory(['-o'])).toThrow('requires an output directory');
    expect(() => storybookOutputDirectory(['--output-dir', '--quiet'])).toThrow('requires an output directory');
    expect(() => storybookOutputDirectory(['--output-dir='])).toThrow('requires an output directory');
  });
});
