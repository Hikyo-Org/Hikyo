// @vitest-environment happy-dom
import { composeStory } from '@storybook/react-vite';
import { createElement } from 'react';
import { describe, expect, it, vi } from 'vitest';

import { appearancePass, interactionOnce } from './interactionCoverage.ts';

describe('interaction coverage is an explicit test project choice', () => {
  it('runs the full play in ordinary dark and light Canvas/Docs', () => {
    expect(appearancePass({ theme: 'dark' })).toBe(false);
    expect(appearancePass({ theme: 'light' })).toBe(false);
    expect(appearancePass({ theme: 'dark', hikyoInteractionPass: 'full' })).toBe(false);
    expect(appearancePass({ theme: 'light', hikyoInteractionPass: 'full' })).toBe(false);
  });

  it('selects the retained appearance only in the explicitly marked light test project', () => {
    expect(appearancePass({ theme: 'light', hikyoInteractionPass: 'appearance' })).toBe(true);
  });

  it('rejects mistyped modes and refuses to remove the default dark interaction gate', () => {
    expect(() => appearancePass({ theme: 'dark', hikyoInteractionPass: 'appearance' })).toThrow('restricted to the light');
    expect(() => appearancePass({ hikyoInteractionPass: 'appearance' })).toThrow('restricted to the light');
    expect(() => appearancePass({ theme: 'light', hikyoInteractionPass: 'skip' })).toThrow('Invalid Storybook interaction pass');
  });

  it('delegates real composed contexts to the full or retained appearance play without skipping the story', async () => {
    const full = vi.fn(async () => {});
    const appearance = vi.fn(async () => {});
    const story = composeStory({
      render: () => createElement('div'),
      play: interactionOnce('pages-overview--create-project-journey', full, appearance),
    }, { title: 'Tests/Interaction coverage' }, {}, 'Journey');
    const play = story.play;
    if (play === undefined) throw new Error('Interaction fixture has no play function');
    await play({ globals: { theme: 'dark', hikyoInteractionPass: 'full' } });
    await play({ globals: { theme: 'light' } });
    expect(full).toHaveBeenCalledTimes(2);
    expect(appearance).not.toHaveBeenCalled();
    await play({ globals: { theme: 'light', hikyoInteractionPass: 'appearance' } });
    expect(full).toHaveBeenCalledTimes(2);
    expect(appearance).toHaveBeenCalledOnce();
  });

  it('awaits appearance readiness and propagates its failure before accessibility can claim success', async () => {
    const readiness = vi.fn(async () => { throw new Error('API route is not ready'); });
    const story = composeStory({
      render: () => createElement('div'),
      play: interactionOnce('pages-overview--create-project-journey', async () => {}, readiness),
    }, { title: 'Tests/Interaction coverage' }, {}, 'Journey');
    const play = story.play;
    if (play === undefined) throw new Error('Interaction fixture has no play function');
    await expect(play({ globals: { theme: 'light', hikyoInteractionPass: 'appearance' } })).rejects.toThrow('API route is not ready');
    expect(readiness).toHaveBeenCalledOnce();
  });
});
