/**
 * The member access rules model (member-access-rules ADR), over the real
 * listings: the org's (or a project's) rules, its legacy grants, its topology
 * and whatever key catalogues the caller may read. Pure and framework-free:
 * every function takes the {@link World} it reads.
 *
 * The server stores one capability per rule; a person reads and edits a set of
 * them that share one Where (D1), so the web groups server rules by principal
 * and Where. Legacy grants join the same evaluation as rules with no key or
 * environment narrowing: an organisation grant reaches every project, a
 * project grant every environment of it, an environment grant that
 * environment, all keys.
 *
 * {@link reach} is a port of `domain.Rule.Reaches` (internal/domain/rule.go),
 * with the reason a rule falls short added for Who can...?. Keep them in step.
 */

import type { zRuleCapability } from '@hikyo/zod';
import type { z } from 'zod';

export type PermId = z.infer<typeof zRuleCapability>;

type PermGroup = 'Values' | 'Secrets' | 'Administration';

/**
 * The narrowest Where a permission can sit on, as the server's `ruleShapes`
 * holds it: `key` may be narrowed by environment and key, `env` by
 * environment only, `project` needs every environment and key of its projects.
 */
type PermShape = 'key' | 'env' | 'project';

export type Perm = {
  readonly id: PermId;
  readonly label: string;
  readonly desc: string;
  readonly group: PermGroup;
  readonly shape: PermShape;
};

/** The vocabulary: the single source for every permission word the screens use (D2). */
export const PERMS: readonly Perm[] = [
  { id: 'read', label: 'See', desc: 'Key names, descriptions, schemas, validation, and config (non-secret) values. Secret values stay masked.', group: 'Values', shape: 'env' },
  { id: 'edit', label: 'Edit', desc: 'Change values as a draft. A draft does nothing until someone publishes it.', group: 'Values', shape: 'key' },
  { id: 'publish', label: 'Publish', desc: 'Make drafts live, and roll back to an earlier revision.', group: 'Values', shape: 'env' },
  { id: 'pin', label: 'Pin', desc: 'Hold workloads on a specific revision of an environment.', group: 'Values', shape: 'env' },
  { id: 'reveal', label: 'Reveal', desc: 'Show current secret values. Asks you to confirm it is you first. Needs See as well.', group: 'Secrets', shape: 'key' },
  { id: 'reveal-history', label: 'Reveal history', desc: 'Show old (replaced) secret values. Needs See as well.', group: 'Secrets', shape: 'key' },
  { id: 'definitions-edit', label: 'Define keys', desc: "Add, rename, move and remove keys inside this rule's folders, and change their rules.", group: 'Administration', shape: 'key' },
  { id: 'manage-members', label: 'Manage access', desc: 'Give other members access, never wider than your own rule.', group: 'Administration', shape: 'key' },
  { id: 'manage-identities', label: 'Manage machines', desc: 'Service accounts and their credentials.', group: 'Administration', shape: 'project' },
  { id: 'manage-adapters', label: 'Manage deploys', desc: 'Deployment adapters and syncing to other systems.', group: 'Administration', shape: 'project' },
  { id: 'project-settings', label: 'Change settings', desc: 'Protected flag, confirm-it-is-you window, retention.', group: 'Administration', shape: 'project' },
];

export const PERM_GROUPS: readonly PermGroup[] = ['Values', 'Secrets', 'Administration'];

/**
 * What the vocabulary has but a rule can never carry, named on one line where
 * the permissions are chosen (DESIGN.md review rule 11). Manage projects needs
 * all projects, and a rule names its projects in this slice.
 */
export const NOT_ON_RULES = 'Manage projects needs all projects, which a rule cannot name yet: it stays an organisation grant.';

const PERM_BY_ID = new Map(PERMS.map((p) => [p.id, p]));

export function perm(id: PermId): Perm {
  const found = PERM_BY_ID.get(id);
  if (found === undefined) throw new Error(`unknown permission ${id}`);
  return found;
}

export const label = (id: PermId) => perm(id).label;

/** The permission a string names, or undefined for an atom outside the rule vocabulary. */
export const permOf = (id: string): PermId | undefined => PERMS.find((p) => p.id === id)?.id;

/** Why a permission of this shape cannot sit on a narrower rule. */
const SHAPE_WHY: Record<Exclude<PermShape, 'key'>, string> = {
  env: 'needs all keys of an environment',
  project: 'needs a whole project (all environments, all keys)',
};

/** What each non-key shape needs, as the glossary and the permission list word it. */
export const SHAPE_NEEDS: Record<Exclude<PermShape, 'key'>, string> = {
  env: 'all keys of an environment',
  project: 'a whole project',
};

/** Presets tick boxes. They are shortcuts, never stored. Admin never ticks a Secrets permission (D2). */
export const PRESETS: readonly { readonly name: string; readonly perms: readonly PermId[] }[] = [
  { name: 'Viewer', perms: ['read'] },
  { name: 'Editor', perms: ['read', 'edit'] },
  { name: 'Publisher', perms: ['read', 'edit', 'publish', 'pin'] },
  { name: 'Admin', perms: PERMS.filter((p) => p.group !== 'Secrets').map((p) => p.id) },
];

/* ---------- Where ---------- */

export type EnvItem = { readonly project: string; readonly environment: string };
/** A folder path (`''` is the catalogue root) or one key by its stable id. */
type FolderItem = { readonly project: string; readonly folder: string };
type SingleKeyItem = { readonly project: string; readonly key: string };
export type KeyItem = FolderItem | SingleKeyItem;
/** `all` reads the items as exceptions, `only` as the complete list. */
export type Axis<T> = { readonly mode: 'all' | 'only'; readonly items: readonly T[] };

export const ALL: Axis<never> = { mode: 'all', items: [] };

type RuleSource =
  /** Server rules, one per permission; `otherProjects` when a project listing hides part of the Where. */
  | { readonly kind: 'rule'; readonly parts: readonly { readonly perm: PermId; readonly id: string }[]; readonly otherProjects: boolean }
  | { readonly kind: 'grant' }
  | { readonly kind: 'draft' };

export type Rule = {
  readonly id: string;
  readonly member: string;
  readonly perms: readonly PermId[];
  /** `'*'` only for an organisation grant: a rule names its projects. */
  readonly projects: '*' | readonly string[];
  readonly envs: Axis<EnvItem>;
  readonly keys: Axis<KeyItem>;
  readonly source: RuleSource;
};

export type Key = { readonly id: string; readonly name: string; readonly folder: string; readonly secret: boolean };
/** `protected` is null when the caller could not read the environment's settings. */
type Env = { readonly id: string; readonly name: string; readonly protected: boolean | null };
/** `keys` is null when the caller may not read the project's key catalogue (it needs See). */
export type Project = { readonly id: string; readonly name: string; readonly envs: readonly Env[]; readonly keys: readonly Key[] | null };
type MemberKind = 'person' | 'machine';
export type Person = { readonly id: string; readonly kind: MemberKind; readonly name: string };

export type World = {
  readonly people: readonly Person[];
  readonly projects: readonly Project[];
  readonly rules: readonly Rule[];
};

/** Machine principal ids carry the `mch_` prefix; rules are for people only in this slice. */
export const kindOfId = (id: string): MemberKind => (id.startsWith('mch_') ? 'machine' : 'person');

export const personName = (world: World, id: string) => world.people.find((p) => p.id === id)?.name ?? id;
export const projectById = (world: World, id: string) => world.projects.find((p) => p.id === id);
export const projectName = (world: World, id: string) => projectById(world, id)?.name ?? id;
const projectsOf = (world: World, rule: Pick<Rule, 'projects'>): readonly string[] =>
  rule.projects === '*' ? world.projects.map((p) => p.id) : rule.projects;
export const envById = (world: World, id: string) => world.projects.flatMap((p) => p.envs).find((e) => e.id === id);
export const envName = (world: World, id: string) => envById(world, id)?.name ?? id;
const keyById = (world: World, id: string) => world.projects.flatMap((p) => p.keys ?? []).find((k) => k.id === id);

const isFolder = (item: KeyItem): item is FolderItem => 'folder' in item;

/** How a key axis item reads: `db/` for a folder, `stripe/STRIPE_SECRET_KEY` for a key, its id when the name is not readable. */
export function itemLabel(world: World, item: KeyItem): { text: string; secret: boolean } {
  if (isFolder(item)) return { text: item.folder === '' ? '(no folder)' : `${item.folder}/`, secret: false };
  const key = keyById(world, item.key);
  if (key === undefined) return { text: item.key, secret: false };
  return { text: key.folder === '' ? key.name : `${key.folder}/${key.name}`, secret: key.secret };
}

const narrowEnvs = (rule: Pick<Rule, 'envs'>) => rule.envs.mode === 'only' || rule.envs.items.length > 0;
export const narrowKeys = (rule: Pick<Rule, 'keys'>) => rule.keys.mode === 'only' || rule.keys.items.length > 0;

type Shaped = Pick<Rule, 'envs' | 'keys'>;

/** Can a rule of this Where carry this permission, and if not, why: exactly what the server refuses. */
export function availability(id: PermId, rule: Shaped): { ok: true } | { ok: false; why: string } {
  const { shape } = perm(id);
  if (shape === 'env' && narrowKeys(rule)) return { ok: false, why: `Not available here: ${SHAPE_WHY.env}` };
  if (shape === 'project' && (narrowKeys(rule) || narrowEnvs(rule))) return { ok: false, why: `Not available here: ${SHAPE_WHY.project}` };
  return { ok: true };
}

export const allowed = (id: PermId, rule: Shaped) => availability(id, rule).ok;

/**
 * The standing condition on a permission row, or undefined. It depends on the
 * permission only, never on the rule's Where, so the row keeps its height and
 * only flips whether the condition currently blocks.
 */
export function requirement(id: PermId): string | undefined {
  if (id === 'manage-members') return 'Saved on the rule, but gives nothing yet: grant Manage access on the project instead.';
  if (id === 'definitions-edit') return 'Takes effect in a project only where the rule covers all of its environments.';
  const { shape } = perm(id);
  return shape === 'key' ? undefined : `Only on rules that cover ${SHAPE_NEEDS[shape]}.`;
}

/** The ticked permissions this rule can actually carry, in vocabulary order. */
export const effective = (rule: Pick<Rule, 'perms' | 'envs' | 'keys'>): PermId[] =>
  PERMS.map((p) => p.id).filter((id) => rule.perms.includes(id) && allowed(id, rule));

/** The preset this rule matches exactly, after its shape drops what it cannot carry. */
export function presetOf(rule: Pick<Rule, 'perms' | 'envs' | 'keys'>): string | null {
  const eff = effective(rule).join();
  return PRESETS.find((preset) => PERMS.map((p) => p.id).filter((id) => preset.perms.includes(id) && allowed(id, rule)).join() === eff)?.name ?? null;
}

/* ---------- evaluation ---------- */

/**
 * The permissions some operation checks against ONE key, so a key-narrowed rule
 * can satisfy them: single-value reveal (read and reveal, but read is never
 * key-narrowed), staging one value (edit) and key create, rename, move and
 * delete (definitions-edit). Everything else (bulk reveal, history, pins, and
 * publishing, which is environment-wide in this slice) names no key and is out
 * of a key-narrowed rule's reach.
 */
const KEY_AWARE: ReadonlySet<PermId> = new Set(['reveal', 'edit', 'definitions-edit']);

/** The level the server evaluates a permission at: key operations need the whole project for Define keys. */
const atProject = (id: PermId) => id === 'definitions-edit' || perm(id).shape === 'project';

export type Reach = { hit: false } | { hit: true; ok: true } | { hit: true; ok: false; why: string };

const MISS: Reach = { hit: false };
const OK: Reach = { hit: true, ok: true };

/**
 * Does this rule give this permission here? `hit` with `ok: false` means it
 * would, but one of its own excepts (or its key limit) leaves this out. A
 * port of `domain.Rule.Reaches`: folder excepts cover subfolders, only-picks
 * match the folder exactly, Manage access on a rule is inert, and a
 * key-narrowed rule counts only for a permission checked against one key.
 */
export function reach(world: World, rule: Rule, id: PermId, project: string, env: string, key: Key | undefined): Reach {
  if (!effective(rule).includes(id)) return MISS;
  if (id === 'manage-members' && rule.source.kind !== 'grant') return MISS;
  if (!projectsOf(world, rule).includes(project)) return MISS;
  const envs = rule.envs.items.filter((e) => e.project === project).map((e) => e.environment);
  if (atProject(id)) {
    if (rule.envs.mode === 'only') return MISS;
    if (envs.length > 0) return { hit: true, ok: false, why: `except ${envs.map((e) => envName(world, e)).join(', ')}` };
  } else {
    const listed = envs.includes(env);
    if (rule.envs.mode === 'only' && !listed) return MISS;
    if (rule.envs.mode === 'all' && listed) return { hit: true, ok: false, why: `except ${envName(world, env)}` };
  }
  const items = rule.keys.items.filter((k) => k.project === project);
  if (rule.keys.mode === 'all' && items.length === 0) return OK;
  const matches = (item: KeyItem, k: Key) =>
    isFolder(item) ? item.folder === k.folder || (rule.keys.mode === 'all' && item.folder !== '' && k.folder.startsWith(`${item.folder}/`)) : item.key === k.id;
  const matched = key === undefined ? undefined : items.find((item) => matches(item, key));
  if (rule.keys.mode === 'only' && key !== undefined && matched === undefined) return MISS;
  if (key === undefined || !KEY_AWARE.has(id)) return { hit: true, ok: false, why: 'limited to some keys, and this is never checked for one key' };
  if (rule.keys.mode === 'only') return OK;
  return matched === undefined ? OK : { hit: true, ok: false, why: `except ${itemLabel(world, matched).text}` };
}

type Resolution =
  | { state: 'yes'; rule: Rule; also?: { rule: Rule; why: string } }
  | { state: 'excepted'; rule: Rule; why: string }
  | { state: 'needsSee'; rule: Rule }
  | { state: 'no' };

/** Who can? Grants and rules add up; showing a secret also needs See in the same environment (D6). */
export function resolve(world: World, member: string, id: PermId, project: string, env: string, key: Key | undefined): Resolution {
  let ok: Rule | undefined;
  let no: { rule: Rule; why: string } | undefined;
  for (const rule of world.rules.filter((r) => r.member === member)) {
    const r = reach(world, rule, id, project, env, key);
    if (!r.hit) continue;
    if (r.ok) ok ??= rule;
    else no ??= { rule, why: r.why };
  }
  if (ok !== undefined && (id === 'reveal' || id === 'reveal-history') && resolve(world, member, 'read', project, env, undefined).state !== 'yes') {
    return { state: 'needsSee', rule: ok };
  }
  if (ok !== undefined) return no === undefined ? { state: 'yes', rule: ok } : { state: 'yes', rule: ok, also: no };
  if (no !== undefined) return { state: 'excepted', ...no };
  return { state: 'no' };
}

type ReachSummary = {
  environments: number;
  /** Keys the rule reaches, or null when a key catalogue it names cannot be read. */
  keys: number | null;
  /** Secret values the rule can reveal, or null when it carries no Reveal or they cannot be counted. */
  secrets: number | null;
  protectedEnvs: number;
  /** Things created later that the rule picks up. */
  grows: 'projects' | 'environments' | null;
};

/** What a rule covers, counted. */
export function reachOf(world: World, rule: Rule): ReachSummary {
  const pairs = projectsOf(world, rule).flatMap((project) => {
    const envs = rule.envs.items.filter((e) => e.project === project).map((e) => e.environment);
    return (projectById(world, project)?.envs ?? [])
      .filter((e) => (rule.envs.mode === 'all' ? !envs.includes(e.id) : envs.includes(e.id)))
      .map((env) => ({ project, env }));
  });
  const readable = pairs.every(({ project }) => projectById(world, project)?.keys != null);
  const keys = new Set<string>();
  let secrets = 0;
  const probe: Rule = { ...rule, perms: ['edit'], source: { kind: 'draft' } };
  for (const { project, env } of pairs) {
    for (const key of projectById(world, project)?.keys ?? []) {
      const at = reach(world, probe, 'edit', project, env.id, key);
      if (at.hit && at.ok) keys.add(key.id);
      const shown = reach(world, rule, 'reveal', project, env.id, key);
      if (key.secret && shown.hit && shown.ok) secrets += 1;
    }
  }
  return {
    environments: pairs.length,
    keys: readable ? keys.size : null,
    secrets: readable && effective(rule).includes('reveal') ? secrets : null,
    protectedEnvs: pairs.filter((pair) => pair.env.protected === true).length,
    grows: rule.projects === '*' ? 'projects' : rule.envs.mode === 'all' ? 'environments' : null,
  };
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`;

export function reachText(r: ReachSummary): string {
  return [
    plural(r.environments, 'environment'),
    r.keys === null ? 'keys not readable' : plural(r.keys, 'key'),
    ...(r.secrets === null ? [] : [`can reveal ${r.secrets} secret values`]),
    r.protectedEnvs > 0 ? `${r.protectedEnvs} protected` : 'no protected environment',
    ...(r.grows === null ? [] : [`new ${r.grows} included`]),
  ].join(' · ');
}

/* ---------- from the listings ---------- */

type ServerRule = {
  readonly id: string;
  readonly principal_id: string;
  readonly capability: PermId;
  readonly other_projects: boolean;
  readonly where: {
    readonly projects: readonly string[];
    readonly environments: Axis<EnvItem>;
    readonly keys: { readonly mode: 'all' | 'only'; readonly items: readonly { readonly project: string; readonly folder?: string; readonly key?: string }[] };
  };
};

type ServerGrant = {
  readonly principal_id: string;
  readonly capability: string;
  readonly scope: { readonly org_id?: string; readonly project_id?: string; readonly environment_id?: string };
};

const keyItemOf = (item: ServerRule['where']['keys']['items'][number]): KeyItem =>
  item.key !== undefined ? { project: item.project, key: item.key } : { project: item.project, folder: item.folder ?? '' };

const itemKey = (item: EnvItem | KeyItem) => ('environment' in item ? `${item.project}|e|${item.environment}` : isFolder(item) ? `${item.project}|f|${item.folder}` : `${item.project}|k|${item.key}`);

/** A Where in one canonical spelling, so two rules with the same Where group together. */
function whereKey(rule: Pick<Rule, 'projects' | 'envs' | 'keys'>): string {
  const sorted = (items: readonly (EnvItem | KeyItem)[]) => items.map(itemKey).sort().join(',');
  const projects = rule.projects === '*' ? '*' : [...rule.projects].sort().join(',');
  return `${projects};${rule.envs.mode}:${sorted(rule.envs.items)};${rule.keys.mode}:${sorted(rule.keys.items)}`;
}

/** Server rules, one capability each, grouped into the rules a person reads: one per principal and Where (D1). */
export function rulesFromServer(items: readonly ServerRule[]): Rule[] {
  const groups = new Map<string, { rule: Rule; parts: { perm: PermId; id: string }[]; other: boolean }>();
  for (const item of items) {
    const base = {
      projects: item.where.projects,
      envs: item.where.environments,
      keys: { mode: item.where.keys.mode, items: item.where.keys.items.map(keyItemOf) },
    };
    const id = `${item.principal_id}#${whereKey(base)}`;
    const group = groups.get(id);
    if (group === undefined) {
      groups.set(id, { rule: { id, member: item.principal_id, perms: [], ...base, source: { kind: 'draft' } }, parts: [{ perm: item.capability, id: item.id }], other: item.other_projects });
    } else {
      group.parts.push({ perm: item.capability, id: item.id });
      group.other ||= item.other_projects;
    }
  }
  return [...groups.values()].map(({ rule, parts, other }) => ({
    ...rule,
    perms: PERMS.map((p) => p.id).filter((id) => parts.some((part) => part.perm === id)),
    source: { kind: 'rule', parts, otherProjects: other },
  }));
}

/**
 * Legacy grants as rules with no key narrowing, one per principal and scope.
 * Atoms outside the rule vocabulary (manage-projects, audit-read, ...) and
 * instance-scope lines do not take part.
 */
export function rulesFromGrants(grants: readonly ServerGrant[]): Rule[] {
  const groups = new Map<string, Rule>();
  for (const grant of grants) {
    const id = permOf(grant.capability);
    const { org_id: org, project_id: project, environment_id: env } = grant.scope;
    if (id === undefined || org === undefined) continue;
    const where: Pick<Rule, 'projects' | 'envs' | 'keys'> =
      project === undefined
        ? { projects: '*', envs: ALL, keys: ALL }
        : env === undefined
          ? { projects: [project], envs: ALL, keys: ALL }
          : { projects: [project], envs: { mode: 'only', items: [{ project, environment: env }] }, keys: ALL };
    const key = `grant:${grant.principal_id}#${whereKey(where)}`;
    const found = groups.get(key);
    groups.set(key, {
      id: key,
      member: grant.principal_id,
      perms: [...(found?.perms ?? []), id],
      ...where,
      source: { kind: 'grant' },
    });
  }
  return [...groups.values()];
}

/* ---------- editing ---------- */

/** A fresh rule for a person: See ticked, no Where yet. */
export const newRule = (member: string): Rule => ({ id: '', member, perms: ['read'], projects: [], envs: ALL, keys: ALL, source: { kind: 'draft' } });

export const hasWhere = (rule: Pick<Rule, 'projects' | 'envs' | 'keys'>) =>
  (rule.projects === '*' || rule.projects.length > 0) && (rule.envs.mode === 'all' || rule.envs.items.length > 0) && (rule.keys.mode === 'all' || rule.keys.items.length > 0);

export const setMode = <T>(axis: Axis<T>, mode: Axis<T>['mode']): Axis<T> => (axis.mode === mode ? axis : { mode, items: [] });

/** Are all of these items on the axis (left out under "All, except...", included under "Only...")? */
export const tapped = <T extends EnvItem | KeyItem>(axis: Axis<T>, items: readonly T[]) =>
  items.length > 0 && items.every((item) => axis.items.some((x) => itemKey(x) === itemKey(item)));

/** Tap a chip: its items all join the axis, or all leave it. */
export function toggleItems<T extends EnvItem | KeyItem>(axis: Axis<T>, items: readonly T[]): Axis<T> {
  const keys = new Set(items.map(itemKey));
  return tapped(axis, items)
    ? { mode: axis.mode, items: axis.items.filter((x) => !keys.has(itemKey(x))) }
    : { mode: axis.mode, items: [...axis.items.filter((x) => !keys.has(itemKey(x))), ...items] };
}

/** Toggle a project; its environment and key items leave with it (the server refuses items outside the projects). */
export function toggleProject(rule: Rule, project: string): Rule {
  const list = rule.projects === '*' ? [] : rule.projects;
  if (!list.includes(project)) return { ...rule, projects: [...list, project] };
  return {
    ...rule,
    projects: list.filter((p) => p !== project),
    envs: { mode: rule.envs.mode, items: rule.envs.items.filter((e) => e.project !== project) },
    keys: { mode: rule.keys.mode, items: rule.keys.items.filter((k) => k.project !== project) },
  };
}

/** Environments of these projects by name: same-named environments are picked together (D3), stored by id. */
export function envChoices(world: World, projects: readonly string[]): { name: string; items: EnvItem[]; protected: boolean }[] {
  const out = new Map<string, { name: string; items: EnvItem[]; protected: boolean }>();
  for (const project of projects) {
    for (const env of projectById(world, project)?.envs ?? []) {
      const choice = out.get(env.name) ?? { name: env.name, items: [], protected: false };
      choice.items.push({ project, environment: env.id });
      choice.protected ||= env.protected === true;
      out.set(env.name, choice);
    }
  }
  return [...out.values()];
}

/** Folders of these projects by path, from the readable catalogues and the rule's own items. */
export function folderChoices(world: World, projects: readonly string[], rule: Pick<Rule, 'keys'>): { folder: string; items: KeyItem[] }[] {
  const out = new Map<string, KeyItem[]>();
  const add = (project: string, folder: string) => {
    const items = out.get(folder) ?? [];
    if (!items.some((i) => i.project === project)) items.push({ project, folder });
    out.set(folder, items);
  };
  for (const project of projects) {
    for (const key of projectById(world, project)?.keys ?? []) add(project, key.folder);
  }
  for (const item of rule.keys.items) if (isFolder(item) && projects.includes(item.project)) add(item.project, item.folder);
  return [...out.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([folder, items]) => ({ folder, items }));
}

/** Single keys of these projects, from the readable catalogues and the rule's own items. */
export function keyChoices(world: World, projects: readonly string[], rule: Pick<Rule, 'keys'>): KeyItem[] {
  const fromCatalogue = projects.flatMap((project) => (projectById(world, project)?.keys ?? []).map((k): KeyItem => ({ project, key: k.id })));
  const fromRule = rule.keys.items.filter((i) => !isFolder(i) && projects.includes(i.project));
  return [...fromCatalogue, ...fromRule.filter((i) => !fromCatalogue.some((c) => itemKey(c) === itemKey(i)))];
}

/** The create body for one permission of a rule. */
export function createBody(rule: Rule, id: PermId) {
  if (rule.projects === '*') throw new Error('a rule names its projects');
  return {
    principal: rule.member,
    capability: id,
    where: {
      projects: [...rule.projects],
      environments: { mode: rule.envs.mode, items: rule.envs.items.map((e) => ({ project: e.project, environment: e.environment })) },
      keys: { mode: rule.keys.mode, items: rule.keys.items.map((k) => (isFolder(k) ? { project: k.project, folder: k.folder } : { project: k.project, key: k.key })) },
    },
  };
}

/**
 * What saving a draft over the rule it edits must do. With the Where
 * unchanged only the permissions that changed move (a duplicate create would
 * store a second row); with a new Where every permission is created fresh and
 * every old row revoked. Creates come first, so a refused create changes
 * nothing.
 */
export function savePlan(before: Rule | null, draft: Rule): { create: PermId[]; revoke: string[] } {
  const next = effective(draft);
  const parts = before?.source.kind === 'rule' ? before.source.parts : [];
  if (before === null || whereKey(before) !== whereKey(draft)) return { create: next, revoke: parts.map((p) => p.id) };
  return {
    create: next.filter((id) => !parts.some((p) => p.perm === id)),
    revoke: parts.filter((p) => !next.includes(p.perm)).map((p) => p.id),
  };
}
