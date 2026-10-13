import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { GLYPH_NAMES, Glyph } from './Glyph.tsx';

const meta = {
  parameters: { docs: { description: { component: "Every icon in the app, Rune Icons outline artwork (Apache-2.0, see web/src/ui/Glyph.LICENSE) as stroked inline SVG in the current colour. Decorative glyphs are hidden from assistive technology; a labelled glyph becomes an image with that accessible name. Colour supplements the surrounding status text." } } },
  title: 'Design system/Glyph',
  id: 'ui-glyph',
  component: Glyph,
  tags: ['ai-generated'],
  args: { name: 'lock' },
  argTypes: { name: { control: 'select', options: GLYPH_NAMES } },
} satisfies Meta<typeof Glyph>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Decorative: Story = {};
export const Named: Story = { args: { name: 'lock', label: 'Secret' } };

/** The whole set, one tile per name, at body size and at the 16px icon box. */
export const Gallery: Story = {
  render: () => (
    <ul style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(120px, 1fr))', gap: 12, margin: 0, padding: 0, listStyle: 'none' }}>
      {GLYPH_NAMES.map((name) => (
        <li key={name} style={{ display: 'grid', justifyItems: 'start', gap: 4 }}>
          <span style={{ fontSize: 'var(--icon)' }}>
            <Glyph name={name} />
          </span>
          <span className="mono" style={{ fontSize: 12, color: 'var(--tx-dim)' }}>{name}</span>
        </li>
      ))}
    </ul>
  ),
  play: async ({ canvasElement }) => {
    const glyphs = canvasElement.querySelectorAll('svg.glyph');
    await expect(glyphs).toHaveLength(GLYPH_NAMES.length);
    // Every name renders its own drawing, the Lucide-sourced `key` included:
    // one tile per name, each with a non-empty path, and no two names share
    // artwork (a copy-paste slip between entries would pass the count alone).
    const rendered = [...glyphs].map((glyph) => glyph.getAttribute('data-glyph'));
    await expect(rendered).toEqual(GLYPH_NAMES);
    await expect(rendered).toContain('key');
    const paths = [...glyphs].map((glyph) => glyph.querySelector('path')?.getAttribute('d') ?? '');
    await expect(paths.filter((d) => d.trim() === '')).toEqual([]);
    await expect(new Set(paths).size).toBe(GLYPH_NAMES.length);
    for (const glyph of glyphs) {
      await expect(glyph).toHaveAttribute('viewBox', '0 0 24 24');
      await expect(glyph).toHaveAttribute('stroke', 'currentColor');
      await expect(glyph).toHaveAttribute('aria-hidden', 'true');
      await expect(glyph.getAttribute('style')).toBeNull();
    }
  },
};

/** The state vocabulary beside the words it accompanies, in the state colour where one applies. */
export const AllStates: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <p style={{ margin: 0 }}>
        <Glyph name="lock" /> DATABASE_URL <span className="mono">••••••••</span>
      </p>
      <p style={{ margin: 0 }}>
        <Glyph name="link" /> LOG_LEVEL, linked in group app/
      </p>
      <p style={{ margin: 0, color: 'var(--danger)' }}>
        <Glyph name="alert" /> required · absent
      </p>
      <p style={{ margin: 0, color: 'var(--danger)' }}>
        <Glyph name="cross" /> value fails declaration
      </p>
      <p style={{ margin: 0 }}>
        <Glyph name="warn" /> filter active: problems
      </p>
      <p style={{ margin: 0, color: 'var(--changed)' }}>
        <Glyph name="history" /> changed since last publish
      </p>
      <p style={{ margin: 0, color: 'var(--changed)' }}>
        <Glyph name="own-draft" /> unpublished draft of your own
      </p>
      <p style={{ margin: 0, color: 'var(--tx-dim)' }}>
        <Glyph name="draft" /> another editor has a draft
      </p>
      <p style={{ margin: 0 }}>
        <Glyph name="check" /> Remote added
      </p>
      <p style={{ margin: 0, fontSize: 13, color: 'var(--tx-dim)' }}>
        <Glyph name="menu" /> row actions, at caption size
      </p>
    </div>
  ),
};
