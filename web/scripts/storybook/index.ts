import { posix } from 'node:path';
import { z } from 'zod';

/** Independent recursive source inventory also catches discovery narrowed in main.ts. */
export const STORY_SOURCE_GLOB = 'src/**/*.stories.{js,jsx,mjs,ts,tsx}';

/** Source convention paired with withApp's runtime refusal of inline Docs. */
export function validateAppDocsFrames(modules: readonly { path: string; text: string }[]): number {
  const failures: string[] = [];
  let framed = 0;
  for (const { path, text } of modules) {
    if (!/\bapp\s*:/.test(text)) continue;
    if (!/(?:\.\.\.|parameters\s*:\s*)topLayerDocs\b/.test(text)) failures.push(`${path}: app fixtures require the shared topLayerDocs frame parameters`);
    if (/\binline\s*:\s*true\b/.test(text)) failures.push(`${path}: app fixtures must not override Docs frames with inline: true`);
    framed++;
  }
  if (failures.length > 0) throw new Error(`Storybook Docs isolation failed:\n${failures.join('\n')}`);
  return framed;
}

const entry = z.object({
  id: z.string().regex(/\S/),
  title: z.string().regex(/\S/),
  name: z.string().regex(/\S/),
  importPath: z.string().regex(/\S/),
  type: z.enum(['story', 'docs']),
  tags: z.array(z.string()),
});

const indexSchema = z.object({
  v: z.literal(5),
  entries: z.record(z.string().min(1), entry),
});

function sourcePath(path: string): string {
  return posix.normalize(path.replaceAll('\\', '/'));
}

/** Validate the complete built index, never a filtered browser suite or title match. */
export function validateStorybookIndex(json: string, sourceModules: readonly string[]) {
  const index = indexSchema.parse(JSON.parse(json));
  const entries = Object.entries(index.entries);
  if (sourceModules.length === 0) throw new Error('No source story modules found');
  if (entries.length === 0) throw new Error('Storybook index is empty');

  const sources = new Set(sourceModules.map(sourcePath));
  const failures: string[] = [];
  const modules = new Map<string, z.infer<typeof entry>[]>();
  for (const [key, value] of entries) {
    if (key !== value.id) failures.push(`${key}: index key differs from entry id ${value.id}`);
    const path = sourcePath(value.importPath);
    if (!sources.has(path)) failures.push(`${value.id}: importPath ${value.importPath} is not a source story module`);
    const existing = modules.get(path) ?? [];
    existing.push(value);
    modules.set(path, existing);
  }

  let stories = 0;
  let docs = 0;
  for (const path of [...sources].sort()) {
    const moduleEntries = modules.get(path) ?? [];
    const moduleStories = moduleEntries.filter((value) => value.type === 'story');
    const moduleDocs = moduleEntries.filter((value) => value.type === 'docs');
    if (moduleStories.length === 0) failures.push(`${path}: no stories in built index (empty module or missed discovery)`);
    if (moduleDocs.length !== 1) failures.push(`${path}: expected one generated Docs entry, found ${moduleDocs.length}`);
    for (const doc of moduleDocs) {
      if (!doc.tags.includes('autodocs')) failures.push(`${path}: Docs is not generated autodocs`);
      if (moduleStories.some((story) => story.title !== doc.title)) failures.push(`${path}: Docs title differs from its stories`);
    }
    stories += moduleStories.length;
    docs += moduleDocs.length;
  }
  if (failures.length > 0) throw new Error(`Storybook coverage failed:\n${failures.join('\n')}`);
  return { modules: sources.size, stories, docs };
}
