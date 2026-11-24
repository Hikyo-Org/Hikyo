import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent } from 'storybook/test';

import { TableScroll } from './TableScroll.tsx';

const meta = {
  title: 'Design system/TableScroll',
  id: 'ui-table-scroll',
  component: TableScroll,
  tags: ['ai-generated'],
  parameters: {
    docs: {
      description: {
        component:
          'The one wrapper for a scrollable data table: a named, keyboard-focusable region (`role="region"`, `tabIndex` 0) so a table that overflows on a phone is reachable without a mouse. Name it with `aria-labelledby` pointing at the table caption id, or `aria-label` where there is no caption.',
      },
    },
  },
} satisfies Meta<typeof TableScroll>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { 'aria-labelledby': 'table-scroll-caption', children: null },
  render: () => (
    <TableScroll aria-labelledby="table-scroll-caption">
      <table>
        <caption id="table-scroll-caption" className="visually-hidden">Example keys and their owners</caption>
        <thead>
          <tr>
            <th scope="col">Key</th>
            <th scope="col">Owner</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>payments/api-token</td>
            <td>platform</td>
          </tr>
        </tbody>
      </table>
    </TableScroll>
  ),
  play: async ({ canvasElement }) => {
    await userEvent.tab();
    const region = canvasElement.querySelector('.table-scroll');
    await expect(region).toHaveFocus();
    await expect(region).toHaveAccessibleName('Example keys and their owners');
  },
};
