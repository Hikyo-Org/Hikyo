import { type CollectionEntry, getCollection } from 'astro:content';
import { structure, type StructuredData } from 'fumadocs-core/mdx-plugins';
import { llms, loader, type StaticSource } from 'fumadocs-core/source';
import path from 'node:path';
import { fumadocsBase } from './site';

const contentRoot = 'src/content/docs';

export const source = loader({
  source: await createSource(),
  baseUrl: fumadocsBase,
});

// `_markdown` is exported by remarkLLMs (astro.config.mjs): the page body with
// imports dropped and MDX components kept as JSX. Keys are made
// relative to the site root so they match a collection entry's `filePath`.
const markdownByFile = new Map(
  Object.entries(
    import.meta.glob<string>('../content/docs/**/*.mdx', { eager: true, import: '_markdown' }),
  ).map(([file, markdown]) => [file.replace(/^\.\.\//, 'src/'), markdown]),
);

export const docsLlms = llms(source, {
  renderPage(page) {
    const file = requireFilePath(page.data._raw);
    // Plain .md pages (the generated license) have no MDX to strip.
    const markdown = file.endsWith('.md') ? requireBody(page.data._raw) : markdownByFile.get(file);
    if (markdown === undefined) {
      throw new Error(`documentation page ${file} has no _markdown export`);
    }
    const description = page.data.description ? `\n> ${page.data.description}\n` : '';
    return `# ${page.data.title} (${page.url})\n${description}\n${markdown.trim()}\n`;
  },
});

function requireFilePath(entry: CollectionEntry<'docs'> | CollectionEntry<'meta'>): string {
  if (entry.filePath === undefined) {
    throw new Error(`documentation entry ${entry.id} has no source file path`);
  }
  return entry.filePath;
}

function requireBody(entry: CollectionEntry<'docs'>): string {
  if (entry.body === undefined) {
    throw new Error(`documentation page ${entry.id} has no Markdown body`);
  }
  return entry.body;
}

async function createSource() {
  const result: StaticSource<{
    metaData: CollectionEntry<'meta'>['data'];
    pageData: CollectionEntry<'docs'>['data'] & {
      _raw: CollectionEntry<'docs'>;
      structuredData: StructuredData;
    };
  }> = { files: [] };

  for (const page of await getCollection('docs')) {
    result.files.push({
      type: 'page',
      path: path.relative(contentRoot, requireFilePath(page)),
      data: {
        ...page.data,
        _raw: page,
        structuredData: structure(requireBody(page)),
      },
    });
  }

  for (const meta of await getCollection('meta')) {
    result.files.push({
      type: 'meta',
      path: path.relative(contentRoot, requireFilePath(meta)),
      data: meta.data,
    });
  }

  return result;
}
