import type { Meta, StoryObj } from '@storybook/react-vite';

import { Badge } from './Badge.tsx';

const meta = {
  title: 'Design system/Badge',
  id: 'ui-badge',
  component: Badge,
  parameters: { docs: { description: { component: 'Compact, noninteractive status label. Tone communicates status alongside visible text; mono displays identifiers and revisions.' } } },
  tags: ['ai-generated'],
  args: { children: 'Active', tone: 'neutral' },
  argTypes: { tone: { control: 'select', options: ['neutral', 'danger', 'changed', 'ok'] } },
} satisfies Meta<typeof Badge>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Neutral: Story = {};
// Compatible prop variants share one captioned comparison; Neutral remains
// the controls-driven example, so changing a tone never rewrites the gallery.
export const AllTones: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: 'flex', gap: 16, alignItems: 'start', flexWrap: 'wrap' }}>
      {[
        { caption: 'Neutral', badge: <Badge>Active</Badge> },
        { caption: 'Changed', badge: <Badge tone="changed">Draft</Badge> },
        { caption: 'Danger', badge: <Badge tone="danger">Unreachable</Badge> },
        { caption: 'OK', badge: <Badge tone="ok">Verified</Badge> },
        { caption: 'Monospace', badge: <Badge mono>rev 12</Badge> },
      ].map(({ caption, badge }) => (
        <figure key={caption} style={{ margin: 0, display: 'grid', gap: 8 }}>
          <figcaption>{caption}</figcaption>
          {badge}
        </figure>
      ))}
    </div>
  ),
};
