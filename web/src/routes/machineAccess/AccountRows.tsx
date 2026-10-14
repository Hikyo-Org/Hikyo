import { type DeliveryTargetsView } from '../../api/deliveryTargets.ts';
import {
  lastUsedLabel,
  setupJourney,
  type JourneyAction,
  type MachineCredential,
  type MachineEnvScope,
  type ServiceAccount,
} from '../../api/identities.ts';
import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';

import { BindingCard } from './FederationBindings.tsx';
import { ExpiryBadge } from './Credentials.tsx';
import { Glyph } from '../../ui/Glyph.tsx';
/**
 * One service account's summary row. Its name opens the account's detail
 * (AccountDetail) in the side panel.
 */
export function AccountRow({
  account,
  scope,
  scopeKnown,
  machineReveal,
  lastUsed,
  onOpen,
}: {
  account: ServiceAccount;
  scope: readonly MachineEnvScope[];
  /** False when the grants or the environments could not be read: unknown is not "none". */
  scopeKnown: boolean;
  /** The project's machine-reveal opt-in, as the server reports it. */
  machineReveal: boolean;
  lastUsed: string;
  onOpen: () => void;
}) {
  const reading = scope.filter((s) => s.read);
  const journey = setupJourney(account.kind, scope, machineReveal);
  const outstanding = journey?.findIndex((step) => step.state !== 'done') ?? -1;
  const { text, ...badge } = journey === null
    ? {
        icon: 'minus' as const,
        title: 'not applicable',
        text: <><span aria-hidden="true">NA</span><span className="visually-hidden">not applicable</span></>,
      }
    : !scopeKnown
      ? { icon: 'question' as const, text: 'unknown' }
      : outstanding === -1
        ? { tone: 'ok' as const, text: 'complete' }
        : { tone: 'changed' as const, icon: 'wait' as const, text: `step ${String(outstanding + 1)} of 5` };
  return (
    <tr>
      <th scope="row">
        <button
          className="values__keyname mono"
          type="button"
          title={account.name}
          onClick={onOpen}
        >
          <Glyph name="chevron" /> <span className="machine__name">{account.name}</span>
        </button>
      </th>
      <td>
        <Badge icon={account.kind}>{account.kind}</Badge>
      </td>
      <td>
        {!scopeKnown ? (
          <span className="machine__none">unknown</span>
        ) : reading.length === 0 ? (
          <span className="machine__none">no environment</span>
        ) : (
          <span className="machine__chips">
            {reading.map((s) => (
              <span className="machine__scope" key={s.id}>
                <Badge>{s.name}{s.reveal ? <> <Glyph name="reveal" label="reveal" /></> : null}</Badge>
                {/* Origin chips per scope, as Members renders them: the one
                    thing that tells a break-glass grant from an ordinary one. */}
                {s.origins.map((origin) => (
                  <Badge
                    mono
                    className="machine__origin"
                    key={`${origin.kind}:${origin.subject}`}
                    title={`${origin.kind}: ${origin.subject}`}
                  >
                    {origin.kind}: {origin.subject}
                  </Badge>
                ))}
              </span>
            ))}
          </span>
        )}
      </td>
      {/* The SERVER's live count: it applies revocation, the credential epoch
          and expiry, none of which a client filtering on `revoked_at` sees. */}
      <td className="col-secondary">{String(account.live_credentials)}</td>
      <td className="col-secondary">{lastUsed}</td>
      <td>
        <Badge {...badge}>{text}</Badge>
      </td>
    </tr>
  );
}

export function AccountDetail({
  account,
  scope,
  machineReveal,
  deliveryTargets,
  targetsKnown,
  reportingUnsupported,
  bearers,
  bindings,
  now,
  ready,
  canDelete,
  onMint,
  onBind,
  onGrant,
  onRevoke,
  onReplaceBinding,
  onDelete,
  onLeave,
}: {
  account: ServiceAccount;
  scope: readonly MachineEnvScope[];
  machineReveal: boolean;
  deliveryTargets: DeliveryTargetsView;
  /** Every readable environment's reports have been read (see targetsKnown). */
  targetsKnown: boolean;
  /** The server does not advertise delivery-target reporting at all. */
  reportingUnsupported: boolean;
  bearers: readonly MachineCredential[];
  bindings: readonly MachineCredential[];
  now: Date;
  /** Every query this surface's warnings are computed from has succeeded. */
  ready: boolean;
  /**
   * Delete's lighter gate: a session and a known listing. It is separate from
   * `ready` because deprovisioning is a narrowing that needs no scope read,
   * requiring one would keep an operator from deleting a compromised account
   * when the membership surface is unreadable.
   */
  canDelete: boolean;
  onMint: (rotating: boolean) => void;
  onBind: () => void;
  onGrant: () => void;
  onRevoke: (credential: MachineCredential) => void;
  onReplaceBinding: (credential: MachineCredential) => void;
  onDelete: () => void;
  /** Closes the side panel before a journey step points at the page behind it. */
  onLeave: () => void;
}) {
  const journey = setupJourney(account.kind, scope, machineReveal);
  const reported = deliveryTargets.reports.reduce(
    (sum, { list }) => sum + list.targets.filter((t) => t.principal_id === account.principal_id).length,
    0,
  );
  return (
    <>
      <div className="machine__grid">
        <div>
          <h3 className="machine__subhead">Credentials</h3>
          {bearers.length === 0 ? (
            <p className="machine__none">
              No bearer credentials. This account authenticates by federation only, or not at all
              until one is minted.
            </p>
          ) : (
            <ul className="machine__creds">
              {bearers.map((credential) => (
                <li className="cred" key={credential.id}>
                  <code className="mono">{`${credential.prefix_hint ?? 'unknown'}…`}</code>
                  <Badge icon="passkey">bearer</Badge>
                  <ExpiryBadge credential={credential} now={now} />
                  <span className="cred__meta">{lastUsedLabel(credential)}</span>
                  <div className="actions">
                    <Button
                      type="button"
                      disabled={!ready}
                      onClick={() => onMint(true)}
                    >
                      {`Rotate ${account.name}`}
                    </Button>
                    <Button type="button" onClick={() => onRevoke(credential)}>
                      {`Revoke ${credential.prefix_hint ?? credential.id}`}
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          )}

          {bindings.length > 0 ? (
            <>
              <h3 className="machine__subhead">Federated bindings</h3>
              <ul className="machine__bindings">
                {bindings.map((credential) => (
                  <li key={credential.id}>
                    <BindingCard
                      account={account}
                      credential={credential}
                      now={now}
                      ready={ready}
                      onReplace={onReplaceBinding}
                      onRevoke={onRevoke}
                    />
                  </li>
                ))}
              </ul>
            </>
          ) : null}
        </div>

        <div>
          <h3 className="machine__subhead">Delivery targets</h3>
          <p role="status" className="machine__none">
            {reportingUnsupported
              ? 'This server does not support delivery-target reporting.'
              : !targetsKnown
              ? 'unknown: not every environment\'s reports have been read. The Kubernetes targets tab says why.'
              : reported === 0
                ? 'No reports from this account in the environments you can read. No report is not health.'
                : `${String(reported)} reported by this account's controller. The Kubernetes targets tab shows their states.`}
          </p>
          {ready ? null : (
            <p className="machine__none" role="status">
              The actions are held back until the accounts, grants, environments and credentials
              have all been read: every one of their warnings is a statement about that state, and a
              query that failed would make it an understatement.
            </p>
          )}
          <div className="actions actions--stack">
            <Button
              variant="primary"
              type="button"
              disabled={!ready}
              onClick={() => onMint(false)}
            >
              {`Mint credential for ${account.name}`}
            </Button>
            <Button type="button" disabled={!ready} onClick={onBind}>
              {`Add federated binding to ${account.name}`}
            </Button>
            <Button type="button" disabled={!ready} onClick={onGrant}>
              {`Add environment grant to ${account.name}`}
            </Button>
            <Button
              variant="danger"
              type="button"
              disabled={!canDelete}
              onClick={onDelete}
            >
              {`Delete ${account.name}`}
            </Button>
          </div>
        </div>
      </div>

      <h3 className="machine__subhead">Setup journey</h3>
      {journey === null ? (
        <p className="machine__none">
          Automation principal: it runs off-box, on a schedule or in CI, and never delivers to a
          workload, so it has no setup journey. Its capability allowlist admits read, edit, publish
          and definitions-edit; never any manage- or instance capability.
        </p>
      ) : (
        <ol className="journey">
          {journey.map((step) => (
            <li className={`journey__step journey__step--${step.state}`} key={step.title}>
              <span className="journey__state">
                {step.state === 'done'
                  ? 'done'
                  : step.state === 'next'
                    ? 'next'
                    : 'blocked'}
              </span>
              <span className="journey__body">
                <span className="journey__title">{step.title}</span>
                <span className="journey__note">{step.note}</span>
                {/* The step's act lives on the step: the same dialog or control
                    the buttons above open, reached from where the gap is named. */}
                {step.action === undefined ? null : (
                  <JourneyActionButton
                    action={step.action}
                    ready={ready}
                    onGrant={onGrant}
                    onLeave={onLeave}
                  />
                )}
              </span>
            </li>
          ))}
        </ol>
      )}
    </>
  );
}

function journeyActionLabel(action: JourneyAction): string {
  switch (action) {
    case 'grant-read':
      return 'Grant read…';
    case 'grant-reveal':
      return 'Grant reveal…';
    case 'enable-opt-in':
      return 'Go to the opt-in…';
  }
}

/**
 * The opt-in control sits in the PolicyStrip at the top of the service
 * accounts tab, behind the side panel the journey renders in, so the step
 * closes the panel, then scrolls to and focuses the control: not a navigation.
 * A grant step opens the same grant dialog the actions do.
 */
function JourneyActionButton({
  action,
  ready,
  onGrant,
  onLeave,
}: {
  action: JourneyAction;
  ready: boolean;
  onGrant: () => void;
  onLeave: () => void;
}) {
  return (
    <Button
      className="journey__action"
      type="button"
      disabled={action !== 'enable-opt-in' && !ready}
      onClick={() => {
        if (action !== 'enable-opt-in') {
          onGrant();
          return;
        }
        onLeave();
        const toggle = document.getElementById('machine-policy-toggle');
        toggle?.scrollIntoView({ block: 'center' });
        toggle?.focus();
      }}
    >
      {journeyActionLabel(action)}
    </Button>
  );
}
