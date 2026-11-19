import { expect, it } from 'vitest';

import { providerLabel } from './adapters.ts';

it('names the sealed-webhook provider type as the encrypted webhook', () => {
  expect(providerLabel('sealed-webhook')).toBe('Encrypted webhook');
});

it('passes an unknown provider through', () => {
  expect(providerLabel('future-provider')).toBe('future-provider');
});
