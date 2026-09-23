// Example data for the landing page matrix and artifacts. Deliberately a copy
// of lib/prototype/hikyo.ts, not an import: the prototypes stay free to change
// without touching the live page. Every claim here is lifted from README.md,
// PRODUCT.md, DESIGN.md, or the shipped docs under src/content/docs.

export type Environment = 'development' | 'staging' | 'production';
export const environments: readonly Environment[] = ['development', 'staging', 'production'];

export type Classification = 'config' | 'secret';

/**
 * Value state vocabulary from DESIGN.md. Every state carries a glyph or word
 * beside its colour so it never depends on colour alone.
 *
 * - set:       explicit config value, shown in plain text
 * - secret:    explicit secret value, always masked
 * - absent:    explicit absence, "· absent"
 * - missing:   presence rule says required, environment is absent: violation
 * - forbidden: presence rule says forbidden, environment is (correctly) absent
 * - draft:     a staged, unpublished change (Δ)
 */
export type CellState =
  | { kind: 'set'; value: string }
  | { kind: 'secret' }
  | { kind: 'absent' }
  | { kind: 'missing' }
  | { kind: 'forbidden' }
  | { kind: 'draft'; value: string };

export interface KeyRow {
  name: string;
  classification: Classification;
  /** Declaration rule, as `hikyo key create --declaration` would receive it. */
  rule: string;
  /** Human wording of the presence rule. */
  presence: string;
  cells: Record<Environment, CellState>;
}

export interface KeyGroup {
  name: string;
  /** All-or-none key group (docs/hierarchy). */
  allOrNone?: boolean;
  keys: KeyRow[];
}

export const project: {
  organisation: string;
  name: string;
  revision: string;
  protectedEnvironments: Environment[];
} = {
  organisation: 'platform',
  name: 'payments-api',
  revision: 'r14',
  protectedEnvironments: ['production'],
};

/** The masked secret placeholder. The landing page must never render plaintext. */
export const masked = '••••••••••••';

export const matrix: KeyGroup[] = [
  {
    name: 'database',
    keys: [
      {
        name: 'DATABASE_URL',
        classification: 'secret',
        rule: '{"rule":{"type":"url","schemes":["postgres"]}}',
        presence: 'required everywhere',
        cells: { development: { kind: 'secret' }, staging: { kind: 'secret' }, production: { kind: 'secret' } },
      },
      {
        name: 'DATABASE_POOL_MAX',
        classification: 'config',
        rule: '{"rule":{"type":"integer","min":1,"max":200}}',
        presence: 'required everywhere',
        cells: {
          development: { kind: 'set', value: '10' },
          staging: { kind: 'set', value: '20' },
          production: { kind: 'draft', value: '40' },
        },
      },
      {
        name: 'DATABASE_SSLMODE',
        classification: 'config',
        rule: '{"rule":{"type":"enum","values":["disable","require","verify-full"]}}',
        presence: 'required everywhere',
        cells: {
          development: { kind: 'set', value: 'disable' },
          staging: { kind: 'set', value: 'verify-full' },
          production: { kind: 'set', value: 'verify-full' },
        },
      },
    ],
  },
  {
    name: 'payments',
    keys: [
      {
        name: 'STRIPE_SECRET_KEY',
        classification: 'secret',
        rule: '{"rule":{"type":"string","pattern":"^sk_(test|live)_"}}',
        presence: 'required in staging, production',
        cells: { development: { kind: 'absent' }, staging: { kind: 'secret' }, production: { kind: 'secret' } },
      },
      {
        name: 'STRIPE_WEBHOOK_SECRET',
        classification: 'secret',
        rule: '{"rule":{"type":"string","pattern":"^whsec_"}}',
        presence: 'required in staging, production',
        cells: { development: { kind: 'absent' }, staging: { kind: 'secret' }, production: { kind: 'missing' } },
      },
      {
        name: 'PAYMENTS_MOCK_PROVIDER',
        classification: 'config',
        rule: '{"rule":{"type":"boolean"}}',
        presence: 'forbidden in production',
        cells: {
          development: { kind: 'set', value: 'true' },
          staging: { kind: 'set', value: 'false' },
          production: { kind: 'forbidden' },
        },
      },
    ],
  },
  {
    name: 'observability',
    keys: [
      {
        name: 'LOG_LEVEL',
        classification: 'config',
        rule: '{"rule":{"type":"enum","values":["debug","info","warn","error"]}}',
        presence: 'required everywhere',
        cells: {
          development: { kind: 'set', value: 'debug' },
          staging: { kind: 'set', value: 'info' },
          production: { kind: 'set', value: 'info' },
        },
      },
      {
        name: 'SENTRY_DSN',
        classification: 'secret',
        rule: '{"rule":{"type":"url","schemes":["https"]}}',
        presence: 'optional',
        cells: { development: { kind: 'absent' }, staging: { kind: 'secret' }, production: { kind: 'secret' } },
      },
    ],
  },
  {
    name: 'smtp',
    allOrNone: true,
    keys: [
      {
        name: 'SMTP_HOST',
        classification: 'config',
        rule: '{"rule":{"type":"hostname"}}',
        presence: 'group: all or none',
        cells: {
          development: { kind: 'absent' },
          staging: { kind: 'set', value: 'smtp.example.com' },
          production: { kind: 'set', value: 'smtp.example.com' },
        },
      },
      {
        name: 'SMTP_PASSWORD',
        classification: 'secret',
        rule: '{"rule":{"type":"string","minLength":16}}',
        presence: 'group: all or none',
        cells: { development: { kind: 'absent' }, staging: { kind: 'missing' }, production: { kind: 'secret' } },
      },
    ],
  },
];

/** Output file of the reveal demo; the matrix records STRIPE_SECRET_KEY as set in production. */
export const revealOutputFile = './stripe-key';
