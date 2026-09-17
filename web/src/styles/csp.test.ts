import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

// Production serves `img-src 'self'` (internal/server/spa.go, asserted by
// spa_test.go). A `data:` URL in a mask-image or background-image is fetched
// as an image and blocked, which silently drops whatever it drew (#773 F01: the
// checkbox tick and dash). vite's `assetsInlineLimit: 0` keeps the build from
// inlining assets; this keeps hand-written ones out of the stylesheets.
describe.each(['app.css', 'tokens.css'])('%s under the production CSP', (file) => {
  it('references no data: URL', () => {
    const css = readFileSync(new URL(`./${file}`, import.meta.url), 'utf8');
    expect(css).not.toMatch(/url\(\s*["']?\s*data:/i);
  });
});
