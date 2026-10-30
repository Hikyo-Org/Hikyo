import type { ReactNode } from 'react';

import { diagnosticWarnings, type RetentionHealth } from '../api/retention.ts';
import { Glyph } from '../ui/Glyph.tsx';

export type DiagnosticSeverity = 'error' | 'warn' | 'unknown';

/** Keep the API's exact byte counts, but use readable units in the banner. */
function formatStorageMessage(message: string): string {
  return message.replace(/\b(\d+) bytes available\b/g, (_match, bytes: string) => {
    let value = Number(bytes);
    const units = ['bytes', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'];
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
      value /= 1024;
      unit++;
    }
    return `${unit === 0 ? value : value.toFixed(2)} ${value === 1 && unit === 0 ? 'byte' : units[unit]} available`;
  });
}

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
      <Glyph name={severity === 'unknown' ? 'question' : 'warn'} className="alert__glyph" />
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
            {finding.code === 'data-volume' ? formatStorageMessage(finding.message) : finding.message}
          </ChromeDiagnostic>
        );
      })}
    </section>
  );
}
