/**
 * Visible middle-dot separator between two pieces of inline text (e.g.
 * "Payments · every environment"), bigger and better spaced than a plain
 * " · ". It is real text, " · ", so assistive technology reads a separator
 * and the words either side never run together; the spaces sit at the edges
 * of an inline-block, where layout drops them, so the margins alone space it.
 */
export function Sep() {
  return <span className="sep"> · </span>;
}
