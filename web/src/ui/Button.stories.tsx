import type { Meta, StoryObj } from '@storybook/react-vite';
import { MemoryRouter, Link } from 'react-router';
import { expect, fn, userEvent } from 'storybook/test';

import { inkContrast, tokenColor, tokenPx } from '../../.storybook/tokens.ts';
import { design } from '../../.storybook/design.ts';
import { Glyph } from './Glyph.tsx';
import { Button } from './Button.tsx';

const meta = {
  title: 'Design system/Button',
  id: 'ui-button',
  component: Button,
  parameters: { docs: { description: { component: 'Native button with shared action styling and an accessible-name requirement for icon-only controls. Secondary is the default; type preserves native form behavior.' } } },
  tags: ['ai-generated'],
  args: { children: 'Save changes', onClick: fn() },
  argTypes: {
    variant: {
      description: 'Action priority: primary leads, danger deletes, quiet is tertiary. A variant never changes the size.',
      control: 'select', options: ['secondary', 'primary', 'danger', 'quiet'],
      table: { type: { summary: "'primary' | 'secondary' | 'danger' | 'quiet'" }, defaultValue: { summary: 'secondary' } },
    },
    children: { description: 'Visible action label, or graphic for an icon-only control.', control: 'text', table: { type: { summary: 'ReactNode' } } },
    type: { description: 'Native button type. Omitted preserves submit behavior inside a form.', control: 'select', options: ['button', 'submit', 'reset'], table: { type: { summary: "'button' | 'submit' | 'reset'" }, defaultValue: { summary: 'native submit in forms' } } },
    disabled: { description: 'Native disabled control; cannot receive focus or activate.', control: 'boolean', table: { type: { summary: 'boolean' }, defaultValue: { summary: 'false' } } },
    icon: { description: 'Icon-only control. Requires an accessible aria-label.', control: 'boolean', table: { type: { summary: 'boolean' }, defaultValue: { summary: 'false' } } },
    'aria-label': { description: 'Required accessible name when icon is true.', control: 'text', table: { type: { summary: 'string' } } },
    onClick: { description: 'Native click handler.', control: false, table: { type: { summary: 'MouseEventHandler<HTMLButtonElement>' } } },
  },
} satisfies Meta<typeof Button>;

export default meta;
type Story = StoryObj<typeof meta>;

// Secondary and quiet buttons carry the accent-tinted boundary and label, so a
// button never reads as one more bordered row or card.
export const Secondary: Story = {
  parameters: { design: design('Button/Secondary') },
  play: async ({ canvas }) => {
    const button = canvas.getByRole('button', { name: 'Save changes' });
    await expect(getComputedStyle(button).borderTopColor).toBe(tokenColor('--btn-line'));
    await expect(getComputedStyle(button).color).toBe(tokenColor('--btn-tx'));
    await expect(getComputedStyle(button).borderTopColor).not.toBe(tokenColor('--line'));
  },
};
export const Primary: Story = { args: { variant: 'primary' }, parameters: { design: design('Button/Primary') } };
export const Disabled: Story = { args: { disabled: true }, parameters: { design: design('Button/Disabled') } };
export const Icon: Story = {
  args: { icon: true, 'aria-label': 'Toggle theme', children: <Glyph name="moon" /> },
  parameters: { design: design('Button/Icon') },
};

// The one story the design tuning actually needs: every variant side by side,
// swept by the theme toolbar for light/dark.

export const AllVariants: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap', alignItems: 'start' }}>
      {[
        { caption: 'Secondary (default)', control: <Button>Save changes</Button> },
        { caption: 'Primary action', control: <Button variant="primary">Save changes</Button> },
        { caption: 'Destructive action', control: <Button variant="danger">Delete adapter</Button> },
        { caption: 'Tertiary action', control: <Button variant="quiet">History</Button> },
        { caption: 'Disabled', control: <Button disabled>Save changes</Button> },
        { caption: 'Primary disabled', control: <Button variant="primary" disabled>Save changes</Button> },
        { caption: 'Named icon', control: <Button icon aria-label="Toggle theme"><Glyph name="moon" /></Button> },
      ].map(({ caption, control }) => (
        <figure key={caption} style={{ margin: 0, display: 'grid', gap: 8 }}>
          <figcaption>{caption}</figcaption>
          {control}
        </figure>
      ))}
    </div>
  ),
};

// Navigation in the button skin: a plain anchor and a router Link. A button to
// look at is never underlined, at rest, on hover or with keyboard focus.
export const LinkSkin: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <MemoryRouter>
      <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
        <a className="btn" href="#members" onClick={(event) => event.preventDefault()}>
          open members
        </a>
        <Link className="btn btn--quiet" to="/history">
          View this key’s revision history
        </Link>
      </div>
    </MemoryRouter>
  ),
  play: async ({ canvas }) => {
    for (const name of ['open members', 'View this key’s revision history']) {
      const link = canvas.getByRole('link', { name });
      await expect(getComputedStyle(link).textDecorationLine).toBe('none');
      await userEvent.hover(link);
      await expect(getComputedStyle(link).textDecorationLine).toBe('none');
      link.focus();
      await expect(getComputedStyle(link).textDecorationLine).toBe('none');
      await userEvent.unhover(link);
    }
  },
};

// Every variant enabled beside disabled. Unavailable is one flat neutral look:
// a tinted fill, no border, a faint label and the not-allowed cursor, so a
// disabled primary never passes for an enabled secondary.
export const EnabledVsDisabled: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, max-content)', gap: 12, alignItems: 'center' }}>
      {(['secondary', 'primary', 'danger', 'quiet'] as const).flatMap((variant) => [
        <Button key={`${variant}-on`} variant={variant} data-pair={variant}>{`${variant} enabled`}</Button>,
        <Button key={`${variant}-off`} variant={variant} data-pair={variant} disabled>{`${variant} disabled`}</Button>,
      ])}
      <a className="btn" href="#members" data-pair="link" onClick={(event) => event.preventDefault()}>link enabled</a>
      <a className="btn" href="#members" data-pair="link" aria-disabled="true" onClick={(event) => event.preventDefault()}>link disabled</a>
    </div>
  ),
  play: async ({ canvasElement }) => {
    for (const pair of ['secondary', 'primary', 'danger', 'quiet', 'link']) {
      const [enabled, disabled] = [...canvasElement.querySelectorAll<HTMLElement>(`[data-pair="${pair}"]`)];
      if (enabled === undefined || disabled === undefined) throw new Error(`${pair} pair missing`);
      const [on, off] = [getComputedStyle(enabled), getComputedStyle(disabled)];
      await expect(off.backgroundColor).not.toBe(on.backgroundColor);
      await expect(off.cursor).toBe('not-allowed');
      await expect(on.cursor).toBe('pointer');
      await expect(off.color).toBe(tokenColor('--tx-faint'));
      await expect(off.opacity).toBe('1');
      // Faint but legible: the label clears 3:1 on its own fill, although
      // WCAG exempts a disabled control.
      await expect(inkContrast(disabled)).toBeGreaterThanOrEqual(3);
      await expect(off.borderTopColor).toBe('rgba(0, 0, 0, 0)');
      // No hover change on the unavailable control.
      const resting = off.backgroundColor;
      await userEvent.hover(disabled);
      await expect(getComputedStyle(disabled).backgroundColor).toBe(resting);
      await userEvent.unhover(disabled);
    }
    // A disabled primary does not pass for an enabled secondary.
    const secondary = canvasElement.querySelector<HTMLElement>('[data-pair="secondary"]');
    const primaryOff = canvasElement.querySelectorAll<HTMLElement>('[data-pair="primary"]')[1];
    if (secondary === null || primaryOff === undefined) throw new Error('buttons missing');
    await expect(getComputedStyle(primaryOff).backgroundColor).not.toBe(getComputedStyle(secondary).backgroundColor);
    await expect(getComputedStyle(primaryOff).borderTopColor).not.toBe(getComputedStyle(secondary).borderTopColor);
  },
};

// A button disabled by its fieldset (no attribute of its own) wears the same
// disabled skin as one with `disabled`, and so does a pressed toggle that is
// disabled: the pressed skin yields to it. It is unavailable, not only grey:
// it takes no focus, no click and no hover change.
const onFieldsetClick = fn();
export const DisabledByFieldset: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: 'flex', gap: 12 }}>
      <Button data-ref="attribute" disabled>Disabled</Button>
      <fieldset disabled style={{ border: 0, margin: 0, padding: 0 }}>
        <Button data-ref="fieldset" onClick={onFieldsetClick}>In a disabled fieldset</Button>
      </fieldset>
      <Button data-ref="pressed" aria-pressed="true" disabled>
        Pressed
      </Button>
    </div>
  ),
  play: async ({ canvasElement }) => {
    const attribute = canvasElement.querySelector<HTMLElement>('[data-ref="attribute"]');
    const inherited = canvasElement.querySelector<HTMLButtonElement>('[data-ref="fieldset"]');
    if (attribute === null || inherited === null) throw new Error('buttons missing');
    await expect(inherited.hasAttribute('disabled')).toBe(false);
    await expect(inherited.matches(':disabled')).toBe(true);
    const [a, b] = [getComputedStyle(attribute), getComputedStyle(inherited)];
    await expect(b.backgroundColor).toBe(a.backgroundColor);
    await expect(b.color).toBe(a.color);
    await expect(b.cursor).toBe('not-allowed');
    await expect(b.borderTopColor).toBe(a.borderTopColor);
    inherited.focus();
    await expect(inherited).not.toHaveFocus();
    await userEvent.click(inherited, { pointerEventsCheck: 0 });
    await expect(onFieldsetClick).not.toHaveBeenCalled();
    await userEvent.hover(inherited);
    await expect(getComputedStyle(inherited).backgroundColor).toBe(a.backgroundColor);
    await userEvent.unhover(inherited);
    const pressed = canvasElement.querySelector<HTMLElement>('[data-ref="pressed"]');
    if (pressed === null) throw new Error('pressed button missing');
    const p = getComputedStyle(pressed);
    await expect(p.backgroundColor).toBe(a.backgroundColor);
    await expect(p.borderColor).toBe(a.borderColor);
    await expect(p.color).toBe(a.color);
  },
};

const VARIANTS = ['secondary', 'primary', 'danger', 'quiet'] as const;

// One size for every variant, enabled and disabled, plus the icon-only
// square: a variant never changes height, padding or type size. The
// `btn--compact` class (the matrix column header's anchor and summary) is the
// one smaller box: `--hit-min` on a fine pointer, `--control` on a coarse one.
export const Sizes: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: 'grid', gap: 16 }}>
      {(['default', 'compact'] as const).map((size) => (
        <div key={size} className="actions" data-size={size}>
          {VARIANTS.flatMap((variant) => [
            <Button key={`${variant}-on`} className={size === 'compact' ? 'btn--compact' : undefined} variant={variant}>{variant}</Button>,
            <Button key={`${variant}-off`} className={size === 'compact' ? 'btn--compact' : undefined} variant={variant} disabled>{`${variant} disabled`}</Button>,
          ])}
          {size === 'default' && <Button icon aria-label="Toggle theme"><Glyph name="moon" /></Button>}
        </div>
      ))}
    </div>
  ),
  play: async ({ canvasElement }) => {
    const fine = matchMedia('(pointer: fine)').matches;
    const expected = { default: tokenPx('--control'), compact: fine ? tokenPx('--hit-min') : tokenPx('--control') };
    for (const size of ['default', 'compact'] as const) {
      const buttons = [...canvasElement.querySelectorAll<HTMLElement>(`[data-size="${size}"] > .btn`)];
      await expect(buttons).toHaveLength(VARIANTS.length * 2 + (size === 'default' ? 1 : 0));
      const first = getComputedStyle(buttons[0]!);
      for (const button of buttons) {
        const box = button.getBoundingClientRect();
        await expect(box.height).toBe(expected[size]);
        if (button.classList.contains('btn--icon')) {
          await expect(box.width).toBe(box.height);
          // The glyph fills about half the square, not a fixed box that reads
          // tiny once the button grows to the touch floor.
          const glyph = button.querySelector('.glyph');
          if (glyph === null) throw new Error('icon button has no glyph');
          const ratio = glyph.getBoundingClientRect().height / box.height;
          await expect(ratio).toBeGreaterThan(0.45);
          await expect(ratio).toBeLessThan(0.55);
          continue;
        }
        const style = getComputedStyle(button);
        await expect(style.paddingLeft).toBe(first.paddingLeft);
        await expect(style.fontSize).toBe(first.fontSize);
      }
    }
  },
};

// The action row: buttons `--space-2` apart, start-aligned, primary first; a
// dialog footer is the same row at the trailing edge with the primary last.
export const ActionRow: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: 'grid', gap: 24, width: 560 }}>
      <div className="actions" data-row="page">
        <Button variant="primary">Apply filter</Button>
        <Button variant="quiet">Clear</Button>
        <Button disabled>Export JSONL</Button>
      </div>
      <div className="dialog__actions" data-row="footer">
        <Button>Cancel</Button>
        <Button variant="primary">Save</Button>
      </div>
    </div>
  ),
  play: async ({ canvasElement }) => {
    const gap = tokenPx('--space-2');
    for (const row of canvasElement.querySelectorAll<HTMLElement>('[data-row]')) {
      const boxes = [...row.children].map((child) => child.getBoundingClientRect());
      for (let i = 1; i < boxes.length; i++) {
        await expect(boxes[i]!.left - boxes[i - 1]!.right).toBe(gap);
        await expect(boxes[i]!.height).toBe(boxes[0]!.height);
      }
    }
    const page = canvasElement.querySelector<HTMLElement>('[data-row="page"]')!;
    const footer = canvasElement.querySelector<HTMLElement>('[data-row="footer"]')!;
    await expect(page.firstElementChild!.getBoundingClientRect().left).toBe(page.getBoundingClientRect().left);
    await expect(footer.lastElementChild!.getBoundingClientRect().right).toBe(footer.getBoundingClientRect().right);
    await expect(footer.lastElementChild).toHaveClass('btn--primary');
  },
};
