import { posix } from 'node:path';
import { babelParse, types } from 'storybook/internal/babel';
import { loadCsf } from 'storybook/internal/csf-tools';
import { z } from 'zod';

const entrySchema = z.object({
  id: z.string().regex(/\S/),
  title: z.string().regex(/\S/),
  name: z.string().regex(/\S/),
  importPath: z.string().regex(/\S/),
  type: z.enum(['story', 'docs']),
  tags: z.array(z.string().regex(/\S/)),
  exportName: z.string().regex(/\S/).optional(),
});
const indexSchema = z.object({ v: z.literal(5), entries: z.record(z.string().min(1), entrySchema) });
const manifestSchema = z.record(z.string().regex(/\S/), z.object({
  reason: z.string().trim().min(1),
  retained: z.array(z.string().regex(/\S/)).min(1),
}));

export type InteractionCoverageManifest = Readonly<Record<string, {
  readonly reason: string;
  readonly retained: readonly string[];
}>>;

function sourcePath(path: string): string {
  return posix.normalize(path.replaceAll('\\', '/'));
}

function helperCalls(node: types.Node, names: ReadonlySet<string>) {
  const calls: types.CallExpression[] = [];
  types.traverseFast(node, (child) => {
    if (types.isCallExpression(child) && types.isIdentifier(child.callee) && names.has(child.callee.name)) calls.push(child);
  });
  return calls;
}

/** Account for every built entry; this is never a filtered test-suite index. */
export function validateInteractionCoverage(
  json: string,
  manifest: InteractionCoverageManifest,
  sources: readonly { path: string; text: string }[],
) {
  const index = indexSchema.parse(JSON.parse(json));
  const optIns = manifestSchema.parse(manifest);
  const entries = Object.entries(index.entries);
  if (entries.length === 0) throw new Error('Storybook interaction index is empty');
  const failures: string[] = [];
  const stories = entries.filter(([, value]) => value.type === 'story');
  const tagged = new Set<string>();
  const manifestIds = new Set(Object.keys(optIns));
  for (const [key, value] of entries) {
    if (key !== value.id) failures.push(`${key}: index key differs from entry id ${value.id}`);
    if (value.tags.includes('!test') || (value.type === 'story' && !value.tags.includes('test'))) failures.push(`${value.id}: test opt-out prevents full palette coverage`);
    if (!value.tags.includes('interaction-once')) continue;
    // The installed indexer copies the first export's tags to generated Docs.
    // Those Docs are not test cases and never satisfy manifest/reference IDs.
    if (value.type !== 'story') continue;
    tagged.add(value.id);
    if (!manifestIds.has(value.id)) failures.push(`${value.id}: interaction-once tag has no manifest entry`);
    if (!value.tags.includes('play-fn')) failures.push(`${value.id}: interaction-once must retain its play function`);
  }
  for (const [id, optIn] of Object.entries(optIns)) {
    const value = index.entries[id];
    if (value?.type !== 'story') failures.push(`${id}: stale manifest ID does not identify a built story`);
    if (!tagged.has(id)) failures.push(`${id}: manifest entry is missing its interaction-once tag`);
    if (new Set(optIn.retained).size !== optIn.retained.length) failures.push(`${id}: duplicate retained story reference`);
    for (const retained of optIn.retained) {
      if (index.entries[retained]?.type !== 'story') failures.push(`${id}: retained ${retained} must identify a built story, not Docs or an absent entry`);
    }
  }

  // Installed CSF parsing supplies IDs/export names; no custom title/ID regex.
  // The helper's semantic equivalence and readiness remain source/browser audit.
  const sourceEntries = new Map<string, z.infer<typeof entrySchema>[]>();
  for (const [, value] of stories) {
    if (!tagged.has(value.id)) continue;
    const path = sourcePath(value.importPath);
    sourceEntries.set(path, [...(sourceEntries.get(path) ?? []), value]);
  }
  const suppliedSources = new Set(sources.map(({ path }) => sourcePath(path)));
  for (const path of sourceEntries.keys()) if (!suppliedSources.has(path)) failures.push(`${path}: opted story source is missing`);
  for (const source of sources) {
    const path = sourcePath(source.path);
    const optedStories = sourceEntries.get(path) ?? [];
    if (optedStories.length === 0 && !source.text.includes('interactionOnce')) continue;
    const ast = babelParse(source.text);
    const helperNames = new Set<string>();
    for (const node of ast.program.body) {
      if (!types.isImportDeclaration(node)) continue;
      const importedPath = sourcePath(posix.join(posix.dirname(path), node.source.value));
      if (importedPath !== '.storybook/interactionCoverage.ts') continue;
      for (const specifier of node.specifiers) {
        if (types.isImportSpecifier(specifier) && types.isIdentifier(specifier.imported, { name: 'interactionOnce' })) helperNames.add(specifier.local.name);
      }
    }
    const allCalls = helperCalls(ast, helperNames);
    if (allCalls.length !== optedStories.length) failures.push(`${path}: helper call count ${allCalls.length} differs from ${optedStories.length} tagged exports (duplicate or unbound call)`);
    if (optedStories.length === 0) continue;
    const csf = loadCsf(source.text, { fileName: path, makeTitle: (title) => title }).parse();
    for (const value of optedStories) {
      const input = csf.indexInputs.find((item) => item.exportName === value.exportName);
      if (value.exportName === undefined || input?.__id !== value.id || input.title !== value.title || input.name !== value.name) {
        failures.push(`${value.id}: built ID/export/title/name does not match its source module`);
        continue;
      }
      if (!input.tags?.includes('interaction-once')) failures.push(`${value.id}: shared policy requires an explicit static source tag`);
      const exported = csf.getStoryExport(value.exportName);
      const calls = helperCalls(exported, helperNames);
      const properties = types.isObjectExpression(exported) ? exported.properties : [];
      const playProperties = properties.filter((property) => (types.isObjectProperty(property) || types.isObjectMethod(property)) && (types.isIdentifier(property.key, { name: 'play' }) || types.isStringLiteral(property.key, { value: 'play' })));
      const play = playProperties[0];
      if (playProperties.length !== 1 || calls.length !== 1 || !types.isObjectProperty(play) || !types.isCallExpression(play.value) || play.value !== calls[0]) {
        failures.push(`${value.id}: requires one direct play: interactionOnce(...) call from the shared helper`);
        continue;
      }
      if (properties.slice(properties.indexOf(play) + 1).some((property) => types.isSpreadElement(property) || property.computed)) failures.push(`${value.id}: a trailing spread or computed property may override the audited play`);
      const [declaredId] = play.value.arguments;
      if (!types.isStringLiteral(declaredId) || declaredId.value !== value.id || play.value.arguments.length !== 3) failures.push(`${value.id}: helper must declare the matching literal ID, full play and appearance play`);
    }
  }
  if (failures.length > 0) throw new Error(`Storybook interaction accounting failed:\n${failures.join('\n')}`);
  const fullPlays = stories.filter(([, value]) => value.tags.includes('play-fn')).length;
  return {
    stories: stories.length,
    docs: entries.length - stories.length,
    docsWithInheritedInteractionTag: entries.filter(([, value]) => value.type === 'docs' && value.tags.includes('interaction-once')).length,
    interactionOnce: tagged.size,
    fullPlays,
    lightFullPlays: fullPlays - tagged.size,
    lightAppearancePlays: tagged.size,
    storiesWithoutPlay: stories.length - fullPlays,
    cases: Object.entries(optIns).map(([id, value]) => ({ id, source: index.entries[id]?.importPath, exportName: index.entries[id]?.exportName, ...value })),
    evidenceLimit: 'Source binding checks named shared-helper imports, literal IDs, static tags and one direct play call without later spread/computed overrides. Generated Docs may inherit the first export tag and do not count as interaction cases. Appearance equivalence, readiness and actual project execution require the documented source audit and browser checks.',
  };
}
