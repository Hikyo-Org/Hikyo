import type { ReactNode } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import type { Person, Rule, World } from './model.ts';
import { RuleSummary } from './parts.tsx';

/**
 * The people of a Members page with their access rules: one card per rule
 * (its permissions, then Where, then what it reaches), and the actions that
 * open the rule editor. Existing scope-wide access can join the same list;
 * machines keep their scope-wide access and are never offered a narrowed rule.
 *
 * A rule that also names projects this page does not show (a project's
 * Members page sees only its own part) is read-only here: editing or removing
 * it would change the part this page cannot see.
 */
export function RulesPanel({
  world,
  people,
  you,
  canEdit,
  showReach = true,
  onEdit,
  onAdd,
  existingAccess,
  personActions,
}: {
  world: World;
  people: readonly Person[];
  you: string;
  canEdit: boolean;
  /** Omit counts when directory metadata is incomplete. */
  showReach?: boolean;
  onEdit: (rule: Rule) => void;
  onAdd: (person: string) => void;
  /** Existing scope-wide access uses the same cards, without changing storage. */
  existingAccess?: (person: string) => ReactNode;
  personActions?: (person: Person) => ReactNode;
}) {
  return (
    <ul className="access-people">
      {people.map((person) => {
        const rules = world.rules.filter((r) => r.member === person.id && r.source.kind === 'rule');
        return (
          <li key={person.id} className="access-person">
            <div className="access-person__who">
              <strong>
                <span className="member-name" title={person.id}>{person.name}</span> {person.id === you ? <Badge>you</Badge> : null}
              </strong>
              <span className="mono">{person.id}</span>
              {person.kind === 'machine' ? <Badge>machine</Badge> : null}
              {personActions?.(person)}
            </div>
            <div className="access-person__rules">
              {rules.length > 0 || existingAccess !== undefined ? (
                <ul className="access-rule-list">
                  {existingAccess?.(person.id)}
                  {rules.map((rule, i) => (
                    <li key={rule.id} className="access-rule">
                      <RuleSummary world={world} rule={rule} reach={showReach} />
                      {rule.source.kind === 'rule' && rule.source.otherProjects ? (
                        <p className="access-hint">Also applies to other projects: change it on the organisation's Members page.</p>
                      ) : canEdit ? (
                        <Button type="button" variant="quiet" data-rule-action onClick={() => onEdit(rule)}>
                          Edit<span className="visually-hidden"> rule {i + 1} of {person.name}</span>
                        </Button>
                      ) : null}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="access-hint">· no rules</p>
              )}
              {canEdit && person.kind === 'person' ? (
                <Button type="button" variant="quiet" className="access-person__action" data-rule-action onClick={() => onAdd(person.id)}>
                  + Add rule<span className="visually-hidden"> for {person.name}</span>
                </Button>
              ) : null}
            </div>
          </li>
        );
      })}
    </ul>
  );
}
