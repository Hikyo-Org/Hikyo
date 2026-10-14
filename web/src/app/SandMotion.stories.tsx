import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

/** The shell's sand layer on its own: `.sand::before` is the raked background. */
function SandLayer() {
  return <div className="sand"><p>The raked sand behind the app shell.</p></div>;
}

const meta = {
  title: 'Shared/Sand motion',
  id: 'app-sandmotion',
  component: SandLayer,
  tags: ['ai-generated'],
  parameters: { docs: { description: { component: 'The classes sandMotion.ts puts on the root: `sand-paused` while the page is hidden or unfocused, `sand-static` without GPU acceleration. Phones and coarse pointers never animate it (app.css). The sand stays visible either way; only its motion stops.' } } },
} satisfies Meta<typeof SandLayer>;

export default meta;
type Story = StoryObj<typeof meta>;

const sandBefore = (canvasElement: HTMLElement) => {
  const sand = canvasElement.querySelector('.sand');
  if (sand === null) throw new Error('sand layer missing');
  return getComputedStyle(sand, '::before');
};

export const PausedWhenHidden: Story = {
  play: async ({ canvasElement }) => {
    const root = document.documentElement.classList;
    root.add('sand-paused');
    try {
      await expect(sandBefore(canvasElement).animationPlayState).toBe('paused');
    } finally {
      root.remove('sand-paused');
    }
    await expect(sandBefore(canvasElement).animationPlayState).toBe('running');
  },
};

export const StaticWithoutGpu: Story = {
  play: async ({ canvasElement }) => {
    const root = document.documentElement.classList;
    root.add('sand-static');
    try {
      await expect(sandBefore(canvasElement).animationName).toBe('none');
      await expect(sandBefore(canvasElement).content).not.toBe('none');
    } finally {
      root.remove('sand-static');
    }
  },
};

/** Phones (the 700px layout) and coarse pointers never animate the sand. */
export const StillOnPhones: Story = {
  play: async () => {
    const still = [...document.styleSheets].flatMap((sheet) => [...sheet.cssRules]).some((rule) =>
      rule instanceof CSSMediaRule
      && rule.conditionText.includes('max-width: 700px')
      && rule.conditionText.includes('pointer: coarse')
      && [...rule.cssRules].some((inner) => inner instanceof CSSStyleRule
        && inner.selectorText === '.sand::before'
        && inner.style.animationName === 'none'));
    await expect(still).toBe(true);
  },
};
