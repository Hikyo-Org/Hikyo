import { useState } from 'react';
import type { FederatedClaimPin } from '@hikyo/client';
import {
  BINDING_LIFETIMES,
  bindingFailureText,
  CI_EVENTS,
  FEDERATION_PRESETS,
  identityRefusalText,
  isoDay,
  KUBERNETES_PRESET,
  parseClaimNumber,
  presetFieldId,
  pullRequestRefusal,
  useCreateBinding,
  useRefreshAccount,
  type ClaimPin,
  type FederationPreset,
  type MachineCredential,
  type MachineDisclosureReach,
  type ProjectRef,
  type ServiceAccount,
} from '../../api/identities.ts';
import { useNavigationGuard } from '../../app/useNavigationGuard.ts';
import { runPasskeyCeremony } from '../../api/values.ts';
import { Alert } from '../../ui/Alert.tsx';
import { Badge } from '../../ui/Badge.tsx';
import { Button } from '../../ui/Button.tsx';
import { Checkbox } from '../../ui/Checkbox.tsx';
import { Dialog } from '../../ui/Dialog.tsx';
import { Glyph } from '../../ui/Glyph.tsx';

import { ExpiryBadge } from './Credentials.tsx';
export function BindingCard({
  account,
  credential,
  now,
  ready,
  onReplace,
  onRevoke,
}: {
  account: ServiceAccount;
  credential: MachineCredential;
  now: Date;
  /**
   * Every query the replacement's warning is computed from has succeeded.
   * Replace mints a new binding, so it carries the same post-state reach the
   * first mint did; revoke is a narrowing and needs no such read, so it is not
   * gated on it.
   */
  ready: boolean;
  onReplace: (credential: MachineCredential) => void;
  onRevoke: (credential: MachineCredential) => void;
}) {
  return (
    <div className="bindrow" data-credential={credential.id}>
      <p className="bindrow__head">
        <code className="mono">{account.name}</code>
        <Badge>federated</Badge>
        <ExpiryBadge credential={credential} now={now} />
        <span className="cred__meta">
          matched byte-for-byte, no wildcards, no case folding; renewal is a mint
        </span>
      </p>
      {/* Every pair is wrapped: a `dl` takes either dt/dd children or `div`
          children, never a mixture, and axe checks exactly that. */}
      <dl className="kv">
        {[
          { term: 'issuer', value: credential.issuer ?? 'unknown' },
          { term: 'subject', value: credential.subject ?? 'unknown' },
          { term: 'audience', value: credential.audience ?? 'unknown' },
          ...(credential.required_claims ?? []).map((pin) => ({
            term: pin.claim,
            value: claimText(pin),
          })),
        ].map((pair) => (
          <div className="kv__pair" key={pair.term}>
            <dt>{pair.term}</dt>
            <dd className="mono">{pair.value}</dd>
          </div>
        ))}
      </dl>
      {credential.reactivated_at === undefined ? null : (
        <Alert tone="warn">{`Quarantined since the restore on ${isoDay(credential.reactivated_at)}: this binding permanently refuses any token issued at or before that instant plus the accepted clock skew.`}</Alert>
      )}
      <div className="machine__actions">
        <Button
          type="button"
          disabled={!ready}
          onClick={() => onReplace(credential)}
        >
          {`Replace binding on ${account.name}`}
        </Button>
        <Button type="button" onClick={() => onRevoke(credential)}>
          {`Revoke binding on ${account.name}`}
        </Button>
      </div>
    </div>
  );
}

function claimText(pin: ClaimPin): string {
  if (pin.string_value !== undefined) {
    return pin.string_value;
  }
  if (pin.number_value !== undefined) {
    return String(pin.number_value);
  }
  if (pin.bool_value !== undefined) {
    return String(pin.bool_value);
  }
  return 'unpinned';
}

/**
 * presetForBinding recovers the platform of a binding being replaced. The
 * credential row carries no platform type, that lives on the issuer, not the
 * binding, so the platform is inferred from the claims it pinned: the preset
 * whose required claims the predecessor pins the most of. It is only ever used
 * to choose which fields the replace form renders; the claim VALUES are seeded
 * straight from the predecessor, so a misdetection would show a spare field,
 * never bind the wrong identity.
 */
export function presetForBinding(credential: MachineCredential): FederationPreset {
  const pinned = new Set((credential.required_claims ?? []).map((pin) => pin.claim));
  let best = KUBERNETES_PRESET;
  let bestScore = -1;
  for (const candidate of FEDERATION_PRESETS) {
    const score = candidate.claims.filter((field) => pinned.has(field.claim)).length;
    if (score > bestScore) {
      best = candidate;
      bestScore = score;
    }
  }
  return best;
}

/**
 * seedClaims fills the replace form's claim inputs from the predecessor. The
 * read-shape `number_value` is a bigint (an int64 repository id does not
 * survive a float), and `claimText` stringifies it losslessly, so the seeded
 * text round-trips back through the numeric parse on submit.
 */
export function seedClaims(
  preset: FederationPreset,
  credential: MachineCredential,
): Record<string, string> {
  const byClaim = new Map((credential.required_claims ?? []).map((pin) => [pin.claim, pin] as const));
  const seeded: Record<string, string> = {};
  for (const field of preset.claims) {
    const pin = byClaim.get(field.claim);
    seeded[field.claim] = pin === undefined ? '' : claimText(pin);
  }
  return seeded;
}

/**
 * carriedClaims are the predecessor's pins that no preset field renders, a
 * custom claim a CLI operator added beyond the platform's required set. A
 * replacement must carry EVERY one of them verbatim: dropping a pin the form
 * cannot show would silently weaken the successor's identity constraints, which
 * is precisely the re-point-without-review the immutable-binding rule forbids.
 * The form displays them read-only so the preservation is visible, not silent.
 */
export function carriedClaims(
  preset: FederationPreset,
  credential: MachineCredential,
): ClaimPin[] {
  const rendered = new Set(preset.claims.map((field) => field.claim));
  return (credential.required_claims ?? [])
    .filter((pin) => !rendered.has(pin.claim));
}

export function unsafeNumericClaim(credential: MachineCredential): string | null {
  for (const pin of credential.required_claims ?? []) {
    if (
      pin.number_value !== undefined &&
      (pin.number_value > BigInt(Number.MAX_SAFE_INTEGER) ||
        pin.number_value < BigInt(Number.MIN_SAFE_INTEGER))
    ) {
      return pin.claim;
    }
  }
  return null;
}

/** Convert only numeric pins the request contract can carry exactly. */
function toRequestPin(pin: ClaimPin): FederatedClaimPin | null {
  if (pin.string_value !== undefined) {
    return { claim: pin.claim, string_value: pin.string_value };
  }
  if (pin.number_value !== undefined) {
    const value = Number(pin.number_value);
    if (!Number.isSafeInteger(value) || BigInt(value) !== pin.number_value) return null;
    return { claim: pin.claim, number_value: value };
  }
  if (pin.bool_value !== undefined) {
    return { claim: pin.claim, bool_value: pin.bool_value };
  }
  return { claim: pin.claim };
}

/** Render preserved read-shape pins without narrowing their numeric values. */
function requestPinText(pin: ClaimPin): string {
  if (pin.string_value !== undefined) {
    return pin.string_value;
  }
  if (pin.number_value !== undefined) {
    return String(pin.number_value);
  }
  if (pin.bool_value !== undefined) {
    return String(pin.bool_value);
  }
  return 'unpinned';
}

/**
 * BindingDialog is the federation form.
 *
 * Two of its rules come straight off the server and are asked for HERE so an
 * operator meets them as a form rather than as a 400: the audience is mandatory
 * and may not be the issuer's default, and each platform's immutable
 * identifiers must be pinned. The third, the pull-request refusal, is the
 * load-bearing one: the protection comes from the pinned `event_name`, never
 * from the subject's shape, because a `pull_request_target` token carries the
 * ordinary ref-form subject a production binding names.
 */
export function BindingDialog({
  project,
  accounts,
  initial,
  replaces,
  reachFor,
  onClose,
  onCreated,
}: {
  project: ProjectRef;
  accounts: readonly ServiceAccount[];
  initial: ServiceAccount;
  /**
   * The binding this mint supersedes, when the operator chose Replace. Bindings
   * are IMMUTABLE, so a replacement is a fresh mint naming `replaces`: the
   * server revokes the predecessor and inserts the successor in ONE
   * transaction, so there is never a gap with no binding nor an overlap with
   * two. The form pre-seeds from it and locks the target account, the
   * predecessor belongs to exactly one, and hides the platform picker, since
   * the platform cannot move under a replacement.
   */
  replaces?: MachineCredential;
  /**
   * The selected account's post-state reach. A binding is a mint (#62), so the
   * server demands the same disclosure formula the credential mint does: one
   * fresh window per environment the account can decrypt in the resulting
   * state, whether current or historical plaintext. The environment union
   * consumes one ceremony even when both disclosure classes reach it.
   */
  reachFor: (accountId: string) => readonly MachineDisclosureReach[];
  onClose: () => void;
  onCreated: (message: string) => void;
}) {
  const create = useCreateBinding(project);
  const refresh = useRefreshAccount(project);
  const replacing = replaces !== undefined;
  // A replacement's platform is the predecessor's, detected from the claims it
  // pinned; a fresh binding starts on Kubernetes. The account is locked to the
  // row the replace was launched from, because a binding belongs to one.
  const seedPreset = replaces === undefined ? KUBERNETES_PRESET : presetForBinding(replaces);
  const unsafeClaim = replaces === undefined ? null : unsafeNumericClaim(replaces);
  // Predecessor pins no form field renders, carried verbatim so a replacement
  // never silently drops an identity constraint the form could not show.
  const carried = replaces === undefined ? [] : carriedClaims(seedPreset, replaces);
  const [account, setAccount] = useState(initial.id);
  const [preset, setPreset] = useState<FederationPreset>(seedPreset);
  const unsupportedPreset = preset.id === 'forgejo';
  const [issuer, setIssuer] = useState(replaces?.issuer ?? seedPreset.issuer);
  const [subject, setSubject] = useState(replaces?.subject ?? seedPreset.subject);
  const [audience, setAudience] = useState(replaces?.audience ?? '');
  const [claims, setClaims] = useState<Record<string, string>>(() =>
    replaces === undefined ? blankClaims(KUBERNETES_PRESET) : seedClaims(seedPreset, replaces),
  );
  const [lifetime, setLifetime] = useState('default');
  const [deliberate, setDeliberate] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // A refusal describes the form as it was when it was refused. Editing any
  // field makes it stale, and a stale refusal sitting beside a fresh one is two
  // alerts saying different things about one form.
  const edited = () => setFailure(null);

  const choose = (next: FederationPreset) => {
    setPreset(next);
    setIssuer(next.issuer);
    setSubject(next.subject);
    setClaims(blankClaims(next));
    setDeliberate(false);
    edited();
  };

  const eventName = claims['event_name'] ?? '';
  const refusal = pullRequestRefusal(eventName);

  /**
   * pinsOrRefusal builds the pinned claims, or names the first field that
   * cannot be one.
   *
   * The numeric fields are the ones worth refusing over: an immutable
   * repository id is what stops a renamed-and-reused path inheriting this
   * binding, and a field that quietly became 0, or rounded to a neighbouring
   * id past 2^53, would bind this service account to somebody else's
   * repository while looking like it worked.
   */
  const pinsOrRefusal = (): FederatedClaimPin[] | string => {
    const pins: FederatedClaimPin[] = [];
    for (const field of preset.claims) {
      const raw = claims[field.claim] ?? '';
      const original = replaces?.required_claims?.find((pin) => pin.claim === field.claim);
      if (original?.string_value !== undefined) {
        pins.push({ claim: field.claim, string_value: raw });
        continue;
      }
      if (original?.bool_value !== undefined) {
        if (raw !== 'true' && raw !== 'false') {
          return `${field.label} (${field.claim}) preserves a boolean pin: enter true or false. Use the CLI for a deliberate type change. Nothing was bound.`;
        }
        pins.push({ claim: field.claim, bool_value: raw === 'true' });
        continue;
      }
      if (original?.number_value !== undefined || field.kind === 'number') {
        const value = parseClaimNumber(raw);
        if (value === null) {
          return `${field.label} (${field.claim}) must be a whole number the issuer actually mints: digits only, and inside the range this contract can carry exactly. Nothing was bound.`;
        }
        pins.push({ claim: field.claim, number_value: value });
        continue;
      }
      if (raw.trim() === '') {
        return `${field.label} (${field.claim}) is required: this issuer's bindings must pin it, and the server refuses a binding without it. Nothing was bound.`;
      }
      pins.push({ claim: field.claim, string_value: raw });
    }
    return pins;
  };

  const submit = async () => {
    if (unsupportedPreset) {
      setFailure('Forgejo Actions tokens do not expose the immutable repository identity required for a binding. Nothing was bound.');
      return;
    }
    if (unsafeClaim !== null) {
      setFailure(
        `Claim ${unsafeClaim} is outside the browser's exact integer range. Replace this binding with the CLI so its constraint is not rounded.`,
      );
      return;
    }
    if (refusal !== null && !deliberate) {
      setFailure(
        'This binding pins a pull-request event. Acknowledge deliberately below, or pin another event.',
      );
      return;
    }
    // Mandatory, and refused HERE rather than as a 400: a token minted for
    // another consumer must not authenticate here, and an empty field is the
    // most likely way to end up with no such constraint at all.
    if (audience.trim() === '') {
      setFailure(
        'An audience is mandatory, and it may never be the issuer’s default. A token minted for another consumer must not authenticate here. Nothing was bound.',
      );
      return;
    }
    if (issuer.trim() === '') {
      setFailure(
        'An issuer is mandatory because federated bindings match it byte-for-byte. Nothing was bound.',
      );
      return;
    }
    if (subject.trim() === '') {
      setFailure(
        'A subject is mandatory because federated bindings match it byte-for-byte. Nothing was bound.',
      );
      return;
    }
    const pins = pinsOrRefusal();
    if (typeof pins === 'string') {
      setFailure(pins);
      return;
    }
    // Carried pins are appended, never merged: a preset field and a carried
    // claim can never share a name (carried is exactly the complement), so
    // there is nothing to reconcile.
    const carriedRequests: FederatedClaimPin[] = [];
    for (const pin of carried) {
      const converted = toRequestPin(pin);
      if (converted === null) {
        setFailure('A preserved numeric pin cannot be carried exactly by this contract. Nothing was bound.');
        return;
      }
      carriedRequests.push(converted);
    }
    const allPins = [...pins, ...carriedRequests];
    setBusy(true);
    setFailure(null);
    // Same issued-vs-nothing-happened line the mint draws: once the request
    // leaves, a failure says nothing about whether a live external login path
    // now exists, and the row list is the only place it would show.
    let issued = false;
    try {
      // A binding is a mint: one reauthentication per environment the account
      // decrypts in the post-state, in the same purpose the server consumes.
      // Current and historical reach share one ceremony per environment.
      for (const environment of reachFor(account)) {
        await runPasskeyCeremony({
          operation: 'mint',
          environmentId: environment.id,
          keyIds: [],
        });
      }
      const seconds = BINDING_LIFETIMES.find((entry) => entry.id === lifetime)?.seconds;
      issued = true;
      const result = await create.mutateAsync({
        serviceAccount: account,
        issuer,
        subject,
        audience,
        requiredClaims: allPins,
        ...(seconds === undefined ? {} : { lifetimeSeconds: seconds }),
        ...(replaces === undefined ? {} : { replaces: replaces.id }),
      });
      const clampNote = result.clamped
        ? ' The instance ceiling shortened the requested lifetime.'
        : '';
      onCreated(
        replacing
          ? `Replaced. The predecessor was revoked and the successor inserted in one transaction: no gap, no overlap. The new binding matches ${subject} byte-for-byte.${clampNote}`
          : `Bound. The binding matches ${subject} byte-for-byte and nothing else.${clampNote}`,
      );
    } catch (error) {
      if (issued) {
        refresh(account);
        setFailure(bindingFailureText(error));
      } else {
        setFailure(identityRefusalText(error));
      }
    } finally {
      setBusy(false);
    }
  };

  // An in-flight binding is not dismissible: Escape, Back or unload here would
  // hide a mutation that may commit, the operator stays until it resolves.
  useNavigationGuard(busy, () => {});

  return (
    <Dialog
      title={replacing ? 'Replace federated binding' : 'Add federated binding'}
      lede={
        replacing
          ? 'Bindings are immutable, so this replaces the predecessor: the server revokes it and inserts this successor in one transaction, with no gap with no binding and no overlap with two. The fields are seeded from the predecessor; change what the replacement should carry.'
          : 'A byte-exact (issuer, subject) pair naming exactly one service account. The audience is mandatory and may not be the issuer’s default: a token minted for another consumer must not authenticate here.'
      }
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) {
          onClose();
        }
      }}
      actions={
        <>
          <Button type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" type="button" disabled={busy || unsafeClaim !== null || unsupportedPreset} onClick={() => void submit()}>
            {busy
              ? replacing
                ? 'Replacing…'
                : 'Binding…'
              : replacing
                ? 'Replace this binding'
                : 'Bind this identity'}
          </Button>
        </>
      }
    >
      {/* One native latch for the whole target: an issued request is for the
          form as submitted, so nothing here may change until it resolves,
          otherwise the success or failure sentence describes one account while
          the operator is looking at another. */}
      {unsafeClaim === null ? null : (
        <Alert>
          A preserved numeric pin cannot be carried exactly by this contract. Claim <code>{unsafeClaim}</code> is
          outside the browser&apos;s exact integer range. Use the CLI to replace this binding without
          rounding its authentication constraint. Nothing was bound.
        </Alert>
      )}
      <fieldset className="machine__lock" disabled={busy || unsafeClaim !== null || unsupportedPreset}>
      {replacing ? null : (
        <div className="machine__presets">
          {FEDERATION_PRESETS.map((entry) => (
            <Button
              key={entry.id}
              type="button"
              aria-pressed={preset.id === entry.id}
              disabled={entry.id === 'forgejo'}
              onClick={() => choose(entry)}
            >
              {preset.id === entry.id ? <><Glyph name="check" /> </> : null}
              {entry.label}
            </Button>
          ))}
        </div>
      )}
      {replacing ? (
        <p className="field__hint">Replacement fields preserve each predecessor pin&apos;s scalar type. Use the CLI for a deliberate type change.</p>
      ) : null}
      {!replacing || unsupportedPreset ? (
        <p className="field__hint">Forgejo Actions is unavailable: its tokens do not expose the immutable repository identity required for a binding. Deployment adapters remain supported.</p>
      ) : null}

      <div className="field">
        <label htmlFor="binding-account">Service account</label>
        <select
          id="binding-account"
          value={account}
          // A replacement belongs to exactly one account, the predecessor's,
          // so the target is fixed and the selector is locked.
          disabled={replacing}
          onChange={(event) => {
            // The acknowledgement is consent for ONE account's fetch authority.
            // A different target is a different decision, so it does not carry.
            setAccount(event.target.value);
            setDeliberate(false);
            edited();
          }}
        >
          {accounts.map((sa) => (
            <option key={sa.id} value={sa.id}>
              {sa.name}
            </option>
          ))}
        </select>
      </div>

      <div className="field">
        <label htmlFor="binding-issuer">Issuer</label>
        <input
          id="binding-issuer"
          className="mono"
          value={issuer}
          onChange={(event) => {
            setIssuer(event.target.value);
            edited();
          }}
        />
      </div>

      <div className="field">
        <label htmlFor="binding-subject">Subject, matched byte-for-byte</label>
        <input
          id="binding-subject"
          className="mono"
          value={subject}
          onChange={(event) => {
            setSubject(event.target.value);
            edited();
          }}
        />
      </div>

      <div className="field">
        <label htmlFor="binding-audience">Audience</label>
        <input
          id="binding-audience"
          className="mono"
          value={audience}
          onChange={(event) => {
            setAudience(event.target.value);
            edited();
          }}
        />
      </div>

      {preset.claims.map((field) =>
        field.kind === 'event' ? (
          <div className="field" key={field.claim}>
            <label htmlFor="binding-event">{field.label}</label>
            <select
              id="binding-event"
              value={eventName}
              onChange={(event) => {
                setClaims((current) => ({ ...current, event_name: event.target.value }));
                setDeliberate(false);
                edited();
              }}
            >
              {CI_EVENTS.map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </select>
          </div>
        ) : (
          <div className="field" key={field.claim}>
            <label htmlFor={presetFieldId(field.claim)}>{`${field.label} (${field.claim})`}</label>
            <input
              id={presetFieldId(field.claim)}
              className="mono"
              inputMode={field.kind === 'number' ? 'numeric' : 'text'}
              value={claims[field.claim] ?? ''}
              onChange={(event) => {
                setClaims((current) => ({ ...current, [field.claim]: event.target.value }));
                edited();
              }}
            />
          </div>
        ),
      )}

      {carried.length > 0 ? (
        <div className="field">
          <span className="field__label">
            Preserved pins, carried byte-for-byte from the binding being replaced
          </span>
          <dl className="kv">
            {carried.map((pin) => (
              <div className="kv__pair" key={pin.claim}>
                <dt>{pin.claim}</dt>
                <dd className="mono">{requestPinText(pin)}</dd>
              </div>
            ))}
          </dl>
        </div>
      ) : null}

      <div className="field">
        <label htmlFor="binding-lifetime">Binding lifetime</label>
        <select
          id="binding-lifetime"
          value={lifetime}
          onChange={(event) => {
            setLifetime(event.target.value);
            edited();
          }}
        >
          {BINDING_LIFETIMES.map((entry) => (
            <option key={entry.id} value={entry.id} disabled={entry.disabled === true}>
              {entry.label}
            </option>
          ))}
        </select>
      </div>

      {refusal === null ? null : (
        <>
          <Alert>{refusal}</Alert>
          <Checkbox
            label="I am deliberately binding a pull-request identity and accept that pull-request authors reach this account's scope."
            checked={deliberate}
            onChange={(event) => setDeliberate(event.target.checked)}
          />
        </>
      )}
      </fieldset>

      {failure !== null ? (
        <Alert>{failure}</Alert>
      ) : null}

      <p className="machine__footnote">
        No wildcards, no namespace patterns, no case folding; canonicalisation merges distinct
        external identities. Bindings are immutable and expire on the same terms as a bearer
        credential: renewal is a mint, never an edit.
      </p>
    </Dialog>
  );
}

/** blankClaims is a preset's pin fields, empty except the event a CI binding defaults to. */
function blankClaims(preset: FederationPreset): Record<string, string> {
  return Object.fromEntries(
    preset.claims.map((field) => [field.claim, field.kind === 'event' ? 'push' : '']),
  );
}
