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
      if (res.status === 404 || res.status === 405) {
        window.location.assign(schemeUrl(node));
        return;
      }
      const { message } = reply.parse(await res.json());
      api.addNotification({ id: ADDON_ID, content: { headline: message }, duration: 4000 });
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
