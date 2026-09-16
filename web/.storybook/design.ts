// The one way a story points at its design. The node is a `Title/Variant`
// name path inside web/design/hikyo.pen; scripts/design/export.ts turns it
// into /design/<slug>.png at build time, so authors never write a URL.
const NODE_PATH = /^[A-Za-z0-9]+( [A-Za-z0-9]+)*(\/[A-Za-z0-9]+( [A-Za-z0-9]+)*)+$/;

export function design(node: string) {
  if (!NODE_PATH.test(node)) {
    throw new Error(`design(): "${node}" must be Title/Variant: letters, digits and single spaces, at least two segments`);
  }
  return { type: 'image' as const, url: `/design/${node.replaceAll('/', '--')}.png`, name: node };
}
