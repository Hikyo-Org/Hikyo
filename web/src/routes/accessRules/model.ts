/**
 * The member-access rules model behind the "Prototypes/Member access" stories:
 * a port of docs/site/public/prototypes/member-access/6 (iteration 6). A design
 * prototype over fixture data, not wired to the API. Pure and framework-free:
 * every function takes the {@link World} it reads, and every change returns a
 * new World, so a story can hold one in `useState`.
 *
 * A rule is ticked permissions plus Where: projects, then environments, then
 * keys. Each axis is "All, except..." or "Only...". An except narrows its own
 * rule only; another rule of the same member can still reach the thing.
 */

export type PermGroup = 'Values' | 'Secrets' | 'Administration';

/**
 * The narrowest rule a permission can live on: `key` works on a folder or a
 * single key, `env` needs all keys of an environment, `project` a whole
 * project, `org` all projects.
 */
export type PermShape = 'key' | 'env' | 'project' | 'org';

export type PermId =
  | 'read'
  | 'edit'
  | 'publish'
  | 'pin'
  | 'reveal'
  | 'reveal-history'
  | 'definitions-edit'
  | 'manage-members'
  | 'manage-identities'
  | 'manage-adapters'
  | 'project-settings'
  | 'manage-projects';

export type Perm = {
  readonly id: PermId;
  readonly label: string;
  readonly desc: string;
  readonly group: PermGroup;
  readonly shape: PermShape;
};

/** The vocabulary: the single source for every permission word the screens use. */
export const PERMS: readonly Perm[] = [
  { id: 'read', label: 'See', desc: 'Key names, descriptions, schemas, validation, and config (non-secret) values. Secret values stay masked.', group: 'Values', shape: 'key' },
  { id: 'edit', label: 'Edit', desc: 'Change values as a draft. A draft does nothing until someone publishes it.', group: 'Values', shape: 'key' },
  { id: 'publish', label: 'Publish', desc: 'Make drafts live, and roll back to an earlier revision.', group: 'Values', shape: 'key' },
  { id: 'pin', label: 'Pin', desc: 'Hold workloads on a specific revision of an environment.', group: 'Values', shape: 'env' },
  { id: 'reveal', label: 'Reveal', desc: 'Show current secret values. Asks you to confirm it is you first. Needs See as well.', group: 'Secrets', shape: 'key' },
  { id: 'reveal-history', label: 'Reveal history', desc: 'Show old (replaced) secret values. Needs See as well.', group: 'Secrets', shape: 'key' },
  { id: 'definitions-edit', label: 'Define keys', desc: "Add, rename and remove keys inside this rule's folders, and change their rules. A new key gets values only in this rule's environments.", group: 'Administration', shape: 'key' },
  { id: 'manage-members', label: 'Manage access', desc: 'Give other members access, never wider than your own rule.', group: 'Administration', shape: 'key' },
  { id: 'manage-identities', label: 'Manage machines', desc: 'Service accounts and their credentials.', group: 'Administration', shape: 'project' },
  { id: 'manage-adapters', label: 'Manage deploys', desc: 'Deployment adapters and syncing to other systems.', group: 'Administration', shape: 'project' },
  { id: 'project-settings', label: 'Change settings', desc: 'Protected flag, confirm-it-is-you window, retention.', group: 'Administration', shape: 'project' },
  { id: 'manage-projects', label: 'Manage projects', desc: 'Create and delete projects.', group: 'Administration', shape: 'org' },
];

export const PERM_GROUPS: readonly PermGroup[] = ['Values', 'Secrets', 'Administration'];

const PERM_BY_ID = new Map(PERMS.map((perm) => [perm.id, perm]));

export function perm(id: PermId): Perm {
  const found = PERM_BY_ID.get(id);
  if (found === undefined) throw new Error(`unknown permission ${id}`);
  return found;
}

export const label = (id: PermId) => perm(id).label;

/** Why a permission of this shape cannot sit on a narrower rule. */
export const SHAPE_WHY: Record<Exclude<PermShape, 'key'>, string> = {
  env: 'needs all keys of an environment',
  project: 'needs a whole project (all environments, all keys)',
  org: 'needs all projects',
};

/** Presets tick boxes. They are shortcuts, never stored: the rule stores the ticked permissions. */
export const PRESETS: readonly { readonly name: string; readonly perms: readonly PermId[] }[] = [
  { name: 'Viewer', perms: ['read'] },
  { name: 'Editor', perms: ['read', 'edit'] },
  { name: 'Publisher', perms: ['read', 'edit', 'publish', 'pin'] },
  { name: 'Admin', perms: PERMS.filter((p) => p.group !== 'Secrets').map((p) => p.id) },
];

/**
 * One step of Where. Environment items are environment names; key items are
 * a folder name (`db`) or a single key by id (`#payments_k5`), see
 * {@link keyPick}.
 */
export type Axis = { readonly mode: 'all'; readonly exc: readonly string[] } | { readonly mode: 'only'; readonly list: readonly string[] };

export const ALL: Axis = { mode: 'all', exc: [] };

export type Rule = {
  readonly id: number;
  readonly member: string;
  readonly perms: readonly PermId[];
  readonly projects: '*' | readonly string[];
  readonly envs: Axis;
  readonly keys: Axis;
};

/** A key keeps its id through renames and moves; that is what a single-key pick follows. */
export type Key = { readonly id: string; readonly project: string; readonly name: string; readonly folder: string; readonly secret: boolean };
export type Env = { readonly id: string; readonly protected: boolean };
export type Project = { readonly id: string; readonly envs: readonly Env[] };
export type Person = { readonly id: string; readonly name: string; readonly handle: string; readonly note: string };

export type World = {
  readonly people: readonly Person[];
  readonly projects: readonly Project[];
  readonly keys: readonly Key[];
  readonly rules: readonly Rule[];
};

/** A key axis item naming one key by id rather than a folder. */
export const keyPick = (keyId: string) => `#${keyId}`;

export const personName = (world: World, id: string) => world.people.find((p) => p.id === id)?.name ?? id;
export const keyById = (world: World, id: string) => world.keys.find((k) => k.id === id);
export const projectKeys = (world: World, project: string) => world.keys.filter((k) => k.project === project);
export const projectEnvs = (world: World, project: string) => world.projects.find((p) => p.id === project)?.envs ?? [];
export const projectsOf = (world: World, rule: Pick<Rule, 'projects'>): readonly string[] =>
  rule.projects === '*' ? world.projects.map((p) => p.id) : rule.projects;

/** Environment names across projects: same-named environments are picked together. */
export const envNames = (world: World, projects: readonly string[]) => [...new Set(projects.flatMap((p) => projectEnvs(world, p).map((e) => e.id)))];
export const isProtectedName = (world: World, name: string) => world.projects.some((p) => p.envs.some((e) => e.id === name && e.protected));
export const folderNames = (world: World, projects: readonly string[]) => [...new Set(projects.flatMap((p) => projectKeys(world, p).map((k) => k.folder)))];

const keyMatch = (item: string, key: Key) => (item.startsWith('#') ? item.slice(1) === key.id : item === key.folder);
const axisNarrow = (axis: Axis) => !(axis.mode === 'all' && axis.exc.length === 0);
export const narrowKeys = (rule: Pick<Rule, 'keys'>) => axisNarrow(rule.keys);
export const narrowEnvs = (rule: Pick<Rule, 'envs'>) => axisNarrow(rule.envs);

/** How a key axis item reads: `db/` for a folder, `stripe/STRIPE_SECRET_KEY` for a single key. */
export function itemLabel(world: World, item: string): { text: string; secret: boolean } {
  if (!item.startsWith('#')) return { text: `${item}/`, secret: false };
  const key = keyById(world, item.slice(1));
  return key === undefined ? { text: '(deleted key)', secret: false } : { text: `${key.folder}/${key.name}`, secret: key.secret };
}

/** Is this permission possible on a rule of this shape? */
export function allowed(id: PermId, rule: Pick<Rule, 'projects' | 'envs' | 'keys'>): boolean {
  const { shape } = perm(id);
  if (shape === 'env') return !narrowKeys(rule);
  if (shape === 'project') return !narrowKeys(rule) && !narrowEnvs(rule);
  if (shape === 'org') return rule.projects === '*' && !narrowKeys(rule) && !narrowEnvs(rule);
  return true;
}

/** The ticked permissions this rule can actually carry, in vocabulary order. */
export const effective = (rule: Pick<Rule, 'perms' | 'projects' | 'envs' | 'keys'>): PermId[] =>
  PERMS.map((p) => p.id).filter((id) => rule.perms.includes(id) && allowed(id, rule));

/** The preset this rule matches exactly, after its shape drops what it cannot carry. */
export function presetOf(rule: Rule): string | null {
  const eff = effective(rule).join();
  const match = PRESETS.find((preset) => PERMS.map((p) => p.id).filter((id) => preset.perms.includes(id) && allowed(id, rule)).join() === eff);
  return match?.name ?? null;
}

export type Reach = { hit: false } | { hit: true; ok: true } | { hit: true; ok: false; why: string };

/**
 * Does this rule reach this permission here? `hit` with `ok: false` means the
 * rule would reach, but one of its own excepts leaves this out. See covers the
 * whole environment (key names and config values are environment-wide), so
 * key limits never narrow it; an environment except removes everything.
 */
export function ruleReaches(world: World, rule: Rule, id: PermId, project: string, env: string, keyId?: string): Reach {
  if (!effective(rule).includes(id)) return { hit: false };
  if (!projectsOf(world, rule).includes(project)) return { hit: false };
  if (rule.envs.mode === 'only' && !rule.envs.list.includes(env)) return { hit: false };
  if (rule.envs.mode === 'all' && rule.envs.exc.includes(env)) return { hit: true, ok: false, why: `except ${env}` };
  if (id === 'read' || keyId === undefined) return { hit: true, ok: true };
  const key = projectKeys(world, project).find((k) => k.id === keyId);
  if (key === undefined) return { hit: false };
  if (rule.keys.mode === 'only' && !rule.keys.list.some((item) => keyMatch(item, key))) return { hit: false };
  if (rule.keys.mode === 'all') {
    const except = rule.keys.exc.find((item) => keyMatch(item, key));
    if (except !== undefined) return { hit: true, ok: false, why: `except ${itemLabel(world, except).text}` };
  }
  return { hit: true, ok: true };
}

export type Resolution =
  | { state: 'yes'; rule: Rule; also?: { rule: Rule; why: string } }
  | { state: 'excepted'; rule: Rule; why: string }
  | { state: 'needsSee'; rule: Rule }
  | { state: 'no' };

/** Who can? Rules add up; a secret shown also needs See in the same environment. */
export function resolve(world: World, member: string, id: PermId, project: string, env: string, keyId?: string): Resolution {
  let ok: Rule | undefined;
  let no: { rule: Rule; why: string } | undefined;
  for (const rule of world.rules.filter((r) => r.member === member)) {
    const reach = ruleReaches(world, rule, id, project, env, keyId);
    if (!reach.hit) continue;
    if (reach.ok) ok ??= rule;
    else no ??= { rule, why: reach.why };
  }
  if (ok !== undefined && (id === 'reveal' || id === 'reveal-history') && resolve(world, member, 'read', project, env).state !== 'yes') {
    return { state: 'needsSee', rule: ok };
  }
  if (ok !== undefined) return no === undefined ? { state: 'yes', rule: ok } : { state: 'yes', rule: ok, also: no };
  if (no !== undefined) return { state: 'excepted', ...no };
  return { state: 'no' };
}

export type ReachSummary = {
  environments: number;
  keys: number;
  /** Secret values the rule can reveal, or null when it carries no Reveal. */
  secrets: number | null;
  protectedEnvs: number;
  /** Things created later that the rule picks up. */
  grows: 'projects' | 'environments' | null;
};

/** What a rule covers, counted. */
export function reachOf(world: World, rule: Rule): ReachSummary {
  const pairs = projectsOf(world, rule).flatMap((project) =>
    projectEnvs(world, project)
      .filter((e) => (rule.envs.mode === 'all' ? !rule.envs.exc.includes(e.id) : rule.envs.list.includes(e.id)))
      .map((e) => ({ project, env: e })),
  );
  const keys = new Set<string>();
  let secrets = 0;
  const probe: Rule = { ...rule, perms: ['edit'] };
  for (const { project, env } of pairs) {
    for (const key of projectKeys(world, project)) {
      const at = ruleReaches(world, probe, 'edit', project, env.id, key.id);
      if (at.hit && at.ok) keys.add(key.id);
      const reveal = ruleReaches(world, rule, 'reveal', project, env.id, key.id);
      if (key.secret && reveal.hit && reveal.ok) secrets += 1;
    }
  }
  return {
    environments: pairs.length,
    keys: keys.size,
    secrets: effective(rule).includes('reveal') ? secrets : null,
    protectedEnvs: pairs.filter((pair) => pair.env.protected).length,
    grows: rule.projects === '*' ? 'projects' : rule.envs.mode === 'all' ? 'environments' : null,
  };
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? '' : 's'}`;

export function reachText(reach: ReachSummary): string {
  return [
    plural(reach.environments, 'environment'),
    plural(reach.keys, 'key'),
    ...(reach.secrets === null ? [] : [`can reveal ${reach.secrets} secret values`]),
    reach.protectedEnvs > 0 ? `${reach.protectedEnvs} protected` : 'no protected environment',
    ...(reach.grows === null ? [] : [`new ${reach.grows} included`]),
  ].join(' · ');
}

export const projectsText = (rule: Pick<Rule, 'projects'>) => (rule.projects === '*' ? 'all projects' : rule.projects.join(', '));

/** Where, as one line of plain text (the Who can...? answers quote it). */
export function whereText(world: World, rule: Pick<Rule, 'projects' | 'envs' | 'keys'>): string {
  const envs = rule.envs.mode === 'all'
    ? `all environments${rule.envs.exc.length > 0 ? ` except ${rule.envs.exc.join(', ')}` : ''}`
    : rule.envs.list.join(', ');
  const keys = rule.keys.mode === 'all'
    ? `all keys${rule.keys.exc.length > 0 ? ` except ${rule.keys.exc.map((i) => itemLabel(world, i).text).join(', ')}` : ''}`
    : `only ${rule.keys.list.map((i) => itemLabel(world, i).text).join(', ')}`;
  return `${projectsText(rule)} › ${envs} › ${keys}`;
}

export const ruleText = (world: World, rule: Rule) => `${effective(rule).map(label).join(', ')} in ${whereText(world, rule)}`;

/* ---------- editing ---------- */

/** Tap an item: in "All, except..." it toggles an except, in "Only..." an inclusion. */
export function toggleItem(axis: Axis, item: string): Axis {
  const flip = (items: readonly string[]) => (items.includes(item) ? items.filter((i) => i !== item) : [...items, item]);
  return axis.mode === 'all' ? { mode: 'all', exc: flip(axis.exc) } : { mode: 'only', list: flip(axis.list) };
}

export const setMode = (axis: Axis, mode: Axis['mode']): Axis =>
  axis.mode === mode ? axis : mode === 'all' ? ALL : { mode: 'only', list: [] };

export type ItemState = 'included' | 'implied' | 'excepted' | 'off';

/** `implied`: covered because the axis is "All, except..." and this item is not left out. */
export const itemState = (axis: Axis, item: string): ItemState =>
  axis.mode === 'all' ? (axis.exc.includes(item) ? 'excepted' : 'implied') : axis.list.includes(item) ? 'included' : 'off';

export const hasWhere = (rule: Pick<Rule, 'projects' | 'envs' | 'keys'>) =>
  (rule.projects === '*' || rule.projects.length > 0) &&
  (rule.envs.mode === 'all' || rule.envs.list.length > 0) &&
  (rule.keys.mode === 'all' || rule.keys.list.length > 0);

/** Save or replace a rule; a new one (id 0) gets the next id. Only the permissions it can carry are stored. */
export function saveRule(world: World, draft: Rule): World {
  const rule = { ...draft, perms: effective(draft), id: draft.id === 0 ? Math.max(0, ...world.rules.map((r) => r.id)) + 1 : draft.id };
  const exists = world.rules.some((r) => r.id === rule.id);
  return { ...world, rules: exists ? world.rules.map((r) => (r.id === rule.id ? rule : r)) : [...world.rules, rule] };
}

export const removeRule = (world: World, id: number): World => ({ ...world, rules: world.rules.filter((r) => r.id !== id) });

/* ---------- definitions changes: how access follows keys ---------- */

const updateKey = (world: World, keyId: string, patch: Partial<Pick<Key, 'name' | 'folder'>>): World => ({
  ...world,
  keys: world.keys.map((k) => (k.id === keyId ? { ...k, ...patch } : k)),
});

/** A folder pick covers whatever is in the folder now, so a moved key leaves it; a single-key pick follows the id. */
export const moveKey = (world: World, keyId: string, folder: string) => updateKey(world, keyId, { folder });
export const renameKey = (world: World, keyId: string, name: string) => updateKey(world, keyId, { name });
export const addKey = (world: World, key: Key): World => ({ ...world, keys: [...world.keys, key] });

/** Every `member|permission|environment` that reaches this key, key-shaped permissions other than See. */
export function accessTo(world: World, keyId: string): Set<string> {
  const out = new Set<string>();
  const key = keyById(world, keyId);
  if (key === undefined) return out;
  for (const person of world.people) {
    for (const p of PERMS) {
      if (p.shape !== 'key' || p.id === 'read') continue;
      for (const env of projectEnvs(world, key.project)) {
        if (resolve(world, person.id, p.id, key.project, env.id, keyId).state === 'yes') out.add(`${person.id}|${p.id}|${env.id}`);
      }
    }
  }
  return out;
}

/** One member's change: which permissions, in which environments. */
export type AccessLine = { member: string; perms: { perm: PermId; envs: string[] }[] };

function linesOf(entries: Iterable<string>): AccessLine[] {
  const lines: AccessLine[] = [];
  for (const entry of entries) {
    const [member = '', id = '', env = ''] = entry.split('|');
    const permId = PERMS.find((p) => p.id === id)?.id;
    if (permId === undefined) continue;
    let line = lines.find((l) => l.member === member);
    if (line === undefined) {
      line = { member, perms: [] };
      lines.push(line);
    }
    let at = line.perms.find((p) => p.perm === permId);
    if (at === undefined) {
      at = { perm: permId, envs: [] };
      line.perms.push(at);
    }
    at.envs.push(env);
  }
  return lines;
}

export function accessDiff(before: World, after: World, keyId: string): { gained: AccessLine[]; lost: AccessLine[] } {
  const a = accessTo(before, keyId);
  const b = accessTo(after, keyId);
  return {
    gained: linesOf([...b].filter((x) => !a.has(x))),
    lost: linesOf([...a].filter((x) => !b.has(x))),
  };
}

export const accessLineText = (world: World, line: AccessLine) =>
  `${personName(world, line.member)}: ${line.perms.map((p) => `${label(p.perm)} (${p.envs.join(', ')})`).join(', ')}`;
