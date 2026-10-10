import { describe, expect, it } from 'vitest';

import { initialAssets } from './initial-assets.ts';

const assets = ['/assets/app.js', '/assets/shared.js', '/assets/app.css'];

describe('build HTML entry-point accounting', () => {
  it('counts equivalent upper/lower-case tags and attributes with browser-valid quoting', async () => {
    expect(await initialAssets('<script type="module" src="/assets/app.js"></script><link rel="modulepreload" href="/assets/shared.js"><link rel="stylesheet" href="/assets/app.css">')).toEqual(assets);
    expect(await initialAssets("<SCRIPT TYPE=MODULE SRC='/assets/app.js'></SCRIPT><LINK REL='modulepreload' HREF=/assets/shared.js><LiNk ReL=StyleSheet HREF='/assets/app.css'>")).toEqual(assets);
  });

  it('ignores comments, script text and unrelated links without truncating quoted attributes', async () => {
    expect(await initialAssets(`
      <!-- <script type="module" src="/assets/comment.js"></script> -->
      <script>const text = '<link rel="stylesheet" href="/assets/script-text.css">';</script>
      <script type="module" data-label="a > b" src="/assets/app.js"></script>
      <link rel="icon" href="/favicon.svg">
      <script src="/assets/classic.js"></script>
      <link rel="stylesheet alternate" href="/assets/a&amp;b.css">
    `)).toEqual(['/assets/app.js', '/assets/a&b.css']);
  });

  it('does not evaluate inline code or follow an iframe while measuring', async () => {
    const html = '<script type="module">throw new Error("must not execute")</script><iframe src="https://example.invalid/"></iframe><link rel="stylesheet">';
    expect(await initialAssets(html)).toEqual([]);
  });
});
