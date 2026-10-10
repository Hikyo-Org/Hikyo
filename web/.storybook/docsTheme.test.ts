// @vitest-environment happy-dom
import { describe, expect, it, vi } from 'vitest';

import { appTheme, DOCS_THEME_ATTRIBUTE, publishDocsTheme, subscribeDocsGlobals, syncStoryTheme } from './docsTheme.ts';

describe('Docs toolbar theme reaches independent story frames', () => {
  it('inherits the Docs theme even when a child frame starts with default dark globals', async () => {
    const docs = document.implementation.createHTMLDocument('Docs');
    const child = document.implementation.createHTMLDocument('Story frame');
    const reset = publishDocsTheme(docs.documentElement, 'light');
    const cleanup = syncStoryTheme(child, docs.documentElement, 'dark');
    try {
      expect(child.documentElement.dataset.theme).toBe('light');
      const input = child.createElement('input');
      input.value = 'Unsaved API form draft';
      child.body.appendChild(input);
      docs.documentElement.setAttribute(DOCS_THEME_ATTRIBUTE, 'dark');
      await vi.waitFor(() => expect(child.documentElement.dataset.theme).toBe('dark'));
      docs.documentElement.setAttribute(DOCS_THEME_ATTRIBUTE, 'light');
      await vi.waitFor(() => expect(child.documentElement.dataset.theme).toBe('light'));
      expect(child.body.firstChild).toBe(input);
      expect(input.value).toBe('Unsaved API form draft');
    } finally { cleanup(); reset(); }
  });

  it('keeps inline Docs on the owner theme and Canvas on its own globals', async () => {
    const docs = document.implementation.createHTMLDocument('Inline Docs');
    const manager = document.implementation.createHTMLDocument('Manager');
    const restore = publishDocsTheme(docs.documentElement, 'light');
    const cleanup = syncStoryTheme(docs, manager.documentElement, 'dark');
    try { expect(docs.documentElement.dataset.theme).toBe('light'); }
    finally { cleanup(); restore(); }
    const canvas = document.implementation.createHTMLDocument('Canvas');
    const cleanupCanvas = syncStoryTheme(canvas, manager.documentElement, 'light');
    try { expect(canvas.documentElement.dataset.theme).toBe('light'); }
    finally { cleanupCanvas(); }
    expect(appTheme({ theme: 'light' })).toBe('light');
    expect(appTheme({})).toBe('dark');
  });

  it('disconnects observers and restores owned attributes on navigation cleanup', async () => {
    const docs = document.implementation.createHTMLDocument('Docs');
    const child = document.implementation.createHTMLDocument('Story frame');
    child.documentElement.dataset.theme = 'dark';
    const reset = publishDocsTheme(docs.documentElement, 'light');
    const cleanup = syncStoryTheme(child, docs.documentElement, 'dark');
    cleanup();
    docs.documentElement.setAttribute(DOCS_THEME_ATTRIBUTE, 'dark');
    await Promise.resolve();
    expect(child.documentElement.dataset.theme).toBe('dark');
    reset();
    expect(docs.documentElement.hasAttribute(DOCS_THEME_ATTRIBUTE)).toBe(false);
    expect(docs.documentElement.hasAttribute('data-theme')).toBe(false);
  });

  it('unsubscribes the public Docs context globals channel on unmount', () => {
    const listeners = new Set<() => void>();
    const update = vi.fn();
    const channel = {
      on: (_event: string, listener: () => void) => { listeners.add(listener); },
      off: (_event: string, listener: () => void) => { listeners.delete(listener); },
    };
    const cleanup = subscribeDocsGlobals(channel, update);
    for (const listener of listeners) listener();
    expect(update).toHaveBeenCalledOnce();
    cleanup();
    for (const listener of listeners) listener();
    expect(update).toHaveBeenCalledOnce();
    expect(listeners.size).toBe(0);
  });
});
