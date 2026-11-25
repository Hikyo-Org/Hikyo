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

it('refuses an unnamed region or one with two names at compile time', () => {
  // @ts-expect-error a region needs aria-labelledby or aria-label
  const unnamed = <TableScroll><table /></TableScroll>;
  // @ts-expect-error aria-labelledby and aria-label are exclusive
  const both = <TableScroll aria-label="Keys" aria-labelledby="cap"><table /></TableScroll>;
  expect([unnamed, both]).toHaveLength(2);
});

it('takes its name from aria-label when the table has no caption', async () => {
  const view = await renderForm(
    <TableScroll aria-label="Policies">
      <table />
    </TableScroll>,
  );
  try {
    const region = view.container.querySelector('[role="region"][aria-label="Policies"]');
    expect(region).not.toBeNull();
    expect(region?.getAttribute('aria-label')).toBe('Policies');
    expect(region?.hasAttribute('aria-labelledby')).toBe(false);
    expect(region?.getAttribute('tabindex')).toBe('0');
  } finally {
    await view.unmount();
  }
});
