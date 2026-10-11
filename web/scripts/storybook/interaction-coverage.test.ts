import { describe, expect, it } from 'vitest';

import { validateInteractionCoverage } from './interaction-coverage.ts';

const path = 'src/ui/Button.stories.tsx';
const opted = { id: 'ui-button--clicks', title: 'Design system/Button', name: 'Clicks', importPath: `./${path}`, type: 'story', exportName: 'Clicks', tags: ['test', 'play-fn', 'interaction-once', 'autodocs'] };
const retained = { ...opted, id: 'ui-button--default', name: 'Default', exportName: 'Default', tags: ['test', 'autodocs'] };
const docs = { ...retained, id: 'ui-button--docs', name: 'Docs', type: 'docs', exportName: 'Docs' };
const manifest = { [opted.id]: { reason: 'The callback does not change the rendered button.', retained: [retained.id] } };
const json = (...entries: (typeof opted)[]) => JSON.stringify({ v: 5, entries: Object.fromEntries(entries.map((entry) => [entry.id, entry])) });
const source = (play = `interactionOnce('${opted.id}', async () => {}, async () => {})`, helper = 'interactionOnce') => [{
  path,
  text: `import { interactionOnce${helper === 'interactionOnce' ? '' : ' as ' + helper} } from '../../.storybook/interactionCoverage.ts';
    const meta = { title: 'Design system/Button', id: 'ui-button' };
    export default meta;
    export const Default = {};
    export const Clicks = { tags: ['interaction-once'], play: ${play} };`,
}];

describe('complete Storybook interaction-once accounting', () => {
  it('accounts for all stories, Docs, full plays, appearance passes and retained examples', () => {
    expect(validateInteractionCoverage(json(opted, retained, docs), manifest, source())).toMatchObject({
      stories: 2, docs: 1, interactionOnce: 1, fullPlays: 1,
      lightFullPlays: 0, lightAppearancePlays: 1, storiesWithoutPlay: 1,
      cases: [{ id: opted.id, source: `./${path}`, exportName: 'Clicks', ...manifest[opted.id] }],
    });
  });

  it('uses the installed CSF parser for aliases, custom display names and source IDs', () => {
    const sources = source(`once('${opted.id}', async () => {}, async () => {})`, 'once');
    const first = sources[0];
    if (first === undefined) throw new Error('Missing test source');
    first.text = first.text.replace("tags: ['interaction-once']", "name: 'Click verification', tags: ['interaction-once']");
    expect(validateInteractionCoverage(json({ ...opted, name: 'Click verification' }, retained, docs), manifest, sources).interactionOnce).toBe(1);
  });

  it('rejects invalid, malformed, unsupported and empty full indexes', () => {
    for (const invalid of ['{', '{}', JSON.stringify({ v: 4, entries: {} }), JSON.stringify({ v: 5, entries: {} }), JSON.stringify({ v: 5, entries: { broken: { type: 'story' } } })]) {
      expect(() => validateInteractionCoverage(invalid, manifest, source())).toThrow();
    }
    expect(() => validateInteractionCoverage(JSON.stringify({ v: 5, entries: { wrong: opted, [retained.id]: retained, [docs.id]: docs } }), manifest, source())).toThrow('index key differs');
  });

  it('rejects extra tags and dropped tags instead of silently narrowing accounting', () => {
    expect(() => validateInteractionCoverage(json(opted, retained, docs), {}, source())).toThrow('tag has no manifest entry');
    expect(() => validateInteractionCoverage(json({ ...opted, tags: ['test', 'play-fn', 'autodocs'] }, retained, docs), manifest, source())).toThrow('missing its interaction-once tag');
    expect(() => validateInteractionCoverage(json({ ...opted, id: 'constructor' }, retained, docs), {}, source())).toThrow('tag has no manifest entry');
  });

  it('rejects stale manifest keys and an opted case without its full play', () => {
    expect(() => validateInteractionCoverage(json(retained, docs), manifest, source())).toThrow('stale manifest ID');
    expect(() => validateInteractionCoverage(json({ ...opted, tags: ['test', 'interaction-once'] }, retained, docs), manifest, source())).toThrow('retain its play function');
  });

  it('requires a meaningful reason and at least one unique retained story reference', () => {
    expect(() => validateInteractionCoverage(json(opted, retained, docs), { [opted.id]: { reason: '  ', retained: [retained.id] } }, source())).toThrow();
    expect(() => validateInteractionCoverage(json(opted, retained, docs), { [opted.id]: { reason: 'same appearance', retained: [] } }, source())).toThrow();
    expect(() => validateInteractionCoverage(json(opted, retained, docs), { [opted.id]: { reason: 'same appearance', retained: [retained.id, retained.id] } }, source())).toThrow('duplicate retained');
  });

  it('rejects nonexistent retained views and same-module Docs substitutions', () => {
    for (const id of ['ui-button--missing', docs.id]) expect(() => validateInteractionCoverage(json(opted, retained, docs), { [opted.id]: { reason: 'same appearance', retained: [id] } }, source())).toThrow('must identify a built story');
    expect(() => validateInteractionCoverage(json(opted, retained, { ...docs, tags: ['test', 'interaction-once'] }), { [docs.id]: { reason: 'same appearance', retained: [retained.id] } }, source())).toThrow('stale manifest ID');
  });

  it('accounts for the installed indexer inheriting first-story tags on generated Docs', () => {
    expect(validateInteractionCoverage(json(opted, retained, { ...docs, tags: ['test', 'autodocs', 'interaction-once'] }), manifest, source())).toMatchObject({ interactionOnce: 1, docs: 1, docsWithInheritedInteractionTag: 1 });
  });

  it('rejects test opt-outs on any story, including retained and unrelated module examples', () => {
    expect(() => validateInteractionCoverage(json(opted, { ...retained, tags: ['autodocs'] }, docs), manifest, source())).toThrow('test opt-out');
    expect(() => validateInteractionCoverage(json({ ...opted, tags: [...opted.tags, '!test'] }, retained, docs), manifest, source())).toThrow('test opt-out');
    expect(() => validateInteractionCoverage(json(opted, retained, { ...retained, id: 'other--default', importPath: './src/other.stories.tsx', tags: ['!test'] }, docs), manifest, source())).toThrow('test opt-out');
  });

  it('binds each opted ID to its actual source import path and exported CSF story', () => {
    expect(() => validateInteractionCoverage(json(opted, retained, docs), manifest, [])).toThrow('source is missing');
    expect(() => validateInteractionCoverage(json({ ...opted, exportName: 'Default' }, retained, docs), manifest, source())).toThrow('does not match its source');
    expect(() => validateInteractionCoverage(json({ ...opted, title: 'Another/Button' }, retained, docs), manifest, source())).toThrow('does not match its source');
    expect(() => validateInteractionCoverage(json(opted, retained, docs), manifest, source(`interactionOnce('${retained.id}', async () => {}, async () => {})`))).toThrow('matching literal ID');
  });

  it('rejects forged static tags and helper calls imported from another owner', () => {
    expect(() => validateInteractionCoverage(json(opted, retained, docs), manifest, source('async () => {}'))).toThrow('requires one direct play');
    const sources = source();
    const first = sources[0];
    if (first === undefined) throw new Error('Missing test source');
    first.text = first.text.replace('../../.storybook/interactionCoverage.ts', './lookalike.ts');
    expect(() => validateInteractionCoverage(json(opted, retained, docs), manifest, sources)).toThrow('requires one direct play');
    const withoutStaticTag = source();
    const untagged = withoutStaticTag[0];
    if (untagged === undefined) throw new Error('Missing test source');
    untagged.text = untagged.text.replace("tags: ['interaction-once'], ", '');
    expect(() => validateInteractionCoverage(json(opted, retained, docs), manifest, withoutStaticTag)).toThrow('explicit static source tag');
  });

  it('rejects duplicate or unbound source helper calls and missing appearance callbacks', () => {
    expect(() => validateInteractionCoverage(json(opted, retained, docs), manifest, source(`interactionOnce('${opted.id}', async () => { interactionOnce('${opted.id}', async () => {}, async () => {}); }, async () => {})`))).toThrow('duplicate or unbound call');
    expect(() => validateInteractionCoverage(json(opted, retained, docs), manifest, source(`interactionOnce('${opted.id}', async () => {})`))).toThrow('full play and appearance play');
    const sources = source();
    const first = sources[0];
    if (first === undefined) throw new Error('Missing test source');
    first.text += `\ninteractionOnce('${opted.id}', async () => {}, async () => {});`;
    expect(() => validateInteractionCoverage(json(opted, retained, docs), manifest, sources)).toThrow('duplicate or unbound call');
  });

  it('rejects a later explicit play that replaces the audited interaction policy', () => {
    for (const override of ['play: async () => {}', "'play': async () => {}", "['play']: async () => {}"]) {
      const sources = source();
      const first = sources[0];
      if (first === undefined) throw new Error('Missing test source');
      first.text = first.text.replace('async () => {}) };', `async () => {}), ${override} };`);
      expect(() => validateInteractionCoverage(json(opted, retained, docs), manifest, sources)).toThrow('requires one direct play');
    }
  });

  it('rejects later spreads and dynamic computed properties that could replace the audited play', () => {
    for (const override of ['...{ play: async () => {} }', "['pl' + 'ay']: async () => {}"] ) {
      const sources = source();
      const first = sources[0];
      if (first === undefined) throw new Error('Missing test source');
      first.text = first.text.replace('async () => {}) };', `async () => {}), ${override} };`);
      expect(() => validateInteractionCoverage(json(opted, retained, docs), manifest, sources)).toThrow('may override the audited play');
    }
  });
});
