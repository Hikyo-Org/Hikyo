import { useId } from 'react';

import type { RetentionDayState } from '../api/settings.ts';
import { Alert } from './Sections.tsx';

/** Shared whole-day and revision-bound controls for both retention editors. */
export function RetentionBoundsFields({
  age,
  count,
  onAgeChange,
  onCountChange,
}: {
  readonly age: RetentionDayState;
  readonly count: string;
  readonly onAgeChange: (next: RetentionDayState) => void;
  readonly onCountChange: (next: string) => void;
}) {
  const ageId = useId();
  const countId = useId();

  return (
    <>
      {age.kind === 'exact' ? (
        <Alert>
          Current maximum age is exact ({age.seconds} seconds), not whole days. The day editor is
          disabled so that exact value cannot look absent.
          <button
            type="button"
            className="btn"
            onClick={() => onAgeChange({ kind: 'days', days: '' })}
          >
            Replace with whole days
          </button>
        </Alert>
      ) : null}
      {age.kind === 'absent' ? (
        <p role="status">No maximum age is present. Enter a whole-day replacement deliberately.</p>
      ) : null}
      <div className="retention__bounds">
        <div className="field">
          <label htmlFor={ageId}>Maximum age, in days</label>
          <input
            id={ageId}
            type="number"
            min={1}
            inputMode="numeric"
            value={age.kind === 'days' ? age.days : ''}
            disabled={age.kind === 'exact'}
            onChange={(event) => onAgeChange({ kind: 'days', days: event.target.value })}
          />
        </div>
        <div className="field">
          <label htmlFor={countId}>Revisions kept per environment</label>
          <input
            id={countId}
            type="number"
            min={1}
            inputMode="numeric"
            value={count}
            onChange={(event) => onCountChange(event.target.value)}
          />
        </div>
      </div>
    </>
  );
}
