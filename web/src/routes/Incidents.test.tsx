import { describe, expect, it } from 'vitest';

import { scopeLabel } from './Incidents.tsx';

describe('scopeLabel', () => {
  it('labels the known scope kinds', () => {
    expect(scopeLabel('account')).toBe('Account');
    expect(scopeLabel('adapter')).toBe('Deployment adapter');
  });

  it('shows an unknown scope kind as its own word, not as an account', () => {
    expect(scopeLabel('instance')).toBe('instance');
  });
});
