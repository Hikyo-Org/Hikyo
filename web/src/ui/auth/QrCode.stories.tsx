import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { QrCode } from './QrCode.tsx';

const meta = {
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

// Black on white in both themes: a scanner needs the contrast, and the theme
// toolbar must not be able to change that.
export const NamedAndScannable: Story = {
  play: async ({ canvas }) => {
    const svg = canvas.getByRole('img', { name: 'Authenticator enrolment QR code' });
    await expect(svg).toBeVisible();
    await expect(svg.querySelector('rect')?.getAttribute('fill')).toBe('#ffffff');
    await expect(svg.querySelector('path')?.getAttribute('fill')).toBe('#000000');
  },
};
