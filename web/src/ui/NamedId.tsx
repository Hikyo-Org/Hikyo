/**
 * A human name with its ID reachable on hover, or the ID in mono when no name is
 * known. `bdi` keeps a name's text direction from reordering what stands around
 * it. `tail` also prints the ID's end, for a screen where someone approves against
 * the name: anyone who may rename the thing chooses what the name says.
 */
export function NamedId({ id, name, tail = false }: { id: string; name: string | undefined; tail?: boolean }) {
  if (name === undefined || name === id) return <span className="mono">{id}</span>;
  return (
    <>
      <bdi title={id}>{name}</bdi>
      {tail ? <> <span className="mono">…{id.slice(-6)}</span></> : null}
    </>
  );
}
