// @vitest-environment happy-dom
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { zRetentionHealth } from '@hikyo/zod';
import { OpsDiagnosticBanners } from './OpsDiagnosticBanners.tsx';

describe('OpsDiagnosticBanners', () => {
  it('renders escaped actionable alerts and explicitly labels unmeasured findings', () => {
    const health = zRetentionHealth.parse({
      last_prune_success: null,
      stale: false,
      stale_after_seconds: 86400,
      peak_project_bytes: 0,
      storage_warn: false,
      backup: {
        scheduled: false, last_success_at: null, artifact_age_seconds: 0,
        rpo_seconds: 93600, rpo_exceeded: false, last_failure_at: null,
        last_failure_reason: '', last_prune_at: null, last_drill_at: null,
        last_drill_ok: false, drill_stale: true,
      },
      adapter_targets_failed: 0, adapter_targets_paused: 0,
      adapter_targets_attention: 0, adapter_jobs_queued: 0,
      diagnostics: [
        { code: 'data-volume', severity: 'unknown', message: 'Check storage on the remote database host.' },
        { code: 'root-escrow', severity: 'warn', message: 'Verify <current> root escrow.' },
        { code: 'reencrypt', severity: 'error', message: 'Re-encryption needs attention.' },
        { code: 'pin-expiry', severity: 'ok', message: 'Hidden healthy finding.' },
      ],
    });
    const container = document.createElement('div');
    container.innerHTML = renderToStaticMarkup(<OpsDiagnosticBanners health={health} />);
    const region = container.querySelector('section[aria-label="Operational diagnostics"]');
    expect(region?.getAttribute('tabindex')).toBe('0');
    const alerts = [...container.querySelectorAll('[role="alert"]')];
    expect(alerts).toHaveLength(3);
    expect(alerts[0]?.textContent).toContain('Unmeasured: Check storage on the remote database host.');
    expect(alerts[1]?.textContent).toContain('Warning: Verify <current> root escrow.');
    expect(alerts[1]?.querySelector('current')).toBeNull();
    expect(alerts[2]?.textContent).toContain('Error: Re-encryption needs attention.');
    expect(container.textContent).not.toContain('Hidden healthy finding.');
    expect(renderToStaticMarkup(<OpsDiagnosticBanners health={null} />)).toBe('');
  });
});
