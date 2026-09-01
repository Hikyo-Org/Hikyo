import type { SamlSpKey } from '@hikyo/client';
import { useState } from 'react';

import { ApiError } from '../api/client.ts';
import {
  samlFailureText,
  useCompromiseRetireSamlSpKey,
  useRetireSamlSpKey,
  useRotateSamlSpKey,
  useSamlSpKeys,
  type SamlAction,
} from '../api/samlProviders.ts';
import { Alert, Done, Panel, TypedNameConfirm } from './Sections.tsx';

const secondFactor = (error: unknown) => error instanceof ApiError && error.status === 403;
const nondisclosed = (error: unknown) => error instanceof ApiError && error.status === 404;

/**
 * SAML SP signing-key lifecycle (#500), a panel on `instance-admin`.
 *
 * Two retirements with distinct ceremonies and consequences: ordinary
 * retirement erases an already-`retiring` key (rotate first, hence the active
 * key refuses retirement), and compromise-retirement erases and replaces the
 * ACTIVE key with no overlap window. Only fingerprints and lifecycle state are
 * ever shown — private key material never leaves the server.
 */
export function SamlSpKeysPanel() {
  const keys = useSamlSpKeys();
  const [feedback, setFeedback] = useState<{ failure: string | null; done: string | null }>({
    failure: null,
    done: null,
  });
  const rotate = useRotateSamlSpKey();
  const report = (error: unknown, action: SamlAction) =>
    setFeedback({ failure: samlFailureText(error, action), done: null });
  const ok = (message: string) => setFeedback({ failure: null, done: message });
  const clear = () => setFeedback({ failure: null, done: null });

  return (
    <Panel id="instance-saml-sp-keys" title="SAML SP signing keys">
      <p>
        The service provider signs its AuthnRequests with these keys. Both the
        active and any overlap-retiring certificate stay published in SP metadata
        until the retiring one is explicitly erased, so an IdP never rejects a
        signature mid-rotation.
      </p>
      {keys.isPending ? <p role="status">Loading SP signing keys…</p> : null}
      {secondFactor(keys.error) ? (
        <Alert>
          Reading SP signing keys needs a second factor and this authority. If
          you hold it, present your authenticator code or passkey in the banner
          above.
        </Alert>
      ) : null}
      {nondisclosed(keys.error) ? (
        <p role="status">SP signing keys are not disclosed to this session.</p>
      ) : null}
      {keys.isError && !secondFactor(keys.error) && !nondisclosed(keys.error) ? (
        <Alert>{samlFailureText(keys.error, 'list')}</Alert>
      ) : null}

      {feedback.failure !== null ? <Alert>{feedback.failure}</Alert> : null}
      {feedback.done !== null ? <Done>{feedback.done}</Done> : null}

      {keys.isSuccess
        ? keys.data.keys.map((key) => (
            <SpKeyRow key={key.fingerprint} spKey={key} onDone={ok} onFailure={report} onBusy={clear} />
          ))
        : null}

      <div className="panel__actions">
        <button
          type="button"
          className="btn btn--primary"
          disabled={rotate.isPending || !keys.isSuccess}
          onClick={() => {
            clear();
            rotate.mutate(undefined, {
              onSuccess: (result) =>
                ok(
                  `Rotated the SP signing key. The new active key is ${result.fingerprint}; the previous key is now retiring and stays in metadata until you retire it.`,
                ),
              onError: (error) => report(error, 'rotate-key'),
            });
          }}
        >
          Rotate the active signing key
        </button>
        <code className="instance-cli">$ hikyo saml sp-key rotate</code>
      </div>
      <p className="field__hint">
        Rotation marks the current active key retiring and publishes a new active
        key. Both certificates remain in SP metadata for the overlap window, so
        signature validation never breaks. Retire the old key once every IdP has
        seen the new one.
      </p>
    </Panel>
  );
}

function SpKeyRow({
  spKey,
  onDone,
  onFailure,
  onBusy,
}: {
  spKey: SamlSpKey;
  onDone: (message: string) => void;
  onFailure: (error: unknown, action: SamlAction) => void;
  onBusy: () => void;
}) {
  const [mode, setMode] = useState<'idle' | 'retire' | 'compromise'>('idle');
  const retire = useRetireSamlSpKey();
  const compromise = useCompromiseRetireSamlSpKey();
  const active = spKey.state === 'active';

  return (
    <div className="settings-row settings-row--stacked" data-sp-key={spKey.fingerprint}>
      <div className="settings-row__copy">
        <span className="settings-row__title mono">{spKey.fingerprint}</span>
        <span className="settings-row__detail">
          {active
            ? 'Active: signs every AuthnRequest right now.'
            : 'Retiring: still published in metadata for the overlap window; erase it once every IdP trusts the new active key.'}
        </span>
        <span className="settings-row__detail mono">created {new Date(spKey.created_at).toLocaleString()}</span>
      </div>
      <span className="settings-row__spacer" />
      <span className={active ? 'settings-tag' : 'settings-tag settings-tag--danger'}>{spKey.state}</span>
      <div className="panel__actions">
        {active ? (
          <button
            type="button"
            className="btn btn--danger"
            onClick={() => {
              onBusy();
              setMode((current) => (current === 'compromise' ? 'idle' : 'compromise'));
            }}
          >
            Compromise-retire
          </button>
        ) : (
          <button
            type="button"
            className="btn btn--danger"
            onClick={() => {
              onBusy();
              setMode((current) => (current === 'retire' ? 'idle' : 'retire'));
            }}
          >
            Retire
          </button>
        )}
      </div>

      {mode === 'retire' ? (
        <div className="danger-zone">
          <p className="danger-zone__hint">
            Retiring erases this key and removes its certificate from SP metadata now.
            An IdP that has not yet seen the current active key will start rejecting
            signatures. This is the ordinary end of an overlap window and cannot be
            undone.
          </p>
          <TypedNameConfirm
            label="Type the fingerprint to erase this retiring key"
            expect={spKey.fingerprint}
            action="Retire key"
            hint="The full sha256: fingerprint, exactly."
            busy={retire.isPending}
            onConfirm={() =>
              retire.mutate(spKey.fingerprint, {
                onSuccess: () => {
                  setMode('idle');
                  onDone(`Retired ${spKey.fingerprint}; it is erased and gone from SP metadata.`);
                },
                onError: (error) => onFailure(error, 'retire-key'),
              })
            }
          />
        </div>
      ) : null}

      {mode === 'compromise' ? (
        <div className="danger-zone">
          <p className="danger-zone__hint">
            Compromise retirement erases this active key immediately and mints a
            replacement with NO overlap window. Any AuthnRequest already in flight
            signed with this key will fail. Use this only when the private key may be
            exposed; for a planned rotation, rotate instead. This cannot be undone.
          </p>
          <TypedNameConfirm
            label="Type the fingerprint to erase and replace the compromised key"
            expect={spKey.fingerprint}
            action="Compromise-retire key"
            hint="The full sha256: fingerprint, exactly."
            busy={compromise.isPending}
            onConfirm={() =>
              compromise.mutate(spKey.fingerprint, {
                onSuccess: (result) => {
                  setMode('idle');
                  onDone(
                    `Erased the compromised key and minted a replacement. The new active key is ${result.fingerprint}, published with no overlap window.`,
                  );
                },
                onError: (error) => onFailure(error, 'compromise-retire-key'),
              })
            }
          />
        </div>
      ) : null}
    </div>
  );
}
