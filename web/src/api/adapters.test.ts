import { expect, it } from 'vitest';

import { providerLabel } from './adapters.ts';

it('names the encrypted-webhook provider type, and its deprecated alias, as the encrypted webhook', () => {
  expect(providerLabel('encrypted-webhook')).toBe('Encrypted webhook');
  expect(providerLabel('sealed-webhook')).toBe('Encrypted webhook');
});

it('passes an unknown provider through', () => {
  expect(providerLabel('future-provider')).toBe('future-provider');
});
