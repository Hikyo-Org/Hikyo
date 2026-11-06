import type { ReactNode } from 'react';

import { ApiError } from '../api/client.ts';
import {
  securityStatusStale,
  useSecurityMaintenance,
  type SecurityMaintenancePublicStatus,
} from '../api/securityMaintenance.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Sep } from '../ui/Sep.tsx';
import { Panel } from './Sections.tsx';

// The read needs only the instance-config grant, so a refusal means no grant, not a missing factor.
const nondisclosed = (error: unknown) =>
  error instanceof ApiError && (error.status === 403 || error.status === 404);

const inProgress = ['applicable', 'staged', 'applying'];
const current = ['current', 'healthy'];

const instant = (value: string | undefined) =>
  value === undefined ? 'never' : new Date(value).toLocaleString();

/**
 * Security maintenance state, read-only, on `instance-admin`. The separate
 * maintenance coordinator publishes it; this server holds no deployment
 * credentials, so nothing here can change the installation.
 */
export function SecurityMaintenancePanel({ now = new Date() }: { now?: Date }) {
  const report = useSecurityMaintenance();
  const data = report.data;
  return (
    <Panel id="instance-security-maintenance" title="Security maintenance">
      <p>
        Read-only. The maintenance coordinator runs separately from this server and publishes this
        status; enrolment, updates and recovery go through it with{' '}
        <span className="mono">hikyo security-maintenance</span>. On a host that is a root command;
        in a Kubernetes install it runs as a one-off Job made from the coordinator’s CronJob, as the
        runbook describes.
      </p>
      {report.isPending ? <p role="status">Loading security maintenance status…</p> : null}
      {nondisclosed(report.error) ? (
        <p role="status">Security maintenance status is not disclosed to this session.</p>
      ) : null}
      {report.isError && !nondisclosed(report.error) ? (
        <Alert>Security maintenance status could not be read. Reload to try again.</Alert>
      ) : null}
      {data?.unreadable === true ? (
        <Alert>
          The coordinator’s status file exists but could not be read or parsed, so the state of
          security maintenance is unknown. Check the file where the coordinator writes it (the
          status ConfigMap in a Kubernetes install) and the coordinator’s logs.
        </Alert>
      ) : null}
      {data !== undefined && !data.unreadable && !data.reported ? (
        <Alert tone="info">
          No status has been reported. Either{' '}
          <span className="mono">HIKYO_SECURITY_MAINTENANCE_STATUS_FILE</span> is not set on this
          server, or the coordinator has not written its status yet.
        </Alert>
      ) : null}
      {data?.status !== undefined ? <Reported status={data.status} now={now} /> : null}
    </Panel>
  );
}

function Reported({ status, now }: { status: SecurityMaintenancePublicStatus; now: Date }) {
  const stale = securityStatusStale(status, now);
  return (
    <>
      <Headline status={status} stale={stale} />
      {stale ? (
        <Alert tone="warn">
          Security metadata is stale: last verified {instant(status.last_verified)}, known until{' '}
          {instant(status.known_until)}. Advisories published since then are not reflected.
        </Alert>
      ) : null}
      {status.detail !== undefined ? <p className="field__hint">{status.detail}</p> : null}
      {status.action !== undefined ? (
        <p>
          Run through the coordinator: <code className="mono">{status.action}</code>
        </p>
      ) : null}
      <Row title="Phase" detail="where the coordinator stands">
        <Badge mono>{status.phase}</Badge>
        {status.reason !== undefined ? <Badge mono>{status.reason}</Badge> : null}
      </Row>
      <Row title="Policy" detail="mode and release channel">
        <code className="mono">
          {status.mode ?? 'unknown'}
          <Sep />
          {status.channel ?? 'unknown'}
        </code>
      </Row>
      <Row title="Versions" detail="running, and the target an update would install">
        <code className="mono">
          {status.running_version ?? 'unknown'}
          <Sep />
          {status.target_version ?? 'no target'}
        </code>
      </Row>
      <Row title="Last check" detail="most recent attempt">
        <code className="mono">{instant(status.last_attempt)}</code>
      </Row>
      <Row title="Last verified" detail="most recent authenticated advisory catalog">
        <code className="mono">{instant(status.last_verified)}</code>
      </Row>
      <Row title="Known until" detail={`catalog sequence ${String(status.catalog_sequence)}`}>
        <code className="mono">{instant(status.known_until)}</code>
      </Row>
      <Row title="Applicable advisories" detail="known and not yet remediated on this installation">
        <Ids ids={status.applicable} />
      </Row>
      <Row
        title="Covered advisories"
        detail={`${status.covered_components.join(', ') || 'no components'}${status.coverage_through === undefined ? '' : `, through ${status.coverage_through}`}`}
      >
        <Ids ids={status.coverage} />
      </Row>
    </>
  );
}

function Headline({ status, stale }: { status: SecurityMaintenancePublicStatus; stale: boolean }) {
  const count = status.applicable.length;
  if (status.protection_gap) {
    return (
      <Alert>
        Protection gap: {count} applicable {count === 1 ? 'advisory remains' : 'advisories remain'}{' '}
        unremediated and this installation is not waiting on a schedule. An operator must act.
      </Alert>
    );
  }
  if (status.phase === 'failed') {
    return <Alert>The last update failed and needs an operator.</Alert>;
  }
  if (!status.enrolled) {
    return (
      <Alert tone="info">
        This installation is not enrolled. The coordinator reports advisories but changes nothing
        until an operator enrols it.
      </Alert>
    );
  }
  if (inProgress.includes(status.phase)) {
    return (
      <Alert tone="info">
        Update in progress
        {status.target_version === undefined ? '' : `: moving to ${status.target_version}`}.
      </Alert>
    );
  }
  if (current.includes(status.phase) && !stale) {
    return <Alert tone="done">Enrolled and current. No applicable advisory is outstanding.</Alert>;
  }
  return null;
}

function Row({ title, detail, children }: { title: string; detail: string; children: ReactNode }) {
  return (
    <div className="settings-row settings-row--compact">
      <div className="settings-row__copy">
        <span className="settings-row__title">{title}</span>
        <span className="settings-row__detail">{detail}</span>
      </div>
      <span className="settings-row__spacer" />
      {children}
    </div>
  );
}

function Ids({ ids }: { ids: readonly string[] }) {
  return ids.length === 0 ? (
    <code className="mono">none</code>
  ) : (
    <code className="mono">{ids.join(', ')}</code>
  );
}
