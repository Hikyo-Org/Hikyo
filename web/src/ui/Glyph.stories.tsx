import type { Meta, StoryObj } from '@storybook/react-vite';

import { Glyph, type GlyphName } from './Glyph.tsx';

const names: readonly GlyphName[] = ['lock', 'link', 'check', 'cross', 'warn', 'delta', 'draft', 'ellipsis', 'chevron'];

const meta = {
  parameters: { docs: { description: { component: "Monochrome SVG state vocabulary. Decorative glyphs are hidden from assistive technology; a labelled glyph becomes an image with that accessible name. Colour supplements the surrounding status text." } } },
  title: 'Design system/Glyph',
  id: 'ui-glyph',
  component: Glyph,
  tags: ['ai-generated'],
  args: { name: 'lock' },
  argTypes: { name: { control: 'select', options: names } },
} satisfies Meta<typeof Glyph>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Decorative: Story = {};
export const Named: Story = { args: { name: 'lock', label: 'Secret' } };

/** Every glyph beside the word it accompanies, at body and caption size, in the state colour where one applies. */
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
        <Glyph name="warn" /> required · absent
      </p>
      <p style={{ margin: 0, color: 'var(--danger)' }}>
        <Glyph name="cross" /> value fails declaration
      </p>
      <p style={{ margin: 0, color: 'var(--changed)' }}>
        <Glyph name="delta" /> changed since last publish
      </p>
      <p style={{ margin: 0, color: 'var(--tx-dim)' }}>
        <Glyph name="draft" /> another editor has a draft
      </p>
      <p style={{ margin: 0 }}>
        <Glyph name="check" /> Remote added
      </p>
      <p style={{ margin: 0, fontSize: 13, color: 'var(--tx-dim)' }}>
        <Glyph name="ellipsis" /> row actions, at caption size
      </p>
    </div>
  ),
};
