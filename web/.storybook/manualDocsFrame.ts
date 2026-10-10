/** The small window surface needed to distinguish a Docs example from Canvas. */
export type DocsFrameWindow = {
  readonly parent: DocsFrameWindow;
  readonly document: { querySelector(selector: string): unknown };
};

/**
 * Storybook 10.6's non-inline Docs iframe uses viewMode=story and ignores
 * docs.story.autoplay=false. A journey must leave that frame ready for manual
 * exploration, while still running its play steps in Canvas and browser tests.
 */
export function isManualDocsFrame(frame: DocsFrameWindow = window): boolean {
  try {
    const parent = frame.parent;
    return parent !== frame && Boolean(parent.document.querySelector('.sbdocs-content'));
  } catch {
    // An unrelated cross-origin parent cannot be inspected as a Docs document.
    return false;
  }
}
