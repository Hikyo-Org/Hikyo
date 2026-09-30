import { useRef, useState, type MutableRefObject } from 'react';
import type { GrantResult } from '@hikyo/client';
import { createGrantsSequentially, grantFailureText } from '../../api/access.ts';
import { useReportingSupport } from '../../api/deliveryTargets.ts';
import {
  grantableFor,
  grantSubmittable,
  grantWideningReach,
  identityRefusalText,
  useGrantEnvironment,
  useKeyCatalogue,
  useRefreshGrants,
  type MachineEnvScope,
  type MachineGrantCapability,
  type ProjectRef,
  type ServiceAccount,
} from '../../api/identities.ts';
import { useNavigationGuard } from '../../app/useNavigationGuard.ts';
import { runPasskeyCeremony } from '../../api/values.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Button } from '../../ui/Button.tsx';
import { Checkbox } from '../../ui/Checkbox.tsx';
import { Dialog } from '../../ui/Dialog.tsx';

/**
 * GrantDialog is the grant-mutation warning.
 *
 * A grant attaches to the SERVICE ACCOUNT, never to a credential, so it
 * re-scopes every credential already in circulation the moment it lands. The
 * warning therefore names two numbers the operator cannot see anywhere else:
 * how many live credentials that is, and exactly which keys become reachable.
 *
 * Only `read` is offered in this setup journey, with `report-delivery-status`
 * beside it for a workload account, and on its own where the account already
 * reads. Conditional disclosure grants are separate operator acts: `reveal`
 * needs the live project opt-in, while `reveal-history` additionally needs an
 * active non-current workload pin.
 *
 * `report-delivery-status` (condition-reporting ADR D1) is workload-only, and
 * no human can hold it, so the server grants it only from `manage-members` at
 * org or instance scope (the grant-unheld rule, grants.go mayGrantUnheld).
 * whoami's `delivery_report_grant` hint is that same predicate, computed by
 * the server from the caller's own grants without an audited read. It is not
 * offered to a caller the server would refuse, as reveal is not offered
 * without the opt-in, nor on a server whose `/meta` does not advertise
 * delivery-target-report.
 */
export function GrantDialog({
  project,
  account,
  scope,
  machineReveal,
  mayGrantReporting,
  liveCredentials,
  onClose,
  onGranted,
}: {
  project: ProjectRef;
  account: ServiceAccount;
  scope: readonly MachineEnvScope[];
  /** The project's machine-reveal opt-in: reveal is grantable only while it is on. */
  machineReveal: boolean;
  /** whoami's grant hint covers this project's organisation. */
  mayGrantReporting: boolean;
  liveCredentials: number;
  onClose: () => void;
  onGranted: (environment: string, results: readonly GrantResult[]) => void;
}) {
  const reportCandidate =
    mayGrantReporting &&
    account.kind === 'workload' &&
    grantableFor(scope, 'report-delivery-status', machineReveal).length > 0;
  // `/meta` is read only where the atom could be offered at all.
  const support = useReportingSupport(reportCandidate);
  const reportGrantable = support === 'supported' && reportCandidate;
  const grantable =
    grantableFor(scope, 'read', machineReveal).length > 0 ||
    grantableFor(scope, 'reveal', machineReveal).length > 0 ||
    reportGrantable;
  // The in-flight latch lives here because the dialog's cancel event does,
  // while the mutation lives in GrantBody, a ref, because the gate needs the
  // truth at event time, not a render.
  const inFlight = useRef(false);
  // The same latch as render state: a landing grant refreshes the scope before
  // the submission settles, and a grant that took the last grantable option
  // must not swap GrantBody (and its navigation guard) for a bare Close while
  // the request is still in flight, nor unmount a failure it then reports: an
  // uncertain widening must stay on screen until the operator dismisses it.
  const [submitting, setSubmitting] = useState(false);
  const showBody = grantable || submitting;

  return (
    <Dialog
      title={`Add environment grant · ${account.name}`}
      lede="Grants attach to the service account, never to a credential."
      onCancel={(e) => {
        e.preventDefault();
        if (!inFlight.current) {
          onClose();
        }
      }}
      // GrantBody owns the action row, and it is not mounted when there is
      // nothing to widen, so that branch would otherwise leave Escape as the
      // only way out. Nothing to reorder: Close is the only button.
      actions={
        showBody ? undefined : (
          <Button type="button" onClick={onClose}>
            Close
          </Button>
        )
      }
    >
      {!showBody && reportCandidate && support === 'pending' ? (
        <p role="status">Checking whether this server accepts delivery-target reports…</p>
      ) : !showBody ? (
        <p role="status">
          {machineReveal
            ? 'This account already reads and reveals every environment in the project. There is nothing to widen.'
            : 'This account already reads every environment in the project. There is nothing to widen; reveal needs the machine-reveal opt-in first.'}
        </p>
      ) : (
        <GrantBody
          project={project}
          account={account}
          scope={scope}
          machineReveal={machineReveal}
          reportGrantable={reportGrantable}
          liveCredentials={liveCredentials}
          inFlightRef={inFlight}
          onSubmitting={setSubmitting}
          onClose={onClose}
          onGranted={onGranted}
        />
      )}
    </Dialog>
  );
}

/**
 * GrantBody exists so its queries and selection state are never mounted when
 * there is nothing grantable. Hooks cannot be conditional, and a query fired
 * for a dialog that only says "nothing to widen" would be a request the
 * surface makes and then ignores.
 */
function GrantBody({
  project,
  account,
  scope,
  machineReveal,
  reportGrantable,
  liveCredentials,
  inFlightRef,
  onSubmitting,
  onClose,
  onGranted,
}: {
  project: ProjectRef;
  account: ServiceAccount;
  scope: readonly MachineEnvScope[];
  machineReveal: boolean;
  /** A workload account, and a caller the server lets grant report-delivery-status. */
  reportGrantable: boolean;
  liveCredentials: number;
  /** GrantDialog's Escape gate, held while the mutation is in flight. */
  inFlightRef: MutableRefObject<boolean>;
  /** GrantDialog keeps this body mounted from submit until it succeeds; a failure stays mounted. */
  onSubmitting: (submitting: boolean) => void;
  onClose: () => void;
  onGranted: (environment: string, results: readonly GrantResult[]) => void;
}) {
  const grant = useGrantEnvironment(project);
  const refreshGrants = useRefreshGrants(project);
  // Reveal is offered only while the project opt-in is on: the UI grants read
  // by default, and the select widens to reveal under the same ceremony.
  const [capability, setCapability] = useState<MachineGrantCapability>(
    grantableFor(scope, 'read', machineReveal).length > 0
      ? 'read'
      : grantableFor(scope, 'reveal', machineReveal).length > 0
        ? 'reveal'
        : 'report-delivery-status',
  );
  // The inputs can move under an open dialog (the opt-in withdrawn, the
  // account's scope refreshed). Derive the effective selection during render so
  // no stale choice survives to the submit, rather than folding it back with an
  // effect. A withdrawn opt-in collapses a stale `reveal` to `read`; grantable
  // follows the effective capability (grantableFor('reveal', false) is empty,
  // so read's list is what a withdrawn opt-in must fall back to), and an
  // environment no longer in that list snaps to its head.
  const effectiveCapability: MachineGrantCapability =
    (capability === 'reveal' && !machineReveal) ||
    (capability === 'report-delivery-status' && !reportGrantable)
      ? 'read'
      : capability;
  const grantable = grantableFor(scope, effectiveCapability, machineReveal);
  const [environment, setEnvironment] = useState(grantable[0]?.id ?? '');
  const effectiveEnvironment =
    environment !== '' && !grantable.some((s) => s.id === environment)
      ? (grantable[0]?.id ?? '')
      : environment;
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // The report atom rides beside read where the chosen environment lacks it.
  const chosen = grantable.find((s) => s.id === effectiveEnvironment);
  const offersReporting =
    reportGrantable && effectiveCapability === 'read' && chosen?.report === false;
  const [report, setReport] = useState(false);
  const withReport = offersReporting && report;
  const capabilities = withReport
    ? [effectiveCapability, 'report-delivery-status']
    : [effectiveCapability];
  const reporting = effectiveCapability === 'report-delivery-status';

  // An in-flight grant is not dismissible by Back or unload either: a widening
  // that commits behind a dismissed dialog is invisible at the moment it most
  // needs review.
  useNavigationGuard(busy, () => {});

  /**
   * What a `read` grant actually delivers, read off the delivery surface rather
   * than guessed: the whole key CATALOGUE, every key's name and its
   * classification, and no value of any classification, config included. So
   * the newly reachable set is every key, not only the secrets. The catalogue
   * endpoint is used rather than the value listing on purpose: a value listing
   * is authorized for the HUMAN reading it and carries config plaintext this
   * dialog never renders, and a fetch is a cached copy (see useKeyCatalogue).
   */
  const values = useKeyCatalogue(project);
  // A read grant reaches the whole catalogue by name; a reveal grant adds
  // standing decryption of the SECRETS only (read is already held, so config
  // values are not new). Presence per environment is not in this read, so the
  // sentence says "when set" rather than claiming plaintext for every row.
  const catalogue = values.data?.items ?? [];
  const reachable =
    effectiveCapability === 'reveal'
      ? catalogue.filter((key) => key.classification === 'secret')
      : catalogue;
  // The opt-in can be withdrawn while this dialog is open: the capability
  // select disappears, but a stale reveal choice or a stale environment must
  // not stay submittable.
  const submittable = grantSubmittable(scope, effectiveEnvironment, effectiveCapability, machineReveal);
  // The mint formula's conjunct for a WIDENING is the delta, not the whole
  // post-state, which is what the server computes in checkMachineWidening.
  const widening = grantWideningReach(scope, effectiveEnvironment, effectiveCapability);

  const submit = async () => {
    setBusy(true);
    inFlightRef.current = true;
    onSubmitting(true);
    setFailure(null);
    // Issued-vs-nothing-happened, the mint's line: once the request leaves, a
    // failure does not mean the widening did not land, and a widening that
    // landed re-scoped every live credential the moment it did.
    let issued = false;
    try {
      // Each newly reachable environment takes its own reauthentication, in the
      // same purpose the server consumes. Empty today, and for the same reason
      // the mint's is: nothing this account can hold reaches plaintext.
      for (const widened of widening) {
        await runPasskeyCeremony({
          operation: 'mint',
          environmentId: widened.id,
          keyIds: [],
        });
      }
      issued = true;
      const results = await createGrantsSequentially(capabilities, (capability) =>
        grant.mutateAsync({
          environment: effectiveEnvironment,
          principal: account.principal_id,
          capability,
        }),
      );
      onGranted(chosen?.name ?? effectiveEnvironment, results);
      onSubmitting(false);
    } catch (error) {
      if (issued) {
        refreshGrants();
        setFailure(grantFailureText(error));
      } else {
        setFailure(identityRefusalText(error));
      }
    } finally {
      inFlightRef.current = false;
      setBusy(false);
    }
  };

  const chooseCapability = (next: MachineGrantCapability) => {
    setCapability(next);
    setEnvironment(grantableFor(scope, next, machineReveal)[0]?.id ?? '');
  };

  return (
    <>
      {machineReveal || reportGrantable ? (
        <div className="field">
          <label htmlFor="grant-capability">Capability</label>
          <select
            id="grant-capability"
            value={effectiveCapability}
            onChange={(event) =>
              chooseCapability(
                event.target.value === 'reveal' || event.target.value === 'report-delivery-status'
                  ? event.target.value
                  : 'read',
              )
            }
          >
            <option value="read">read (configuration and secret presence)</option>
            {machineReveal ? (
              <option value="reveal">reveal (standing secret plaintext)</option>
            ) : null}
            {reportGrantable ? (
              <option value="report-delivery-status">
                report-delivery-status (value-free delivery-target status)
              </option>
            ) : null}
          </select>
        </div>
      ) : null}
      <div className="field">
        <label htmlFor="grant-environment">{`Environment (${effectiveCapability})`}</label>
        <select
          id="grant-environment"
          value={effectiveEnvironment}
          onChange={(event) => setEnvironment(event.target.value)}
        >
          {grantable.length === 0 ? <option value="">Nothing left to widen</option> : null}
          {grantable.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
      </div>
      {offersReporting ? (
        <Checkbox
          label="Also grant report-delivery-status: its controller may report value-free delivery-target status here"
          checked={withReport}
          onChange={(event) => setReport(event.target.checked)}
        />
      ) : null}

      <p className="ceremony__cap" role="status">
        <span className="alert__glyph" aria-hidden="true">
          !
        </span>
        <span>
          {`This grant re-scopes every credential already in circulation. ${account.name} has ${String(liveCredentials)} live credential${liveCredentials === 1 ? '' : 's'}, and each one gains ${
            effectiveCapability === 'read'
              ? 'read (configuration and secret presence)'
              : reporting
                ? 'report-delivery-status (value-free delivery-target status reports)'
                : 'reveal (standing secret plaintext decryption)'
          }${withReport ? ' and report-delivery-status' : ''} on ${chosen?.name ?? 'that environment'} the moment this lands.`}
        </span>
      </p>

      <p className="ceremony__stepup">
        <span className="alert__glyph" aria-hidden="true">
          ⚿
        </span>
        <span>
          <strong>The formula.</strong>{' '}
          {reporting ? (
            'manage-members at organisation or instance scope: no human holds report-delivery-status, so every grant of it is an unheld grant, which a project- or environment-scope member manager may not make. It decrypts nothing, so there is no disclosure conjunct and no reauthentication.'
          ) : (
            <>
              manage-identities on this project, manage-members over the environment, and a
              disclosure capability over every environment this grant NEWLY lets the account
              decrypt, the delta, not the whole post-state, because that is what the grant adds.{' '}
              {widening.length === 0
                ? 'This grant newly decrypts nothing, so the disclosure conjunct is vacuous and no reauthentication is required.'
                : `It newly decrypts ${widening.map((w) => w.name).join(', ')}, so each takes its own passkey reauthentication before the grant lands.`}
              {withReport
                ? ' report-delivery-status additionally takes manage-members at organisation or instance scope: no human holds it, so it is always an unheld grant.'
                : ''}
            </>
          )}
        </span>
      </p>

      {/* FAIL CLOSED. A pending or failed catalogue read cannot be rendered as
          "nothing becomes reachable": that is the one answer it does not have,
          and it is the answer that makes the grant look harmless. */}
      {reporting ? (
        <p className="ceremony__scope">
          A status report carries no key name and no value, so this grant makes nothing
          reachable.
        </p>
      ) : values.isSuccess ? (
        <>
          <p className="ceremony__scope">
            {reachable.length === 0
              ? effectiveCapability === 'reveal' && catalogue.length > 0
                ? 'This project declares no secrets today, so the grant decrypts nothing yet, and every secret declared later.'
                : 'This project declares no keys, so the grant reaches an empty catalogue today, and every key declared later.'
              : effectiveCapability === 'read'
                ? 'Newly reachable: every key below, by name and classification. A read grant delivers configuration values and secret presence; plaintext needs reveal.'
                : 'Newly decryptable: every secret below, as standing authority over its value wherever it is set. Configuration keys are not listed: read already reaches them.'}
          </p>
          {reachable.length === 0 ? null : (
            // Focusable: the list scrolls past 30vh, and a scroll region must
            // be reachable by keyboard.
            <ul
              className="ceremony__keys"
              aria-label="Keys this grant makes reachable"
              tabIndex={0}
            >
              {reachable.map((key) => (
                <li className="mono" key={key.id}>
                  {`${key.name} · ${key.classification}`}
                </li>
              ))}
            </ul>
          )}
        </>
      ) : (
        <Alert>
          {values.isError
            ? 'The key catalogue could not be read, so what this grant makes reachable cannot be named, and a grant whose blast radius is unknown is not one to make from here.'
            : 'Reading what this grant would make reachable…'}
        </Alert>
      )}

      {failure !== null ? (
        <Alert>{failure}</Alert>
      ) : null}

      {/* This row belongs to GrantBody, which the dialog renders conditionally,
          so it stays in the body and carries the atom's action class. Primary
          last, as everywhere else. */}
      <div className="dialog__actions">
        <Button type="button" onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button
          variant="primary"
          type="button"
          disabled={busy || !submittable || !(reporting || values.isSuccess)}
          onClick={() => void submit()}
        >
          {busy ? 'Granting…' : `Grant ${capabilities.join(' and ')}`}
        </Button>
      </div>
    </>
  );
}
