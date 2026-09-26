import { zDeliveryTargetList } from '@hikyo/zod';
import type { z } from 'zod';

import type { DeliveryTargetsView } from '../api/deliveryTargets.ts';
import { PROD, STAGING } from './ids.ts';
import { serviceAccount } from './machineAccess.ts';

// Delivery-target report fixtures (#790): one wire listing per state, parsed
// through the generated schema exactly as the page parses a response.

export const NOW = new Date('2026-09-25T12:00:00Z');
const CLUSTER = '0d9e8f7a-1b2c-4d3e-8f4a-5b6c7d8e9f01';
const INSTANCE = '1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d';
const principal = serviceAccount.principal_id;

type WireTarget = z.input<typeof zDeliveryTargetList>['targets'][number];
type WireCondition = WireTarget['conditions'][number];

const healthy: readonly WireCondition[] = [
  { type: 'Ready', status: 'True', reason: 'Reconciled', observed_generation: 3 },
  { type: 'Synced', status: 'True', reason: 'Delivered', observed_generation: 3 },
];
export const target = (
  n: number,
  state: WireTarget['state'],
  conditions: readonly WireCondition[] = healthy,
  extra: Partial<WireTarget> = {},
): WireTarget => ({
  id: `dtg_123e4567-e89b-12d3-a456-4266141741${String(10 + n)}`,
  principal_id: principal,
  target: {
    cluster_id: CLUSTER,
    instance_uid: INSTANCE,
    namespace: n % 2 === 0 ? 'payments' : 'checkout',
    name: `api-secrets-${String(n)}`,
    uid: `2b3c4d5e-6f7a-4b8c-9d0e-1f2a3b4c5d${String(10 + n)}`,
  },
  vocabulary: 1,
  generation: 3,
  observed_generation: 3,
  reported_at: '2026-09-25T11:55:00Z',
  received_at: state === 'stale' ? '2026-09-25T08:00:00Z' : '2026-09-25T11:55:00Z',
  report_interval_seconds: 300,
  lifecycle: 'Synced',
  conditions: [...conditions],
  reporter: { integration: 'kubernetes-operator', version: '1.4.0' },
  state,
  ...extra,
});

export const refusedTarget = (n: number) =>
  target(n, 'refused', healthy, {
    refusal: { cause: 'vocabulary', refused_at: '2026-09-25T11:50:00Z' },
  });

type Listing = {
  readonly targets?: readonly WireTarget[];
  readonly quotaRefused?: boolean;
  readonly observed?: boolean;
};

/** One environment's listing, parsed like a response. */
export function listing({ targets = [], quotaRefused = false, observed = true }: Listing) {
  return zDeliveryTargetList.parse({
    principals: observed
      ? [
          {
            principal_id: principal,
            last_contact_at: '2026-09-25T11:58:00Z',
            ...(quotaRefused ? { quota_refused_at: '2026-09-25T11:40:00Z' } : {}),
          },
        ]
      : [],
    targets: [...targets],
  });
}

const production = { id: PROD, name: 'production' };
export const staging = { id: STAGING, name: 'staging' };

/** A settled view over production only. */
export function viewOf(list: ReturnType<typeof listing>): DeliveryTargetsView {
  return {
    support: 'supported',
    reports: [{ environment: production, list }],
    failures: [],
    isPending: false,
  };
}
