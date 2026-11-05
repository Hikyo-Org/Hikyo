import { useKeyCatalogue } from '../api/identities.ts';
import { querySettled, useScopeNames } from '../api/scopeNames.ts';
import { useProjectEnvironments } from '../api/adapters.ts';
import { Glyph } from '../ui/Glyph.tsx';
import { IdTail } from '../ui/NamedId.tsx';

export type ConsentKey = { id: string; name: string; classification?: 'secret' | 'config' };

/** Name each bound key from the catalogue; one the catalogue lacks stays listed by its ID. */
export function consentKeys(
  catalogue: readonly { id: string; name: string; classification: 'secret' | 'config' }[] | undefined,
  ids: readonly string[],
): ConsentKey[] {
  const byId = new Map(catalogue?.map((key) => [key.id, key]));
  return ids.map((id) => {
    const key = byId.get(id);
    return key === undefined ? { id, name: id } : { id, name: key.name, classification: key.classification };
  });
}

/**
 * Names for a consent screen, read through the ordinary authorized endpoints
 * (never from the CLI or URL). A name the caller cannot read stays the ID, which
 * is the bound value anyway. `settled` is true once every lookup has succeeded
 * or failed, so a screen can hold its approve button until nothing is still
 * showing IDs; a failed lookup does not block. An empty org or project issues no
 * request.
 */
export function useConsentNames(org: string, project: string, wantKeys: boolean) {
  const scope = useScopeNames(org, project);
  const environments = useProjectEnvironments({ org, project });
  const catalogue = useKeyCatalogue({ org, project }, wantKeys);
  const environmentNames = new Map(environments.data?.items.map((item) => [item.id, item.name]));
  return {
    org: scope.org,
    project: scope.project,
    environmentName: (id: string) => environmentNames.get(id),
    keys: (ids: readonly string[]) => consentKeys(catalogue.data?.items, ids),
    settled: scope.settled && querySettled(environments) && querySettled(catalogue),
  };
}

/**
 * The enumerated key set a consent decision binds, led by its count and listed
 * by name like the in-app ceremony. Every key stays listed, with its ID's end beside
 * the name and the whole ID on hover;
 * the focusable region keeps a long list reachable by keyboard.
 */
export function ConsentKeys({
  keys,
  label = 'Keys this decision covers',
  insideScrollRegion = false,
}: {
  keys: readonly ConsentKey[];
  /** Several lists on one page each need their own name. */
  label?: string;
  /** The surrounding region scrolls and takes focus, so the list must not as well. */
  insideScrollRegion?: boolean;
}) {
  return (
    <>
      <p className="ceremony__scope">
        {keys.length} {keys.length === 1 ? 'key' : 'keys'}
      </p>
      <ul className="ceremony__keys" aria-label={label} tabIndex={insideScrollRegion ? undefined : 0}>
        {keys.map((key) => (
          <li className="mono" key={key.id} title={key.id}>
            {key.classification === 'secret' ? (
              <>
                <Glyph name="lock" /> <span className="visually-hidden">secret </span>
              </>
            ) : null}
            {key.name}
            {key.name === key.id ? null : <> <IdTail id={key.id} /></>}
          </li>
        ))}
      </ul>
    </>
  );
}
