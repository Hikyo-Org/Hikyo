import type { APIRoute } from 'astro';
import { docsLlms, source } from '../lib/source';

export const prerender = true;

// https://llmstxt.org/ index, rendered by Fumadocs from the sidebar's page tree
// so it cannot drift from navigation. The summary lives here rather than as a
// root meta.json description, which would be serialized into every page's
// hydration props.
const summary =
  'Fully open-source, self-hosted control plane for secrets and configuration across development, staging, and production. Every value is explicitly set or absent in every environment: no inheritance, no hidden fallback, no paid operational core. Source: https://github.com/Hikyo-Org/Hikyo';

export const GET: APIRoute = async ({ site }) => {
  if (site === undefined) {
    throw new Error('llms.txt needs `site` in astro.config.mjs for absolute links');
  }
  // indexNode() emits root-relative links; llmstxt.org consumers may read the
  // file away from hikyo.app, so anchor them to the site origin.
  const origin = site.origin;
  const body = [
    `# ${source.pageTree.name}`,
    '',
    `> ${summary}`,
    '',
    `Every page is also available as Markdown at its URL plus \`.md\` (for example ${origin}/docs/getting-started.md), and all pages concatenated at ${origin}/llms-full.txt.`,
    '',
    ...(await Promise.all(source.pageTree.children.map((node) => docsLlms.indexNode(node)))),
  ]
    .join('\n')
    .replaceAll('](/', `](${origin}/`);
  return new Response(body, { headers: { 'Content-Type': 'text/plain; charset=utf-8' } });
};
