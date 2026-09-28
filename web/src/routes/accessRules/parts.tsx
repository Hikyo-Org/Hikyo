import type { ReactNode } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Glyph } from '../../ui/Glyph.tsx';
import { isProtectedName, itemLabel, perm, projectsText, type ItemState, type PermId, type Rule, type World } from './model.ts';

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

/** Where, as the rule card shows it: projects › environments › keys, excepts marked. */
export function RuleWhere({ world, rule }: { world: World; rule: Rule }) {
  const { envs, keys } = rule;
  return (
    <p className="access-where">
      <span>{projectsText(rule)}</span>
      <span className="access-where__sep" aria-hidden="true">›</span>
      {envs.mode === 'all' ? (
        <>
          <span>all environments</span>
          {envs.exc.length > 0 ? <Except>{joined(envs.exc.map((e) => <EnvName key={e} world={world} name={e} />))}</Except> : null}
        </>
      ) : (
        <span>{joined(envs.list.map((e) => <EnvName key={e} world={world} name={e} />))}</span>
      )}
      <span className="access-where__sep" aria-hidden="true">›</span>
      {keys.mode === 'all' ? (
        <>
          <span>all keys</span>
          {keys.exc.length > 0 ? <Except>{joined(keys.exc.map((i) => <KeyItem key={i} world={world} item={i} />))}</Except> : null}
        </>
      ) : (
        <span>only {joined(keys.list.map((i) => <KeyItem key={i} world={world} item={i} />))}</span>
      )}
    </p>
  );
}

const STATE_WORD: Record<ItemState, string> = {
  included: 'included',
  implied: 'included',
  excepted: 'left out',
  off: 'not included',
};

/**
 * One tappable Where item. Pressed means the tap is in effect: included under
 * "Only...", left out under "All, except...". The state is named in text as
 * well as by the glyph and the tone.
 */
export function Pick({ state, disabled, onToggle, children }: { state: ItemState; disabled?: boolean; onToggle: () => void; children: ReactNode }) {
  return (
    <Button
      type="button"
      className="access-pick"
      data-state={state}
      aria-pressed={state === 'included' || state === 'excepted'}
      disabled={disabled}
      onClick={onToggle}
    >
      {state === 'off' ? <span aria-hidden="true">+</span> : <Glyph name={state === 'excepted' ? 'cross' : 'check'} />}
      <span>{children}</span>
      <span className="visually-hidden">, {STATE_WORD[state]}</span>
    </Button>
  );
}
