import { useMemo, useRef, useState } from 'react';

import type { CreateKeyPresence, CreateKeyRule, CreateKeyType } from '../api/matrix.ts';
import type { EnvironmentList } from '../api/values.ts';
import { useModalDialog } from './useModalDialog.ts';

type Environment = EnvironmentList['items'][number];
type PresenceMode = CreateKeyPresence['mode'];

/** The first value a declared key opens with, staged after the key exists. */
type MatrixKeyCreateFirstValue = {
  readonly value: string;
  readonly environmentIds: readonly string[];
};

export type MatrixKeyCreatePayload = {
  readonly name: string;
  readonly classification: 'config' | 'secret';
  readonly rule: CreateKeyRule;
  readonly folderPath: string;
  readonly description: string;
  readonly required: CreateKeyPresence;
  readonly forbidden: CreateKeyPresence;
  readonly firstValue: MatrixKeyCreateFirstValue | null;
};

// The type picker keeps the prototype's short labels; the API type is the value.
const TYPE_OPTIONS: readonly { readonly type: CreateKeyType; readonly label: string }[] = [
  { type: 'string', label: 'string' },
  { type: 'integer', label: 'int' },
  { type: 'boolean', label: 'bool' },
  { type: 'enum', label: 'enum' },
  { type: 'url', label: 'url' },
  { type: 'json', label: 'json' },
];

// Contract caps (zKeyRule): reject anything the server would refuse up front so
// the operator fixes it in the form, not after a round-trip.
const MAX_ENUM_MEMBERS = 64;
const MAX_MEMBER_LENGTH = 512;
const MAX_PATTERN_LENGTH = 512;
const MAX_SCHEMES = 32;
const MAX_JSON_SCHEMA_LENGTH = 16384;
const INT64_MIN = -9223372036854775808n;
const INT64_MAX = 9223372036854775807n;
const SAFE_MIN = BigInt(Number.MIN_SAFE_INTEGER);
const SAFE_MAX = BigInt(Number.MAX_SAFE_INTEGER);

/**
 * Declare-key modal (env-matrix 31 / #492). A group is a folder: a name that is
 * not yet a folder simply creates it, so there is no separate "create group"
 * step. The declaration carries one value rule with its type-specific
 * constraints and per-environment presence (required / forbidden, each
 * none / all / an explicit set). `all` is offered as an explicit choice — it is
 * SYMBOLIC and covers environments created later, so it is never inferred from
 * "these happen to be every environment today". Richer editing (any_of unions,
 * deprecation, key groups) lives in the key's own editor.
 */
export function MatrixKeyCreate({
  folders,
  environments,
  protectedEnvironmentIds,
  initialFolder,
  existingKeyNames,
  busy,
  mutationError,
  onClose,
  onCreate,
}: {
  folders: readonly string[];
  environments: readonly Environment[];
  protectedEnvironmentIds: readonly string[];
  initialFolder: string | null;
  existingKeyNames: readonly string[];
  busy: boolean;
  mutationError: string | null;
  onClose: () => void;
  onCreate: (payload: MatrixKeyCreatePayload) => Promise<void>;
}) {
  const nameField = useRef<HTMLInputElement>(null);
  const dialog = useModalDialog(nameField);
  const [folder, setFolder] = useState(initialFolder ?? '');
  const [name, setName] = useState('');
  const [type, setType] = useState<CreateKeyType>('string');
  const [secret, setSecret] = useState(false);
  const [description, setDescription] = useState('');
  // Constraint fields — kept as raw strings so an in-progress edit never coerces
  // to NaN; parsed and range-checked only at submit.
  const [minLength, setMinLength] = useState('');
  const [maxLength, setMaxLength] = useState('');
  const [pattern, setPattern] = useState('');
  const [allowEmpty, setAllowEmpty] = useState(false);
  const [min, setMin] = useState('');
  const [max, setMax] = useState('');
  const [members, setMembers] = useState('');
  const [schemes, setSchemes] = useState('https');
  const [jsonSchema, setJsonSchema] = useState('');
  const [value, setValue] = useState('');
  const [valueEnvironmentIds, setValueEnvironmentIds] = useState<readonly string[]>(() =>
    environments
      .filter((environment) => !protectedEnvironmentIds.includes(environment.id))
      .map((environment) => environment.id),
  );
  const [requiredMode, setRequiredMode] = useState<PresenceMode>('none');
  const [requiredEnvironmentIds, setRequiredEnvironmentIds] = useState<readonly string[]>([]);
  const [forbiddenMode, setForbiddenMode] = useState<PresenceMode>('none');
  const [forbiddenEnvironmentIds, setForbiddenEnvironmentIds] = useState<readonly string[]>([]);
  const [applying, setApplying] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const existing = useMemo(() => new Set(existingKeyNames), [existingKeyNames]);

  const submit = () => {
    const normalizedFolder = folder.trim().toLowerCase().replace(/[^a-z0-9_/-]/g, '');
    if (normalizedFolder === '') {
      setError('Enter a group, e.g. app.');
      return;
    }
    const normalizedName = name.trim().toUpperCase().replace(/[^A-Z0-9_]/g, '_');
    if (normalizedName === '') {
      setError('Enter a key name.');
      return;
    }
    // Match the server's KeyName rule up front so a bad shape (e.g. a leading
    // digit) fails with a specific message, not a generic server refusal.
    if (!/^[A-Z_][A-Z0-9_]*$/.test(normalizedName)) {
      setError('Key names start with a letter or underscore, then letters, digits or underscores.');
      return;
    }
    if (existing.has(normalizedName)) {
      setError(`${normalizedName} already exists.`);
      return;
    }

    const built = buildRule(type, {
      minLength,
      maxLength,
      pattern,
      allowEmpty,
      min,
      max,
      members,
      schemes,
      jsonSchema,
    });
    if ('error' in built) {
      setError(built.error);
      return;
    }
    const rule = built.rule;

    const required: CreateKeyPresence = {
      mode: requiredMode,
      environmentIds: requiredMode === 'explicit' ? requiredEnvironmentIds : [],
    };
    const forbidden: CreateKeyPresence = {
      mode: forbiddenMode,
      environmentIds: forbiddenMode === 'explicit' ? forbiddenEnvironmentIds : [],
    };
    const presenceError = validatePresence(required, forbidden);
    if (presenceError !== null) {
      setError(presenceError);
      return;
    }

    const trimmedValue = value.trim();
    let firstValue: MatrixKeyCreateFirstValue | null = null;
    if (trimmedValue !== '') {
      const valueError = validateFirstValue(rule, trimmedValue);
      if (valueError !== null) {
        setError(valueError);
        return;
      }
      if (valueEnvironmentIds.length === 0) {
        setError('Pick at least one environment for the first value.');
        return;
      }
      firstValue = { value: trimmedValue, environmentIds: valueEnvironmentIds };
    }
    setError(null);
    setApplying(true);
    void onCreate({
      name: normalizedName,
      classification: secret ? 'secret' : 'config',
      rule,
      folderPath: normalizedFolder,
      description: description.trim(),
      required,
      forbidden,
      firstValue,
    })
      // A declaration failure is surfaced by the parent through `mutationError`;
      // swallow the rejection here so the modal shows one message, not two.
      .catch(() => undefined)
      .finally(() => setApplying(false));
  };

  const toggle = (
    setter: (updater: (current: readonly string[]) => readonly string[]) => void,
    id: string,
  ) =>
    setter((current) =>
      current.includes(id) ? current.filter((value) => value !== id) : [...current, id],
    );

  return (
    <dialog
      className="matrix-editor matrix-key-create"
      ref={dialog}
      onClose={onClose}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <form
        method="dialog"
        onSubmit={(event) => {
          event.preventDefault();
          submit();
        }}
      >
        <div className="matrix-editor__head">
          <div>
            <p className="matrix-editor__eyebrow">Declare key</p>
            <h2>New key</h2>
            <p>
              Each environment gets its own explicit value — nothing inherits. A new group name
              creates that group.
            </p>
          </div>
          <button
            type="button"
            className="btn matrix-editor__close"
            aria-label="Close new key"
            onClick={onClose}
          >
            ✕
          </button>
        </div>

        <div className="matrix-key-create__field">
          <label htmlFor="matrix-create-folder">Group</label>
          <input
            id="matrix-create-folder"
            className="mono"
            list="matrix-create-folders"
            autoComplete="off"
            placeholder="group (e.g. app)"
            value={folder}
            onChange={(event) => setFolder(event.target.value)}
          />
          <datalist id="matrix-create-folders">
            {folders.map((option) => (
              <option key={option} value={option} />
            ))}
          </datalist>
        </div>

        <div className="matrix-key-create__field">
          <label htmlFor="matrix-create-name">Key name</label>
          <input
            id="matrix-create-name"
            ref={nameField}
            className="mono"
            autoComplete="off"
            placeholder="KEY_NAME"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        </div>

        <div className="matrix-key-create__type">
          <label htmlFor="matrix-create-type">Type</label>
          <select
            id="matrix-create-type"
            value={type}
            onChange={(event) => setType(event.target.value as CreateKeyType)}
          >
            {TYPE_OPTIONS.map((option) => (
              <option key={option.type} value={option.type}>
                {option.label}
              </option>
            ))}
          </select>
          <label className="matrix-key-create__secret">
            <input
              type="checkbox"
              checked={secret}
              onChange={(event) => setSecret(event.target.checked)}
            />
            <span>🔒 secret</span>
          </label>
        </div>

        {type === 'string' ? (
          <fieldset className="matrix-key-create__constraints">
            <legend>String constraints (optional)</legend>
            <div className="matrix-key-create__constraint-row">
              <label>
                <span>Min length</span>
                <input
                  type="number"
                  min={0}
                  inputMode="numeric"
                  value={minLength}
                  onChange={(event) => setMinLength(event.target.value)}
                />
              </label>
              <label>
                <span>Max length</span>
                <input
                  type="number"
                  min={0}
                  inputMode="numeric"
                  value={maxLength}
                  onChange={(event) => setMaxLength(event.target.value)}
                />
              </label>
            </div>
            <label className="matrix-key-create__field">
              <span>Pattern (RE2, whole value)</span>
              <input
                className="mono"
                autoComplete="off"
                placeholder="^[a-z][a-z0-9-]*$"
                value={pattern}
                onChange={(event) => setPattern(event.target.value)}
              />
            </label>
            <label className="matrix-key-create__secret">
              <input
                type="checkbox"
                checked={allowEmpty}
                onChange={(event) => setAllowEmpty(event.target.checked)}
              />
              <span>allow empty value</span>
            </label>
          </fieldset>
        ) : null}

        {type === 'integer' ? (
          <fieldset className="matrix-key-create__constraints">
            <legend>Integer constraints (optional)</legend>
            <div className="matrix-key-create__constraint-row">
              <label>
                <span>Minimum</span>
                <input
                  type="number"
                  inputMode="numeric"
                  value={min}
                  onChange={(event) => setMin(event.target.value)}
                />
              </label>
              <label>
                <span>Maximum</span>
                <input
                  type="number"
                  inputMode="numeric"
                  value={max}
                  onChange={(event) => setMax(event.target.value)}
                />
              </label>
            </div>
          </fieldset>
        ) : null}

        {type === 'enum' ? (
          <fieldset className="matrix-key-create__constraints">
            <legend>Enum members</legend>
            <label className="matrix-key-create__field">
              <span>One member per line</span>
              <textarea
                className="mono"
                rows={4}
                autoComplete="off"
                placeholder={'debug\ninfo\nwarn\nerror'}
                value={members}
                onChange={(event) => setMembers(event.target.value)}
              />
            </label>
          </fieldset>
        ) : null}

        {type === 'url' ? (
          <fieldset className="matrix-key-create__constraints">
            <legend>Allowed URL schemes</legend>
            <label className="matrix-key-create__field">
              <span>Comma-separated (blank allows any)</span>
              <input
                className="mono"
                autoComplete="off"
                placeholder="https, http"
                value={schemes}
                onChange={(event) => setSchemes(event.target.value)}
              />
            </label>
          </fieldset>
        ) : null}

        {type === 'json' ? (
          <fieldset className="matrix-key-create__constraints">
            <legend>JSON Schema (optional, 2020-12)</legend>
            <label className="matrix-key-create__field">
              <span>Schema document</span>
              <textarea
                className="mono"
                rows={5}
                autoComplete="off"
                placeholder='{ "type": "object" }'
                value={jsonSchema}
                onChange={(event) => setJsonSchema(event.target.value)}
              />
            </label>
          </fieldset>
        ) : null}

        <div className="matrix-key-create__field">
          <label htmlFor="matrix-create-description">Description (optional)</label>
          <textarea
            id="matrix-create-description"
            rows={2}
            autoComplete="off"
            placeholder="What this key configures"
            value={description}
            onChange={(event) => setDescription(event.target.value)}
          />
        </div>

        <div className="matrix-key-create__field">
          <label htmlFor="matrix-create-value">First value (optional)</label>
          {secret ? (
            <input
              id="matrix-create-value"
              type="password"
              className="mono matrix-editor__value"
              autoComplete="off"
              placeholder="first value (hidden)"
              value={value}
              onChange={(event) => setValue(event.target.value)}
            />
          ) : (
            <textarea
              id="matrix-create-value"
              className="mono matrix-editor__value"
              rows={type === 'json' ? 5 : 1}
              autoComplete="off"
              placeholder={type === 'json' ? 'first value (json)' : 'first value'}
              value={value}
              onChange={(event) => setValue(event.target.value)}
            />
          )}
        </div>

        <fieldset className="matrix-editor__copy">
          <legend>Set that value in</legend>
          {environments.map((environment) => (
            <label key={environment.id}>
              <input
                type="checkbox"
                checked={valueEnvironmentIds.includes(environment.id)}
                onChange={() => toggle(setValueEnvironmentIds, environment.id)}
              />
              <span>
                {environment.name}
                {protectedEnvironmentIds.includes(environment.id) ? ' · protected' : ''}
              </span>
            </label>
          ))}
        </fieldset>

        <PresenceField
          legend="Required in"
          hint="Values must resolve to set in these environments."
          idPrefix="matrix-create-required"
          mode={requiredMode}
          setMode={setRequiredMode}
          environmentIds={requiredEnvironmentIds}
          onToggle={(id) => toggle(setRequiredEnvironmentIds, id)}
          environments={environments}
          protectedEnvironmentIds={protectedEnvironmentIds}
        />

        <PresenceField
          legend="Forbidden in"
          hint="Values must stay absent in these environments."
          idPrefix="matrix-create-forbidden"
          mode={forbiddenMode}
          setMode={setForbiddenMode}
          environmentIds={forbiddenEnvironmentIds}
          onToggle={(id) => toggle(setForbiddenEnvironmentIds, id)}
          environments={environments}
          protectedEnvironmentIds={protectedEnvironmentIds}
        />

        {error === null ? null : (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">!</span>
            <span>{error}</span>
          </p>
        )}
        {mutationError === null ? null : (
          <p className="alert" role="alert">
            <span className="alert__glyph" aria-hidden="true">!</span>
            <span>{mutationError}</span>
          </p>
        )}

        <div className="matrix-editor__actions">
          <button type="submit" className="btn btn--primary" disabled={busy || applying}>
            {busy || applying ? 'Declaring…' : 'Declare'}
          </button>
          <button type="button" className="btn" onClick={onClose}>
            Cancel
          </button>
        </div>
        <p className="matrix-editor__hint">
          <b>Secret</b> is permanent: values are hidden and reveal-gated everywhere.
        </p>
      </form>
    </dialog>
  );
}

/**
 * One presence axis (required / forbidden). `all` is a first-class radio, not a
 * "select every environment" — the symbolic mode covers future environments
 * that an explicit set cannot, so the operator states it directly.
 */
function PresenceField({
  legend,
  hint,
  idPrefix,
  mode,
  setMode,
  environmentIds,
  onToggle,
  environments,
  protectedEnvironmentIds,
}: {
  legend: string;
  hint: string;
  idPrefix: string;
  mode: PresenceMode;
  setMode: (mode: PresenceMode) => void;
  environmentIds: readonly string[];
  onToggle: (id: string) => void;
  environments: readonly Environment[];
  protectedEnvironmentIds: readonly string[];
}) {
  const modes: readonly { readonly value: PresenceMode; readonly label: string }[] = [
    { value: 'none', label: 'none' },
    { value: 'all', label: 'all (current & future)' },
    { value: 'explicit', label: 'these environments' },
  ];
  return (
    <fieldset className="matrix-key-create__presence">
      <legend>{legend}</legend>
      <div className="matrix-key-create__presence-modes" role="radiogroup" aria-label={legend}>
        {modes.map((option) => (
          <label key={option.value}>
            <input
              type="radio"
              name={idPrefix}
              value={option.value}
              checked={mode === option.value}
              onChange={() => setMode(option.value)}
            />
            <span>{option.label}</span>
          </label>
        ))}
      </div>
      {mode === 'explicit' ? (
        <div className="matrix-editor__copy">
          {environments.map((environment) => (
            <label key={environment.id}>
              <input
                type="checkbox"
                checked={environmentIds.includes(environment.id)}
                onChange={() => onToggle(environment.id)}
              />
              <span>
                {environment.name}
                {protectedEnvironmentIds.includes(environment.id) ? ' · protected' : ''}
              </span>
            </label>
          ))}
        </div>
      ) : (
        <p className="matrix-key-create__presence-hint">{hint}</p>
      )}
    </fieldset>
  );
}

type ConstraintInputs = {
  readonly minLength: string;
  readonly maxLength: string;
  readonly pattern: string;
  readonly allowEmpty: boolean;
  readonly min: string;
  readonly max: string;
  readonly members: string;
  readonly schemes: string;
  readonly jsonSchema: string;
};

/**
 * buildRule assembles the type's value rule from the raw inputs, applying only
 * the constraints that type owns and rejecting shapes the server would refuse
 * (a min above a max, an empty or oversized enum). It returns either the rule
 * or a single caller-facing message.
 */
function buildRule(
  type: CreateKeyType,
  inputs: ConstraintInputs,
): { readonly rule: CreateKeyRule } | { readonly error: string } {
  switch (type) {
    case 'string': {
      const minLength = parseOptionalInt(inputs.minLength);
      const maxLength = parseOptionalInt(inputs.maxLength);
      if (minLength === 'invalid' || maxLength === 'invalid') {
        return { error: 'Length limits must be whole numbers of 0 or more.' };
      }
      if (
        (minLength !== undefined && minLength < 0) ||
        (maxLength !== undefined && maxLength < 0)
      ) {
        return { error: 'Length limits must be whole numbers of 0 or more.' };
      }
      if (minLength !== undefined && maxLength !== undefined && minLength > maxLength) {
        return { error: 'Min length cannot exceed max length.' };
      }
      const pattern = inputs.pattern.trim();
      if (pattern.length > MAX_PATTERN_LENGTH) {
        return { error: `A pattern is at most ${String(MAX_PATTERN_LENGTH)} characters.` };
      }
      return {
        rule: {
          type: 'string',
          ...(minLength === undefined ? {} : { minLength }),
          ...(maxLength === undefined ? {} : { maxLength }),
          ...(pattern === '' ? {} : { pattern }),
          ...(inputs.allowEmpty ? { allowEmpty: true } : {}),
        },
      };
    }
    case 'integer': {
      const min = parseInt64(inputs.min);
      const max = parseInt64(inputs.max);
      if (min === 'invalid' || max === 'invalid') {
        return { error: 'Minimum and maximum must be whole numbers within the int64 range.' };
      }
      if (min === 'unsafe' || max === 'unsafe') {
        return {
          error: 'Minimum and maximum must stay within ±9,007,199,254,740,991 to keep exact precision.',
        };
      }
      if (min !== undefined && max !== undefined && min > max) {
        return { error: 'Minimum cannot exceed maximum.' };
      }
      return {
        rule: {
          type: 'integer',
          ...(min === undefined ? {} : { min }),
          ...(max === undefined ? {} : { max }),
        },
      };
    }
    case 'enum': {
      const list = inputs.members
        .split('\n')
        .map((member) => member.trim())
        .filter((member) => member !== '');
      if (list.length === 0) {
        return { error: 'Enter at least one enum member, one per line.' };
      }
      if (new Set(list).size !== list.length) {
        return { error: 'Enum members must be distinct.' };
      }
      if (list.length > MAX_ENUM_MEMBERS) {
        return { error: `An enum carries at most ${String(MAX_ENUM_MEMBERS)} members.` };
      }
      if (list.some((member) => member.length > MAX_MEMBER_LENGTH)) {
        return { error: `Each enum member is at most ${String(MAX_MEMBER_LENGTH)} characters.` };
      }
      return { rule: { type: 'enum', members: list } };
    }
    case 'url': {
      const list = inputs.schemes
        .split(',')
        .map((scheme) => scheme.trim().toLowerCase())
        .filter((scheme) => scheme !== '');
      if (new Set(list).size !== list.length) {
        return { error: 'URL schemes must be distinct.' };
      }
      if (list.length > MAX_SCHEMES) {
        return { error: `At most ${String(MAX_SCHEMES)} URL schemes.` };
      }
      if (list.some((scheme) => !/^[a-z][a-z0-9+.-]*$/.test(scheme))) {
        return { error: 'A URL scheme starts with a letter, then letters, digits, +, . or -.' };
      }
      return {
        rule: { type: 'url', ...(list.length === 0 ? {} : { schemes: list }) },
      };
    }
    case 'json': {
      const trimmed = inputs.jsonSchema.trim();
      if (trimmed.length > MAX_JSON_SCHEMA_LENGTH) {
        return { error: `A JSON Schema is at most ${String(MAX_JSON_SCHEMA_LENGTH)} characters.` };
      }
      if (trimmed !== '') {
        try {
          JSON.parse(trimmed);
        } catch {
          return { error: 'The JSON Schema must be valid JSON.' };
        }
      }
      return { rule: { type: 'json', ...(trimmed === '' ? {} : { jsonSchema: trimmed }) } };
    }
    default:
      return { rule: { type: 'boolean' } };
  }
}

function parseOptionalInt(raw: string): number | undefined | 'invalid' {
  const trimmed = raw.trim();
  if (trimmed === '') return undefined;
  if (!/^[0-9]+$/.test(trimmed)) return 'invalid';
  return Number.parseInt(trimmed, 10);
}

/**
 * Parses a signed integer bound, rejecting values outside int64 (the contract's
 * range) and — separately — values a JS number cannot hold exactly, because the
 * request carries a `number` and `Number.parseInt` would silently round a value
 * past 2^53. Better to refuse than to transmit a different number than typed.
 */
function parseInt64(raw: string): number | undefined | 'invalid' | 'unsafe' {
  const trimmed = raw.trim();
  if (trimmed === '') return undefined;
  if (!/^-?[0-9]+$/.test(trimmed)) return 'invalid';
  const big = BigInt(trimmed);
  if (big < INT64_MIN || big > INT64_MAX) return 'invalid';
  if (big < SAFE_MIN || big > SAFE_MAX) return 'unsafe';
  return Number(trimmed);
}

/**
 * A key cannot be both required and forbidden in the same environment, and a
 * symbolic `all` on one axis collides with any presence on the other. Catch the
 * guaranteed refusal here so it reads as one fixable message, not a 4xx.
 */
function validatePresence(required: CreateKeyPresence, forbidden: CreateKeyPresence): string | null {
  if (required.mode === 'explicit' && required.environmentIds.length === 0) {
    return 'Pick at least one environment where the value is required, or choose none.';
  }
  if (forbidden.mode === 'explicit' && forbidden.environmentIds.length === 0) {
    return 'Pick at least one environment where the value is forbidden, or choose none.';
  }
  if (required.mode === 'all' && forbidden.mode !== 'none') {
    return 'Required in all environments leaves none to forbid.';
  }
  if (forbidden.mode === 'all' && required.mode !== 'none') {
    return 'Forbidden in all environments leaves none to require.';
  }
  if (required.mode === 'explicit' && forbidden.mode === 'explicit') {
    const forbiddenSet = new Set(forbidden.environmentIds);
    if (required.environmentIds.some((id) => forbiddenSet.has(id))) {
      return 'An environment cannot be both required and forbidden.';
    }
  }
  return null;
}

/**
 * Light first-value check mirroring the prototype's declare-time validation.
 * The server is the authority on the staged write; this only rejects values
 * that cannot satisfy the declared rule at all — the type, and an enum's
 * membership, which is knowable without the service.
 */
function validateFirstValue(rule: CreateKeyRule, value: string): string | null {
  switch (rule.type) {
    case 'boolean':
      return value === 'true' || value === 'false' ? null : 'Enter true or false.';
    case 'integer':
      return /^-?[0-9]+$/.test(value) ? null : 'Enter a base-10 integer.';
    case 'url':
      return /^[a-z][a-z0-9+.-]*:\/\//i.test(value)
        ? null
        : 'Enter a full URL, e.g. https://example.dev.';
    case 'enum':
      // Never echo the members back: a declared member could itself be
      // credential-shaped, and the members are already listed in the field
      // above. Name the rule, not the values.
      return rule.members === undefined || rule.members.includes(value)
        ? null
        : 'Enter one of the declared enum members.';
    case 'json':
      try {
        JSON.parse(value);
        return null;
      } catch {
        return 'Enter valid JSON.';
      }
    default:
      return null;
  }
}
