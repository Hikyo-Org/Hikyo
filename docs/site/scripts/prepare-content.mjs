import { mkdir, readFile, rm, writeFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const siteRoot = resolve(scriptDirectory, '..');
const repositoryRoot = resolve(siteRoot, '../..');
const docsRoot = resolve(siteRoot, 'src/content/docs');

// Keep the first-use upgrade entrypoint identical to the reviewed source.
await mkdir(resolve(siteRoot, 'public'), { recursive: true });
await writeFile(
  resolve(siteRoot, 'public/upgrade-nightly.sh'),
  await readFile(resolve(repositoryRoot, 'install/upgrade-nightly.sh')),
);

// Targets are .mdx, not .md: Astro only applies the Fumadocs `components`
// override (the overflow-scrolling table wrapper and the anchor-linked headings)
// when the page is MDX. A plain .md page renders bare `<table>`/`<h2 id>`, which
// is what broke horizontal table scrolling and copyable heading links on these
// generated policy pages.
const pages = [
  { source: 'SECURITY.md', target: 'security.mdx', title: 'Security policy' },
  { source: 'SUPPORT.md', target: 'support.mdx', title: 'Support policy' },
  { source: 'GOVERNANCE.md', target: 'governance.mdx', title: 'Governance' },
  {
    source: 'docs/status/README.md',
    target: 'implementation-status.mdx',
    title: 'Implementation status',
  },
  { source: 'TRADEMARK.md', target: 'trademark.mdx', title: 'Trademark policy' },
  { source: 'CONTRIBUTING.md', target: 'contributing.mdx', title: 'Contributing' },
  {
    source: 'docs/release/signing.md',
    target: 'release/signing.mdx',
    title: 'Release signing ceremony',
  },
  {
    source: 'docs/release/online-signing.md',
    target: 'release/online-signing.mdx',
    title: 'Online stable release guide',
  },
  {
    source: 'docs/release/legacy-offline-signing.md',
    target: 'release/legacy-offline-signing.mdx',
    title: 'Legacy offline release reference',
  },
];

const siteLinks = new Map([
  ['online-signing.md', '/release/online-signing/'],
  ['online-signing.md#one-time-setup', '/release/online-signing/#one-time-setup'],
  ['online-signing.md#custody-and-recovery', '/release/online-signing/#custody-and-recovery'],
  ['legacy-offline-signing.md', '/release/legacy-offline-signing/'],
  ['../adr/stable-workflow-signing.md', 'https://github.com/Hikyo-Org/Hikyo/blob/main/docs/adr/stable-workflow-signing.md'],
  ['../research/release-signing-ceremonies.md', 'https://github.com/Hikyo-Org/Hikyo/blob/main/docs/research/release-signing-ceremonies.md'],
  ['./CONTRIBUTING.md', '/contributing/'],
  ['./.agents/skills/hikyo-contributor-bootstrap/SKILL.md', 'https://github.com/Hikyo-Org/Hikyo/blob/main/.agents/skills/hikyo-contributor-bootstrap/SKILL.md'],
  ['./GOVERNANCE.md', '/governance/'],
  ['./GOVERNANCE.md#amendment-procedure', '/governance/#amendment-procedure'],
  ['./docs/status/README.md', '/implementation-status/'],
  ['./docs/status/README.md#obl-repository-transfer', '/implementation-status/#obl-repository-transfer'],
  ['./docs/adr/README.md', 'https://github.com/Hikyo-Org/Hikyo/blob/main/docs/adr/README.md'],
  ['./docs/spec/README.md', 'https://github.com/Hikyo-Org/Hikyo/blob/main/docs/spec/README.md'],
  ['./docs/adr/oss-mechanics.md', 'https://github.com/Hikyo-Org/Hikyo/blob/main/docs/adr/oss-mechanics.md'],
  [
    './docs/handoff/github-org-transfer.md',
    'https://github.com/Hikyo-Org/Hikyo/blob/main/docs/handoff/github-org-transfer.md',
  ],
  ['./SECURITY.md', '/security/'],
  ['./SUPPORT.md', '/support/'],
  ['./TRADEMARK.md', '/trademark/'],
]);

for (const page of pages) {
  await rm(resolve(docsRoot, page.target), { force: true });
  // Remove the pre-.mdx artifact too so a stale local .md never collides with
  // the .mdx page for the same slug.
  await rm(resolve(docsRoot, page.target.replace(/\.mdx$/, '.md')), { force: true });
}
await rm(resolve(docsRoot, 'policies'), { recursive: true, force: true });
await rm(resolve(docsRoot, 'license.md'), { force: true });
await mkdir(resolve(docsRoot, 'release'), { recursive: true });

for (const page of pages) {
  const source = await readFile(resolve(repositoryRoot, page.source), 'utf8');
  let body = source.replace(/^# .+\n+/, '');
  for (const [repositoryLink, siteLink] of siteLinks) {
    body = body.replaceAll(`(${repositoryLink})`, `(${siteLink})`);
  }
  // MDX parses HTML comments as JSX and chokes on them; the generated status
  // page carries a "do not edit" note that is only meaningful on the repo source.
  // Strip to a fixpoint: a single pass can leave a fresh `<!-- -->` behind when
  // comment markers overlap, which CodeQL flags as incomplete multi-character
  // sanitization.
  let beforeStrip;
  do {
    beforeStrip = body;
    body = body.replace(/<!--[\s\S]*?-->\n?/g, '');
  } while (body !== beforeStrip);
  // A leftover `<!--` is an unterminated/malformed comment the loop cannot
  // remove; fail loud rather than ship MDX the build would choke on later.
  if (body.includes('<!--')) {
    throw new Error(`${page.source}: unterminated HTML comment (<!--) after stripping`);
  }
  // Fail the build if any repo-relative Markdown link survived the rewrite above.
  // siteLinks is hand-maintained; without this guard a new relative link ships
  // as a 404 on the site (as ./docs/adr/oss-mechanics.md once did). Flag every
  // inline-link destination that is not absolute: this catches bare-relative
  // links like `online-signing.md` too, not only `./`/`../` forms.
  const unrewritten = [...body.matchAll(/\]\(([^)]+)\)/g)]
    .map((match) => match[1])
    .filter((destination) => !/^(?:https?:|mailto:|#|\/)/.test(destination));
  if (unrewritten.length > 0) {
    throw new Error(
      `${page.source}: relative links have no site mapping in prepare-content.mjs: ${unrewritten.join(', ')}`,
    );
  }
  const destination = resolve(docsRoot, page.target);
  await mkdir(dirname(destination), { recursive: true });
  await writeFile(
    destination,
    `---\ntitle: ${page.title}\neditUrl: https://github.com/Hikyo-Org/Hikyo/edit/main/${page.source}\n---\n\n${body}`,
  );
}

const license = await readFile(resolve(repositoryRoot, 'LICENSE'), 'utf8');
await writeFile(
  resolve(docsRoot, 'license.md'),
  `---\ntitle: Mozilla Public License 2.0\neditUrl: https://github.com/Hikyo-Org/Hikyo/edit/main/LICENSE\n---\n\n\`\`\`text\n${license}\`\`\`\n`,
);
