import { z } from 'zod';
// The release ceremony's sync-trust step records each published stable release
// here, and docs.yml redeploys the site when this file changes on main.
import trustMetadata from '../../../../release/trust/metadata.json' with { type: 'json' };

const metadata = z
  .object({ highest_release: z.string().regex(/^\d+\.\d+\.\d+$/) })
  .parse(trustMetadata);

export const stableVersion = metadata.highest_release;
export const stableReleaseUrl = `https://github.com/Hikyo-Org/Hikyo/releases/tag/v${stableVersion}`;

// Docs pages write %STABLE_VERSION% in prose, code and link URLs. The
// substitution runs in the page pipeline (HTML, llms.txt) and in the search
// index, which src/lib/source.ts builds from the raw page body.
const token = /%STABLE_VERSION%/g;

function substitute(node) {
  if (node.type === 'text' || node.type === 'inlineCode' || node.type === 'code') {
    node.value = node.value.replace(token, stableVersion);
  }
  if (typeof node.url === 'string') node.url = node.url.replace(token, stableVersion);
  node.children?.forEach(substitute);
}

export function remarkStableVersion() {
  return substitute;
}
