import type { APIRoute, GetStaticPaths } from 'astro';
import { docsLlms, source } from '../lib/source';

export const prerender = true;

// Markdown twin of every documentation page: /docs/getting-started/ is also
// served as /docs/getting-started.md, the convention llms.txt consumers probe.
export const getStaticPaths = (() =>
  source.getPages().map((page) => ({
    params: { slug: page.slugs.join('/') },
    props: { page },
  }))) satisfies GetStaticPaths;

export const GET: APIRoute<{ page: ReturnType<typeof source.getPages>[number] }> = async ({ props }) =>
  new Response(await docsLlms.page(props.page), {
    headers: { 'Content-Type': 'text/markdown; charset=utf-8' },
  });
