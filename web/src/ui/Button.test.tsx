// @vitest-environment happy-dom
import { act } from 'react';
import { expect, it, vi } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { Button } from './Button.tsx';

it('activates once while enabled and refuses activation while disabled', async () => {
  const activate = vi.fn();
  const view = await renderForm(<Button onClick={activate}>Save changes</Button>);
  try {
    const button = view.container.querySelector('button');
    if (button === null) throw new Error('Missing action button');
    await act(async () => button.click());
    expect(activate).toHaveBeenCalledTimes(1);
    await view.rerender(<Button disabled onClick={activate}>Save changes</Button>);
    await act(async () => button.click());
    expect(activate).toHaveBeenCalledTimes(1);
    expect(button.disabled).toBe(true);
  } finally {
    await view.unmount();
  }
});
