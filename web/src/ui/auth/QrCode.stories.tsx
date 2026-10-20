import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { QrCode } from './QrCode.tsx';

const meta = {
  title: 'Design system/Auth/QrCode',
  id: 'ui-auth-qrcode',
  component: QrCode,
  tags: ['ai-generated'],
  args: {
    value: 'otpauth://totp/Hikyo:alex?secret=JBSWY3DPEHPK3PXP&issuer=Hikyo&digits=6&period=30',
    title: 'Authenticator enrolment QR code',
  },
} satisfies Meta<typeof QrCode>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const Large: Story = {
  args: { size: 'large', value: 'public-pairing-invitation-'.repeat(20), title: 'Invitation QR code' },
  play: async ({ canvas, canvasElement }) => {
    const svg = canvas.getByRole('img', { name: 'Invitation QR code' });
    await expect(svg).toBeVisible();
    await expect(svg.getBoundingClientRect().width).toBe(320);
    const previousWidth = canvasElement.style.width;
    try {
      canvasElement.style.width = '240px';
      const bounds = svg.getBoundingClientRect();
      await expect(bounds.width).toBeLessThanOrEqual(240);
      await expect(Math.abs(bounds.width - bounds.height)).toBeLessThan(1);
    } finally { canvasElement.style.width = previousWidth; }
  },
};

// Black on white in both themes: a scanner needs the contrast, and the theme
// toolbar must not be able to change that.
export const NamedAndScannable: Story = {
  play: async ({ canvas }) => {
    const svg = canvas.getByRole('img', { name: 'Authenticator enrolment QR code' });
    await expect(svg).toBeVisible();
    await expect(svg.getBoundingClientRect().width).toBe(176);
    await expect(svg.querySelector('rect')?.getAttribute('fill')).toBe('#ffffff');
    await expect(svg.querySelector('path')?.getAttribute('fill')).toBe('#000000');
  },
};
