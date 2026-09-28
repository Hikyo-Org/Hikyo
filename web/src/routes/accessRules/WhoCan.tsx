import { useId, useState, type ReactNode } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Glyph, type GlyphName } from '../../ui/Glyph.tsx';
import { Select } from '../../ui/Select.tsx';
import { Panel } from '../Sections.tsx';
import { DEFAULT_QUESTION } from './fixture.ts';
import { keyById, label, PERMS, projectEnvs, projectKeys, resolve, type Person, type PermId, type Rule, type World } from './model.ts';
import { Lock, RuleSummary } from './parts.tsx';
import { PermissionList } from './PermissionList.tsx';

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

/**
 * Who can...? A plain form in dependency order (permission, then project,
 * which decides the environments and keys), answered per member and grouped
 * by why: yes, left out by an except, has Reveal but not See, or no rule
 * reaches. Each answer shows the deciding rule exactly as the Members card does.
 */
export function WhoCan({ world, initial = DEFAULT_QUESTION }: { world: World; initial?: Question }) {
  const [q, setQ] = useState(initial);
  const envs = projectEnvs(world, q.project);
  const keys = projectKeys(world, q.project);
  // A project switch keeps the question askable: fall back to its first environment and key.
  const env = envs.some((e) => e.id === q.env) ? q.env : (envs[0]?.id ?? '');
  const key = keys.some((k) => k.id === q.key) ? q.key : (keys[0]?.id ?? '');
  const asked = keyById(world, key);
  const answers = world.people.map((person) => ({ person, res: resolve(world, person.id, q.perm, q.project, env, key) }));
  const yes = answers.flatMap(({ person, res }) => (res.state === 'yes' ? [{ person, res }] : []));
  const excepted = answers.flatMap(({ person, res }) => (res.state === 'excepted' ? [{ person, res }] : []));
  const needsSee = answers.flatMap(({ person, res }) => (res.state === 'needsSee' ? [{ person, res }] : []));
  const no = answers.filter(({ res }) => res.state === 'no');
  const permLabel = label(q.perm);

  return (
    <>
      <Panel id="access-question" title="Question">
        <PermissionList mode="single" perms={ASKABLE} selected={q.perm} onChange={(perm) => setQ({ ...q, perm })} />
        <div className="access-ask">
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
        <AnswerGroup title={`Yes: ${yes.length}`}>
          {yes.length === 0 ? <li className="access-hint">· no one</li> : null}
          {yes.map(({ person, res }) => (
            <Answer key={person.id} glyph="check" tone="yes" person={person} world={world} rule={res.rule}>
              {res.also === undefined ? null : <>Another of their rules has {res.also.why}, but an except only narrows its own rule.</>}
            </Answer>
          ))}
        </AnswerGroup>
        {excepted.length > 0 ? (
          <AnswerGroup title={`No, left out by an except: ${excepted.length}`}>
            {excepted.map(({ person, res }) => (
              <Answer key={person.id} glyph="cross" tone="except" person={person} world={world} rule={res.rule}>
                This rule reaches here, but has {res.why}.
              </Answer>
            ))}
          </AnswerGroup>
        ) : null}
        {needsSee.length > 0 ? (
          <AnswerGroup title={`No, has ${permLabel} but not See: ${needsSee.length}`}>
            {needsSee.map(({ person, res }) => (
              <Answer key={person.id} glyph="warn" tone="warn" person={person} world={world} rule={res.rule}>
                This rule gives {permLabel}, but showing a secret needs See here too.
              </Answer>
            ))}
          </AnswerGroup>
        ) : null}
        <details className="access-disclosure">
          <summary>No, no rule reaches: {no.length}</summary>
          <ul className="access-answer" aria-label={`No, no rule reaches: ${no.length}`}>
            {no.map(({ person }) => (
              <Answer key={person.id} glyph="ellipsis" tone="none" person={person} world={world}>
                None of their rules gives this here.
              </Answer>
            ))}
          </ul>
        </details>
      </Panel>
    </>
  );
}

function AnswerGroup({ title, children }: { title: string; children: ReactNode }) {
  const id = useId();
  return (
    <>
      <h3 id={id}>{title}</h3>
      <ul className="access-answer" aria-labelledby={id}>
        {children}
      </ul>
    </>
  );
}

function Answer({
  glyph,
  tone,
  person,
  world,
  rule,
  children,
}: {
  glyph: GlyphName;
  tone: 'yes' | 'except' | 'warn' | 'none';
  person: Person;
  world: World;
  rule?: Rule;
  children?: ReactNode;
}) {
  return (
    <li>
      <Glyph name={glyph} className={`access-answer__glyph access-answer__glyph--${tone}`} />
      <strong>
        {person.name} {person.kind === 'machine' ? <Badge tone="changed">Machine</Badge> : null}
      </strong>
      <div className="access-answer__why">
        {rule === undefined ? null : <RuleSummary world={world} rule={rule} />}
        {children === undefined || children === null ? null : <p>{children}</p>}
      </div>
    </li>
  );
}

/** The Who can...? page. */
export function WhoCanPage({ world, initialQuestion }: { world: World; initialQuestion?: Question }) {
  return (
    <div className="page page--chrome access-rules">
      <h1>Who can…?</h1>
      <p className="page__lede">Ask about one key in one environment. Permission names and their meaning are the same as in the rule editor.</p>
      <WhoCan world={world} initial={initialQuestion} />
    </div>
  );
}
