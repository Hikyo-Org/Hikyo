import { useId, useState, type ReactNode } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Glyph, type GlyphName } from '../../ui/Glyph.tsx';
import { Select } from '../../ui/Select.tsx';
import { Panel } from '../Sections.tsx';
import { DEFAULT_QUESTION } from './fixture.ts';
import { keyById, label, perm, PERM_GROUPS, PERMS, projectEnvs, projectKeys, removeRule, resolve, saveRule, type Person, type PermId, type Rule, type World } from './model.ts';
import { Lock, RulePerms, RuleWhere } from './parts.tsx';
import { RuleEditorDialog } from './RuleEditorDialog.tsx';

type Question = { perm: PermId; project: string; env: string; key: string };

/** Only key-shaped permissions are asked about one key. */
const ASKABLE = PERMS.filter((p) => p.shape === 'key');

const EXAMPLES: readonly Question[] = [
  { perm: 'reveal', project: 'payments', env: 'prod', key: 'payments_k3' },
  { perm: 'definitions-edit', project: 'payments', env: 'prod', key: 'payments_k3' },
  { perm: 'manage-members', project: 'payments', env: 'staging', key: 'payments_k2' },
  { perm: 'read', project: 'payments', env: 'prod', key: 'payments_k7' },
  { perm: 'publish', project: 'payments', env: 'dev', key: 'payments_k1' },
];

type Row = { person: Person; rule: Rule; note?: string };

/**
 * Who can...? A compact form in dependency order (permission, project,
 * environment, key: the project decides the other two), answered as tables
 * like the Members page: one per outcome, each row the member, the deciding
 * rule's permissions and its Where, and an action that opens that rule in
 * the editor. People and machines both answer. Members no rule reaches are
 * not listed: the question is who CAN.
 */
export function WhoCan({ world, initial = DEFAULT_QUESTION, onEditRule }: { world: World; initial?: Question; onEditRule: (rule: Rule) => void }) {
  const [q, setQ] = useState(initial);
  const envs = projectEnvs(world, q.project);
  const keys = projectKeys(world, q.project);
  // A project switch keeps the question askable: fall back to its first environment and key.
  const env = envs.some((e) => e.id === q.env) ? q.env : (envs[0]?.id ?? '');
  const key = keys.some((k) => k.id === q.key) ? q.key : (keys[0]?.id ?? '');
  const asked = keyById(world, key);
  const permLabel = label(q.perm);
  const yes: Row[] = [];
  const excepted: Row[] = [];
  const needsSee: Row[] = [];
  for (const person of world.people) {
    const res = resolve(world, person.id, q.perm, q.project, env, key);
    if (res.state === 'yes') yes.push({ person, rule: res.rule, note: res.also === undefined ? undefined : `Another of their rules has ${res.also.why}, but an except only narrows its own rule.` });
    else if (res.state === 'excepted') excepted.push({ person, rule: res.rule, note: `Left out: this rule has ${res.why}.` });
    else if (res.state === 'needsSee') needsSee.push({ person, rule: res.rule, note: `Gives ${permLabel}, but showing a secret needs See here too.` });
  }

  return (
    <>
      <Panel id="access-question" title="Question">
        <div className="access-ask">
          <Select
            label="Permission"
            hint={perm(q.perm).desc}
            value={q.perm}
            onChange={(e) => {
              const next = ASKABLE.find((p) => p.id === e.target.value);
              if (next !== undefined) setQ({ ...q, perm: next.id });
            }}
          >
            {PERM_GROUPS.map((group) => (
              <optgroup key={group} label={group}>
                {ASKABLE.filter((p) => p.group === group).map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </optgroup>
            ))}
          </Select>
          <Select label="Project" value={q.project} onChange={(e) => setQ({ ...q, project: e.target.value })}>
            {world.projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.id}
              </option>
            ))}
          </Select>
          <Select label="Environment" value={env} onChange={(e) => setQ({ ...q, env: e.target.value })}>
            {envs.map((e) => (
              <option key={e.id} value={e.id}>
                {e.id}
                {e.protected ? ' (protected)' : ''}
              </option>
            ))}
          </Select>
          <Select label="Key" mono value={key} onChange={(e) => setQ({ ...q, key: e.target.value })}>
            {[...new Set(keys.map((k) => k.folder))].map((folder) => (
              <optgroup key={folder} label={`${folder}/`}>
                {keys
                  .filter((k) => k.folder === folder)
                  .map((k) => (
                    <option key={k.id} value={k.id}>
                      {k.name}
                      {k.secret ? ' (secret)' : ''}
                    </option>
                  ))}
              </optgroup>
            ))}
          </Select>
        </div>
        <div className="access-examples" role="group" aria-label="Example questions">
          {EXAMPLES.map((example) => (
            <Button key={`${example.perm}|${example.env}|${example.key}`} type="button" variant="quiet" onClick={() => setQ(example)}>
              {label(example.perm)} · {example.env} {keyById(world, example.key)?.name ?? example.key}
            </Button>
          ))}
        </div>
      </Panel>

      <Panel id="access-answer" title="Answer">
        <p className="access-answer__question" aria-live="polite">
          Who has <strong>{permLabel}</strong> on <strong className="mono">{asked?.name ?? key}</strong>
          {asked?.secret === true ? <Lock word="secret" /> : null} in{' '}
          <strong>
            {q.project} {env}
          </strong>
          ?
        </p>
        <AnswerTable glyph="check" tone="yes" caption={`Yes: ${yes.length}`} rows={yes} world={world} onEditRule={onEditRule} empty="· no one" />
        {excepted.length > 0 ? (
          <AnswerTable glyph="cross" tone="except" caption={`No, left out by an except: ${excepted.length}`} rows={excepted} world={world} onEditRule={onEditRule} />
        ) : null}
        {needsSee.length > 0 ? (
          <AnswerTable glyph="warn" tone="warn" caption={`No, has ${permLabel} but not See: ${needsSee.length}`} rows={needsSee} world={world} onEditRule={onEditRule} />
        ) : null}
      </Panel>
    </>
  );
}

/**
 * One outcome as a table, in the Members page's `.grants` anatomy: member,
 * the deciding rule's permissions, its Where (with the why as a second line).
 * The wrapper scrolls sideways on a phone, and is focusable so a keyboard can
 * scroll it too.
 */
function AnswerTable({
  glyph,
  tone,
  caption,
  rows,
  world,
  onEditRule,
  empty,
}: {
  glyph: GlyphName;
  tone: 'yes' | 'except' | 'warn';
  caption: string;
  rows: readonly Row[];
  world: World;
  onEditRule: (rule: Rule) => void;
  empty?: ReactNode;
}) {
  const captionId = useId();
  return (
    <div className="access-table" role="region" aria-labelledby={captionId} tabIndex={0}>
      <table className="grants">
        <caption id={captionId} className="access-table__caption">
          <Glyph name={glyph} className={`access-answer__glyph--${tone}`} /> {caption}
        </caption>
        <thead>
          <tr>
            <th scope="col">Member</th>
            <th scope="col">Permissions</th>
            <th scope="col">Where</th>
            <th scope="col">
              <span className="visually-hidden">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={4} className="access-hint">
                {empty}
              </td>
            </tr>
          ) : null}
          {rows.map(({ person, rule, note }) => (
            <tr key={person.id}>
              <th scope="row">
                <span className="access-table__member">
                  {person.name}
                  {person.kind === 'machine' ? <Badge tone="changed">Machine</Badge> : null}
                </span>
              </th>
              <td>
                <RulePerms world={world} rule={rule} />
              </td>
              <td>
                <RuleWhere world={world} rule={rule} />
                {note === undefined ? null : <p className="access-table__note">{note}</p>}
              </td>
              <td>
                <Button type="button" variant="quiet" onClick={() => onEditRule(rule)}>
                  Edit rule<span className="visually-hidden"> of {person.name}</span>
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * The Who can...? page. It owns the world so an edit made from an answer
 * (the same rule editor Members and Machine access use) recomputes the answer.
 */
export function WhoCanPage({ initialWorld, initialQuestion }: { initialWorld: World; initialQuestion?: Question }) {
  const [world, setWorld] = useState(initialWorld);
  const [editing, setEditing] = useState<Rule | null>(null);
  return (
    <div className="page page--chrome access-rules">
      <h1>Who can…?</h1>
      <p className="page__lede">Ask about one key in one environment. Permission names and their meaning are the same as in the rule editor.</p>
      <WhoCan world={world} initial={initialQuestion} onEditRule={setEditing} />
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
