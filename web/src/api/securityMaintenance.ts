import { getSecurityMaintenanceOp } from '@hikyo/operations';
import type { zSecurityMaintenancePublicStatus } from '@hikyo/zod';
import { useQuery, type UseQueryResult } from '@tanstack/react-query';
import type { z } from 'zod';

import { parsed } from './client.ts';

export type SecurityMaintenancePublicStatus = z.infer<typeof zSecurityMaintenancePublicStatus>;
export type SecurityMaintenanceReport = Awaited<ReturnType<typeof readSecurityMaintenance>>;

const refreshMs = 5 * 60 * 1_000;

const readSecurityMaintenance = () => parsed(getSecurityMaintenanceOp, {});

/**
 * The server re-reads the coordinator's status file on every request, so this
 * poll is the only freshness the page has; a stopped coordinator shows up as
 * stale status, never as an error here.
 */
export function useSecurityMaintenance(): UseQueryResult<SecurityMaintenanceReport> {
  return useQuery({
    queryKey: ['security-maintenance'],
    queryFn: readSecurityMaintenance,
    staleTime: refreshMs,
    refetchInterval: refreshMs,
  });
}

/**
 * The same rule `hikyo security-maintenance status` applies on the host:
 * never verified, expired metadata, or the coordinator itself reporting stale.
 */
export function securityStatusStale(status: SecurityMaintenancePublicStatus, now: Date): boolean {
  return (
    status.phase === 'stale' ||
    status.last_verified === undefined ||
    status.known_until === undefined ||
    Date.parse(status.known_until) <= now.getTime()
  );
}
