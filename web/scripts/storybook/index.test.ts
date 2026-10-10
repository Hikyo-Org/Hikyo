import { describe, expect, it } from 'vitest';

import { validateAppDocsFrames, validateStorybookIndex } from './index.ts';

const button = './src/ui/Button.stories.tsx';
const story = { id: 'ui-button--default', title: 'UI/Button', name: 'Default', importPath: button, type: 'story', tags: ['autodocs'] };
const docs = { id: 'ui-button--docs', title: 'UI/Button', name: 'Docs', importPath: button, type: 'docs', tags: ['autodocs'] };
const json = (...entries: (typeof story)[]) => JSON.stringify({ v: 5, entries: Object.fromEntries(entries.map((entry) => [entry.id, entry])) });

describe('complete built Storybook Docs coverage', () => {
  it('rejects an app export when only its sibling uses the shared frame', () => {
    const text = `
      import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
      export default { title: 'UI/Button' };
      export const Framed = { parameters: { ...topLayerDocs } };
      export const Unframed = { parameters: { app: {} } };
    `;
    expect(() => validateAppDocsFrames([{ path: button, text }])).toThrow('Unframed');
  });

  it('matches generated Docs by normalized source import path and counts variants once as a module', () => {
    expect(validateStorybookIndex(json(story, { ...story, id: 'ui-button--busy', name: 'Busy' }, docs), ['src/ui/Button.stories.tsx'])).toEqual({ modules: 1, stories: 2, docs: 1 });
  });

  it('rejects invalid JSON, unsupported versions, malformed entries and empty indexes', () => {
    for (const input of ['{', '{}', JSON.stringify({ v: 4, entries: {} }), JSON.stringify({ v: 5, entries: { broken: { type: 'story' } } }), json()]) {
      expect(() => validateStorybookIndex(input, [button])).toThrow();
    }
    expect(() => validateStorybookIndex(json(story, docs), [])).toThrow('No source story modules');
    expect(() => validateStorybookIndex(json({ ...story, title: ' ' }, docs), [button])).toThrow();
  });

  it('rejects stripped Docs and module opt-outs without accepting another same-title Docs page', () => {
    expect(() => validateStorybookIndex(json(story), [button])).toThrow('expected one generated Docs entry, found 0');
    const unrelated = { ...docs, importPath: './src/ui/Other.stories.tsx' };
    expect(() => validateStorybookIndex(json(story, unrelated), [button])).toThrow('not a source story module');
    expect(() => validateStorybookIndex(json(story, { ...docs, tags: ['!autodocs'] }), [button])).toThrow('not generated autodocs');
    expect(() => validateStorybookIndex(json(story, docs, { ...docs, id: 'ui-button--other-docs' }), [button])).toThrow('found 2');
  });

  it('rejects recursively inventoried modules absent from discovery and modules with no story exports', () => {
    expect(() => validateStorybookIndex(json(story, docs), [button, 'src/features/deep/Page.stories.tsx'])).toThrow('src/features/deep/Page.stories.tsx: no stories');
    expect(() => validateStorybookIndex(json(docs), [button])).toThrow('no stories');
  });

  it('rejects malformed identities and mismatched Docs titles', () => {
    const wrongKey = JSON.stringify({ v: 5, entries: { wrong: story, [docs.id]: docs } });
    expect(() => validateStorybookIndex(wrongKey, [button])).toThrow('index key differs');
    expect(() => validateStorybookIndex(json(story, { ...docs, title: 'Unrelated title' }), [button])).toThrow('Docs title differs');
  });
});
