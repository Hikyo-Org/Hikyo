import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { test } from 'node:test';

const checker = await readFile(new URL('./prepare-content.mjs', import.meta.url));
const inputs = [
  'SECURITY.md', 'SUPPORT.md', 'GOVERNANCE.md', 'TRADEMARK.md', 'CONTRIBUTING.md',
  'docs/status/README.md', 'docs/release/signing.md', 'docs/release/online-signing.md',
  'docs/release/legacy-offline-signing.md', 'LICENSE', 'install/upgrade-nightly.sh',
];
async function put(root, path, content) {
  await mkdir(dirname(join(root, path)), { recursive: true });
  await writeFile(join(root, path), content);
}
async function snapshot(root, prefix = '') {
  const files = {};
  for (const entry of await readdir(join(root, prefix), { withFileTypes: true })) {
    const path = join(prefix, entry.name);
    if (entry.isDirectory()) Object.assign(files, await snapshot(root, path));
    else files[path] = (await readFile(join(root, path))).toString('base64');
  }
  return files;
}

test('external source check catches bad links without creating, removing, or overwriting files', async (t) => {
  const work = await mkdtemp(join(tmpdir(), 'hikyo-docs-prepare-'));
  t.after(() => rm(work, { recursive: true, force: true }));
  const tooling = join(work, 'tooling');
  const source = join(work, 'source');
  for (const root of [tooling, source]) {
    for (const input of inputs) await put(root, input, '# Fixture\n\nContent\n');
    for (const path of ['docs/site/public/upgrade-nightly.sh', 'docs/site/src/content/docs/contributing.mdx', 'docs/site/src/content/docs/contributing.md', 'docs/site/src/content/docs/license.md', 'docs/site/src/content/docs/policies/keep.md']) {
      await put(root, path, 'preserve this existing output\n');
    }
  }
  const script = join(tooling, 'docs/site/scripts/prepare-content.mjs');
  await put(tooling, 'docs/site/scripts/prepare-content.mjs', checker);
  // An old source script cannot understand --check. It must never be executed.
  await put(source, 'docs/site/scripts/prepare-content.mjs', 'throw new Error("source generator executed");\n');
  await put(source, 'CONTRIBUTING.md', '# Contributing\n\n[Feedback](scripts/dev/README.md)\n');
  const run = (...args) => spawnSync(process.execPath, [script, ...args], { encoding: 'utf8' });
  const unchanged = async (...args) => {
    const before = await snapshot(work);
    const result = run(...args);
    assert.deepEqual(await snapshot(work), before, 'read-only invocation must preserve both complete trees');
    return result;
  };
  assert.equal((await unchanged('--check', '--source-root', source)).status, 0);
  await put(source, 'CONTRIBUTING.md', '# Contributing\n\n[Broken](missing.md)\n');
  const broken = await unchanged('--check', '--source-root', source);
  assert.equal(broken.status, 1);
  assert.match(broken.stderr, /CONTRIBUTING.md: relative links have no site mapping.*missing.md/);
  await put(source, 'CONTRIBUTING.md', '# Contributing\n\n<!-- unterminated\n');
  assert.match((await unchanged('--check', '--source-root', source)).stderr, /unterminated HTML comment/);
  await put(source, 'CONTRIBUTING.md', '# Contributing\n\nContent\n');
  await rm(join(source, 'LICENSE'));
  const missing = await unchanged('--check', '--source-root', source);
  assert.equal(missing.status, 1, 'missing consumed inputs fail');
  assert.match(missing.stderr, /ENOENT.*LICENSE/);
  for (const args of [['--source-root', source], ['--check', '--source-root'], ['--check', '--unknown'], ['--check', '--check']]) {
    assert.equal((await unchanged(...args)).status, 1, 'unsupported arguments must fail before generation');
  }
  // Existing default preparation still generates from its own repository.
  await put(tooling, 'CONTRIBUTING.md', '# Contributing\n\n[Feedback](scripts/dev/README.md)\n');
  assert.equal(run().status, 0);
  assert.match(await readFile(join(tooling, 'docs/site/src/content/docs/contributing.mdx'), 'utf8'), /https:\/\/github.com\/Hikyo-Org\/Hikyo\/blob\/main\/scripts\/dev\/README.md/);
  assert.equal(await readFile(join(tooling, 'docs/site/public/upgrade-nightly.sh'), 'utf8'), '# Fixture\n\nContent\n');
});
