import type { DocsContainerProps } from '@storybook/addon-docs/blocks';
import { lazy, Suspense, useCallback, useLayoutEffect, useSyncExternalStore, type PropsWithChildren, type ReactNode } from 'react';
import { themes } from 'storybook/theming';

import { appTheme, docsParentRoot, publishDocsTheme, subscribeDocsGlobals, syncStoryTheme, type AppTheme } from './docsTheme.ts';

// Canvas does not need Docs blocks. Keep their existing lazy loading boundary.
const DocsContainer = lazy(() => import('@storybook/addon-docs/blocks').then((blocks) => ({ default: blocks.DocsContainer })));

/** The public Docs context owns the toolbar globals; framed examples omit them in their URL. */
export function HikyoDocsContainer({ context, children }: PropsWithChildren<DocsContainerProps>) {
  const currentTheme = useCallback((): AppTheme => {
    const story = context.componentStories()[0];
    return story === undefined ? 'dark' : appTheme(context.getStoryContext(story).globals);
  }, [context]);
  const subscribe = useCallback((update: () => void) => {
    return subscribeDocsGlobals(context.channel, update);
  }, [context]);
  const theme = useSyncExternalStore(subscribe, currentTheme, currentTheme);
  useLayoutEffect(() => publishDocsTheme(document.documentElement, theme), [theme]);
  return <Suspense fallback={<p role="status">Loading documentation…</p>}>
    <DocsContainer context={context} theme={themes[theme]}>{children}</DocsContainer>
  </Suspense>;
}

/** Attribute updates preserve the independent frame's provider, requests and interaction state. */
export function StoryTheme({ theme, children }: { theme: AppTheme; children: ReactNode }) {
  useLayoutEffect(() => syncStoryTheme(document, docsParentRoot(window), theme), [theme]);
  return children;
}
