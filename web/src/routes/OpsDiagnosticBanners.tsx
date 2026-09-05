import { diagnosticWarnings, type RetentionHealth } from '../api/retention.ts';

/** Reuses the authorized health read and the existing operational warning style. */
export function OpsDiagnosticBanners({ health }: {
  readonly health: RetentionHealth | null | undefined;
}) {
  const warnings = diagnosticWarnings(health);
  if (warnings.length === 0) return null;
  return (
    <section className="ops-diagnostic-warnings" aria-label="Operational diagnostics" tabIndex={0}>
      {warnings.map((finding) => (
        <p className="retention-warning" role="alert" key={finding.code}>
          <span className="alert__glyph" aria-hidden="true">!</span>
          <span>
            <strong>{finding.severity === 'unknown' ? 'Unmeasured' : finding.severity === 'error' ? 'Error' : 'Warning'}: </strong>
            {finding.message}
          </span>
        </p>
      ))}
    </section>
  );
}
