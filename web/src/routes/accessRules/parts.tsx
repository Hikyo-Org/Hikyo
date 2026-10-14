import { Fragment, type ReactNode } from 'react';

import { Badge } from '../../ui/Badge.tsx';
import { Glyph, type GlyphName } from '../../ui/Glyph.tsx';
import { dotted } from '../dotted.tsx';
import { effective, envById, itemLabel, mergeByName, perm, presetOf, projectName, reachOf, reachText, type EnvItem, type KeyItem, type PermId, type Rule, type World } from './model.ts';

/** The icon on each Values permission's tag. */
const VALUE_ICON: Partial<Record<PermId, GlyphName>> = { read: 'reveal', edit: 'edit', publish: 'arrow-up', pin: 'pin' };

/** A permission as a badge: Secrets carry the lock and the danger tone, Administration the slate one. */
export function PermBadge({ id }: { id: PermId }) {
  const { label, group } = perm(id);
  if (group === 'Secrets') {
    return (
      <Badge tone="danger" icon="lock">
        {label}
      </Badge>
    );
  }
  return group === 'Administration'
    ? <Badge tone="changed" icon="settings">{label}</Badge>
    : <Badge icon={VALUE_ICON[id]}>{label}</Badge>;
}

/** A service account among people: one tag wherever a member list marks it. */
export function MachineBadge() {
  return <Badge icon="workload">machine</Badge>;
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

type Labels = { label: string; flag: boolean }[];

const envLabels = (world: World, rule: Rule, items: readonly EnvItem[]): Labels =>
  mergeByName(
    items.map((item) => {
      const env = envById(world, item.environment);
      return { project: item.project, name: env?.name ?? item.environment, flag: env?.protected === true };
    }),
    ruleProjectIds(world, rule),
    (id) => projectName(world, id),
  );

const keyLabels = (world: World, rule: Rule, items: readonly KeyItem[]): Labels =>
  mergeByName(
    items.map((item) => {
      const { text, secret } = itemLabel(world, item);
      return { project: item.project, name: text, flag: secret };
    }),
    ruleProjectIds(world, rule),
    (id) => projectName(world, id),
  );

/** The list as plain text, the lock spelled as its word, as a screen reader hears it. */
const labelsText = (labels: Labels, word: 'protected' | 'secret') =>
  labels.map((item) => (item.flag ? `${item.label} (${word})` : item.label)).join(', ');

function LabelList({ labels, word }: { labels: Labels; word: 'protected' | 'secret' }) {
  return joined(
    labels.map((item) => (
      <Fragment key={item.label}>
        {item.label}
        {item.flag ? <Lock word={word} /> : null}
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

/** The value may ellipsise in its column; `title` carries its whole text. */
function WherePart({ label, title, children }: { label: string; title: string; children: ReactNode }) {
  return (
    <span className="access-where__part">
      <span className="access-where__label">{label}</span>
      <span className="access-where__value" title={title}>
        {children}
      </span>
    </span>
  );
}

/**
 * Where, as labelled parts: Projects, Environments, Keys, each with its
 * value in the value face. Not arrows: nothing here is a link or a path to
 * follow. Three fixed columns, so the parts align across cards.
 */
export function RuleWhere({ world, rule }: { world: World; rule: Rule }) {
  const { envs, keys } = rule;
  const projects = rule.projects === '*' ? 'all' : rule.projects.map((p) => projectName(world, p)).join(', ');
  const envList = envLabels(world, rule, envs.items);
  const keyList = keyLabels(world, rule, keys.items);
  const envText = labelsText(envList, 'protected');
  const keyText = labelsText(keyList, 'secret');
  return (
    <p className="access-where">
      <WherePart label="Projects" title={projects}>{projects}</WherePart>
      <WherePart
        label="Environments"
        title={envs.mode === 'all' ? (envList.length > 0 ? `all except ${envText}` : 'all') : envText}
      >
        {envs.mode === 'all' ? (
          <>
            all{envList.length > 0 ? ' ' : null}
            {envList.length > 0 ? (
              <Except>
                <LabelList labels={envList} word="protected" />
              </Except>
            ) : null}
          </>
        ) : (
          <LabelList labels={envList} word="protected" />
        )}
      </WherePart>
      <WherePart
        label="Keys"
        title={keys.mode === 'all' ? (keyList.length > 0 ? `all except ${keyText}` : 'all') : `only ${keyText}`}
      >
        {keys.mode === 'all' ? (
          <>
            all{keyList.length > 0 ? ' ' : null}
            {keyList.length > 0 ? (
              <Except>
                <LabelList labels={keyList} word="secret" />
              </Except>
            ) : null}
          </>
        ) : (
          <>
            only <LabelList labels={keyList} word="secret" />
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
      {reach ? (
        <p className="access-rule__reach">{dotted(reachText(reachOf(world, rule)))}</p>
      ) : null}
    </div>
  );
}
