import { useState } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Input } from '../../ui/Input.tsx';
import { Panel } from '../Sections.tsx';
import { removeRule, saveRule, type Person, type Rule, type World } from './model.ts';
import { RuleSummary } from './parts.tsx';
import { newRule, RuleEditorDialog } from './RuleEditorDialog.tsx';

const matches = (person: Person, query: string) => {
  const q = query.trim().toLowerCase();
  return q === '' || [person.name, person.handle, person.note].some((field) => field.toLowerCase().includes(q));
};

/**
 * The Members page of the member-access prototype: people only, found by
 * name, handle or note (machines and their rules belong to Machine access).
 * Each person lists one card per rule. Edits go through {@link RuleEditorDialog} and land in local
 * state; nothing is saved anywhere.
 */
export function AccessRulesMembers({ initialWorld, you = 'marc' }: { initialWorld: World; you?: string }) {
  const [world, setWorld] = useState(initialWorld);
  const [editing, setEditing] = useState<Rule | null>(null);
  const [query, setQuery] = useState('');
  // Machines live in Machine access, which owns their credentials and their rules.
  const people = world.people.filter((p) => p.kind === 'person');
  const found = people.filter((p) => matches(p, query));
  const ruleCount = world.rules.filter((r) => people.some((p) => p.id === r.member)).length;

  return (
    <div className="page page--chrome access-rules">
      <h1>Members · acme</h1>
      <p className="page__lede">
        Each card is one <strong>rule</strong>: the permissions it gives, then where they apply, <strong>projects › environments › keys</strong>. Every word is
        defined in the Glossary.
      </p>
      <div className="access-filter">
        <Input label="Find a person" type="search" hint="By name, handle or note." value={query} onChange={(e) => setQuery(e.target.value)} />
      </div>
      <Panel id="access-people" title="Members">
        <p className="access-hint">
          {people.length} people, {ruleCount} rules. Service accounts are managed in Machine access.
        </p>
        {found.length === 0 ? (
          <p className="access-hint">· no one matches “{query.trim()}”</p>
        ) : (
          <ul className="access-people">
            {found.map((person) => (
              <MemberRow key={person.id} world={world} person={person} you={person.id === you} onEdit={setEditing} />
            ))}
          </ul>
        )}
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

function MemberRow({ world, person, you, onEdit }: { world: World; person: Person; you: boolean; onEdit: (rule: Rule) => void }) {
  const rules = world.rules.filter((r) => r.member === person.id);
  return (
    <li className="access-person">
      <div className="access-person__who">
        <strong>
          {person.name} {you ? <Badge>you</Badge> : null}
        </strong>
        <span className="mono">{person.handle}</span>
        <span>{person.note}</span>
      </div>
      <div className="access-person__rules">
        {rules.length > 0 ? (
          <ul className="access-rule-list">
            {rules.map((rule, i) => (
              <li key={rule.id} className="access-rule">
                <RuleSummary world={world} rule={rule} reach />
                <Button type="button" variant="quiet" onClick={() => onEdit(rule)}>
                  Edit<span className="visually-hidden"> rule {i + 1} of {person.name}</span>
                </Button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="access-hint">· no access</p>
        )}
        <Button type="button" variant="quiet" className="access-person__action" onClick={() => onEdit(newRule(person.id))}>
          + Add rule<span className="visually-hidden"> for {person.name}</span>
        </Button>
      </div>
    </li>
  );
}
