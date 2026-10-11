import { describe, expect, it, vi } from 'vitest';

import { type DocsFrameWindow, isManualDocsFrame } from './manualDocsFrame.ts';

function topWindow(hasDocs: boolean): DocsFrameWindow {
  const frame: DocsFrameWindow = {
    get parent() { return frame; },
    document: { querySelector: vi.fn().mockReturnValue(hasDocs ? {} : null) },
  };
  return frame;
}

describe('manual Docs frame detection', () => {
  it('recognizes a separate iframe inside the same-origin Docs document', () => {
    const parent = topWindow(true);
    const frame: DocsFrameWindow = { parent, document: topWindow(false).document };
    expect(isManualDocsFrame(frame)).toBe(true);
    expect(parent.document.querySelector).toHaveBeenCalledWith('.sbdocs-content');
  });

  it('keeps a standalone Canvas play active even when its own document has Docs markup', () => {
    const frame = topWindow(true);
    expect(isManualDocsFrame(frame)).toBe(false);
    expect(frame.document.querySelector).not.toHaveBeenCalled();
  });

  it('keeps the Canvas manager iframe and browser test iframe active', () => {
    const frame: DocsFrameWindow = { parent: topWindow(false), document: topWindow(false).document };
    expect(isManualDocsFrame(frame)).toBe(false);
  });

  it('does not fail a journey when a cross-origin parent document is inaccessible', () => {
    const parent: DocsFrameWindow = {
      parent: topWindow(false),
      get document(): DocsFrameWindow['document'] { throw new Error('Blocked cross-origin document'); },
    };
    const frame: DocsFrameWindow = { parent, document: topWindow(false).document };
    expect(isManualDocsFrame(frame)).toBe(false);
  });

  it('handles an inaccessible parent window safely', () => {
    const frame: DocsFrameWindow = {
      get parent(): DocsFrameWindow { throw new Error('Blocked parent window'); },
      document: topWindow(false).document,
    };
    expect(isManualDocsFrame(frame)).toBe(false);
  });
});
