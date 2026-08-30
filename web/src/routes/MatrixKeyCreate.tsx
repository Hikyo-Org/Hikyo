import { useMemo, useRef, useState } from 'react';

import type { CreateKeyType } from '../api/matrix.ts';
import type { EnvironmentList } from '../api/values.ts';
import { useModalDialog } from './useModalDialog.ts';

type Environment = EnvironmentList['items'][number];

/** The first value a declared key opens with, staged after the key exists. */
export type MatrixKeyCreateFirstValue = {
  readonly value: string;
  readonly environmentIds: readonly string[];
};

export type MatrixKeyCreatePayload = {
  readonly name: string;
  readonly type: CreateKeyType;
  readonly classification: 'config' | 'secret';
  readonly folderPath: string;
  readonly requiredEnvironmentIds: readonly string[];
  readonly firstValue: MatrixKeyCreateFirstValue | null;
};

// The type picker keeps the prototype's short labels; the API type is the value.
const TYPE_OPTIONS: readonly { readonly type: CreateKeyType; readonly label: string }[] = [
  { type: 'string', label: 'string' },
  { type: 'integer', label: 'int' },
  { type: 'boolean', label: 'bool' },
  { type: 'url', label: 'url' },
  { type: 'json', label: 'json' },
];

/**
 * Declare-key modal (env-matrix 31). A group is a folder: a name that is not
 * yet a folder simply creates it, so there is no separate "create group" step —
 * declaring a key into a new folder is the group's first act. The type maps to
 * a single-rule declaration; the richer schema/constraints are edited later
 * from the key's own editor, exactly as the prototype defers them.
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
  const [value, setValue] = useState('');
  const [valueEnvironmentIds, setValueEnvironmentIds] = useState<readonly string[]>(() =>
    environments
      .filter((environment) => !protectedEnvironmentIds.includes(environment.id))
      .map((environment) => environment.id),
  );
  const [requiredEnvironmentIds, setRequiredEnvironmentIds] = useState<readonly string[]>([]);
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
    const trimmedValue = value.trim();
    let firstValue: MatrixKeyCreateFirstValue | null = null;
    if (trimmedValue !== '') {
      const valueError = validateFirstValue(type, trimmedValue);
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
      type,
      classification: secret ? 'secret' : 'config',
      folderPath: normalizedFolder,
      requiredEnvironmentIds,
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

        <div className="matrix-key-create__field">
          <label htmlFor="matrix-create-value">First value (optional)</label>
          <textarea
            id="matrix-create-value"
            className="mono matrix-editor__value"
            rows={type === 'json' ? 5 : 1}
            autoComplete="off"
            placeholder={type === 'json' ? 'first value (json)' : 'first value'}
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
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

        <fieldset className="matrix-editor__copy">
          <legend>Required in</legend>
          {environments.map((environment) => (
            <label key={environment.id}>
              <input
                type="checkbox"
                checked={requiredEnvironmentIds.includes(environment.id)}
                onChange={() => toggle(setRequiredEnvironmentIds, environment.id)}
              />
              <span>
                {environment.name}
                {protectedEnvironmentIds.includes(environment.id) ? ' · protected' : ''}
              </span>
            </label>
          ))}
        </fieldset>

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
 * Light first-value check mirroring the prototype's declare-time validation.
 * The declaration carries no constraints yet (schemes, enum members, JSON
 * schema come later from the key editor), and the server is the authority on
 * the staged write — so this only rejects values that cannot be the type at all.
 */
function validateFirstValue(type: CreateKeyType, value: string): string | null {
  switch (type) {
    case 'boolean':
      return value === 'true' || value === 'false' ? null : 'Enter true or false.';
    case 'integer':
      return /^-?[0-9]+$/.test(value) ? null : 'Enter a base-10 integer.';
    case 'url':
      return /^[a-z][a-z0-9+.-]*:\/\//i.test(value)
        ? null
        : 'Enter a full URL, e.g. https://example.dev.';
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
