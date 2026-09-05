// @vitest-environment happy-dom
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { GIT_DEFINITIONS_NOTICE } from '../api/definitions.ts';
import { LastApplyProvenance, monoCommands } from './DefinitionsBundlePanel.tsx';

describe('the Git-mode bundle notice', () => {
  it('keeps the normative sentence verbatim with its command names in mono', () => {
    const container = document.createElement('div');
    container.innerHTML = renderToStaticMarkup(<p>{monoCommands(GIT_DEFINITIONS_NOTICE)}</p>);
    expect(container.textContent).toBe(GIT_DEFINITIONS_NOTICE.replaceAll('`', ''));
    expect([...container.querySelectorAll('.mono')].map((m) => m.textContent)).toEqual([
      'definitions plan',
      'definitions apply',
    ]);
  });

  it('shows last-applied commit, ref and actor as display-only labels', () => {
    const container = document.createElement('div');
    container.innerHTML = renderToStaticMarkup(
      <LastApplyProvenance
        lastApply={{
          plan_id: 'pln_123e4567-e89b-12d3-a456-426614174000',
          applied_at: '2026-09-01T10:00:00Z',
          applied_by: 'prn_123e4567-e89b-12d3-a456-426614174000',
          commit: 'abc1234',
          ref: 'refs/heads/main',
          revision: 7n,
        }}
      />,
    );
    expect([...container.querySelectorAll('.mono')].map((m) => m.textContent)).toEqual([
      'abc1234',
      'refs/heads/main',
    ]);
    expect(container.textContent).not.toContain('Actor');
    expect(container.textContent).toContain('(display only, not verified)');
    expect(container.querySelector('time')?.getAttribute('datetime')).toBe('2026-09-01T10:00:00Z');
  });
});
