export type AppTheme = 'dark' | 'light';
export const DOCS_THEME_ATTRIBUTE = 'data-storybook-docs-theme';

/** The installed Docs Controls block subscribes to this same canonical globals event. */
export function subscribeDocsGlobals(channel: {
  on: (event: string, update: () => void) => void;
  off: (event: string, update: () => void) => void;
}, update: () => void): () => void {
  channel.on(GLOBALS_UPDATED, update);
  return () => channel.off(GLOBALS_UPDATED, update);
}

export function publishDocsTheme(root: HTMLElement, theme: AppTheme): () => void {
  const previousTheme = root.getAttribute('data-theme');
  const previousPublished = root.getAttribute(DOCS_THEME_ATTRIBUTE);
  root.dataset.theme = theme;
  root.setAttribute(DOCS_THEME_ATTRIBUTE, theme);
  return () => {
    if (previousTheme === null) root.removeAttribute('data-theme');
    else root.setAttribute('data-theme', previousTheme);
    if (previousPublished === null) root.removeAttribute(DOCS_THEME_ATTRIBUTE);
    else root.setAttribute(DOCS_THEME_ATTRIBUTE, previousPublished);
  };
}

export function appTheme(globals: { theme?: string }): AppTheme {
  return globals.theme === 'light' ? 'light' : 'dark';
}

/** Only read the containing document when browser same-origin access permits it. */
export function docsParentRoot(frame: Window): HTMLElement | undefined {
  if (frame.parent === frame) return undefined;
  try {
    return frame.parent.document.documentElement;
  } catch {
    return undefined;
  }
}

function publishedTheme(root: HTMLElement | undefined): AppTheme | undefined {
  const value = root?.getAttribute(DOCS_THEME_ATTRIBUTE);
  return value === 'dark' || value === 'light' ? value : undefined;
}

/** Synchronize CSS only; changing an iframe URL or React key would lose live form state. */
export function syncStoryTheme(document: Document, parent: HTMLElement | undefined, fallback: AppTheme): () => void {
  const root = document.documentElement;
  const previous = root.getAttribute('data-theme');
  const apply = () => {
    root.dataset.theme = publishedTheme(root) ?? publishedTheme(parent) ?? fallback;
  };
  apply();
  const observer = new MutationObserver(apply);
  const options = { attributes: true, attributeFilter: [DOCS_THEME_ATTRIBUTE] };
  observer.observe(root, options);
  if (parent !== undefined) observer.observe(parent, options);
  return () => {
    observer.disconnect();
    if (previous === null) root.removeAttribute('data-theme');
    else root.setAttribute('data-theme', previous);
  };
}
import { GLOBALS_UPDATED } from 'storybook/internal/core-events';
