import { useId, type ReactNode } from 'react';

import { Checkbox } from '../../ui/Checkbox.tsx';
import { ChoiceGroup } from '../../ui/ChoiceGroup.tsx';
import { cx } from '../../ui/cx.ts';
import { Glyph } from '../../ui/Glyph.tsx';
import { Radio } from '../../ui/Radio.tsx';
import { PERM_GROUPS, PERMS, requirement, type MemberKind, type Perm, type PermId } from './model.ts';

type Selection =
  | { mode: 'multi'; selected: readonly PermId[]; onChange: (ids: PermId[]) => void }
  | { mode: 'single'; selected: PermId; onChange: (id: PermId) => void };

/**
 * The permission vocabulary as a choice list, grouped Values / Secrets /
 * Administration, each permission with what it lets you do. One component
 * for the rule editor (`multi`: checkboxes) and Who can...? (`single`:
 * radios), so both read the same words.
 *
 * A permission's standing condition ({@link requirement}) is ALWAYS shown on
 * its row; `blocked` only flips the cross glyph in a reserved slot and the
 * disabled state. Changing the rule's Where therefore never changes a row's
 * height, and nothing below the rows jumps. The live reason is in the
 * accessible description.
 */
export function PermissionList({
  perms = PERMS,
  kind = 'person',
  blocked = () => undefined,
  ...selection
}: Selection & {
  perms?: readonly Perm[];
  kind?: MemberKind;
  /** Why this permission cannot be picked right now, or undefined when it can. */
  blocked?: (id: PermId) => string | undefined;
}) {
  const name = useId();
  return (
    <div className="access-perm-list">
      {PERM_GROUPS.map((group) => {
        const inGroup = perms.filter((p) => p.group === group);
        if (inGroup.length === 0) return null;
        return (
          <ChoiceGroup key={group} legend={group}>
            {inGroup.map((p) => (
              <PermRow
                key={p.id}
                perm={p}
                note={requirement(p.id, kind)}
                why={blocked(p.id)}
                control={(describedBy, why) => {
                  const disabled = why !== undefined;
                  return selection.mode === 'multi' ? (
                    <Checkbox
                      label={p.label}
                      checked={selection.selected.includes(p.id) && !disabled}
                      disabled={disabled}
                      aria-describedby={describedBy}
                      onChange={(e) => selection.onChange(e.target.checked ? [...selection.selected, p.id] : selection.selected.filter((x) => x !== p.id))}
                    />
                  ) : (
                    <Radio
                      name={name}
                      label={p.label}
                      checked={selection.selected === p.id}
                      disabled={disabled}
                      aria-describedby={describedBy}
                      onChange={() => selection.onChange(p.id)}
                    />
                  );
                }}
              />
            ))}
          </ChoiceGroup>
        );
      })}
    </div>
  );
}

function PermRow({
  perm,
  note,
  why,
  control,
}: {
  perm: Perm;
  note: string | undefined;
  why: string | undefined;
  control: (describedBy: string, why: string | undefined) => ReactNode;
}) {
  const id = useId();
  return (
    <div className={cx('access-perm', why !== undefined && 'access-perm--blocked')}>
      {control(id, why)}
      <p id={id} className="access-perm__desc">
        {perm.desc}
        {note === undefined ? null : (
          <span className="access-perm__note">
            {' '}
            <Glyph name="cross" className="access-perm__mark" />
            {why === undefined ? null : <span className="visually-hidden">{why}. </span>}
            {note}
          </span>
        )}
      </p>
    </div>
  );
}
