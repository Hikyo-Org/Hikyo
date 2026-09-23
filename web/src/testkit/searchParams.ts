/**
 * Story `beforeEach`: put `params` on the story frame's URL for the story's
 * lifetime, for pages that read their state from the real location in a
 * useState initialiser (so it has to be there before mount). The cleanup
 * removes only those parameters: Storybook has moved the frame's own
 * `id`/`globals` on by the time it runs, and restoring a captured href would
 * drag the canvas back to this story.
 */
export const withSearchParams = (params: Record<string, string>) => () => {
  const next = new URL(globalThis.location.href);
  for (const [key, value] of Object.entries(params)) {
    next.searchParams.set(key, value);
  }
  globalThis.history.replaceState(null, '', next);
  return () => {
    const current = new URL(globalThis.location.href);
    for (const key of Object.keys(params)) {
      current.searchParams.delete(key);
    }
    globalThis.history.replaceState(null, '', current);
  };
};
