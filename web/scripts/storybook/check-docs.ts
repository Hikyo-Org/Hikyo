import { glob, readFile } from 'node:fs/promises';
import { resolve } from 'node:path';

import { INTERACTION_ONCE } from '../../.storybook/interactionCoverage.ts';
import { STORY_SOURCE_GLOB, validateAppDocsFrames, validateStorybookIndex } from './index.ts';
import { validateInteractionCoverage } from './interaction-coverage.ts';

const web = resolve(import.meta.dirname, '../..');
const build = resolve(web, process.argv[2] ?? 'storybook-static');
const modules: string[] = [];
for await (const path of glob(STORY_SOURCE_GLOB, { cwd: web })) modules.push(path);
const sources = await Promise.all(modules.map(async (path) => ({ path, text: await readFile(resolve(web, path), 'utf8') })));
const framed = validateAppDocsFrames(sources);
const json = await readFile(resolve(build, 'index.json'), 'utf8');
const result = validateStorybookIndex(json, modules);
const interactions = validateInteractionCoverage(json, INTERACTION_ONCE, sources);
console.log(`storybook-docs: ${result.modules} modules, ${result.stories} stories, ${result.docs} generated Docs; ${framed} app modules require isolated frames`);
console.log(JSON.stringify({ type: 'storybook-interaction-coverage', ...interactions }));
