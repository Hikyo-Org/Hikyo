import type { ReactNode } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Glyph } from '../../ui/Glyph.tsx';
import { effective, isProtectedName, itemLabel, kindOf, perm, presetOf, reachOf, reachText, type PermId, type Rule, type World } from './model.ts';

/** A permission as a badge: Secrets carry the lock and the danger tone, Administration the slate one. */
export function PermBadge({ id }: { id: PermId }) {
  const { label, group } = perm(id);
  if (group === 'Secrets') {
    return (
      <Badge tone="danger">
        <Glyph name="lock" />
        {label}
      </Badge>
    );
  }
  return <Badge tone={group === 'Administration' ? 'changed' : 'neutral'}>{label}</Badge>;
}

/** A lock after a name, with the word for assistive tech: never glyph-only. */
export function Lock({ word }: { word: 'protected' | 'secret' }) {
  return (
    <>
      {' '}
      <Glyph name="lock" />
      <span className="visually-hidden">({word})</span>
    </>
  );
}

export function EnvName({ world, name }: { world: World; name: string }) {
  return (
    <>
      {name}
      {isProtectedName(world, name) ? <Lock word="protected" /> : null}
    </>
  );
}

export function KeyItem({ world, item }: { world: World; item: string }) {
  const { text, secret } = itemLabel(world, item);
  return (
    <>
      {text}
      {secret ? <Lock word="secret" /> : null}
    </>
  );
}

const joined = (nodes: ReactNode[]) => nodes.map((node, i) => (i === 0 ? node : [', ', node]));

/** An except: danger ink and a cross, and the word "except" for a screen reader. */
function Except({ children }: { children: ReactNode }) {
  return (
    <span className="access-except">
      <Glyph name="cross" />
      <span className="visually-hidden">except </span>
      {children}
    </span>
  );
}

function WherePart({ label, children }: { label: string; children: ReactNode }) {
  return (
    <span className="access-where__part">
      <span className="access-where__label">{label}</span>
      <span className="access-where__value">{children}</span>
    </span>
  );
}

/**
 * Where, as labelled parts: Projects, Environments, Keys, each with its
 * value in the value face. Not arrows: nothing here is a link or a path to
 * follow. The parts wrap onto their own lines in a narrow cell.
 */
export function RuleWhere({ world, rule }: { world: World; rule: Rule }) {
  const { envs, keys } = rule;
  return (
    <p className="access-where">
      <WherePart label="Projects">{rule.projects === '*' ? 'all' : rule.projects.join(', ')}</WherePart>
      <WherePart label="Environments">
        {envs.mode === 'all' ? (
          <>
            all{envs.exc.length > 0 ? ' ' : null}
            {envs.exc.length > 0 ? <Except>{joined(envs.exc.map((e) => <EnvName key={e} world={world} name={e} />))}</Except> : null}
          </>
        ) : (
          joined(envs.list.map((e) => <EnvName key={e} world={world} name={e} />))
        )}
      </WherePart>
      <WherePart label="Keys">
        {keys.mode === 'all' ? (
          <>
            all{keys.exc.length > 0 ? ' ' : null}
            {keys.exc.length > 0 ? <Except>{joined(keys.exc.map((i) => <KeyItem key={i} world={world} item={i} />))}</Except> : null}
          </>
        ) : (
          <>only {joined(keys.list.map((i) => <KeyItem key={i} world={world} item={i} />))}</>
        )}
      </WherePart>
    </p>
  );
}

/** A rule's permissions as badges, led by the preset name when it matches one exactly. */
export function RulePerms({ world, rule }: { world: World; rule: Rule }) {
  const kind = kindOf(world, rule.member);
  const preset = presetOf(rule, kind);
  return (
    <div className="access-rule__perms">
      {preset === null ? null : <span className="access-hint">{preset}:</span>}
      {effective(rule, kind).map((id) => (
        <PermBadge key={id} id={id} />
      ))}
    </div>
  );
}

/**
 * One rule as the Members card and the rule editor's "Saves as" both show it:
 * the permissions it gives, then Where, then optionally what that reaches.
 */
export function RuleSummary({ world, rule, reach = false }: { world: World; rule: Rule; reach?: boolean }) {
  return (
    <div className="access-rule__main">
      <RulePerms world={world} rule={rule} />
      <RuleWhere world={world} rule={rule} />
      {reach ? <p className="access-rule__reach">{reachText(reachOf(world, rule))}</p> : null}
    </div>
  );
}
