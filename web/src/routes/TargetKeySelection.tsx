import type { AdapterKeySelection } from '@hikyo/client';
import { useId, useState } from 'react';

import { Checkbox } from '../ui/Checkbox.tsx';
import { ChoiceGroup } from '../ui/ChoiceGroup.tsx';

/** Raw editor state; save-time selectors never replace the operator's explicit ids. */
export type TargetKeySelectionValue = {
  readonly keyIds: readonly string[];
  readonly names: string;
  readonly include: string;
  readonly exclude: string;
  readonly classification: '' | 'secret' | 'config';
};

function entries(raw: string): string[] {
  return raw.split(',').map((entry) => entry.trim()).filter((entry) => entry !== '');
}

/**
 * Initial membership is a snapshot, not a live catalogue selection. The server
 * unions these ids with exact names and the automatic selection when saved.
 */
export function useTargetKeySelection(initialIds: readonly string[] = []) {
  const [value, onChange] = useState<TargetKeySelectionValue>(() => ({
    keyIds: [...new Set(initialIds)],
    names: '',
    include: '',
    exclude: '',
    classification: '',
  }));
  const names = entries(value.names);
  const include = entries(value.include);
  const exclude = entries(value.exclude);
  const keySelection: AdapterKeySelection | undefined =
    names.length === 0 && include.length === 0 && exclude.length === 0 && value.classification === ''
      ? undefined
      : {
          ...(names.length === 0 ? {} : { names }),
          ...(include.length === 0 ? {} : { include }),
          ...(exclude.length === 0 ? {} : { exclude }),
          ...(value.classification === '' ? {} : { classification: value.classification }),
        };

  return { value, onChange, keyIds: [...value.keyIds], keySelection };
}

/** Names-only controls shared by push adapters and pull-based file targets. */
export function TargetKeySelection({
  keys,
  value,
  onChange,
  disabled = false,
}: {
  readonly keys: readonly { readonly id: string; readonly name: string }[];
  readonly value: TargetKeySelectionValue;
  readonly onChange: (value: TargetKeySelectionValue) => void;
  readonly disabled?: boolean;
}) {
  const hintId = useId();
  const selectedIds = new Set(value.keyIds);
  const toggle = (id: string) => {
    const next = new Set(value.keyIds);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange({ ...value, keyIds: [...next] });
  };

  return (
    <>
      {keys.length === 0 ? <p className="adapters__empty">This project has no keys yet.</p> : null}
      <ChoiceGroup legend="Keys" variant="chips" layout="wrap">
        {keys.map((key) => (
          <Checkbox
            key={key.id}
            mono
            label={key.name}
            checked={selectedIds.has(key.id)}
            disabled={disabled}
            onChange={() => toggle(key.id)}
          />
        ))}
      </ChoiceGroup>
      <div className="adapters__form adapters__form--two">
        <label className="field">
          <span className="field__label">Explicit names</span>
          <input
            name="key_names"
            value={value.names}
            disabled={disabled}
            onChange={(event) => onChange({ ...value, names: event.target.value })}
            placeholder="DATABASE_URL, API_TOKEN"
            autoCapitalize="none"
            spellCheck={false}
            aria-describedby={hintId}
          />
        </label>
        <label className="field">
          <span className="field__label">Include patterns</span>
          <input
            name="key_include"
            value={value.include}
            disabled={disabled}
            onChange={(event) => onChange({ ...value, include: event.target.value })}
            placeholder="DB_*, API_*"
            autoCapitalize="none"
            spellCheck={false}
            aria-describedby={hintId}
          />
        </label>
        <label className="field">
          <span className="field__label">Exclude patterns</span>
          <input
            name="key_exclude"
            value={value.exclude}
            disabled={disabled}
            onChange={(event) => onChange({ ...value, exclude: event.target.value })}
            placeholder="*_TEST"
            autoCapitalize="none"
            spellCheck={false}
            aria-describedby={hintId}
          />
        </label>
        <label className="field">
          <span className="field__label">Classification</span>
          <select
            name="key_classification"
            value={value.classification}
            disabled={disabled}
            onChange={(event) => {
              const classification = event.target.value;
              onChange({ ...value, classification: classification === 'secret' || classification === 'config' ? classification : '' });
            }}
            aria-describedby={hintId}
          >
            <option value="">Any</option>
            <option value="secret">Secrets only</option>
            <option value="config">Config only</option>
          </select>
        </label>
      </div>
      <p className="field__hint" id={hintId}>
        Enter comma-separated exact names or patterns. Patterns and classification are resolved to explicit keys
        when you save and are not stored: a key created later is never added on its own. Exclusions and
        classification narrow only automatic selections, never checked keys or explicit names. Exclude
        patterns need an include pattern or a classification to narrow. An unknown exact name is refused.
      </p>
    </>
  );
}
