// @vitest-environment happy-dom
import { expect, it } from 'vitest';

import { renderForm } from '../testkit/renderForm.tsx';
import { TableScroll } from './TableScroll.tsx';

it('renders a focusable region named by its caption', async () => {
  const view = await renderForm(
    <TableScroll aria-labelledby="cap">
      <table>
        <caption id="cap">Keys</caption>
      </table>
    </TableScroll>,
  );
  try {
    const region = view.container.querySelector('.table-scroll');
    if (region === null) throw new Error('Missing table scroll region');
    expect(region.getAttribute('role')).toBe('region');
    expect(region.getAttribute('tabindex')).toBe('0');
    expect(region.getAttribute('aria-labelledby')).toBe('cap');
  } finally {
    await view.unmount();
  }
});
