import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { expect, userEvent } from 'storybook/test';

import { TabPanel, Tabs } from './Tabs.tsx';

type Section = 'accounts' | 'connections' | 'federation';
const sections = [
  { id: 'accounts', label: 'Accounts' },
  { id: 'connections', label: 'Connections' },
  { id: 'federation', label: 'Federation' },
] as const satisfies readonly { id: Section; label: string }[];

function Demo({ initial = 'accounts' }: { initial?: Section }) {
  const [selected, setSelected] = useState<Section>(initial);
  return (
    <>
      <Tabs label="Machine access sections" idPrefix="machine" tabs={sections} selected={selected} onSelect={setSelected} />
      <TabPanel idPrefix="machine" selected={selected}>
        <p style={{ margin: 0 }}>{sections.find((s) => s.id === selected)?.label} panel</p>
      </TabPanel>
    </>
  );
}

const meta = {
  title: 'ui/Tabs',
  component: Demo,
  tags: ['ai-generated'],
} satisfies Meta<typeof Demo>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};
export const SecondSelected: Story = { args: { initial: 'connections' } };

// One tab stop; arrows move and select; the panel follows.
export const KeyboardMovesAndSelects: Story = {
  play: async ({ canvas }) => {
    const tabs = canvas.getAllByRole('tab');
    await expect(tabs.filter((tab) => tab.tabIndex === 0)).toHaveLength(1);
    tabs[0]?.focus();
    await userEvent.keyboard('{ArrowRight}');
    await expect(canvas.getByRole('tab', { name: 'Connections' })).toHaveAttribute('aria-selected', 'true');
    await expect(canvas.getByRole('tabpanel', { name: 'Connections' })).toBeVisible();
    await userEvent.keyboard('{End}');
    await expect(canvas.getByRole('tab', { name: 'Federation' })).toHaveAttribute('aria-selected', 'true');
    // Sized on the control token, not the 44px floor.
    const control = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--control'));
    await expect(Math.round(tabs[0]?.getBoundingClientRect().height ?? 0)).toBe(Math.round(control));
  },
};
