/** The content (mark) of a chrome identity is at most this many grapheme clusters. */
export const MAX_MARK_GRAPHEMES = 2;

const segmenter = new Intl.Segmenter(undefined, { granularity: 'grapheme' });

export function graphemes(text: string): string[] {
  return Array.from(segmenter.segment(text), (part) => part.segment);
}

/** limitMark trims and keeps the first two graphemes; the empty result means "use the monogram". */
export function limitMark(text: string): string {
  return graphemes(text.trim()).slice(0, MAX_MARK_GRAPHEMES).join('');
}
