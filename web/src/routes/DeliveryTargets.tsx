import { Link } from 'react-router';

import {
  deliveryTargetsRefusalText,
  groupTargets,
  type DeliveryTarget,
  type DeliveryTargetsView,
} from '../api/deliveryTargets.ts';
import type { ProjectRef, ServiceAccount } from '../api/identities.ts';
import { useWorkspaceContext, withRemote } from '../api/transport.tsx';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Glyph } from '../ui/Glyph.tsx';
import { relativeAge } from './history-state.ts';

/**
 * The Kubernetes tab's reports (#790, condition-reporting ADR D2, D5, D11).
 *
 * Two labelled layers, never one health bit: what THIS server observed per
 * reporting service account (its last authenticated fetch, the quota notice),
 * then what each controller asserted per target, grouped by cluster and
 * namespace. A report is the controller's word; the only state that carries
 * the "reported by the controller" line is a fresh one, and no state takes the
 * `ok` tone, because Hikyo holds no cluster credential and verifies nothing.
 */

/** Delivery reasons name keys the payload may not carry (D4): link the key view instead. */
const KEY_REASONS: ReadonlySet<string> = new Set(['UndeliveredSecrets', 'KeysMissing', 'InvalidSecretData']);

/** The derived state vocabulary, D5's plus the client-side `unknown`. */
export type TargetState = DeliveryTarget['state'] | 'unknown';

/**
 * StateBadge carries the state as its word and a glyph; the tone only echoes
 * it. Refused and revoked are refusals (danger), stale is a warning on a
 * slate tint, unknown the dashed hairline; reported stays neutral.
 */
export function StateBadge({ state }: { state: TargetState }) {
  switch (state) {
    case 'reported':
      return <Badge data-state="reported">reported</Badge>;
    case 'stale':
      return (
        <Badge data-state="stale">
          <Glyph name="warn" /> stale
        </Badge>
      );
    case 'refused':
      return (
        <Badge tone="danger" data-state="refused">
          <Glyph name="cross" /> refused
        </Badge>
      );
    case 'reporter-revoked':
      return (
        <Badge tone="danger" data-state="reporter-revoked">
          <Glyph name="warn" /> reporter revoked
        </Badge>
      );
    case 'unknown':
      return <Badge data-state="unknown">? unknown</Badge>;
  }
}

/** stateSentence is the words beside the badge, in the server's own times. */
export function stateSentence(target: DeliveryTarget, now: Date): string {
  // received_at, the server's clock, is what D5 staleness is measured on; the
  // operator's reported_at can be skewed by up to five minutes.
  const received = relativeAge(target.received_at, now);
  switch (target.state) {
    case 'reported':
      return `reported by the controller, ${received}`;
    case 'stale':
      return `no report within its heartbeat; the last was received ${received}`;
    case 'refused':
      return target.refusal === undefined
        ? `the latest report was refused; the last accepted one was received ${received}`
        : `the latest report was refused (${target.refusal.cause}) ${relativeAge(target.refusal.refused_at, now)}; the last accepted one was received ${received}`;
    case 'reporter-revoked':
      return `its service account no longer holds report-delivery-status or has no live credential; the last report was received ${received}`;
  }
}

export function DeliveryTargetsPanel({
  project,
  view,
  known,
  accounts,
  now,
}: {
  project: ProjectRef;
  view: DeliveryTargetsView;
  /**
   * Every readable environment's listing has been read. Until then an empty
   * layer is `unknown`, never "no reports": no environments read is not none.
   */
  known: boolean;
  /** Names the reporting principals; an unlisted one shows its id. */
  accounts: readonly ServiceAccount[];
  now: Date;
}) {
  const workspace = useWorkspaceContext();
  const remote = workspace?.remote ?? '';
  const nameOf = (principal: string) =>
    accounts.find((account) => account.principal_id === principal)?.name ?? principal;
  const groups = groupTargets(view.reports);
  const observed = view.reports.flatMap(({ environment, list }) =>
    list.principals.map((principal) => ({
      environment,
      principal,
      reporting: list.targets.some((t) => t.principal_id === principal.principal_id),
    })),
  );
  const keysPath = (environment: string) =>
    withRemote(
      `/orgs/${project.org}/projects/${project.project}/environments/${environment}/values`,
      remote,
    );

  return (
    <>
      <h2>Kubernetes delivery targets</h2>
      <p className="machine__lede">
        Two layers, never merged: what this server observed, and what each controller reported. A
        report is the controller&apos;s own assertion; this server holds no cluster credential and
        verifies none of it.
      </p>

      {view.failures.map(({ environment, error }) => (
        <Alert key={environment.id}>
          {deliveryTargetsRefusalText(error, environment.name, remote !== '')}
        </Alert>
      ))}
      {view.isPending ? <p role="status">Reading delivery-target reports…</p> : null}

      <h3>Observed by this server</h3>
      {observed.length === 0 ? (
        <p className="machine__none">
          {known
            ? 'No reporting service account is observed in the environments you can read.'
            : 'unknown'}
        </p>
      ) : (
        <table className="values__table machine__table">
          <caption className="visually-hidden">
            Per reporting service account: this server&apos;s last authenticated delivery fetch and
            any quota refusal.
          </caption>
          <thead>
            <tr>
              <th scope="col">Service account</th>
              <th scope="col">Environment</th>
              <th scope="col">Last authenticated fetch</th>
              <th scope="col">Reports</th>
            </tr>
          </thead>
          <tbody>
            {observed.map(({ environment, principal, reporting }) => (
              <tr key={`${environment.id} ${principal.principal_id}`}>
                <td>{nameOf(principal.principal_id)}</td>
                <td>{environment.name}</td>
                <td>
                  {principal.last_contact_at === undefined
                    ? 'never observed'
                    : relativeAge(principal.last_contact_at, now)}
                </td>
                <td>
                  <span className="machine__chips">
                    {reporting ? (
                      <span>listed below</span>
                    ) : (
                      <>
                        <StateBadge state="unknown" />
                        <span>no report received</span>
                      </>
                    )}
                    {principal.quota_refused_at === undefined ? null : (
                      <>
                        <Badge tone="danger" data-state="quota-refused">
                          <Glyph name="cross" /> quota-refused
                        </Badge>
                        <span>{`a report past the account's target quota was refused ${relativeAge(principal.quota_refused_at, now)}`}</span>
                      </>
                    )}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <h3>Reported by controllers</h3>
      {groups.length === 0 ? (
        <p role="status" className="machine__none">
          {known ? 'No reports.' : 'unknown'}
        </p>
      ) : (
        groups.map((group) => (
          <section key={`${group.cluster} ${group.namespace}`} className="delivery-targets__group">
            <h4 className="machine__subhead">
              {'Cluster '}
              <code className="mono">{group.cluster}</code>
              {', namespace '}
              <code className="mono">{group.namespace}</code>
            </h4>
            <table className="values__table machine__table">
              <caption className="visually-hidden">
                {`Delivery targets in namespace ${group.namespace}, as their controller reported them.`}
              </caption>
              <thead>
                <tr>
                  <th scope="col">Target</th>
                  <th scope="col">Environment</th>
                  <th scope="col">Reported by</th>
                  <th scope="col">State</th>
                  <th scope="col">Conditions (as asserted)</th>
                </tr>
              </thead>
              <tbody>
                {group.rows.map(({ environment, target }) => (
                  <tr key={target.id} data-state={target.state}>
                    <td className="mono">{target.target.name}</td>
                    <td>{environment.name}</td>
                    <td>{nameOf(target.principal_id)}</td>
                    <td>
                      <span className="machine__chips">
                        <StateBadge state={target.state} />
                        <span>{stateSentence(target, now)}</span>
                      </span>
                    </td>
                    <td>
                      <ul className="delivery-targets__conditions">
                        <li className="mono">{`lifecycle=${target.lifecycle}`}</li>
                        {target.conditions.map((condition) => (
                          <li className="mono" key={condition.type}>
                            {`${condition.type}=${condition.status}/`}
                            {KEY_REASONS.has(condition.reason) ? (
                              <Link
                                to={keysPath(environment.id)}
                                aria-label={`${condition.reason}: open the ${environment.name} keys`}
                              >
                                {condition.reason}
                              </Link>
                            ) : (
                              condition.reason
                            )}
                          </li>
                        ))}
                      </ul>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </section>
        ))
      )}
    </>
  );
}
