import type { Meta, StoryObj } from '@storybook/react-vite';
import { useId, useState } from 'react';
import { expect, userEvent, within } from 'storybook/test';

import { tokenColor } from '../../.storybook/tokens.ts';
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
  parameters: { docs: { description: { component: "Controlled tabs and their labelled panels. Each example owns a generated identifier prefix so inline Docs instances remain independent; arrow navigation updates the selected tab and its associated panel." } } },
  title: 'Design system/Tabs',
  id: 'ui-tabs',
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

// A count is a badge after the label, not "(n)" text; the accessible
// name still carries it ("Accounts, 3"), and a zero count is muted.
export const WithCounts: Story = {
  render: () => {
    const counted = [
      { id: 'accounts', label: 'Accounts', count: 3 },
      { id: 'connections', label: 'Connections', count: 0 },
      { id: 'federation', label: 'Federation', count: 'unknown' as const },
    ] as const;
    return (
      <>
        <Tabs label="Machine access sections" idPrefix="counted" tabs={counted} selected="accounts" onSelect={() => {}} />
        <TabPanel idPrefix="counted" selected="accounts"><p style={{ margin: 0 }}>Accounts panel</p></TabPanel>
      </>
    );
  },
  play: async ({ canvas }) => {
    const accounts = canvas.getByRole('tab', { name: 'Accounts, 3' });
    const badge = accounts.querySelector('.tab__count');
    await expect(badge?.textContent).toBe('3');
    await expect(getComputedStyle(badge as Element).borderRadius).not.toBe('0px');
    await expect(accounts.textContent).not.toMatch(/\(\d+\)/);
    await expect(canvas.getByRole('tab', { name: 'Connections, 0' }).querySelector('.tab__count')).toHaveAttribute('data-zero');
    await expect(canvas.getByRole('tab', { name: 'Federation, unknown' })).toBeVisible();
    // A count pill; the selected tab's count wears the accent, a zero count
    // the faint ink.
    await expect(Number.parseFloat(getComputedStyle(badge as Element).borderTopLeftRadius)).toBeGreaterThanOrEqual(
      (badge as Element).getBoundingClientRect().height / 2,
    );
    await expect(getComputedStyle(badge as Element).backgroundColor).toBe(tokenColor('--accent'));
    const zero = canvas.getByRole('tab', { name: 'Connections, 0' }).querySelector('.tab__count') as Element;
    await expect(getComputedStyle(zero).color).toBe(tokenColor('--tx-faint'));
  },
};

// Five tabs that do not fit one row at a phone width. Reused from the
// same ids a real machine-access tab bar would carry.
type WideId = 'accounts' | 'federation' | 'providers' | 'leases' | 'certificates';
const wideTabs = [
  { id: 'accounts', label: 'Service accounts' },
  { id: 'federation', label: 'Federation' },
  { id: 'providers', label: 'Providers' },
  { id: 'leases', label: 'Leases' },
  { id: 'certificates', label: 'Certificates' },
] as const satisfies readonly { id: WideId; label: string }[];

function WideDemo({ initial = 'accounts' }: { initial?: WideId }) {
  const [selected, setSelected] = useState<WideId>(initial);
  const idPrefix = useId();
  return (
    <>
      <Tabs label="Machine access sections" idPrefix={idPrefix} tabs={wideTabs} selected={selected} onSelect={setSelected} />
      <TabPanel idPrefix={idPrefix} selected={selected}>
        <p style={{ margin: 0 }}>{wideTabs.find((tab) => tab.id === selected)?.label} panel</p>
      </TabPanel>
    </>
  );
}

// At the phone breakpoint the row wraps onto pills across more than one
// row instead of scrolling sideways, and the row itself never overflows.
export const PhoneWraps: Story = {
  render: () => <WideDemo />,
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas, canvasElement }) => {
    const list = canvasElement.querySelector('.tabs');
    if (!(list instanceof HTMLElement)) throw new Error('tablist missing');
    await expect(list.scrollWidth).toBeLessThanOrEqual(list.clientWidth);
    const tabs = canvas.getAllByRole('tab');
    const tops = new Set(tabs.map((tab) => Math.round(tab.getBoundingClientRect().top)));
    await expect(tops.size).toBeGreaterThan(1);
    const touch = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--touch'));
    for (const tab of tabs) {
      await expect(tab.getBoundingClientRect().height).toBeGreaterThanOrEqual(touch - 1);
    }
    // Each row's pills grow to share its leftover space, so the row spans
    // the bar edge to edge: the first pill's
    // left matches the bar's left and the last pill's right matches the
    // bar's right, in every row, not only the last.
    const listBox = list.getBoundingClientRect();
    const rows = new Map<number, typeof tabs>();
    for (const tab of tabs) {
      const top = Math.round(tab.getBoundingClientRect().top);
      rows.set(top, [...(rows.get(top) ?? []), tab]);
    }
    for (const row of rows.values()) {
      const sorted = row.toSorted((a, b) => a.getBoundingClientRect().left - b.getBoundingClientRect().left);
      const first = sorted[0]!.getBoundingClientRect();
      const last = sorted.at(-1)!.getBoundingClientRect();
      await expect(Math.abs(first.left - listBox.left)).toBeLessThanOrEqual(1);
      await expect(Math.abs(last.right - listBox.right)).toBeLessThanOrEqual(1);
    }
    // An unselected pill carries a discreet but visible border, and every
    // pill (selected or not) is bold and smaller than the desktop tab's text,
    // so weight and size never shift when the selection moves.
    const unselected = tabs.find((tab) => tab.getAttribute('aria-selected') === 'false');
    if (unselected === undefined) throw new Error('no unselected tab');
    const unselectedStyle = getComputedStyle(unselected);
    await expect(Number.parseFloat(unselectedStyle.borderTopWidth)).toBeGreaterThanOrEqual(1);
    await expect(unselectedStyle.borderTopStyle).toBe('solid');
    // The desktop tab sets no font-size of its own, so it inherits the body
    // size (--fs-md); the phone pill's token must read smaller than that.
    const desktopFontSize = Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--fs-md'));
    for (const tab of tabs) {
      const style = getComputedStyle(tab);
      await expect(Number.parseInt(style.fontWeight, 10)).toBeGreaterThanOrEqual(600);
      await expect(Number.parseFloat(style.fontSize)).toBeLessThan(desktopFontSize);
    }
    // Selecting a pill moves no pill and resizes no pill: every pill's full
    // box (x, y, width, height) and the row's own height are identical before
    // and after, so a border, padding or weight difference between the two
    // states can never show up as a layout shift.
    const boxes = () => canvas.getAllByRole('tab').map((tab) => {
      const box = tab.getBoundingClientRect();
      return [box.left, box.top, box.width, box.height].map(Math.round).join(',');
    });
    const before = boxes();
    const rowHeightBefore = Math.round(list.getBoundingClientRect().height);
    await userEvent.click(canvas.getByRole('tab', { name: 'Federation' }));
    await expect(canvas.getByRole('tab', { name: 'Federation' })).toHaveAttribute('aria-selected', 'true');
    await expect(boxes()).toEqual(before);
    await expect(Math.round(list.getBoundingClientRect().height)).toBe(rowHeightBefore);
  },
};

// Selecting a tab moves no tab: every box is identical before and after.
export const SelectionKeepsBoxes: Story = {
  play: async ({ canvas }) => {
    const boxes = () => canvas.getAllByRole('tab').map((tab) => {
      const box = tab.getBoundingClientRect();
      return [box.left, box.width, box.height].map(Math.round).join(',');
    });
    const before = boxes();
    await userEvent.click(canvas.getByRole('tab', { name: 'Federation' }));
    await expect(canvas.getByRole('tab', { name: 'Federation' })).toHaveAttribute('aria-selected', 'true');
    await expect(boxes()).toEqual(before);
  },
};

// Wider than a phone, an overflowing row scrolls sideways with a visible
// (thin) scrollbar as the cue, and a selected tab restored past the edge is
// scrolled into view inside the row, without scrolling the page.
export const OverflowScrollsRow: Story = {
  render: () => (
    <div style={{ width: 240 }}>
      <WideDemo initial="certificates" />
    </div>
  ),
  play: async ({ canvas, canvasElement }) => {
    const list = canvasElement.querySelector('.tabs');
    if (!(list instanceof HTMLElement)) throw new Error('tablist missing');
    await expect(getComputedStyle(list).scrollbarWidth).toBe('thin');
    await expect(list.scrollLeft).toBeGreaterThan(0);
    const tab = canvas.getByRole('tab', { name: 'Certificates' });
    await expect(tab.getBoundingClientRect().right).toBeLessThanOrEqual(list.getBoundingClientRect().right + 1);
    await expect(document.scrollingElement?.scrollLeft ?? 0).toBe(0);
  },
};
