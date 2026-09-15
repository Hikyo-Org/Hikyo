import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { ChromeDiagnostic } from './OpsDiagnosticBanners.tsx';

const meta = {
  component: ChromeDiagnostic,
  tags: ['ai-generated'],
  args: {
    severity: 'error',
    children: 'A prune run failed and payloads may be over their retention bound.',
  },
} satisfies Meta<typeof ChromeDiagnostic>;

export default meta;
type Story = StoryObj<typeof meta>;

export const ErrorSeverity: Story = {};

export const Warn: Story = {
  args: { severity: 'warn', children: 'Storage is approaching the publish refusal threshold.' },
};

export const Unknown: Story = {
  args: { severity: 'unknown', children: 'Retention health could not be measured this cycle.' },
};

// The single project-wide CssCheck: a concrete computed style proving the shared
// preview actually loaded tokens.css + app.css. `.retention-warning` takes
// border-radius: var(--radius-container) = 6px. Theme-stable (not a colour).
export const CssCheck: Story = {
  play: async ({ canvasElement }) => {
    const el = canvasElement.querySelector('.retention-warning');
    if (el === null) {
      throw new Error('CssCheck: .retention-warning element not found — did the CSS/markup change?');
    }
    await expect(getComputedStyle(el).borderRadius).toBe('6px');
  },
};
