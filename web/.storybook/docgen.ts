import type { PluginOption } from 'vite';

// This generated API client contains transport, schemas and types, not React
// components. Storybook 10.6's default docgen filter otherwise parses every TS
// file, including its large generated schemas. Keep all frontend owners and
// stories eligible so descriptions, props and defaults are still extracted.
export const generatedClient = /\/clients\/ts\/src\/generated\//;

export async function scopeDocgen(entry: PluginOption): Promise<PluginOption> {
  const plugin = await entry;
  if (Array.isArray(plugin)) return Promise.all(plugin.map(scopeDocgen));
  if (!plugin || plugin.name !== 'storybook:react-docgen-plugin') return plugin;
  // The installed plugin exposes a function. Fail visibly if an upgrade
  // changes that contract instead of silently dropping existing hook filters.
  if (!('transform' in plugin) || typeof plugin.transform !== 'function') throw new Error('Storybook docgen transform contract changed; review its generated-client filter');
  return {
    ...plugin,
    transform: {
      filter: { id: { exclude: generatedClient } },
      handler: plugin.transform,
    },
  };
}
