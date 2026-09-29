import type { ReactNode } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Glyph } from '../../ui/Glyph.tsx';
import { effective, envById, itemLabel, perm, presetOf, projectName, reachOf, reachText, type EnvItem, type KeyItem, type PermId, type Rule, type World } from './model.ts';

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

/** Environment items by name: same-named environments of several projects read as one. */
function envNames(world: World, items: readonly EnvItem[]): { name: string; protected: boolean }[] {
  const out = new Map<string, boolean>();
  for (const item of items) {
    const env = envById(world, item.environment);
    const name = env?.name ?? item.environment;
    out.set(name, (out.get(name) ?? false) || env?.protected === true);
  }
  return [...out.entries()].map(([name, isProtected]) => ({ name, protected: isProtected }));
}

function EnvList({ world, items }: { world: World; items: readonly EnvItem[] }) {
  return joined(
    envNames(world, items).map((env) => (
      <span key={env.name}>
        {env.name}
        {env.protected ? <Lock word="protected" /> : null}
      </span>
    )),
  );
}

export function KeyLabel({ world, item }: { world: World; item: KeyItem }) {
  const { text, secret } = itemLabel(world, item);
  return (
    <>
      {text}
      {secret ? <Lock word="secret" /> : null}
    </>
  );
}

const keyOf = (item: KeyItem) => `${item.project}|${'folder' in item ? `f:${item.folder}` : `k:${item.key}`}`;

function KeyList({ world, items }: { world: World; items: readonly KeyItem[] }) {
  // A folder picked in several projects reads once.
  const seen = new Set<string>();
  const unique = items.filter((item) => {
    const text = itemLabel(world, item).text;
    if (seen.has(text)) return false;
    seen.add(text);
    return true;
  });
  return joined(unique.map((item) => <KeyLabel key={keyOf(item)} world={world} item={item} />));
}

const joined = (nodes: ReactNode[]) => <>{nodes.map((node, i) => (i === 0 ? node : [', ', node]))}</>;

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
      <WherePart label="Projects">{rule.projects === '*' ? 'all' : rule.projects.map((p) => projectName(world, p)).join(', ')}</WherePart>
      <WherePart label="Environments">
        {envs.mode === 'all' ? (
          <>
            all{envs.items.length > 0 ? ' ' : null}
            {envs.items.length > 0 ? (
              <Except>
                <EnvList world={world} items={envs.items} />
              </Except>
            ) : null}
          </>
        ) : (
          <EnvList world={world} items={envs.items} />
        )}
      </WherePart>
      <WherePart label="Keys">
        {keys.mode === 'all' ? (
          <>
            all{keys.items.length > 0 ? ' ' : null}
            {keys.items.length > 0 ? (
              <Except>
                <KeyList world={world} items={keys.items} />
              </Except>
            ) : null}
          </>
        ) : (
          <>
            only <KeyList world={world} items={keys.items} />
          </>
        )}
      </WherePart>
    </p>
  );
}

/** A rule's permissions as badges, led by the preset name when it matches one exactly. */
export function RulePerms({ rule }: { rule: Rule }) {
  const preset = presetOf(rule);
  return (
    <div className="access-rule__perms">
      {preset === null ? null : <span className="access-hint">{preset}:</span>}
      {effective(rule).map((id) => (
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
      <RulePerms rule={rule} />
      <RuleWhere world={world} rule={rule} />
      {reach ? <p className="access-rule__reach">{reachText(reachOf(world, rule))}</p> : null}
    </div>
  );
}
