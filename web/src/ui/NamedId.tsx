import { Fragment } from 'react';

/**
 * A human name with its ID reachable on hover, or the ID in mono when no name is
 * known. `bdi` keeps a name's text direction from reordering what stands around
 * it. `tail` also prints the ID's end, for a screen where someone approves against
 * the name: anyone who may rename the thing chooses what the name says.
 * `breakable` lets a long snake_case name wrap, but only after an underscore.
 */
export function NamedId({ id, name, tail = false, breakable = false }: { id: string; name: string | undefined; tail?: boolean; breakable?: boolean }) {
  if (name === undefined || name === id) return <span className="mono">{id}</span>;
  return (
    <>
      <bdi title={id}>{breakable ? <Breakable text={name} /> : name}</bdi>
      {tail ? <> <IdTail id={id} /></> : null}
    </>
  );
}

/** The last characters of an ID, printed beside a name someone approves against. */
export function IdTail({ id }: { id: string }) {
  return (
    <>
      <span className="visually-hidden">ID ending </span>
      <span className="mono id-tail">…{id.slice(-6)}</span>
    </>
  );
}

/**
 * Breakable is a word that may break only after an underscore: a long key name or
 * error code wraps at its own word boundaries and is never split mid-word.
 */
export function Breakable({ text }: { text: string }) {
  const parts = text.split(/(?<=_)/);
  return (
    <>
      {parts.map((part, index) => (
        <Fragment key={`${String(index)}-${part}`}>
          {part}
          {index < parts.length - 1 ? <wbr /> : null}
        </Fragment>
      ))}
    </>
  );
}
