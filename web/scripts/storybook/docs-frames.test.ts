import { describe, expect, it } from 'vitest';

import { validateAppDocsFrames } from './docs-frames.ts';

const path = 'src/ui/Button.stories.tsx';
const source = (meta: string, stories: string, declarations = '') => [{ path, text: `
  import { topLayerDocs as frames } from '../../.storybook/topLayerDocs.ts';
  ${declarations}
  const meta = { title: 'Design system/Button', ${meta} };
  export default meta;
  ${stories}
` }];

describe('effective app Docs isolation', () => {
  it('inherits frames and app fixtures from metadata while merging story descriptions and height', () => {
    expect(validateAppDocsFrames(source('parameters: { ...frames, app: {} }', `
      export const Default = {};
      export const Described = { parameters: { docs: { description: { story: 'Explore' }, story: { height: '500px' } } } };
    `))).toBe(1);
  });

  it('accepts a frame on the app export without requiring unrelated siblings to be framed', () => {
    expect(validateAppDocsFrames(source('', `
      export const App = { parameters: { ...frames, app: {} } };
      export const Static = {};
    `))).toBe(1);
  });

  it('rejects an unrelated constant, comment or string as frame evidence', () => {
    for (const declaration of [
      'const unused = { ...frames };',
      '// parameters: frames',
      'const description = "parameters: frames";',
    ]) {
      expect(() => validateAppDocsFrames(source('', 'export const App = { parameters: { app: {} } };', declaration))).toThrow('#App');
    }
  });

  it('resolves local parameter aliases, shallow spreads and helpers without executing them', () => {
    expect(validateAppDocsFrames(source('parameters: inherited', `
      export const Default = { parameters: fixture() };
      export const Copied = { ...Default, parameters: { ...fixture(), docs: { ...frames.docs, story: { ...frames.docs.story, autoplay: false } } } };
    `, `
      const inherited = { ...frames };
      const fixture = () => ({ app: { responses: [] } });
    `))).toBe(1);
  });

  it('rejects later inline overrides, including quoted/computed keys and sibling defaults', () => {
    for (const story of [
      '{ parameters: { docs: { story: { inline: true } } } }',
      '{ parameters: { docs: { story: { ["inline"]: true } } } }',
      '{ parameters: { ...frames, ...inlineOverride } }',
      '{ parameters: { docs: null } }',
    ]) {
      expect(() => validateAppDocsFrames(source('parameters: { ...frames, app: {} }', `export const App = ${story};`, 'const inlineOverride = { docs: { story: { inline: true } } };'))).toThrow('#App');
    }
  });

  it('respects shallow object replacement before Storybook merges parameter levels', () => {
    expect(() => validateAppDocsFrames(source('', 'export const App = { parameters: { ...frames, docs: { description: {} }, app: {} } };'))).toThrow('#App');
    expect(validateAppDocsFrames(source('parameters: frames', 'export const App = { parameters: { docs: { description: {} }, app: {} } };'))).toBe(1);
  });

  it('does not treat a same-named local object or another import as the shared owner', () => {
    for (const text of [
      "const topLayerDocs = { docs: { story: { inline: false } } };",
      "import { topLayerDocs } from './different-owner.ts';",
    ]) {
      expect(() => validateAppDocsFrames([{ path, text: `${text} export default { title: 'Button', parameters: topLayerDocs }; export const App = { parameters: { app: {} } };` }])).toThrow('isolation');
    }
  });

  it('reads CSF2 annotation overrides and rejects unresolved isolation spreads', () => {
    expect(() => validateAppDocsFrames(source('parameters: { ...frames, app: {} }', `
      export function App() { return null; }
      App.parameters = { docs: { story: { inline: true } } };
    `))).toThrow('#App');
    expect(() => validateAppDocsFrames(source('parameters: frames', 'export const App = { parameters: { app: {}, ...unknownParameters } };'))).toThrow('cannot statically verify');
  });

  it('allows modules and stories without effective app fixtures', () => {
    expect(validateAppDocsFrames(source('', 'export const Static = { parameters: { docs: { description: {} } } };'))).toBe(0);
    for (const value of ['false', 'null', 'undefined']) {
      expect(() => validateAppDocsFrames(source('parameters: { app: {} }', `export const Static = { parameters: { app: ${value} } };`))).toThrow('app fixture parameters must be an object');
    }
  });

  it('never resolves shadowed helper parameters as module-level frame imports', () => {
    expect(() => validateAppDocsFrames(source('', `
      export const App = { parameters: fixture({ docs: { story: { inline: true } } }) };
    `, 'const fixture = (frames) => ({ ...frames, app: {} });'))).toThrow('local parameter');
  });

  it('fails closed for mutable aliases, top-level mutations and asynchronous helpers', () => {
    for (const declaration of [
      'let isolation = { ...frames, app: {} };',
      'const isolation = { ...frames, app: {} }; isolation.docs.story.inline = true;',
      'const isolation = { ...frames, app: {} }; const changed = (isolation.docs.story.inline = true);',
      'const isolation = { ...frames, app: {} }; if (true) isolation.docs.story.inline = true;',
      'const isolation = { ...frames, app: {} }; const changed = isolation.docs.story.height++;',
      'const isolation = { ...frames, app: {} }; mutate(isolation);',
      'const fixture = async () => ({ ...frames, app: {} }); const isolation = fixture();',
      'const isolation = { ...isolation, app: {} };',
    ]) {
      expect(() => validateAppDocsFrames(source('', 'export const App = { parameters: isolation };', declaration))).toThrow('isolation');
    }
  });
});
