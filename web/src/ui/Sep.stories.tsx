import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { Sep } from './Sep.tsx';

const meta = {
  title: 'Design system/Sep',
  id: 'ui-sep',
  component: Sep,
  tags: ['ai-generated'],
  parameters: {
    docs: {
      description: {
        component:
          'Visible middle-dot separator for inline text ("Payments · every environment"), bigger and better spaced than a plain " · ". It is real text (" · "), so a screen reader hears a separator and the words either side never run together; the spaces collapse at the inline-block edges, so only the margins space it.',
      },
    },
  },
} satisfies Meta<typeof Sep>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  render: () => (
    <span data-testid="wrap" style={{ fontSize: 14 }}>
      Payments<Sep />every environment
    </span>
  ),
};

// Bigger than the surrounding text and clear of it on both sides, and read as
// a separator by assistive technology.
export const BiggerAndSpaced: Story = {
  render: () => (
    <span data-testid="wrap" style={{ fontSize: 14 }}>
      Payments<Sep />every environment
    </span>
  ),
  play: async ({ canvasElement }) => {
    const wrap = canvasElement.querySelector('[data-testid="wrap"]') as HTMLElement;
    const dot = canvasElement.querySelector('.sep') as HTMLElement;
    const wrapSize = Number.parseFloat(getComputedStyle(wrap).fontSize);
    const dotStyle = getComputedStyle(dot);
    await expect(Number.parseFloat(dotStyle.fontSize)).toBeGreaterThan(wrapSize);
    await expect(Number.parseFloat(dotStyle.marginLeft)).toBeGreaterThan(0);
    await expect(Number.parseFloat(dotStyle.marginRight)).toBeGreaterThan(0);
    await expect(dot.hasAttribute('aria-hidden')).toBe(false);
    await expect(wrap.textContent).toBe('Payments · every environment');
    // The spaces are text for assistive technology only: the box is as wide
    // as a bare dot, so the visible spacing is the margins alone.
    const bare = document.createElement('span');
    bare.className = 'sep';
    bare.textContent = '·';
    wrap.append(bare);
    const width = (el: HTMLElement) => el.getBoundingClientRect().width;
    await expect(Math.abs(width(dot) - width(bare))).toBeLessThan(0.5);
    bare.remove();
  },
};
