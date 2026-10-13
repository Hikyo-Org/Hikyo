import type { Meta, StoryObj } from '@storybook/react-vite';
import { type CSSProperties, useRef, useState } from 'react';
import { expect, userEvent, waitFor, within } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { Button } from './Button.tsx';
import { Glyph } from './Glyph.tsx';
import { Input } from './Input.tsx';
import { SidePanel } from './SidePanel.tsx';

// The panel is routed in the app (closing navigates back); here a state flag
// stands in for the route, and the opener is an ordinary button.
function Demo({ rows = 40, floorSlack }: { rows?: number; floorSlack?: number }) {
  const [open, setOpen] = useState(true);
  const opener = useRef<HTMLButtonElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  return (
    <div style={floorSlack === undefined ? undefined : chrome(floorSlack)}>
      <Button ref={opener} type="button" onClick={() => setOpen(true)}>
        Open DATABASE_URL
      </Button>
      {open ? (
        <SidePanel
          label="Key declaration"
          onClose={() => setOpen(false)}
          initialFocus={heading}
          opener={opener}
          header={
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <h2 ref={heading} tabIndex={-1} className="mono" style={{ margin: 0 }}>
                DATABASE_URL
              </h2>
              <Button type="button" style={{ marginLeft: 'auto' }} onClick={() => setOpen(false)}>
                <Glyph name="cross" /> Close
              </Button>
            </div>
          }
        >
          {Array.from({ length: rows }, (_, index) => (
            <p key={index} style={{ margin: 0 }}>{`Declaration detail line ${String(index + 1)}`}</p>
          ))}
          <Button type="button" variant="primary">Save declaration</Button>
        </SidePanel>
      ) : null}
    </div>
  );
}

const meta = {
  title: 'Design system/SidePanel',
  id: 'ui-sidepanel',
  component: Demo,
  tags: ['ai-generated'],
  parameters: {
    ...topLayerDocs,
    docs: {
      ...topLayerDocs.docs,
      description: {
        component:
          'A routed detail surface that slides in from the right over the content area. The scrim stops at the app\'s side menu; the panel starts one sidebar width further right, or at the menu when that would leave it narrower than `--width-side-panel-min`, and spans the full width on a phone (and here, outside the app chrome). The header stays pinned while the body scrolls; Escape, an outside press (not on a toast) and the header Close all close it, and focus returns to the opener. Once something in the body has been edited, an outside press does nothing and Escape or Close asks "Discard unsaved changes?" first; the caller closing it after a save never asks. Modal over the content area: the rest of the page\'s `<main>` is inert and Tab wraps inside the panel, while the side menu outside `<main>` stays live for the pointer.',
      },
    },
  },
} satisfies Meta<typeof Demo>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Open: Story = {};

// Focus lands on the heading; scrolling the body leaves the header in place.
export const PinnedHeader: Story = {
  play: async ({ canvas }) => {
    const heading = canvas.getByRole('heading', { name: 'DATABASE_URL' });
    await waitFor(() => expect(heading).toHaveFocus());
    const panel = canvas.getByRole('complementary', { name: 'Key declaration' });
    const body = panel.querySelector('.side-panel__body');
    if (!(body instanceof HTMLElement)) throw new Error('side panel body missing');
    const before = heading.getBoundingClientRect().top;
    body.scrollTop = body.scrollHeight;
    await expect(body.scrollTop).toBeGreaterThan(0);
    await expect(heading.getBoundingClientRect().top).toBe(before);
  },
};

// Escape closes and returns focus to the opener; so do a press on the scrim
// and the header's own Close.
export const Dismissal: Story = {
  play: async ({ canvas }) => {
    const opener = canvas.getByRole('button', { name: 'Open DATABASE_URL' });
    await waitFor(() => expect(canvas.getByRole('heading', { name: 'DATABASE_URL' })).toHaveFocus());
    await userEvent.keyboard('{Escape}');
    await expect(canvas.queryByRole('complementary', { name: 'Key declaration' })).toBeNull();
    await waitFor(() => expect(opener).toHaveFocus());

    await userEvent.click(opener);
    const scrim = document.querySelector('.side-panel__scrim');
    if (!(scrim instanceof HTMLElement)) throw new Error('scrim missing');
    await userEvent.click(scrim);
    await expect(canvas.queryByRole('complementary', { name: 'Key declaration' })).toBeNull();
    await waitFor(() => expect(opener).toHaveFocus());

    await userEvent.click(opener);
    await userEvent.click(canvas.getByRole('button', { name: 'Close' }));
    await expect(canvas.queryByRole('complementary', { name: 'Key declaration' })).toBeNull();
    await waitFor(() => expect(opener).toHaveFocus());
  },
};

// A native dialog the panel opened owns Escape and outside presses: neither
// collapses the panel behind it. Once the dialog is gone, Escape is the
// panel's again.
function WithSheet() {
  const [open, setOpen] = useState(true);
  const sheet = useRef<HTMLDialogElement>(null);
  return open ? (
    <SidePanel title="DATABASE_URL" onClose={() => setOpen(false)}>
      <Button type="button" onClick={() => sheet.current?.showModal()}>Open sheet</Button>
      <dialog ref={sheet} aria-label="Sheet">
        <p style={{ margin: 0 }}>Reauthenticate to continue.</p>
      </dialog>
    </SidePanel>
  ) : null;
}

export const DialogOwnsDismissal: Story = {
  render: () => <WithSheet />,
  play: async ({ canvas }) => {
    await userEvent.click(canvas.getByRole('button', { name: 'Open sheet' }));
    const sheet = await canvas.findByRole('dialog', { name: 'Sheet' });
    await userEvent.pointer({ keys: '[MouseLeft]', target: document.body });
    await userEvent.keyboard('{Escape}');
    await expect(canvas.getByRole('complementary', { name: 'DATABASE_URL' })).toBeVisible();
    if (!(sheet instanceof HTMLDialogElement)) throw new Error('sheet is not a dialog');
    sheet.close();
    await userEvent.keyboard('{Escape}');
    await expect(canvas.queryByRole('complementary', { name: 'DATABASE_URL' })).toBeNull();
  },
};

// Modal over the content: the page behind is inert while the panel is open,
// Tab wraps inside the panel, Escape closes it from anywhere in the document,
// and pressing a toast leaves it open.
export const Modality: Story = {
  args: { rows: 2 },
  play: async ({ canvas }) => {
    const opener = canvas.getByRole('button', { name: 'Open DATABASE_URL' });
    const heading = canvas.getByRole('heading', { name: 'DATABASE_URL' });
    await waitFor(() => expect(heading).toHaveFocus());
    await expect(opener.closest('[inert]')).not.toBeNull();

    await userEvent.tab({ shift: true });
    await expect(canvas.getByRole('button', { name: 'Save declaration' })).toHaveFocus();
    await userEvent.tab();
    await expect(canvas.getByRole('button', { name: 'Close' })).toHaveFocus();

    const toast = document.createElement('div');
    toast.className = 'toast';
    toast.innerHTML = '<button type="button">Dismiss notification</button>';
    document.body.append(toast);
    await userEvent.click(toast.querySelector('button') as HTMLElement);
    toast.remove();
    await expect(canvas.getByRole('complementary', { name: 'Key declaration' })).toBeVisible();

    (document.activeElement as HTMLElement | null)?.blur();
    await userEvent.keyboard('{Escape}');
    await expect(canvas.queryByRole('complementary', { name: 'Key declaration' })).toBeNull();
    await expect(opener.closest('[inert]')).toBeNull();
    await waitFor(() => expect(opener).toHaveFocus());
  },
};

// Geometry. The app's chrome sets `--chrome-menu` (rail + sidebar) and
// `--side-panel-gutter` (one sidebar width); here a wrapper sets them, and
// the floor is sized against the story's own viewport at render, so each
// regime holds at any width.
const MENU = 40;
const GUTTER = 60;
const edges = async (canvasElement: HTMLElement) => {
  const panel = canvasElement.ownerDocument.querySelector('.side-panel');
  const scrim = canvasElement.ownerDocument.querySelector('.side-panel__scrim');
  if (!(panel instanceof HTMLElement) || !(scrim instanceof HTMLElement)) throw new Error('side panel missing');
  await waitFor(() => expect(panel.getAnimations()).toHaveLength(0));
  return { panel: panel.getBoundingClientRect().left, scrim: scrim.getBoundingClientRect().left };
};
function chrome(floorSlack: number): CSSProperties {
  if (floorSlack === Infinity) {
    return { ['--chrome-menu' as string]: '0px', ['--side-panel-gutter' as string]: '0px' };
  }
  return {
    ['--chrome-menu' as string]: `${String(MENU)}px`,
    ['--side-panel-gutter' as string]: `${String(GUTTER)}px`,
    // The panel's floor, `floorSlack` px under the width the gutter leaves.
    ['--width-side-panel-min' as string]: `${String(window.innerWidth - MENU - GUTTER - floorSlack)}px`,
  };
}

// Wide: room for the gutter, so the panel starts one gutter past the menu;
// the scrim still starts at the menu.
export const WideKeepsGutter: Story = {
  args: { rows: 4, floorSlack: 10 },
  play: async ({ canvasElement }) => {
    await expect(await edges(canvasElement)).toEqual({ panel: MENU + GUTTER, scrim: MENU });
  },
};

// Narrower: the gutter would push the panel under its floor, so it drops the
// gutter and starts at the menu.
export const NarrowDropsGutter: Story = {
  args: { rows: 4, floorSlack: -10 },
  play: async ({ canvasElement }) => {
    await expect(await edges(canvasElement)).toEqual({ panel: MENU, scrim: MENU });
  },
};

// Phone: the menu is a drawer, so the chrome zeroes both and the panel is the
// full screen width.
export const PhoneFullWidth: Story = {
  // Infinity stands for the phone chrome: menu and gutter both zero.
  args: { rows: 4, floorSlack: Infinity },
  play: async ({ canvasElement }) => {
    await expect(await edges(canvasElement)).toEqual({ panel: 0, scrim: 0 });
  },
};

// A panel in the built-in `title` mode (no custom `header`).
function TitleDemo() {
  const [open, setOpen] = useState(true);
  return (
    <div>
      <Button type="button" onClick={() => setOpen(true)}>Open DATABASE_URL</Button>
      {open ? (
        <SidePanel title="DATABASE_URL" onClose={() => setOpen(false)}>
          <p>Declaration detail.</p>
        </SidePanel>
      ) : null}
    </div>
  );
}

// `title` mode: the landmark takes the title as its name, the panel renders
// and focuses its own heading, its Close closes it, and focus goes back to
// whatever had it when the panel mounted.
export const TitleMode: Story = {
  render: () => <TitleDemo />,
  play: async ({ canvas }) => {
    // The story mounts open, with nothing focused; close and reopen from the
    // opener so the panel snapshots a real opener.
    await userEvent.click(await canvas.findByRole('button', { name: 'Close' }));
    const opener = canvas.getByRole('button', { name: 'Open DATABASE_URL' });
    await userEvent.click(opener);
    const panel = canvas.getByRole('complementary', { name: 'DATABASE_URL' });
    await waitFor(() => expect(canvas.getByRole('heading', { level: 2, name: 'DATABASE_URL' })).toHaveFocus());
    await userEvent.click(within(panel).getByRole('button', { name: 'Close' }));
    await expect(canvas.queryByRole('complementary', { name: 'DATABASE_URL' })).toBeNull();
    await waitFor(() => expect(opener).toHaveFocus());
  },
};

// The built-in `title`-mode header (no custom `header`) at a phone width:
// its Close collapses to icon-only, square, and keeps "Close" as its
// accessible name.
export const ClosePhone: Story = {
  render: () => <TitleDemo />,
  globals: { viewport: { value: 'phone', isRotated: false } },
  play: async ({ canvas }) => {
    const close = await canvas.findByRole('button', { name: 'Close' });
    const label = close.querySelector('.btn__label');
    if (!(label instanceof HTMLElement)) throw new Error('close label missing');
    await expect(label.getBoundingClientRect().width).toBeLessThanOrEqual(1);
    const box = close.getBoundingClientRect();
    await expect(Math.round(box.width)).toBe(Math.round(box.height));
  },
};

// A form in the panel. Typing marks it edited: an outside press then leaves
// it open, and Escape or Close asks before discarding. Saving closes it from
// the caller, which never asks.
function FormDemo() {
  const [open, setOpen] = useState(true);
  return (
    <div>
      <Button type="button" onClick={() => setOpen(true)}>New issuer</Button>
      {open ? (
        <SidePanel title="New issuer" onClose={() => setOpen(false)}>
          <Input label="Issuer URL" name="issuer" />
          <div className="actions">
            <Button type="button" variant="primary" onClick={() => setOpen(false)}>Save issuer</Button>
          </div>
        </SidePanel>
      ) : null}
    </div>
  );
}

const scrimPress = async () => {
  const scrim = document.querySelector('.side-panel__scrim');
  if (!(scrim instanceof HTMLElement)) throw new Error('scrim missing');
  await userEvent.click(scrim);
};

export const UnsavedChanges: Story = {
  render: () => <FormDemo />,
  play: async ({ canvas }) => {
    const panel = () => canvas.queryByRole('complementary', { name: 'New issuer' });

    // Untouched: an outside press closes as before.
    await scrimPress();
    await expect(panel()).toBeNull();

    // Edited: an outside press leaves it open with the text.
    await userEvent.click(canvas.getByRole('button', { name: 'New issuer' }));
    const field = canvas.getByRole('textbox', { name: 'Issuer URL' });
    await userEvent.type(field, 'https://id.example');
    await scrimPress();
    await expect(panel()).toBeVisible();
    await expect(canvas.queryByRole('alertdialog')).toBeNull();

    // Escape asks; Keep editing (focused first) returns to the field with the text.
    await userEvent.click(field);
    await userEvent.keyboard('{Escape}');
    const ask = await canvas.findByRole('alertdialog', { name: 'Discard unsaved changes?' });
    const keep = within(ask).getByRole('button', { name: 'Keep editing' });
    await waitFor(() => expect(keep).toHaveFocus());
    await userEvent.click(keep);
    await expect(canvas.queryByRole('alertdialog')).toBeNull();
    await expect(field).toHaveFocus();
    await expect(field).toHaveValue('https://id.example');

    // Escape while asking is Keep editing too.
    await userEvent.keyboard('{Escape}');
    await canvas.findByRole('alertdialog');
    await userEvent.keyboard('{Escape}');
    await expect(canvas.queryByRole('alertdialog')).toBeNull();
    await expect(panel()).toBeVisible();

    // Close asks as well; Discard closes.
    await userEvent.click(canvas.getByRole('button', { name: 'Close' }));
    await userEvent.click(within(await canvas.findByRole('alertdialog')).getByRole('button', { name: 'Discard' }));
    await expect(panel()).toBeNull();

    // The caller closing after a save never asks.
    await userEvent.click(canvas.getByRole('button', { name: 'New issuer' }));
    await userEvent.type(canvas.getByRole('textbox', { name: 'Issuer URL' }), 'https://id.example');
    await userEvent.click(canvas.getByRole('button', { name: 'Save issuer' }));
    await expect(panel()).toBeNull();
  },
};
