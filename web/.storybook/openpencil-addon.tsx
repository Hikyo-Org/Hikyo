// "Open in OpenPencil" toolbar button. Tries the dev middleware first; on
// 404/405 (static build, or middleware removed after the upstream scheme
// ships) it falls back to the openpencil:// link, which the OS routes to the app.
// `React` must stay imported even though the app builds with the automatic JSX
// runtime: Storybook's manager builder bundles this file with its own esbuild
// config (`jsx: 'transform'`, `jsxFactory: 'React.createElement'`), so the JSX
// below compiles to a bare `React.createElement` call that needs the binding.
import React from 'react';
import { IconButton } from 'storybook/internal/components';
import { addons, types, useParameter, useStorybookApi } from 'storybook/manager-api';
import { z } from 'zod';

const ADDON_ID = 'hikyo/openpencil';
const reply = z.object({ message: z.string() });
const MIN_APP_VERSION = '0.15.0';
const PEN_FILE = 'web/design/hikyo.pen';

function schemeUrl(node: string) {
  return `openpencil://open?file=${encodeURIComponent(PEN_FILE)}&node=${encodeURIComponent(node)}`;
}

function OpenButton() {
  const design = useParameter<{ name?: string } | undefined>('design');
  const api = useStorybookApi();
  const node = design?.name;
  if (!node) return null;
  const onClick = async () => {
    try {
      const res = await fetch('/__openpencil/open', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ node }),
      });
      // The body shape decides, not the status: the middleware answers 404 with a
      // real `{ message }` when the node is missing from hikyo.pen, and that is the
      // most common failure. Only a reply the middleware could not have written
      // (a static host's HTML 404, or 405 from a static host's method handling)
      // means there is no middleware, so only that falls back to the scheme.
      const parsed = res.status === 405 ? null : reply.safeParse(await res.json().catch(() => null));
      if (!parsed?.success) {
        window.location.assign(schemeUrl(node));
        return;
      }
      api.addNotification({ id: ADDON_ID, content: { headline: parsed.data.message }, duration: 4000 });
    } catch {
      window.location.assign(schemeUrl(node));
    }
  };
  return (
    <IconButton title={`Open ${node} in OpenPencil (needs OpenPencil >= ${MIN_APP_VERSION} installed)`} onClick={onClick}>
      ✎
    </IconButton>
  );
}

addons.register(ADDON_ID, () => {
  addons.add(`${ADDON_ID}/tool`, {
    type: types.TOOL,
    title: 'Open in OpenPencil',
    match: ({ viewMode }) => viewMode === 'story' || viewMode === 'docs',
    render: () => <OpenButton />,
  });
});
