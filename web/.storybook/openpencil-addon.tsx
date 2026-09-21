// "Open in OpenPencil" toolbar button. Routes the design node to the app
// through the openpencil:// URL scheme, which shipped in OpenPencil v0.15.1
// (open-pencil #708). The OS hands the link to the installed app.
// `React` must stay imported even though the app builds with the automatic JSX
// runtime: Storybook's manager builder bundles this file with its own esbuild
// config (`jsx: 'transform'`, `jsxFactory: 'React.createElement'`), so the JSX
// below compiles to a bare `React.createElement` call that needs the binding.
import React from 'react';
import { IconButton } from 'storybook/internal/components';
import { addons, types, useParameter } from 'storybook/manager-api';

const ADDON_ID = 'hikyo/openpencil';
const PEN_FILE = 'web/design/hikyo.pen';

function schemeUrl(node: string) {
  return `openpencil://open?file=${encodeURIComponent(PEN_FILE)}&node=${encodeURIComponent(node)}`;
}

function OpenButton() {
  const design = useParameter<{ name?: string } | undefined>('design');
  const node = design?.name;
  if (!node) return null;
  return (
    <IconButton title={`Open ${node} in OpenPencil`} onClick={() => window.location.assign(schemeUrl(node))}>
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
