import type { ReactNode } from 'react';

import { diagnosticWarnings, type RetentionHealth } from '../api/retention.ts';

export type DiagnosticSeverity = 'error' | 'warn' | 'unknown';

/**
 * A persistent chrome diagnostic: `role="status"` because it stays for the
 * whole session rather than interrupting one, and the severity is data the
 * glyph and the stylesheet both read so the state is never colour alone.
 */
export function ChromeDiagnostic({
  severity,
  children,
}: {
  readonly severity: DiagnosticSeverity;
  readonly children: ReactNode;
}) {
  return (
    <p className="retention-warning" role="status" data-severity={severity}>
      <span className="alert__glyph" aria-hidden="true">
        {severity === 'unknown' ? '?' : '!'}
      </span>
      <span>{children}</span>
    </p>
  );
}

/** Reuses the authorized health read and the persistent chrome diagnostic style. */
export function OpsDiagnosticBanners({ health }: {
  readonly health: RetentionHealth | null | undefined;
}) {
  const warnings = diagnosticWarnings(health);
  if (warnings.length === 0) return null;
  return (
    <section className="ops-diagnostic-warnings" aria-label="Operational diagnostics" tabIndex={0}>
      {warnings.map((finding) => {
        const severity: DiagnosticSeverity = finding.severity === 'ok' ? 'unknown' : finding.severity;
        return (
          <ChromeDiagnostic severity={severity} key={finding.code}>
            <strong>{severity === 'unknown' ? 'Unmeasured' : severity === 'error' ? 'Error' : 'Warning'}: </strong>
            {finding.message}
          </ChromeDiagnostic>
        );
      })}
    </section>
  );
}
