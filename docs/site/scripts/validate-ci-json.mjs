import { readFileSync } from 'node:fs';

import { z } from 'zod';

const iconSchema = z.object({
  src: z.string(),
  sizes: z.string(),
  type: z.string().optional(),
});

const manifestSchema = z.object({
  id: z.literal('/'),
  start_url: z.literal('/'),
  scope: z.literal('/'),
  display: z.literal('standalone'),
  icons: z.array(iconSchema),
});

const builtManifestSchema = manifestSchema.extend({
  name: z.literal('Hikyo — fully open secrets and configuration'),
  short_name: z.literal('Hikyo'),
  theme_color: z.literal('#1b2225'),
});

const dnsResponseSchema = z.object({
  Status: z.literal(0),
  Answer: z.array(
    z.object({
      type: z.number(),
      data: z.string(),
    }),
  ),
});

const [mode, inputPath] = process.argv.slice(2);
const input = inputPath === undefined ? readFileSync(0, 'utf8') : readFileSync(inputPath, 'utf8');

function hasIcon(manifest, src, sizes, requireType) {
  return manifest.icons.some(
    (icon) =>
      icon.src === src &&
      icon.sizes === sizes &&
      (!requireType || icon.type === 'image/png'),
  );
}

try {
  if (mode === 'built-manifest') {
    const manifest = builtManifestSchema.parse(JSON.parse(input));
    if (
      !hasIcon(manifest, '/pwa-192x192.png', '192x192', true) ||
      !hasIcon(manifest, '/pwa-512x512.png', '512x512', true)
    ) {
      process.exitCode = 1;
    }
  } else if (mode === 'live-manifest') {
    const manifest = manifestSchema.parse(JSON.parse(input));
    if (
      !hasIcon(manifest, '/pwa-192x192.png', '192x192', false) ||
      !hasIcon(manifest, '/pwa-512x512.png', '512x512', false)
    ) {
      process.exitCode = 1;
    }
  } else if (mode === 'dns-mx') {
    const response = dnsResponseSchema.parse(JSON.parse(input));
    if (!response.Answer.some((answer) => answer.type === 15 && answer.data.length > 0)) {
      process.exitCode = 1;
    }
  } else {
    throw new Error(`unknown validation mode: ${String(mode)}`);
  }
} catch {
  process.exitCode = 1;
}
