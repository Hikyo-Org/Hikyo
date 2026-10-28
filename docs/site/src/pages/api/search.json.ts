import { createFromSource } from 'fumadocs-core/search/server';
import type { APIRoute } from 'astro';
import { source } from '../../lib/source';
import { docsSearchOptions } from '../../lib/search-options';

export const prerender = true;

const { staticGET } = createFromSource(source, docsSearchOptions);

export const GET: APIRoute = () => staticGET();
