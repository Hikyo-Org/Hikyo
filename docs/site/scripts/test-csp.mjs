import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { readFile, glob } from 'node:fs/promises';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// hikyo.app is a static GitHub Pages deploy, so the Content-Security-Policy
// ships as a build-time <meta http-equiv>. This gate keeps that policy present
// and strict across *every* generated page.
//
// Threat model: the attacker this gate defends against is our own build emitting
// something unexpected (a new unhashed inline script, a dropped directive, a
// widened source) that silently disables CSP. It is NOT a defence against someone
// who already controls the dist HTML: such an attacker just deletes the meta. So
// this parses with a scoped tokenizer, not a full spec parser -- a spec parser
// buys nothing against a threat model that starts with "attacker owns the file".
//
// The dist splits into two page classes:
//   - 47 Astro-generated pages: each MUST carry exactly one CSP meta. script-src
//     is hash-strict (every inline script after the meta must be hashed into it;
//     the two inline scripts before the meta are a fixed, pinned allowlist).
//   - static public/prototypes/** pages: Astro does not process public/, so these
//     carry NO meta and are ungoverned (pre-existing design prototypes, static
//     HTML with hardcoded demo data, no injection sink). The gate asserts this
//     partition so a new public/ page, or a lost meta on an Astro page, trips it.
// style-src is not hash-strict (Radix injects <style> elements at runtime and
// React renders inline style="" from SSR; see astro.config.mjs), so styles get a
// fixed-posture assertion instead of hashing.
// --demo DIST checks the standalone demo or its copied Pages directory. It has
// no prototype exceptions or bootstrap allowlist, and keeps scripts/styles
// self-only while refusing connections, base tags, and form submissions.

const args = process.argv.slice(2);
const demo = args[0] === '--demo';
assert.ok(args.length === 0 || (demo && args.length === 2), 'usage: test-csp.mjs [--demo DIST]');
const dist = demo ? resolve(args[1]) : resolve(fileURLToPath(new URL('../dist/', import.meta.url)));

const sha256 = (body) => `sha256-${createHash('sha256').update(body, 'utf8').digest('base64')}`;
const quote = (hash) => `'${hash}'`;

// The two inline scripts Astro emits before the meta on every governed page. They
// are is:inline in the head layout, so they run before the browser has a policy
// and Astro does not hash them. Pinned here as an exact set: a new pre-meta script
// (which would be ungoverned by CSP) fails the gate and forces a conscious review.
//   - theme bootstrap: reads localStorage 'hikyo-theme' and sets the html class
//     before paint to avoid a flash (src/layouts theme snippet).
//   - service-worker registration: navigator.serviceWorker.register on load.
const PRE_META_ALLOWED = new Set([
  'sha256-pMR9YEhiENpsrW9UxovrRXAjfCQoraSsZnNYMdwyKYw=', // theme bootstrap
  'sha256-/DGFFild64ARl/fhqgEOyfyzvghXjHChJV19q1MJWUM=', // service-worker registration
]);

// HTML comments carry no active markup: a <script> or CSP <meta> written inside
// <!-- --> does not run or apply. Strip them first so a commented-out CSP meta
// cannot make a policy-free page look governed, and a commented script is not
// mistaken for a live one. (A live script body containing "<!--" would be
// mangled here and fail its hash match -- fail-closed, which trips the gate
// rather than passing it.)
//
// Applied to a fixpoint: a single pass can splice text that reforms a new
// "<!--" (e.g. "<!--<!-- -->-->" leaves "<!--" + "-->"), so repeat until the
// string stops changing and no comment sequence survives.
const stripComments = (html) => {
  let prev;
  do {
    prev = html;
    html = html.replace(/<!--[\s\S]*?-->/g, '');
  } while (html !== prev);
  return html;
};

// Parse an open-tag's attributes into a map. Browsers keep the FIRST occurrence
// of a duplicated attribute and ignore the rest, so first-wins here too:
// <script type="text/javascript" type="application/json"> is a live JS script.
// The value matcher is quote-aware at the call sites (the tag body is captured
// without splitting on a ">" that sits inside a quoted value).
const attrPattern = /([^\s=/>]+)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+)))?/g;
const parseAttrs = (source) => {
  const attrs = {};
  for (const m of source.matchAll(attrPattern)) {
    const name = m[1].toLowerCase();
    if (!(name in attrs)) attrs[name] = m[2] ?? m[3] ?? m[4] ?? '';
  }
  return attrs;
};

// Fail-closed classifier: an inline script is GOVERNED (its hash must be in
// script-src) unless it is provably inert. Provably inert means either it has a
// real `src` (the body is ignored) or its type is an exact, known-inert token.
// Anything else -- classic JS, empty type, module, importmap, speculationrules,
// an unknown type, or a type carrying a character reference we will not decode
// (e.g. type="text/jav&#x61;script", which the browser resolves to JS) -- must
// be hashed. The inert set holds only what this build actually emits (verified:
// governed pages emit `type="module"` and one `application/ld+json`); a new
// inert type would trip the gate and force a conscious review.
const INERT_SCRIPT_TYPES = new Set(['application/ld+json']);
const isGovernedInlineScript = (attrs) => {
  if ('src' in attrs) return false;
  const type = attrs.type;
  if (type === undefined) return true;
  if (type.includes('&')) return true; // character reference -- do not trust, hash it
  return !INERT_SCRIPT_TYPES.has(type.trim().toLowerCase());
};

// Match an open <script> tag, quote-aware: the attribute body may contain a ">"
// inside a quoted value, which a naive [^>]* would truncate at.
const scriptOpenPattern = /<script\b((?:"[^"]*"|'[^']*'|[^>"'])*)>/gi;

// Slice each <script>...</script> body exactly as the browser hashes it (raw text
// content, whitespace included), matching the close tag case-insensitively.
const scriptBodies = function* (html, wantAfter, metaIndex) {
  for (const open of html.matchAll(scriptOpenPattern)) {
    const after = open.index >= metaIndex;
    if (after !== wantAfter) continue;
    const attrs = parseAttrs(open[1]);
    if (!isGovernedInlineScript(attrs)) continue;
    const bodyStart = open.index + open[0].length;
    const rest = html.slice(bodyStart);
    const close = rest.search(/<\/script\s*>/i);
    const body = close === -1 ? rest : rest.slice(0, close);
    if (body.trim() === '') continue;
    yield body;
  }
};

const styleOpenPattern = /<style\b((?:"[^"]*"|'[^']*'|[^>"'])*)>/gi;

// Text inside <script>/<style> is a text node, not markup: a "<meta ...>" written
// there (e.g. a string inside an application/ld+json block) is never applied by
// the browser. Collect those body ranges so meta detection can ignore any match
// that lands inside one.
const textNodeRanges = (html) => {
  const ranges = [];
  for (const [open, closeRe] of [
    [scriptOpenPattern, /<\/script\s*>/i],
    [styleOpenPattern, /<\/style\s*>/i],
  ]) {
    for (const m of html.matchAll(open)) {
      const start = m.index + m[0].length;
      const rel = html.slice(start).search(closeRe);
      ranges.push([start, rel === -1 ? html.length : start + rel]);
    }
  }
  return ranges;
};

// Match a CSP meta, quote-aware for the same reason as the script pattern.
const metaPattern =
  /<meta\b((?:"[^"]*"|'[^']*'|[^>"'])*\bhttp-equiv=["']content-security-policy["'](?:"[^"]*"|'[^']*'|[^>"'])*)>/gi;

// Real CSP metas only: matches that are actual <head> markup, not a "<meta ...>"
// string sitting inside a <script>/<style> text node.
const findCspMetas = (html) => {
  const ranges = textNodeRanges(html);
  return [...html.matchAll(metaPattern)].filter(
    (m) => !ranges.some(([s, e]) => m.index >= s && m.index < e),
  );
};

// Self-test: the classifier must match browser parsing on the known bypass shapes
// (duplicate-attribute first-wins, character references, comment-embedded meta).
// This runs on every `pnpm run test`; if it breaks, the whole gate is untrusted.
{
  const gov = (tag) => isGovernedInlineScript(parseAttrs(tag));
  assert.equal(gov('type="text/javascript" type="application/json"'), true, 'duplicate type: first (JS) wins');
  assert.equal(gov('type="text/jav&#x61;script"'), true, 'character-reference type must be governed');
  assert.equal(gov('type="application/ld+json"'), false, 'ld+json is inert data');
  assert.equal(gov('src="x.js"'), false, "external script's body is inert");
  assert.equal(gov('type=""'), true, 'empty type is classic JS');
  assert.equal(gov(''), true, 'no type is classic JS');
  assert.equal(gov('type="module"'), true, 'module must be hashed');
  assert.equal(gov('type="importmap"'), true, 'importmap must be hashed');
  assert.equal(gov('type=" APPLICATION/LD+JSON "'), false, 'inert match is trimmed + case-insensitive');
  const commented = stripComments('<!-- <meta http-equiv="content-security-policy" content="x"> -->');
  assert.equal([...commented.matchAll(metaPattern)].length, 0, 'CSP meta inside a comment must not count');
  assert.doesNotMatch(
    stripComments('<!--<!-- -->-->'),
    /<!--[\s\S]*?-->/,
    'stripComments reaches a fixpoint: no complete comment survives, even reformed ones',
  );
  const truncation = '<script type="a>b" type="text/javascript">alert(1)</script>';
  const open = [...truncation.matchAll(scriptOpenPattern)][0];
  assert.ok(open && isGovernedInlineScript(parseAttrs(open[1])), 'quoted ">" must not truncate the tag scan');
  // A "<meta CSP>" string inside a script text node is not a real policy.
  const fakeInJson =
    '<head><script type="application/ld+json">{"x":"<meta http-equiv=\\"content-security-policy\\" content=\\"default-src *\\">"}</script></head>';
  assert.equal(findCspMetas(fakeInJson).length, 0, 'a CSP meta inside a script body must not count as a policy');
  const realMeta = '<head><meta http-equiv="content-security-policy" content="default-src \'self\'"></head>';
  assert.equal(findCspMetas(realMeta).length, 1, 'a real <head> CSP meta must count');
}

let governed = 0;
let ungoverned = 0;

for await (const rel of glob('**/*.html', { cwd: dist })) {
  const page = rel.replaceAll('\\', '/');
  // Strip comments before any markup detection so commented-out scripts/metas
  // are treated as absent (matching the browser).
  const html = stripComments(await readFile(resolve(dist, rel), 'utf8'));
  const metas = findCspMetas(html);

  // Partition: prototype pages are static public/ files with no CSP; every other
  // page is Astro-generated and must carry exactly one meta.
  if (!demo && page.startsWith('prototypes/')) {
    assert.equal(metas.length, 0, `${page} is a static prototype but carries a CSP meta`);
    ungoverned++;
    continue;
  }
  assert.equal(metas.length, 1, `${page} must carry exactly one Content-Security-Policy meta (found ${metas.length})`);
  governed++;

  const metaIndex = metas[0].index;
  const headStart = html.search(/<head\b/i);
  const headEnd = html.search(/<\/head\s*>/i);
  assert.ok(
    headStart !== -1 && headEnd !== -1 && metaIndex > headStart && metaIndex < headEnd,
    `${page} CSP meta is not inside <head>`,
  );

  // Vite escapes apostrophes in injected attribute values. Decode that known
  // build output before splitting directives; the browser does the same.
  const policy = (parseAttrs(metas[0][1]).content ?? '').replaceAll('&#39;', "'");
  const directive = (name) => {
    const found = policy.split(';').map((p) => p.trim()).find((p) => p === name || p.startsWith(`${name} `));
    return found ? found.slice(name.length).trim() : undefined;
  };
  const scriptSrc = directive('script-src');
  assert.ok(scriptSrc !== undefined, `${page} CSP has no script-src`);
  assert.doesNotMatch(scriptSrc, /'unsafe-inline'/, `${page} script-src allows 'unsafe-inline'`);
  assert.match(scriptSrc, /'self'/, `${page} script-src omits 'self'`);

  assert.equal(directive('default-src'), "'self'", `${page} default-src is not 'self'`);
  assert.equal(directive('object-src'), "'none'", `${page} object-src is not 'none'`);
  assert.equal(directive('base-uri'), demo ? "'none'" : "'self'", `${page} has an unexpected base-uri`);
  assert.equal(directive('form-action'), demo ? "'none'" : "'self'", `${page} has an unexpected form-action`);
  if (demo) {
    assert.equal(scriptSrc, "'self'", `${page} demo scripts must be same-origin only`);
    for (const override of ['script-src-elem', 'script-src-attr', 'style-src-elem']) {
      assert.equal(directive(override), undefined, `${page} demo must not override ${override}`);
    }
    assert.equal(directive('connect-src'), "'none'", `${page} demo must refuse network connections`);
    assert.equal(directive('img-src'), "'self'", `${page} demo images must be same-origin only`);
    assert.equal(directive('font-src'), "'self'", `${page} demo fonts must be same-origin only`);
  }

  // Every governed inline script after the meta must be hashed into script-src.
  // Recompute the browser's sha256 and assert it is present -- this is the check
  // that trips if a route adds an unhashed post-meta inline script.
  for (const body of scriptBodies(html, true, metaIndex)) {
    assert.ok(
      scriptSrc.includes(quote(sha256(body))),
      `${page} has an inline script after the CSP meta whose hash is not in script-src`,
    );
  }

  // Scripts before the meta are ungoverned by the browser (no policy yet). EVERY
  // one -- inline or external -- must be a known, reviewed bootstrap: an external
  // <script src> before the meta would fetch and run with no policy at all, so it
  // is rejected outright (none of the known pre-meta scripts are external). Inert
  // data blocks (e.g. application/ld+json) do not execute and are skipped; every
  // executable inline script must be in the exact allowlist.
  for (const open of html.matchAll(scriptOpenPattern)) {
    if (open.index >= metaIndex) continue;
    const attrs = parseAttrs(open[1]);
    assert.ok(
      !('src' in attrs),
      `${page} has an external <script src> BEFORE the CSP meta; it would run ungoverned by CSP`,
    );
    if (!isGovernedInlineScript(attrs)) continue;
    const bodyStart = open.index + open[0].length;
    const rest = html.slice(bodyStart);
    const close = rest.search(/<\/script\s*>/i);
    const body = close === -1 ? rest : rest.slice(0, close);
    if (body.trim() === '') continue;
    assert.ok(!demo, `${page} demo has a script BEFORE the CSP meta`);
    assert.ok(
      PRE_META_ALLOWED.has(sha256(body)),
      `${page} has an unrecognised inline script BEFORE the CSP meta (hash ${sha256(body)}); it would be ungoverned by CSP`,
    );
  }

  // PostHog: when array.js is present the page is an analytics-enabled build, so
  // script-src must pin the exact loader URL (not the bare origin) and connect-src
  // must carry the origin for ingestion. When array.js is absent, script-src must
  // carry no cross-origin source at all.
  const host = parseAttrs(
    (html.match(/<[^>]*\bdata-posthog-host=["']([^"']+)["'][^>]*>/i) ?? [, ''])[0] ?? '',
  )['data-posthog-host'];
  if (/\/static\/array\.js/.test(html)) {
    assert.ok(host, `${page} references array.js but renders no PostHog host`);
    const origin = new URL(host).origin;
    const loader = `${origin}/static/array.js`;
    assert.ok(scriptSrc.includes(loader), `${page} script-src omits the pinned PostHog loader ${loader}`);
    // The pin is only meaningful if the bare origin is not also a standalone token.
    assert.doesNotMatch(
      ` ${scriptSrc} `,
      new RegExp(`\\s${origin.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}\\s`),
      `${page} script-src lists the bare PostHog origin as a token; only the exact loader URL may appear`,
    );
    assert.ok(directive('connect-src')?.includes(origin), `${page} connect-src omits the PostHog origin ${origin}`);
  } else {
    assert.doesNotMatch(scriptSrc, /https?:\/\//, `${page} has no array.js but script-src carries a cross-origin source`);
  }

  // style-src is not hash-strict (see the header / astro.config.mjs): Radix
  // injects <style> at runtime and React renders inline style="" from SSR, neither
  // hashable. Pin the exact posture so a dropped 'self' or a stray re-add of style
  // hashing trips.
  assert.equal(directive('style-src'), demo ? "'self'" : "'self' 'unsafe-inline'", `${page} has an unexpected style-src`);
  assert.equal(directive('style-src-attr'), undefined, `${page} emits a separate style-src-attr; fold it into style-src`);
}

// Floors, not exact counts: the partition asserts every non-prototype page carries
// a meta, so a lost meta or a stray public page already trips above. These just
// catch a glob that matched nothing (a broken dist path).
assert.ok(governed > 0, 'no governed pages found; dist path or glob is broken');
if (!demo) assert.ok(ungoverned > 0, 'no static prototype pages found; dist path or glob is broken');
console.log(
  `CSP gate: ${governed} governed ${demo ? 'demo' : 'docs'} pages, ${ungoverned} ungoverned prototype pages`,
);
