import { useId, useState, type ReactNode } from 'react';

import { Button } from '../../ui/Button.tsx';
import { Glyph, type GlyphName } from '../../ui/Glyph.tsx';
import { Select } from '../../ui/Select.tsx';
import { Panel } from '../Sections.tsx';
import { DEFAULT_QUESTION } from './fixture.ts';
import { keyById, label, perm, PERMS, projectEnvs, projectKeys, resolve, ruleText, type PermId, type World } from './model.ts';
import { DefinitionsSimulator } from './DefinitionsSimulator.tsx';

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
 * Who can...? One question as a sentence, answered per member and grouped by
 * why: yes, left out by an except, has Reveal but not See, or no rule reaches.
 */
export function WhoCan({ world, initial = DEFAULT_QUESTION }: { world: World; initial?: Question }) {
  const [q, setQ] = useState(initial);
  const envs = projectEnvs(world, q.project);
  const keys = projectKeys(world, q.project);
  // A project switch keeps the question askable: fall back to its first environment and key.
  const env = envs.some((e) => e.id === q.env) ? q.env : (envs[0]?.id ?? '');
  const key = keys.some((k) => k.id === q.key) ? q.key : (keys[0]?.id ?? '');
  const answers = world.people.map((person) => ({ person, res: resolve(world, person.id, q.perm, q.project, env, key) }));
  const yes = answers.flatMap(({ person, res }) => (res.state === 'yes' ? [{ person, res }] : []));
  const excepted = answers.flatMap(({ person, res }) => (res.state === 'excepted' ? [{ person, res }] : []));
  const needsSee = answers.flatMap(({ person, res }) => (res.state === 'needsSee' ? [{ person, res }] : []));
  const no = answers.filter(({ res }) => res.state === 'no');
  const permLabel = label(q.perm);

  return (
    <Panel id="access-who-can" title="Question">
      <div className="access-ask">
        <span>Who has</span>
        <Select
          label="Permission"
          value={q.perm}
          onChange={(e) => {
            const next = ASKABLE.find((p) => p.id === e.target.value);
            if (next !== undefined) setQ({ ...q, perm: next.id });
          }}
        >
          {ASKABLE.map((p) => (
            <option key={p.id} value={p.id}>
              {p.label}
            </option>
          ))}
        </Select>
        <span>on</span>
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
        <span>in</span>
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
        <span>?</span>
      </div>
      <p className="access-hint">
        <strong>{permLabel}</strong>: {perm(q.perm).desc}
      </p>
      <div className="access-examples" role="group" aria-label="Example questions">
        {EXAMPLES.map((example) => (
          <Button key={`${example.perm}|${example.env}|${example.key}`} type="button" variant="quiet" onClick={() => setQ(example)}>
            {label(example.perm)} · {example.env} {keyById(world, example.key)?.name ?? example.key}
          </Button>
        ))}
      </div>

      <AnswerGroup title={`Yes: ${yes.length}`}>
        {yes.length === 0 ? <li className="access-hint">· no one</li> : null}
        {yes.map(({ person, res }) => (
          <Answer key={person.id} glyph="check" tone="yes" name={person.name}>
            rule: <code>{ruleText(world, res.rule)}</code>
            {res.also === undefined ? null : (
              <>
                . Another of their rules has <code>{res.also.why}</code>, but an except only narrows its own rule.
              </>
            )}
          </Answer>
        ))}
      </AnswerGroup>
      {excepted.length > 0 ? (
        <AnswerGroup title={`No, left out by an except: ${excepted.length}`}>
          {excepted.map(({ person, res }) => (
            <Answer key={person.id} glyph="cross" tone="except" name={person.name}>
              rule <code>{ruleText(world, res.rule)}</code> reaches here, but has <code>{res.why}</code>
            </Answer>
          ))}
        </AnswerGroup>
      ) : null}
      {needsSee.length > 0 ? (
        <AnswerGroup title={`No, has ${permLabel} but not See: ${needsSee.length}`}>
          {needsSee.map(({ person, res }) => (
            <Answer key={person.id} glyph="warn" tone="warn" name={person.name}>
              rule <code>{ruleText(world, res.rule)}</code> gives {permLabel}, but showing a secret needs See here too
            </Answer>
          ))}
        </AnswerGroup>
      ) : null}
      <details className="access-noreach">
        <summary>No, no rule reaches: {no.length}</summary>
        <ul className="access-answer" aria-label={`No, no rule reaches: ${no.length}`}>
          {no.map(({ person }) => (
            <Answer key={person.id} glyph="ellipsis" tone="none" name={person.name}>
              none of their rules gives this here
            </Answer>
          ))}
        </ul>
      </details>
    </Panel>
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

function Answer({ glyph, tone, name, children }: { glyph: GlyphName; tone: 'yes' | 'except' | 'warn' | 'none'; name: string; children: ReactNode }) {
  return (
    <li>
      <Glyph name={glyph} className={`access-answer__glyph access-answer__glyph--${tone}`} />
      <strong>{name}</strong>
      <span className="access-answer__why">{children}</span>
    </li>
  );
}

/** The Who can...? page: the question, and the definitions simulator that changes what it answers. */
export function WhoCanPage({ initialWorld, initialQuestion }: { initialWorld: World; initialQuestion?: Question }) {
  const [world, setWorld] = useState(initialWorld);
  return (
    <div className="page page--chrome access-rules">
      <h1>Who can…?</h1>
      <p className="page__lede">Ask about one key in one environment. Permission names are the same as on the Members page.</p>
      <WhoCan world={world} initial={initialQuestion} />
      <DefinitionsSimulator world={world} onChange={setWorld} />
    </div>
  );
}
