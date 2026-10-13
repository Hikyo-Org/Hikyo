import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { capabilitiesAt } from '../api/access.ts';
import { CAPABILITY_TAG, CapabilityTag } from './capabilityTags.tsx';

const meta = {
  title: 'Shared/CapabilityTag',
  id: 'routes-capabilitytag',
  component: CapabilityTag,
  tags: ['ai-generated'],
  parameters: { docs: { description: { component: 'A capability id as a tag: one tone and icon per capability in the closed registry, the id itself as the word (source case, the accessible text). Every checkbox list and plain-word capability listing renders it, so a capability looks the same everywhere.' } } },
  args: { id: 'reveal' },
} satisfies Meta<typeof CapabilityTag>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Reveal: Story = {};

// The whole vocabulary: every id in the map renders its tone and icon, and
// every capability a person can be granted has an entry, so none falls back
// to the plain neutral tag.
export const Vocabulary: Story = {
  render: () => (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 'var(--space-2)' }}>
      {Object.keys(CAPABILITY_TAG).map((id) => (
        <CapabilityTag key={id} id={id} />
      ))}
    </div>
  ),
  play: async ({ canvasElement }) => {
    const grantable = capabilitiesAt('instance').map((atom) => atom.id);
    await expect(grantable.filter((id) => CAPABILITY_TAG[id] === undefined)).toEqual([]);
    const tags = [...canvasElement.querySelectorAll('.badge')];
    await expect(tags.map((tag) => tag.textContent)).toEqual(Object.keys(CAPABILITY_TAG));
    for (const tag of tags) {
      const entry = CAPABILITY_TAG[tag.textContent ?? ''];
      if (entry === undefined) throw new Error(`no entry for ${tag.textContent ?? ''}`);
      await expect(tag.querySelector('svg.glyph')).toHaveAttribute('data-glyph', entry.icon);
      await expect(tag.classList.contains(`badge--${entry.tone}`)).toBe(entry.tone !== 'neutral');
    }
  },
};
