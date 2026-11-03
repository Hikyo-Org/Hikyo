import { useRef, useState } from 'react';

import { zJSONText } from '../lib/jsonText.ts';

import {
  BUILDER_TYPES,
  builderSchemaError,
  changeType,
  fieldNameProblem,
  newField,
  parseBuilderSchema,
  serializeBuilderSchema,
  type BuilderNode,
  type BuilderType,
} from '../lib/jsonSchemaBuilder.ts';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { Disclosure } from '../ui/Disclosure.tsx';
import { Input } from '../ui/Input.tsx';
import { Select } from '../ui/Select.tsx';
import { Textarea } from '../ui/Textarea.tsx';
import { selectOption } from './selectOption.ts';

/**
 * A no-code editor for the JSON Schema of a `json` key. The schema TEXT stays
 * the single source of truth in the parent: this is a view that is rebuilt from
 * `value` whenever it changes from outside, and it reports edits as text. It
 * never calls `onChange` on mount, so an untouched declaration is written back
 * exactly. A schema using anything the builder cannot show is left to the raw
 * editor, unchanged. `onChange`'s second argument is a refusal the parent must
 * block saving on (a blank or duplicate field name), paired with the last valid
 * text; the parent hands it back as `error`.
 */
export function JsonSchemaBuilder({
  value,
  error,
  onChange,
}: {
  value: string;
  error: string | null;
  onChange: (text: string, error: string | null) => void;
}) {
  // The tree is local because an in-progress row (a blank or duplicate name)
  // cannot exist in a string. `seen` is the text the tree reflects: when `value`
  // differs, someone else wrote it, so rebuild (derive-during-render).
  const [seen, setSeen] = useState(value);
  const [model, setModel] = useState(() => parseBuilderSchema(value));
  // A tree the parent no longer holds a refusal for would show rows the text
  // does not have, and saving would silently drop them. parseBuilderSchema only
  // returns clean trees, so this rebuild cannot repeat.
  const orphaned = error === null && model !== null && builderSchemaError(model.root) !== null;
  // Held while typing in the raw editor, so the builder appearing and vanishing
  // on each keystroke does not move the text under the cursor (DESIGN.md).
  const [rawFocused, setRawFocused] = useState(false);
  if ((value !== seen || orphaned) && !rawFocused) {
    setSeen(value);
    setModel(parseBuilderSchema(value));
  }

  const edit = (root: BuilderNode): void => {
    if (model === null) return;
    setModel({ root, dialect: model.dialect });
    const refusal = builderSchemaError(root);
    if (refusal !== null) {
      onChange(value, refusal);
      return;
    }
    const text = serializeBuilderSchema(root, model.dialect);
    // Mark the echo as ours, so the parent's new `value` does not rebuild the
    // tree (that would drop retained fields and the focused row's identity).
    setSeen(text);
    onChange(text, null);
  };

  return (
    <div className="schema-builder">
      {model === null ? (
        <p className="field__hint">
          {zJSONText.safeParse(seen).success
            ? 'This schema uses rules the builder cannot show. It stays unchanged unless you edit it under Advanced below.'
            : 'This is not valid JSON yet. The builder returns once it is.'}
        </p>
      ) : (
        <>
          <ShapeSelect
            label="Value is"
            type={model.root.type}
            onChange={(type) => edit(changeType(model.root, type))}
          />
          <Nested node={model.root} owner="" onChange={edit} />
        </>
      )}
      <Disclosure label="Advanced: edit as JSON Schema (2020-12)" defaultOpen={model === null}>
        <Textarea
          label="Schema document"
          mono
          rows={6}
          autoComplete="off"
          spellCheck={false}
          placeholder='{ "type": "object" }'
          value={value}
          onFocus={() => setRawFocused(true)}
          onBlur={() => setRawFocused(false)}
          onChange={(event) => onChange(event.currentTarget.value, null)}
        />
      </Disclosure>
    </div>
  );
}

function ShapeSelect({
  label,
  accessibleName,
  type,
  onChange,
}: {
  label: string;
  accessibleName?: string;
  type: BuilderType;
  onChange: (type: BuilderType) => void;
}) {
  return (
    <Select
      label={label}
      aria-label={accessibleName}
      value={type}
      onChange={(event) => onChange(selectOption(BUILDER_TYPES, event.currentTarget.value))}
    >
      {BUILDER_TYPES.map((candidate) => (
        <option key={candidate} value={candidate}>
          {candidate === 'any' ? 'Any JSON' : candidate}
        </option>
      ))}
    </Select>
  );
}

/** What sits under a node: an object's fields, or an array's item shape. `owner` is the field's name, for accessible names. */
function Nested({
  node,
  owner,
  onChange,
}: {
  node: BuilderNode;
  owner: string;
  onChange: (next: BuilderNode) => void;
}) {
  // Removing a row takes its button away; focus must land somewhere, not on body.
  const addField = useRef<HTMLButtonElement>(null);
  if (node.type === 'object') {
    return (
      <div className="schema-builder__nested">
        {node.fields.map((field) => (
          <FieldRow
            key={field.id}
            field={field}
            siblings={node.fields}
            onChange={(next) =>
              onChange({ ...node, fields: node.fields.map((other) => (other.id === next.id ? next : other)) })
            }
            onRemove={() => {
              addField.current?.focus();
              onChange({ ...node, fields: node.fields.filter((other) => other.id !== field.id) });
            }}
          />
        ))}
        <Button
          ref={addField}
          type="button"
          className="schema-builder__add"
          aria-label={owner === '' ? undefined : `Add field to ${owner}`}
          onClick={() => onChange({ ...node, fields: [...node.fields, newField()] })}
        >
          Add field
        </Button>
      </div>
    );
  }
  if (node.type === 'array' && node.items !== null) {
    const items = node.items;
    return (
      <div className="schema-builder__nested">
        <ShapeSelect
          label="Each item is"
          accessibleName={owner === '' ? undefined : `${owner}: Each item is`}
          type={items.type}
          onChange={(type) => onChange({ ...node, items: changeType(items, type) })}
        />
        <Nested node={items} owner={owner} onChange={(next) => onChange({ ...node, items: next })} />
      </div>
    );
  }
  return null;
}

function FieldRow({
  field,
  siblings,
  onChange,
  onRemove,
}: {
  field: BuilderNode;
  siblings: readonly BuilderNode[];
  onChange: (next: BuilderNode) => void;
  onRemove: () => void;
}) {
  // A new row starts blank: that is not a mistake yet. Saving still refuses it.
  const problem = field.name === '' ? null : fieldNameProblem(siblings, field);
  const named = field.name.trim() === '' ? null : field.name;
  return (
    <div className="schema-builder__field">
      <div className="schema-builder__row">
        <Input
          label="Field name"
          mono
          autoComplete="off"
          spellCheck={false}
          value={field.name}
          error={problem ?? undefined}
          onChange={(event) => onChange({ ...field, name: event.currentTarget.value })}
        />
        <ShapeSelect
          label="Type"
          accessibleName={named === null ? undefined : `Type of ${named}`}
          type={field.type}
          onChange={(type) => onChange(changeType(field, type))}
        />
        <Checkbox
          label="Required"
          aria-label={named === null ? undefined : `Required: ${named}`}
          checked={field.required}
          onChange={(event) => onChange({ ...field, required: event.currentTarget.checked })}
        />
        <Button
          type="button"
          variant="quiet"
          aria-label={named === null ? undefined : `Remove field ${named}`}
          onClick={onRemove}
        >
          Remove
        </Button>
      </div>
      <Nested node={field} owner={named ?? ''} onChange={onChange} />
    </div>
  );
}
