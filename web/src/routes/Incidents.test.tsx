import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import type { Incident } from '../api/incidents.ts';
import { Counts, scopeLabel } from './Incidents.tsx';

describe('scopeLabel', () => {
  it('labels the known scope kinds', () => {
    expect(scopeLabel('account')).toBe('Account');
    expect(scopeLabel('adapter')).toBe('Deployment adapter');
  });

  it('shows an unknown scope kind as its own word, not as an account', () => {
    expect(scopeLabel('instance')).toBe('instance');
  });
});

describe('Counts', () => {
  it('shows the work not yet run apart from the refused items the server counts under pending', () => {
    const incident: Incident = {
      id: 'inc_1', scope_kind: 'account', scope_id: 'usr_1', status: 'open', opened_by: 'usr_1', opened_at: '2026-11-23T09:00:00Z',
      counts: { pending: 5, refused: 2, unknown: 0, blocked: 0, done: 0, attested: 0, out_of_scope: 0 },
    };
    const html = renderToStaticMarkup(<Counts incident={incident} />);
    expect(html).toContain('<dt>Not yet run</dt><dd>3</dd>');
    expect(html).toContain('<dt>Refused</dt><dd>2</dd>');
  });
});
