import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// llms.txt advertises a Markdown twin (`<url>.md`) for every page and a single
// /llms-full.txt with all of them. Keep the index, the twins, and the full file
// in step: every indexed page must have a non-empty twin that llms-full.txt
// carries whole, and llms-full.txt must carry no page the index omits.
const dist = resolve(fileURLToPath(new URL('..', import.meta.url)), 'dist');
const index = await readFile(resolve(dist, 'llms.txt'), 'utf8');
const full = await readFile(resolve(dist, 'llms-full.txt'), 'utf8');

// Page entries only: `- [Title](url)` or `- [Title](url): description`, with
// Fumadocs' `\[` `\]` escapes in titles and its `\(` `\)` escapes in URLs.
const links = [...index.matchAll(/^\s*- \[(?:\\.|[^\]\\])*\]\((\S+?)\)(?::|$)/gm)].map((match) => match[1].replace(/\\([()])/g, '$1'));
for (const link of links) {
  assert.ok(link.startsWith('https://hikyo.app/'), `llms.txt link is not absolute: ${link}`);
}
const urls = links.map((link) => new URL(link).pathname);
assert.ok(urls.length > 50, `llms.txt lists only ${urls.length} pages`);
// The reverse direction: llms-full.txt must not carry pages the index omits.
const fullUrls = [...full.matchAll(/^# .+ \((\/[^\r\n]*)\)$/gm)].map((match) => match[1]);
assert.deepEqual([...new Set(fullUrls)].sort(), [...new Set(urls)].sort(), 'llms.txt and llms-full.txt list different pages');

for (const url of urls) {
  const twin = await readFile(resolve(dist, `.${url}.md`), 'utf8');
  const heading = twin.split('\n', 1)[0];
  assert.match(heading, new RegExp(`^# .+ \\(${url.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\)$`), `${url}.md heading`);
  assert.ok(twin.trim().split('\n').length > 1, `${url}.md has no body`);
  assert.ok(full.includes(twin.trim()), `llms-full.txt misses the complete ${url}.md`);
}

console.log(`llms gate: ${urls.length} pages indexed, each with a Markdown twin in llms-full.txt`);
