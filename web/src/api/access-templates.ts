export type Level = 'instance' | 'org' | 'project' | 'environment';

export type RoleTemplateId =
  | 'viewer'
  | 'editor'
  | 'publisher'
  | 'revealer'
  | 'historian'
  | 'maintainer'
  | 'admin'
  | 'operator';

export type RoleTemplate = {
  readonly id: RoleTemplateId;
  readonly levels: readonly Level[];
  readonly seeds: readonly string[];
  readonly orgOnly: readonly string[];
};

/** Shared by the real UI and prototype API so grant expansion cannot drift. */
export const ROLE_TEMPLATES: readonly RoleTemplate[] = [
  { id: 'viewer', levels: ['org', 'project', 'environment'], seeds: ['read'], orgOnly: [] },
  { id: 'editor', levels: ['org', 'project', 'environment'], seeds: ['read', 'edit'], orgOnly: [] },
  {
    id: 'publisher',
    levels: ['org', 'project', 'environment'],
    seeds: ['read', 'edit', 'publish', 'pin'],
    orgOnly: [],
  },
  { id: 'revealer', levels: ['org', 'project', 'environment'], seeds: ['reveal'], orgOnly: [] },
  { id: 'historian', levels: ['org', 'project', 'environment'], seeds: ['reveal-history'], orgOnly: [] },
  {
    id: 'maintainer',
    levels: ['org', 'project'],
    seeds: [
      'read',
      'edit',
      'publish',
      'pin',
      'definitions-edit',
      'manage-identities',
      'manage-adapters',
    ],
    orgOnly: [],
  },
  {
    id: 'admin',
    levels: ['org', 'project'],
    seeds: [
      'read',
      'edit',
      'publish',
      'pin',
      'definitions-edit',
      'manage-identities',
      'manage-adapters',
      'project-settings',
      'manage-members',
      'reveal',
      'reveal-history',
    ],
    orgOnly: ['manage-projects'],
  },
  {
    id: 'operator',
    levels: ['instance'],
    seeds: [
      'backup-export',
      'restore',
      'rotate-root-key',
      'rotate-master-key',
      'rotate-dek',
      'reencrypt',
      'instance-config',
      'manage-members',
    ],
    orgOnly: [],
  },
];

export function templatesAt(level: Level): readonly RoleTemplate[] {
  return ROLE_TEMPLATES.filter((template) => template.levels.includes(level));
}

export function expandTemplate(templateId: RoleTemplateId, level: Level): readonly string[] {
  const template = ROLE_TEMPLATES.find((candidate) => candidate.id === templateId);
  if (template === undefined || !template.levels.includes(level)) {
    throw new Error(`role template ${templateId} is not admitted at ${level} scope`);
  }
  return level === 'org' ? [...template.seeds, ...template.orgOnly] : template.seeds;
}
