import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import type { Person, Rule, World } from './model.ts';
import { RuleSummary } from './parts.tsx';

/**
 * The people of a Members page with their access rules: one card per rule
 * (its permissions, then Where, then what it reaches), and the actions that
 * open the rule editor. People only: machines keep their grants (rules are
 * for people in this slice, and machines are managed on Machine access).
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
  onEdit,
  onAdd,
}: {
  world: World;
  people: readonly Person[];
  you: string;
  canEdit: boolean;
  onEdit: (rule: Rule) => void;
  onAdd: (person: string) => void;
}) {
  return (
    <ul className="access-people">
      {people.map((person) => {
        const rules = world.rules.filter((r) => r.member === person.id && r.source.kind === 'rule');
        return (
          <li key={person.id} className="access-person">
            <div className="access-person__who">
              <strong>
                {person.name} {person.id === you ? <Badge>you</Badge> : null}
              </strong>
              <span className="mono">{person.id}</span>
            </div>
            <div className="access-person__rules">
              {rules.length > 0 ? (
                <ul className="access-rule-list">
                  {rules.map((rule, i) => (
                    <li key={rule.id} className="access-rule">
                      <RuleSummary world={world} rule={rule} reach />
                      {rule.source.kind === 'rule' && rule.source.otherProjects ? (
                        <p className="access-hint">Also applies to other projects: change it on the organisation's Members page.</p>
                      ) : canEdit ? (
                        <Button type="button" variant="quiet" onClick={() => onEdit(rule)}>
                          Edit<span className="visually-hidden"> rule {i + 1} of {person.name}</span>
                        </Button>
                      ) : null}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className="access-hint">· no rules</p>
              )}
              {canEdit ? (
                <Button type="button" variant="quiet" className="access-person__action" onClick={() => onAdd(person.id)}>
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
