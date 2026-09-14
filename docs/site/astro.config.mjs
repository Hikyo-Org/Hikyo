import mdx from '@astrojs/mdx';
import { unified } from '@astrojs/markdown-remark';
import react from '@astrojs/react';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'astro/config';
import { loadEnv } from 'vite';
import { pwaAssetGraph } from './scripts/pwa-asset-graph.mjs';
import {
  rehypeCode,
  remarkCodeTab,
  remarkHeading,
  remarkNpm,
  remarkStructure,
} from 'fumadocs-core/mdx-plugins';

// PostHog is the only third party the site talks to, and only after opt-in. Its
// origin has to reach the Content-Security-Policy so `array.js` (script-src) and
// the ingestion calls (connect-src) survive the policy. Read the same variables
// posthog.astro reads, from `.env` (loadEnv) or the CI environment, and fail the
// build loudly when the deploy declares analytics required but omits the host,
// mirroring the guard in posthog.astro so config and component cannot disagree.
const buildEnv = { ...loadEnv(process.env.NODE_ENV || 'production', process.cwd(), ''), ...process.env };
const postHogRequired = buildEnv.POSTHOG_REQUIRED === 'true';
const rawPostHogHost = buildEnv.PUBLIC_POSTHOG_HOST;
const postHogToken = buildEnv.PUBLIC_POSTHOG_PROJECT_TOKEN;
if (postHogRequired && !postHogToken) {
  throw new Error('PUBLIC_POSTHOG_PROJECT_TOKEN is required to build the Hikyo site');
}
if (postHogRequired && !rawPostHogHost) {
  throw new Error('PUBLIC_POSTHOG_HOST is required to build the Hikyo site');
}
// posthog.astro renders the consent bootstrap only when host AND token both
// exist, so the CSP may widen only then. A host-only build must not open
// script-src at all, or the policy would trust a host the page never loads from.
const postHogEnabled = Boolean(postHogToken && rawPostHogHost);
let postHogScriptSources = [];
let postHogConnectSources = [];
if (postHogEnabled) {
  const url = new URL(rawPostHogHost);
  if (url.protocol !== 'https:') {
    throw new Error('PUBLIC_POSTHOG_HOST must be an https origin');
  }
  // The vendored PostHog stub builds `api_host + "/static/array.js"` from the RAW
  // host string, while the CSP pins `${origin}/static/array.js`. A trailing slash
  // or path on the env value would make the browser request `.../static/array.js`
  // off a URL the pin does not match, silently blocking the loader. Require a bare
  // origin so the two are byte-identical.
  if (rawPostHogHost !== url.origin) {
    throw new Error('PUBLIC_POSTHOG_HOST must be a bare https origin (no path, no trailing slash)');
  }
  // script-src is pinned to the exact loader URL, not the whole origin, so the
  // policy admits PostHog's array.js and nothing else on that host. This is only
  // viable because init sets `disable_external_dependency_loading: true`, so
  // array.js pulls no sibling scripts (recorder.js, surveys, etc.); enabling any
  // of those later trips this pin by design. connect-src keeps the bare origin
  // because ingestion posts to several paths under it.
  postHogScriptSources = [`${url.origin}/static/array.js`];
  postHogConnectSources = [url.origin];
}

const remarkPlugins = [
  remarkHeading,
  remarkCodeTab,
  remarkNpm,
  [remarkStructure, { exportAs: 'structuredData' }],
];

const rehypePlugins = [rehypeCode];

export default defineConfig({
  site: 'https://hikyo.app',
  trailingSlash: 'always',
  // hikyo.app is a static GitHub Pages deploy, which cannot emit response
  // headers, so the Content-Security-Policy ships as a build-time
  // `<meta http-equiv>` element. Astro computes SHA-256 hashes only for the
  // scripts and styles it *processes* (the PostHog consent bootstrap and its
  // scoped style), so script-src stays strict without `'unsafe-inline'`. The
  // `is:inline` head scripts (theme bootstrap, service-worker registration) are
  // not hashed; they run only because they render before this meta, while the
  // browser has no policy yet. Any inline script emitted *after* the meta must
  // be Astro-processed so its hash lands here (enforced by test-csp.mjs).
  // `frame-ancestors`, `report-*` and `sandbox` are intentionally absent: a meta
  // policy silently ignores them, so clickjacking defence needs a real header
  // (an edge/CDN in front of Pages) and is out of scope here.
  security: {
    csp: {
      directives: [
        "default-src 'self'",
        "base-uri 'self'",
        "form-action 'self'",
        "object-src 'none'",
        "img-src 'self' data:",
        "font-src 'self'",
        "manifest-src 'self'",
        "worker-src 'self'",
        `connect-src 'self'${postHogConnectSources.map((s) => ` ${s}`).join('')}`,
      ],
      scriptDirective: {
        // Fumadocs' RootProvider (next-themes) emits its no-flash theme script
        // as raw SSR HTML in the body, after this meta. Astro only hashes the
        // scripts it processes, so that one is unhashed and strict CSP would
        // block it, breaking docs theming. This pins that exact script. The hash
        // changes if next-themes is upgraded or any `theme` prop in Docs.tsx
        // (attribute, storageKey, defaultTheme, themes) changes; scripts/test-csp.mjs
        // recomputes it from dist and fails the build when it drifts.
        hashes: ['sha256-9OYLeHDy5TB7oToxE4aM8UAKF8Lw7HpI/jURzsI3BWQ='],
        // Astro contributes the inline-script hashes but not an origin, so
        // 'self' is listed explicitly to admit the bundled `/_astro` modules.
        // PostHog's array.js is the one cross-origin script, injected at runtime
        // after consent, pinned to the exact array.js URL.
        resources: ["'self'", ...postHogScriptSources],
      },
      styleDirective: {
        // style-src is intentionally not hash-strict, for two reasons that both
        // rule out tightening to `style-src-attr 'none'` or hashing:
        // 1. SSR: React/fumadocs render inline `style=""` attributes into the
        //    static HTML (docs/index.html ships 4). `style-src-attr 'none'` blocks
        //    them at first paint and hydration does not re-apply attributes it
        //    assumes already match, so the layout breaks and stays broken.
        // 2. Runtime: Radix (search dialog, mobile drawer, scroll-area) injects
        //    <style> *elements* at runtime via react-remove-scroll ->
        //    react-style-singleton (document.createElement('style') on open). No
        //    build-time hash can cover a string the browser generates at runtime,
        //    and a nonce baked into a static file is readable by anyone, so it is
        //    not a control.
        // CSS is not code execution: the finding's XSS risk lives in script-src,
        // which stays hash-strict with no 'unsafe-inline'. This is the standard
        // strict-CSP shape (Google's guidance hashes scripts only). Astro drops
        // all style hashes from the meta once 'unsafe-inline' is present, so the
        // scoped-style hashes are expectedly absent here.
        resources: ["'self'", "'unsafe-inline'"],
      },
    },
  },
  markdown: {
    processor: unified({
      syntaxHighlight: false,
      remarkPlugins,
      rehypePlugins,
    }),
  },
  integrations: [
    pwaAssetGraph(),
    react(),
    mdx({
      extendMarkdownConfig: true,
      syntaxHighlight: false,
    }),
  ],
  vite: {
    plugins: [tailwindcss()],
  },
});
