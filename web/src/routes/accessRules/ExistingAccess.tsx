import { scopeOf, type membershipRows, type Names } from '../../api/access.ts';
import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Disclosure } from '../../ui/Disclosure.tsx';
import { PERMS, permOf } from './model.ts';
import { Lock, PermBadge } from './parts.tsx';

export type ExistingAccess = ReturnType<typeof membershipRows>[number];

/** Scope-wide permissions have the same presentation as rules. Keep every
 * capability, including atoms the narrowed-rule editor cannot represent. */
export function ExistingAccessCard({ access, names, protectedScope, onEdit, revoking }: {
  access: ExistingAccess;
  names: Names;
  protectedScope: boolean;
  onEdit?: () => void;
  revoking?: string;
}) {
  const first = access.grants[0];
  if (first === undefined) return null;
  const scope = scopeOf(first);
  const permissions = [...access.grants].sort((a, b) => {
    const rank = (capability: string) => {
      const index = PERMS.findIndex((p) => p.id === capability);
      return index < 0 ? PERMS.length : index;
    };
    return rank(a.capability) - rank(b.capability) || a.capability.localeCompare(b.capability);
  });
  return (
    <li className="access-rule">
      <div className="access-rule__main">
        <div className="access-rule__perms">
          {permissions.map((grant) => {
            const id = permOf(grant.capability);
            return id === undefined
              ? <Badge key={grant.id} mono>{grant.capability}</Badge>
              : <PermBadge key={grant.id} id={id} />;
          })}
        </div>
        <p className="access-where">
          {scope.kind === 'instance' ? (
            <span className="access-where__part">This instance · every organisation</span>
          ) : (
            <span className="access-where__part">
              <span className="access-where__label">Projects</span>
              <span className="access-where__value">{scope.kind === 'org' ? 'all' : names.project(scope.project)}</span>
            </span>
          )}
          <span className="access-where__part">
            <span className="access-where__label">Environments</span>
            <span className="access-where__value">
              {scope.kind === 'environment' ? names.environment(scope.environment) : 'all'}
              {protectedScope ? <Lock word="protected" /> : null}
            </span>
          </span>
          <span className="access-where__part">
            <span className="access-where__label">Keys</span>
            <span className="access-where__value">all</span>
          </span>
        </p>
        {scope.kind === 'org' || scope.kind === 'instance' ? (
          <p className="access-rule__reach">Includes projects and environments created later.</p>
        ) : scope.kind === 'project' ? (
          <p className="access-rule__reach">Includes environments created later.</p>
        ) : null}
        <Disclosure label="Permission origins">
          <ul className="capabilities">
            {access.grants.map((grant) => (
              <li className="capability" key={grant.id}>
                <span className="capability__name mono">{grant.capability}</span>
                {grant.origins.map((origin) => (
                  <Badge key={`${origin.kind}:${origin.subject}`}>{origin.kind}: {origin.subject}</Badge>
                ))}
              </li>
            ))}
          </ul>
        </Disclosure>
        {revoking === undefined ? null : <p role="status" aria-busy="true">Revoking {revoking}…</p>}
      </div>
      {onEdit === undefined ? null : (
        <Button type="button" variant="quiet" onClick={onEdit}>
          Edit<span className="visually-hidden"> access on {access.scopeLabel}</span>
        </Button>
      )}
    </li>
  );
}
