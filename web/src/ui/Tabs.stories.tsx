import type { Meta, StoryObj } from '@storybook/react-vite';
import { useId, useState } from 'react';
import { expect, userEvent, within } from 'storybook/test';

import { TabPanel, Tabs } from './Tabs.tsx';

type Section = 'accounts' | 'connections' | 'federation';
const sections = [
  { id: 'accounts', label: 'Accounts' },
  { id: 'connections', label: 'Connections' },
  { id: 'federation', label: 'Federation' },
] as const satisfies readonly { id: Section; label: string }[];

// One generated prefix per Demo: the Docs page mounts every example inline in
// one document, so a fixed prefix would repeat tab and panel ids across
// examples and cross their aria-controls / aria-labelledby references.
function Demo({ initial = 'accounts', label = 'Machine access sections' }: { initial?: Section; label?: string }) {
  const [selected, setSelected] = useState<Section>(initial);
  const idPrefix = useId();
  return (
    <>
      <Tabs label={label} idPrefix={idPrefix} tabs={sections} selected={selected} onSelect={setSelected} />
      <TabPanel idPrefix={idPrefix} selected={selected}>
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
    // Home returns to the first, and the moved tab takes focus, not just the
    // selection: one tab stop means the roving tabindex has to follow.
    await userEvent.keyboard('{Home}');
    const first = canvas.getByRole('tab', { name: 'Accounts' });
    await expect(first).toHaveAttribute('aria-selected', 'true');
    await expect(first).toHaveFocus();
    // Both arrows wrap: left off the first lands on the last, right off the
    // last lands on the first.
    await userEvent.keyboard('{ArrowLeft}');
    const last = canvas.getByRole('tab', { name: 'Federation' });
    await expect(last).toHaveAttribute('aria-selected', 'true');
    await expect(last).toHaveFocus();
    await userEvent.keyboard('{ArrowRight}');
    await expect(first).toHaveAttribute('aria-selected', 'true');
    await expect(first).toHaveFocus();
    // And left moves one without wrapping.
    await userEvent.keyboard('{ArrowRight}{ArrowLeft}');
    await expect(first).toHaveAttribute('aria-selected', 'true');
    await expect(first).toHaveFocus();
    // Every other key is the tablist's business to ignore: no move, no focus
    // change, and the panel stays where it was.
    await userEvent.keyboard('{Enter}a{ArrowDown}');
    await expect(first).toHaveAttribute('aria-selected', 'true');
    await expect(first).toHaveFocus();
    await expect(canvas.getByRole('tabpanel', { name: 'Accounts' })).toBeVisible();
    // Sized on the control token, not the 44px floor.
    const control = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--control'));
    await expect(Math.round(tabs[0]?.getBoundingClientRect().height ?? 0)).toBe(Math.round(control));
  },
};

// Two examples in one document, as the Docs page composes them: operating the
// second keeps focus, selection and the aria references inside the second.
export const TwoDemosStayIndependent: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <section aria-label="First example">
        <Demo label="First" />
      </section>
      <section aria-label="Second example">
        <Demo label="Second" />
      </section>
    </div>
  ),
  play: async ({ canvas }) => {
    const first = within(canvas.getByRole('region', { name: 'First example' }));
    const second = within(canvas.getByRole('region', { name: 'Second example' }));
    const ids = canvas.getAllByRole('tab').map((tab) => tab.id);
    await expect(new Set(ids).size).toBe(ids.length);
    second.getAllByRole('tab')[0]?.focus();
    await userEvent.keyboard('{ArrowRight}');
    await expect(second.getByRole('tab', { name: 'Connections' })).toHaveAttribute('aria-selected', 'true');
    await expect(second.getByRole('tab', { name: 'Connections' })).toHaveFocus();
    await expect(second.getByRole('tabpanel', { name: 'Connections' })).toBeVisible();
    await expect(first.getByRole('tab', { name: 'Accounts' })).toHaveAttribute('aria-selected', 'true');
    await expect(first.getByRole('tabpanel', { name: 'Accounts' })).toBeVisible();
  },
};
