// @vitest-environment happy-dom
import { act } from 'react';
import { describe, expect, it, vi } from 'vitest';

import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { CompactOrgRetention } from './OrgSettings.tsx';

describe('CompactOrgRetention', () => {
  it('refuses fewer than one revision out loud and resets the field', async () => {
    const onSave = vi.fn();
    const { container, unmount } = await renderForm(
      <CompactOrgRetention
        policy={{ mode: 'keep-if-either', max_age_seconds: 7_776_000, last_revisions: 10 }}
        busy={false}
        onSave={onSave}
      />,
    );
    const input = container.querySelector('input');
    if (!(input instanceof HTMLInputElement)) throw new Error('no revisions input');

    await act(async () => typeInto(input, '0'));
    await act(async () => input.dispatchEvent(new FocusEvent('focusout', { bubbles: true })));

    expect(onSave).not.toHaveBeenCalled();
    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      'Refused: keep at least 1 revision.',
    );
    expect(input.value).toBe('10');

    await act(async () => typeInto(input, '4'));
    await act(async () => input.dispatchEvent(new FocusEvent('focusout', { bubbles: true })));
    expect(onSave).toHaveBeenCalledWith({
      mode: 'keep-if-either',
      max_age_seconds: 7_776_000,
      last_revisions: 4,
    });
    expect(container.querySelector('[role="alert"]')).toBeNull();
    await unmount();
  });
});
