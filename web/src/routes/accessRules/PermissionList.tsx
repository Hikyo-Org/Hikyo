import { useId } from 'react';

import { Checkbox } from '../../ui/Checkbox.tsx';
import { ChoiceGroup } from '../../ui/ChoiceGroup.tsx';
import { cx } from '../../ui/cx.ts';
import { Glyph } from '../../ui/Glyph.tsx';
import { NOT_ON_RULES, PERM_GROUPS, PERMS, requirement, type Perm, type PermId } from './model.ts';

/**
 * The permission vocabulary as the rule editor's checklist, grouped Values /
 * Secrets / Administration, each permission with what it lets you do.
 *
 * What a rule can NEVER carry is not listed at all; one muted line under
 * Administration names it. What depends on the rule's Where stays listed: its
 * condition sits on its own line under the description, always, and `blocked`
 * only turns on the cross glyph (in a reserved slot) and disables the box.
 * Changing Where therefore never changes a row's height. The live reason is
 * in the accessible description.
 */
export function PermissionList({
  selected,
  onChange,
  blocked,
}: {
  selected: readonly PermId[];
  onChange: (ids: PermId[]) => void;
  /** Why this permission cannot be picked with the current Where, or undefined when it can. */
  blocked: (id: PermId) => string | undefined;
}) {
  return (
    <div className="access-perm-list">
      {PERM_GROUPS.map((group) => (
        <ChoiceGroup key={group} legend={group}>
          {PERMS.filter((p) => p.group === group).map((p) => (
            <PermRow
              key={p.id}
              perm={p}
              note={requirement(p.id)}
              why={blocked(p.id)}
              checked={selected.includes(p.id)}
              onChange={(on) => onChange(on ? [...selected, p.id] : selected.filter((x) => x !== p.id))}
            />
          ))}
          {group === 'Administration' ? <p className="access-perm__never">{NOT_ON_RULES}</p> : null}
        </ChoiceGroup>
      ))}
    </div>
  );
}

function PermRow({
  perm,
  note,
  why,
  checked,
  onChange,
}: {
  perm: Perm;
  note: string | undefined;
  why: string | undefined;
  checked: boolean;
  onChange: (on: boolean) => void;
}) {
  const descId = useId();
  const noteId = useId();
  const disabled = why !== undefined;
  return (
    <div className={cx('access-perm', disabled && 'access-perm--blocked')}>
      <Checkbox
        label={perm.label}
        checked={checked && !disabled}
        disabled={disabled}
        aria-describedby={note === undefined ? descId : `${descId} ${noteId}`}
        onChange={(e) => onChange(e.target.checked)}
      />
      <p id={descId} className="access-perm__desc">
        {perm.desc}
      </p>
      {note === undefined ? null : (
        <p id={noteId} className="access-perm__note">
          {perm.shape === 'key' ? null : <Glyph name="cross" className="access-perm__mark" />}
          {disabled ? <span className="visually-hidden">{why}. </span> : null}
          <span>{note}</span>
        </p>
      )}
    </div>
  );
}
