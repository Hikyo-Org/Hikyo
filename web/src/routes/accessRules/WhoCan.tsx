import { useId, useState, type ReactNode } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Disclosure } from '../../ui/Disclosure.tsx';
import { Glyph, type GlyphName } from '../../ui/Glyph.tsx';
import { Select } from '../../ui/Select.tsx';
import { label, perm, PERM_GROUPS, PERMS, projectById, resolve, type Person, type PermId, type Rule, type World } from './model.ts';
import { Lock, RulePerms, RuleWhere } from './parts.tsx';

export type Question = { readonly perm: PermId; readonly project: string; readonly env: string; readonly key: string };

/** Only permissions a rule may hold on a key are asked about one key. */
const ASKABLE = PERMS.filter((p) => p.shape !== 'project');

type Row = { person: Person; rule: Rule; note?: string };

/**
 * Who can...? over grants AND rules (ADR D10), in dependency order:
 * permission, project, environment, key (the project decides the other two).
 * Answered as tables in the Members page's anatomy, one per outcome: the
 * member, the deciding rule or grant, and for a rule an action that opens it
 * in the editor. People and machines both answer, machines marked. Members
 * nothing reaches appear in a collapsed outcome, alongside the other reasons.
 *
 * Key names come from the project's key catalogue, which needs See; without
 * it the question falls back to the whole environment and says so.
 */
export function WhoCan({
  world,
  projects,
  onEditRule,
}: {
  world: World;
  /** The projects this surface may ask about. */
  projects: readonly string[];
  /** Opens a rule in the editor; absent where rules cannot be edited from here. */
  onEditRule?: (rule: Rule) => void;
}) {
  const [asked, setAsked] = useState<Partial<Question>>({ perm: 'reveal' });
  const permId = asked.perm ?? 'reveal';
  const project = projects.includes(asked.project ?? '') ? (asked.project ?? '') : (projects[0] ?? '');
  const node = projectById(world, project);
  const envs = node?.envs ?? [];
  const keys = node?.keys ?? null;
  // A project switch keeps the question askable: a protected environment first, then the first one.
  const env = envs.some((e) => e.id === asked.env) ? (asked.env ?? '') : (envs.find((e) => e.protected === true)?.id ?? envs[0]?.id ?? '');
  const keyId = keys?.some((k) => k.id === asked.key) === true ? (asked.key ?? '') : (keys?.[0]?.id ?? '');
  const key = keys?.find((k) => k.id === keyId);
  const envNode = envs.find((e) => e.id === env);
  const permLabel = label(permId);
  const yes: Row[] = [];
  const excepted: Row[] = [];
  const needsSee: Row[] = [];
  const notReached: Person[] = [];
  if (envNode !== undefined) {
    for (const person of world.people) {
      const res = resolve(world, person.id, permId, project, env, key);
      if (res.state === 'yes') yes.push({ person, rule: res.rule, note: res.also === undefined ? undefined : `Another of their rules has ${res.also.why}, but an except only narrows its own rule.` });
      else if (res.state === 'excepted') excepted.push({ person, rule: res.rule, note: `Left out: this rule ${res.why.startsWith('except') ? 'has' : 'is'} ${res.why}.` });
      else if (res.state === 'needsSee') needsSee.push({ person, rule: res.rule, note: `Gives ${permLabel}, but showing a secret needs See here too.` });
      else notReached.push(person);
    }
  }

  return (
    <>
      <div className="access-ask">
        <Select
          label="Permission"
          hint={perm(permId).desc}
          value={permId}
          onChange={(e) => {
            const next = ASKABLE.find((p) => p.id === e.target.value);
            if (next !== undefined) setAsked({ ...asked, perm: next.id });
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
        <Select label="Project" value={project} onChange={(e) => setAsked({ ...asked, project: e.target.value })}>
          {projects.map((p) => (
            <option key={p} value={p}>
              {projectById(world, p)?.name ?? p}
            </option>
          ))}
        </Select>
        <Select label="Environment" value={env} onChange={(e) => setAsked({ ...asked, env: e.target.value })}>
          {envs.map((e) => (
            <option key={e.id} value={e.id}>
              {e.name}
              {e.protected === true ? ' (protected)' : ''}
            </option>
          ))}
        </Select>
        <Select
          label="Key"
          mono
          value={keyId}
          disabled={keys === null}
          hint={keys === null ? 'Key names need See in this project, which you do not hold: answered for the whole environment.' : undefined}
          onChange={(e) => setAsked({ ...asked, key: e.target.value })}
        >
          {keys === null ? <option value="">(every key)</option> : null}
          {[...new Set((keys ?? []).map((k) => k.folder))].sort().map((folder) => (
            <optgroup key={folder} label={folder === '' ? '(no folder)' : `${folder}/`}>
              {(keys ?? [])
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

      {envNode === undefined ? (
        <p className="access-hint" role="status">
          {projects.length === 0 ? 'No project to ask about yet.' : 'This project has no environment to ask about yet.'}
        </p>
      ) : (
        <>
          <p className="access-answer__question" aria-live="polite">
            Who has <strong>{permLabel}</strong> on <strong className="mono">{key?.name ?? 'every key'}</strong>
            {key?.secret === true ? <Lock word="secret" /> : null} in{' '}
            <strong>
              {node?.name ?? project} {envNode.name}
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
          <Disclosure label={`No, no rule reaches: ${notReached.length}`}>
            {notReached.length === 0 ? <p className="access-hint">· no one</p> : (
              <ul>
                {notReached.map((person) => (
                  <li key={person.id}>
                    <strong>{person.name}</strong>{person.kind === 'machine' ? <> <Badge tone="changed">Machine</Badge></> : null}
                    <p className="access-hint">None of their rules or scope-wide access gives {permLabel} here.</p>
                  </li>
                ))}
              </ul>
            )}
          </Disclosure>
        </>
      )}
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
  onEditRule: ((rule: Rule) => void) | undefined;
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
                <RulePerms rule={rule} />
              </td>
              <td>
                <RuleWhere world={world} rule={rule} />
                {note === undefined ? null : <p className="access-table__note">{note}</p>}
                {rule.source.kind === 'grant' ? <p className="access-table__note">Scope-wide access: edit its card in Members.</p> : null}
              </td>
              <td>
                {onEditRule !== undefined && rule.source.kind === 'rule' && !rule.source.otherProjects ? (
                  <Button type="button" variant="quiet" onClick={() => onEditRule(rule)}>
                    Edit rule<span className="visually-hidden"> of {person.name}</span>
                  </Button>
                ) : null}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
