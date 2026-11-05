/** A human name with its ID reachable on hover, or the ID in mono when no name is known. */
export function NamedId({ id, name }: { id: string; name: string | undefined }) {
  return name === undefined || name === id ? (
    <span className="mono">{id}</span>
  ) : (
    <span title={id}>{name}</span>
  );
}
