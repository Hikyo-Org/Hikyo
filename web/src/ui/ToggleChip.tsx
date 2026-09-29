import type { ComponentProps } from 'react';

import { cx } from './cx.ts';
import { Glyph } from './Glyph.tsx';

type ChipState = 'included' | 'off' | 'implied' | 'excluded';

const WORD: Record<ChipState, string> = {
  included: 'included',
  off: 'not included',
  implied: 'included',
  excluded: 'left out',
};

/**
 * A toggle chip: one item of a dense pick set (environments, folders, keys)
 * as a pressable button, `aria-pressed` carrying whether the tap is in
 * effect. Unlike a {@link ChoiceGroup} chip (a checkbox, so "checked" always
 * means "in"), a toggle chip can also leave things OUT:
 *
 * - `mode="include"` (default): the set starts empty and a tap adds the item.
 *   Pressed: check glyph, accent fill. Not pressed: `+`, dim ink.
 * - `mode="exclude"`: the set is everything, and a tap leaves the item out.
 *   Not pressed: check glyph on a dashed accent hairline (in, because nothing
 *   left it out). Pressed: cross glyph, danger tone, struck through.
 *
 * The state is never colour-only: the glyph changes and the state word is in
 * the accessible name. Chips are badges by shape (3px radius) and controls by
 * height (`--control`). `type` is locked to `button`.
 */
type ToggleChipProps = Omit<ComponentProps<'button'>, 'type' | 'aria-pressed'> & {
  pressed: boolean;
  mode?: 'include' | 'exclude';
  /** Set the label in the value face (identifiers, key and folder names). */
  mono?: boolean;
};

export function ToggleChip({ pressed, mode = 'include', mono, className, children, ...rest }: ToggleChipProps) {
  const state: ChipState = mode === 'include' ? (pressed ? 'included' : 'off') : pressed ? 'excluded' : 'implied';
  return (
    <button {...rest} type="button" className={cx('toggle-chip', mono === true && 'mono', className)} data-state={state} aria-pressed={pressed}>
      {state === 'off' ? (
        <span className="toggle-chip__mark" aria-hidden="true">
          +
        </span>
      ) : (
        <Glyph name={state === 'excluded' ? 'cross' : 'check'} className="toggle-chip__mark" />
      )}
      <span className="toggle-chip__label">{children}</span>
      <span className="visually-hidden">({WORD[state]})</span>
    </button>
  );
}
