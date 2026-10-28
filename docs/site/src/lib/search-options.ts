import type { AdvancedOptions } from 'fumadocs-core/search/server';

// The dialog uses relevance ranking, never field sorting. Omitting those unused
// indexes keeps all documents and snippets without exporting a second text copy.
export const docsSearchOptions = {
  sort: { enabled: false },
} satisfies Pick<AdvancedOptions, 'sort'>;
