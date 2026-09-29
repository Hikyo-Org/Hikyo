import { Fragment, type ReactNode } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Glyph } from '../../ui/Glyph.tsx';
import { effective, envById, itemLabel, mergeByName, perm, presetOf, projectName, reachOf, reachText, type EnvItem, type KeyItem, type PermId, type Rule, type World } from './model.ts';

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

const ruleProjectIds = (world: World, rule: Rule) => (rule.projects === '*' ? world.projects.map((p) => p.id) : rule.projects);

function EnvList({ world, rule, items }: { world: World; rule: Rule; items: readonly EnvItem[] }) {
  const labels = mergeByName(
    items.map((item) => {
      const env = envById(world, item.environment);
      return { project: item.project, name: env?.name ?? item.environment, flag: env?.protected === true };
    }),
    ruleProjectIds(world, rule),
    (id) => projectName(world, id),
  );
  return joined(
    labels.map((env) => (
      <Fragment key={env.label}>
        {env.label}
        {env.flag ? <Lock word="protected" /> : null}
      </Fragment>
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

function KeyList({ world, rule, items }: { world: World; rule: Rule; items: readonly KeyItem[] }) {
  const labels = mergeByName(
    items.map((item) => {
      const { text, secret } = itemLabel(world, item);
      return { project: item.project, name: text, flag: secret };
    }),
    ruleProjectIds(world, rule),
    (id) => projectName(world, id),
  );
  return joined(
    labels.map((key) => (
      <Fragment key={key.label}>
        {key.label}
        {key.flag ? <Lock word="secret" /> : null}
      </Fragment>
    )),
  );
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
                <EnvList world={world} rule={rule} items={envs.items} />
              </Except>
            ) : null}
          </>
        ) : (
          <EnvList world={world} rule={rule} items={envs.items} />
        )}
      </WherePart>
      <WherePart label="Keys">
        {keys.mode === 'all' ? (
          <>
            all{keys.items.length > 0 ? ' ' : null}
            {keys.items.length > 0 ? (
              <Except>
                <KeyList world={world} rule={rule} items={keys.items} />
              </Except>
            ) : null}
          </>
        ) : (
          <>
            only <KeyList world={world} rule={rule} items={keys.items} />
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
