import { useState } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Panel } from '../Sections.tsx';
import { effective, presetOf, reachOf, reachText, removeRule, saveRule, type Rule, type World } from './model.ts';
import { PermBadge, RuleWhere } from './parts.tsx';
import { newRule, RuleEditorDialog } from './RuleEditorDialog.tsx';

/**
 * The Members page of the member-access prototype: one card per rule, the
 * permissions it gives, then where, then what that reaches. Edits go through
 * {@link RuleEditorDialog} and land in local state; nothing is saved anywhere.
 */
export function AccessRulesMembers({ initialWorld, you = 'marc' }: { initialWorld: World; you?: string }) {
  const [world, setWorld] = useState(initialWorld);
  const [editing, setEditing] = useState<Rule | null>(null);

  return (
    <div className="page page--chrome access-rules">
      <h1>Members · acme</h1>
      <p className="page__lede">
        Each card is one <strong>rule</strong>: the permissions it gives, then where they apply, <strong>projects › environments › keys</strong>. Every word is
        defined in the Glossary.
      </p>
      <Panel id="access-members" title="Members">
        <p className="access-hint">
          {world.people.length} members, {world.rules.length} rules
        </p>
        <ul className="access-people">
          {world.people.map((person) => {
            const rules = world.rules.filter((r) => r.member === person.id);
            return (
              <li key={person.id} className="access-person">
                <div className="access-person__who">
                  <strong>
                    {person.name} {person.id === you ? <Badge>you</Badge> : null}
                  </strong>
                  <span className="mono">{person.handle}</span>
                  <span>{person.note}</span>
                </div>
                <div className="access-person__rules">
                  {rules.length > 0 ? (
                    <ul className="access-rule-list">
                      {rules.map((rule, i) => (
                        <RuleCard key={rule.id} world={world} rule={rule} name={`rule ${i + 1} of ${person.name}`} onEdit={() => setEditing(rule)} />
                      ))}
                    </ul>
                  ) : (
                    <p className="access-hint">· no access</p>
                  )}
                  <Button type="button" variant="quiet" className="access-person__add" onClick={() => setEditing(newRule(person.id))}>
                    + Add rule<span className="visually-hidden"> for {person.name}</span>
                  </Button>
                </div>
              </li>
            );
          })}
        </ul>
      </Panel>
      {editing === null ? null : (
        <RuleEditorDialog
          key={editing.id}
          world={world}
          rule={editing}
          onCancel={() => setEditing(null)}
          onRemove={(id) => {
            setWorld(removeRule(world, id));
            setEditing(null);
          }}
          onSave={(rule) => {
            setWorld(saveRule(world, rule));
            setEditing(null);
          }}
        />
      )}
    </div>
  );
}

function RuleCard({ world, rule, name, onEdit }: { world: World; rule: Rule; name: string; onEdit: () => void }) {
  const preset = presetOf(rule);
  return (
    <li className="access-rule">
      <div className="access-rule__main">
        <div className="access-rule__perms">
          {preset === null ? null : <span className="access-hint">{preset}:</span>}
          {effective(rule).map((id) => (
            <PermBadge key={id} id={id} />
          ))}
        </div>
        <RuleWhere world={world} rule={rule} />
        <p className="access-rule__reach">{reachText(reachOf(world, rule))}</p>
      </div>
      <Button type="button" variant="quiet" onClick={onEdit}>
        Edit<span className="visually-hidden"> {name}</span>
      </Button>
    </li>
  );
}
