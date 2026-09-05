import qrcode from 'qrcode-generator';
import { useEffect, useId, useMemo, useState } from 'react';

import { useSensitiveState } from '../api/sensitiveMutation.ts';
import {
  accountFailureText,
  useAuthMethods,
  useConfirmTotp,
  useEnrolPasskey,
  useEnrolTotpStart,
  useIdentities,
  useLinkIdentity,
  usePasskeys,
  useRegenerateRecoveryCodes,
  useRemovePasskey,
  useRemoveTotp,
  useTotpStatus,
  useUnlinkIdentity,
} from '../api/account.ts';
import { rememberOIDCReturn } from '../api/oidcChannel.ts';
import { useRevokeSession, useSessions, type ActiveSession } from '../api/remotes.ts';
import { useAuth } from '../app/AuthProvider.tsx';
import { themeLabel, useThemeChoice, type ThemeChoice } from '../app/theme.ts';
import { clearNotification, notifyFailure } from '../app/notifications.tsx';
import { Alert, DisplayOnceCopy, Done, JumpIndex, Panel } from './Sections.tsx';
import { useFeedback, useModalDialog } from './useModalDialog.ts';

const prototypeMode = import.meta.env.MODE === 'prototype';

/**
 * Account & security (registry surface `settings`, #60; locked prototype
 * app-chrome iteration 15, sections Profile · Sign-in factors · Recovery ·
 * Active sessions · Linked identities · Preferences).
 *
 * This surface absorbed what #71 shipped as a standalone session list: the
 * kill switch is one panel of the account, not a page of its own, which is
 * where a human looks for it.
 *
 * Every mutation on this page is an account-security mutation, and they share
 * one rule the prototype drew as a blue "confirm it's you" step-up: the proof
 * is the PRE-EXISTING credential — the password, or a confirmed code — never
 * the credential being added or removed. One dialog asks for it, so the rule
 * is stated once and cannot be half-applied.
 *
 * Deliberately absent, each for a stated reason:
 *
 *  - **Editing the profile.** There is no operation that writes a display name
 *    or an email anywhere in the contract; the fields are shown as what they
 *    are, read-only facts about the signed-in principal.
 *  - **Passkey-only sign-in.** The prototype's toggle has no backing state or
 *    operation; the passwordless floor is enforced server-side at removal
 *    time, and a switch that only pretended to set a policy is worse than
 *    none.
 *  - **Notification preferences.** No operation, and the ADR's own answer is
 *    that security alerts are not disableable — so there is nothing to offer.
 */
export function AccountSecurity() {
  const auth = useAuth();
  const passkeys = usePasskeys();
  const totpStatus = useTotpStatus();
  const identities = useIdentities();
  const methods = useAuthMethods();
  const sessions = useSessions();
  const revokeSession = useRevokeSession();

  const enrolPasskey = useEnrolPasskey();
  const removePasskey = useRemovePasskey();
  const totpStart = useEnrolTotpStart();
  const totpConfirm = useConfirmTotp();
  const totpRemove = useRemoveTotp();
  const regenerate = useRegenerateRecoveryCodes();
  const unlink = useUnlinkIdentity();
  const link = useLinkIdentity();

  const [proof, setProof] = useState<ProofRequest | null>(null);
  const { done, failure, report: recordFailure, ok: recordSuccess } = useFeedback(accountFailureText);
  const report = (error: unknown) => {
    notifyFailure(accountFailureText(error));
    recordFailure(error);
  };
  const ok = (message: string) => {
    clearNotification();
    recordSuccess(message);
  };
  const [otpauth, setOtpauth] = useSensitiveState<string | null>(null);
  const [totpCode, setTotpCode] = useSensitiveState('');
  const codeId = useId();

  // The locally-held seed is SPENT once the server no longer has a matching
  // mid-flight enrolment: a settled status that reports a confirmed factor
  // (done) or, with nothing pending, no factor at all (the window lapsed). The
  // `!isFetching` guard scopes the "no factor" case to a SETTLED read, so the
  // refetch a fresh start kicks off — which resolves to pending — does not count
  // the still-cached previous value as spent.
  const totpSeedSpent =
    totpStatus.isSuccess &&
    (totpStatus.data.confirmed || (!totpStatus.isFetching && !totpStatus.data.pending));

  // A real enrolment is in progress exactly while we hold an unspent seed. The
  // ceremony and the status line both read THIS rather than the query, so
  // neither flashes the lagging cached status during the post-start refetch.
  const totpEnrolmentInProgress = otpauth !== null && !totpSeedSpent;
  useEffect(() => {
    if (totpSeedSpent) {
      setOtpauth(null);
      setTotpCode('');
    }
  }, [totpSeedSpent]);

  const runProof = (value: string) => {
    const request = proof;
    if (request === null) {
      return;
    }
    setProof(null);
    switch (request.kind) {
      case 'add-passkey':
        enrolPasskey.mutate(
          { password: value },
          {
            onSuccess: () => ok('Passkey enrolled. Every other session you held has ended.'),
            onError: report,
          },
        );
        return;
      case 'remove-passkey':
        removePasskey.mutate(
          { id: request.id, password: value },
          { onSuccess: () => ok('Passkey removed.'), onError: report },
        );
        return;
      case 'totp-start':
        totpStart.mutate(
          { password: value },
          {
            onSuccess: (result) => {
              setOtpauth(result.otpauth_uri);
              ok('Enrolment started. The secret below is shown exactly once.');
            },
            onError: report,
          },
        );
        return;
      case 'totp-remove':
        totpRemove.mutate(
          { password: value },
          { onSuccess: () => ok('Authenticator factor removed.'), onError: report },
        );
        return;
      case 'recovery':
        regenerate.mutate(
          { proof: value },
          {
            onError: report,
          },
        );
        return;
      case 'unlink':
        unlink.mutate(
          { id: request.id, password: value },
          { onSuccess: () => ok('Identity unlinked.'), onError: report },
        );
        return;
      case 'link':
        link.mutate(
          { provider: request.provider, kind: request.providerKind, proof: value },
          {
            onSuccess: (redirect) => {
              if (request.providerKind === 'oidc') {
                const state = new URL(redirect).searchParams.get('state');
                if (state !== null) rememberOIDCReturn(state, window.location.href);
              }
              window.location.assign(redirect);
            },
            onError: report,
          },
        );
        return;
    }
  };

  const principal = auth.identity?.principal;
  const profileDisplayName = prototypeMode
    ? 'Alex'
    : principal?.display_name ?? '';
  const deliveryEmail = prototypeMode ? 'alex@example.com' : '';

  return (
    <div className="page page--chrome">
      <h1>Account &amp; security</h1>
      <p className="page__lede">
        Security changes ask for a possession factor first: the blue &quot;confirm it&apos;s you&quot;
        step-up, deliberately unlike the teal reveal ceremony.
      </p>

      <JumpIndex
        sections={[
          { id: 'account-profile', label: 'Profile' },
          { id: 'account-factors', label: 'Sign-in factors' },
          { id: 'account-recovery', label: 'Recovery' },
          { id: 'account-sessions', label: 'Sessions' },
          { id: 'account-identities', label: 'Identities' },
          { id: 'account-preferences', label: 'Preferences' },
        ]}
      />

      {done !== null ? <Done>{done}</Done> : null}
      {failure !== null ? <Alert>{failure}</Alert> : null}

      <Panel id="account-profile" title="Profile" tight>
        <div className="settings-grid">
          <div className="field field--readonly">
            <label htmlFor="account-display-name">
              Display name
              <span className="field__readonly-tag">
                <span aria-hidden="true">🔒 </span>read-only
              </span>
            </label>
            <input
              id="account-display-name"
              value={profileDisplayName}
              readOnly
              aria-readonly="true"
            />
          </div>
          <div className="field field--readonly">
            <label htmlFor="account-delivery-email">
              Email (delivery only)
              <span className="field__readonly-tag">
                <span aria-hidden="true">🔒 </span>read-only
              </span>
            </label>
            <input
              id="account-delivery-email"
              value={deliveryEmail}
              readOnly
              aria-readonly="true"
            />
          </div>
        </div>
        <p className="settings-note">
          Neither field is editable here: nothing in the API contract writes a display name or a
          delivery email, so both are shown as read-only facts about the signed-in principal. Email
          is where invitations and expiry warnings land; it is never an identity and never links
          accounts (#16).
        </p>
      </Panel>

      <Panel id="account-factors" title="Sign-in factors">
        {passkeys.isPending ? <p role="status">Loading passkeys…</p> : null}
        {passkeys.isError ? <Alert>Your passkeys could not be listed.</Alert> : null}
        {passkeys.isSuccess && passkeys.data.passkeys.length === 0 ? (
          <p role="status">No passkey is enrolled on this account.</p>
        ) : null}
        {passkeys.isSuccess ? passkeys.data.passkeys.map((passkey) => (
          <div className="settings-row account-passkey" key={passkey.id}>
            <div className="settings-row__copy">
              <span className="settings-row__title">🔑 {passkey.label}</span>
              <span className="settings-row__detail">
                passkey · added {new Date(passkey.created_at).toISOString().slice(0, 10)}
                {passkey.disabled ? ' · disabled after clone signal' : ''}
              </span>
            </div>
            <span className="settings-row__spacer" />
            <button
              type="button"
              className="capability__revoke"
              aria-label={`Remove passkey ${passkey.label}`}
              onClick={() => setProof({ kind: 'remove-passkey', id: passkey.id })}
            >
              ✕
            </button>
          </div>
        )) : null}

        <div className="settings-row settings-row--compact">
          <div className="settings-row__copy">
            <span className="settings-row__title">Authenticator app</span>
            <span className="settings-row__detail">TOTP · single-use per step</span>
          </div>
          <span className="settings-row__spacer" />
          {totpStatus.isSuccess && totpStatus.data.confirmed ? (
            <button
              type="button"
              className="settings-tag account-factor-status"
              aria-label="Remove the authenticator"
              disabled={totpRemove.isPending}
              title="Remove authenticator app"
              onClick={() => setProof({ kind: 'totp-remove' })}
            >
              enrolled
            </button>
          ) : (
            <button
              type="button"
              className="btn"
              disabled={totpStart.isPending || totpStatus.isPending}
              onClick={() => setProof({ kind: 'totp-start' })}
            >
              enrol
            </button>
          )}
        </div>
        {totpStatus.isError ? (
          <Alert>Your authenticator state could not be read. Reload to try again.</Alert>
        ) : null}

        <div className="settings-row settings-row--compact">
          <div className="settings-row__copy">
            <span className="settings-row__title">Password</span>
            <span className="settings-row__detail">signs you in, never authorises security changes</span>
          </div>
          <span className="settings-row__spacer" />
          <button type="button" className="btn" disabled title="Password changes are not in the API contract">
            change
          </button>
        </div>

        <div className="settings-row settings-row--compact">
          <div className="settings-row__copy">
            <span className="settings-row__title">Add passkey</span>
            <span className="settings-row__detail">
              a new credential never authorizes its own enrollment
            </span>
          </div>
          <span className="settings-row__spacer" />
          <button
            type="button"
            className="btn"
            aria-label="Add a passkey"
            disabled={enrolPasskey.isPending}
            onClick={() => setProof({ kind: 'add-passkey' })}
          >
            + add
          </button>
        </div>

        {totpEnrolmentInProgress ? (
          <p role="status">
            An enrolment is staged — add it to your authenticator and confirm it with the code below.
          </p>
        ) : null}
        {otpauth !== null && totpEnrolmentInProgress ? (
          <div className="enrolment">
            <QrCode value={otpauth} title="Authenticator setup QR code" />
            <p className="field__hint">
              Scan this with your authenticator, or enter the secret below by hand. Shown exactly
              once: the seed is never retrievable again. Confirming with a code completes the
              enrolment and reissues this session carrying only the password class, so you present
              the new factor separately.
            </p>
            <p className="enrolment__uri mono">{otpauth}</p>
            <DisplayOnceCopy
              value={otpauth}
              success="Authenticator setup copied. Store it somewhere safe; clipboard history may retain it."
            />
            <div className="field">
              <label htmlFor={codeId}>Code from the authenticator</label>
              <input
                id={codeId}
                inputMode="numeric"
                autoComplete="one-time-code"
                value={totpCode}
                onChange={(event) => setTotpCode(event.target.value)}
              />
            </div>
            <div className="panel__actions">
              <button
                type="button"
                className="btn btn--primary"
                disabled={totpConfirm.isPending || totpCode.length < 6}
                onClick={() =>
                  totpConfirm.mutate(
                    { code: totpCode },
                    {
                      onSuccess: () => {
                        setOtpauth(null);
                        setTotpCode('');
                        ok('Authenticator enrolled. Present it to step up when you need to.');
                      },
                      onError: report,
                      onSettled: () => setTotpCode(''),
                    },
                  )
                }
              >
                Confirm enrolment
              </button>
            </div>
          </div>
        ) : null}
      </Panel>

      <Panel id="account-recovery" title="Recovery">
        {prototypeMode ? <>
          <div className="settings-row">
            <div className="settings-row__copy"><span className="settings-row__title">Recovery codes</span><span className="settings-row__detail">not generated yet</span></div>
            <span className="settings-row__spacer" />
            <button type="button" className="btn" disabled={regenerate.isPending} onClick={() => setProof({ kind: 'recovery' })}>generate</button>
          </div>
          <div className="settings-row">
            <div className="settings-row__copy"><span className="settings-row__title">Passkey-only sign-in</span><span className="settings-row__detail">requires recovery codes + at least 2 passkeys</span></div>
            <span className="settings-row__spacer" />
            <button type="button" className="btn" disabled>enable</button>
          </div>
          <p className="settings-note">Locked until preconditions are met: 2/2 passkeys · recovery codes ✗. Codes restore access; they never satisfy a disclosure reauth.</p>
        </> : <>
          <p>
            Recovery codes restore <strong>access</strong> when every factor is gone. They never
            satisfy a disclosure reauthentication, and they never authorise their own regeneration —
            the proof is a code from your authenticator where one stands, otherwise your password.
          </p>
          <div className="panel__actions">
            <button type="button" className="btn" disabled={regenerate.isPending} onClick={() => setProof({ kind: 'recovery' })}>Replace recovery codes</button>
          </div>
          <p className="field__hint">Replacing them invalidates the previous batch atomically, and the new codes are displayed once.</p>
        </>}
      </Panel>

      <Panel id="account-sessions" title="Active sessions">
        {prototypeMode ? (
          <PrototypeSessions
            sessions={sessions.data?.items ?? []}
            busy={revokeSession.isPending}
            onRevoke={(session) =>
              revokeSession.mutate(session.id, {
                onSuccess: () => ok(`Revoked the ${session.artifact} session ${session.id}.`),
                onError: report,
              })
            }
          />
        ) : <>
        <p>
          Every artifact currently holding your account. A <span className="mono">workspace</span>{' '}
          session belongs to another instance&apos;s shell operating this one as you — revoking it
          ends that immediately, mid-flight, because the row is re-resolved on the next request.
        </p>
        {sessions.isError ? (
          <Alert>Your sessions could not be loaded. Reload to try again.</Alert>
        ) : null}
        {sessions.isSuccess && sessions.data.items.length === 0 ? (
          <p role="status">No active sessions.</p>
        ) : null}
        <ul className="sessions">
          {sessions.isSuccess ? sessions.data.items.map((item) => (
            <li key={item.id} className="session">
              <div className="session__head">
                {/* The artifact type is text in a badge, never a colour: it is
                    the single most load-bearing fact in the row. */}
                <span className="badge" data-artifact={item.artifact}>
                  {item.artifact}
                </span>
                <span className="mono session__id">{item.id}</span>
              </div>
              <p className="session__detail">{sessionDetail(item)}</p>
              <button
                className="btn"
                type="button"
                aria-label={`Revoke the ${item.artifact} session ${item.id}`}
                onClick={() =>
                  revokeSession.mutate(item.id, {
                    onSuccess: () => ok(`Revoked the ${item.artifact} session ${item.id}.`),
                    onError: report,
                  })
                }
                disabled={revokeSession.isPending}
              >
                Revoke
              </button>
            </li>
          )) : null}
        </ul>
        </>}
      </Panel>

      <Panel id="account-identities" title="Linked identities">
        {prototypeMode ? <>
          {identities.data?.identities.map((identity) => (
            <div className="settings-row" key={identity.id}>
              <div className="settings-row__copy"><span className="settings-row__title">git.example.com</span><span className="settings-row__detail">(issuer, subject) = (git.example.com, {identity.subject}) · linked 2026-06-02</span></div>
              <span className="settings-row__spacer" />
              <button type="button" className="capability__revoke" aria-label={`Unlink ${identity.issuer}`} onClick={() => setProof({ kind: 'unlink', id: identity.id })}>✕</button>
            </div>
          ))}
          <div className="settings-row">
            <div className="settings-row__copy"><span className="settings-row__title">Link another identity</span><span className="settings-row__detail">explicit binding: an unknown identity at sign-in is never a login, email never links</span></div>
            <span className="settings-row__spacer" />
            <button
              type="button"
              className="btn"
              disabled={link.isPending || methods.data?.providers[0] === undefined}
              onClick={() => {
                const provider = methods.data?.providers[0];
                if (provider !== undefined && (provider.kind === 'oidc' || provider.kind === 'saml')) setProof({ kind: 'link', provider: provider.slug, providerKind: provider.kind });
              }}
            >link…</button>
          </div>
        </> : <>
        <p>
          An external identity is an explicit binding of (issuer, subject) to this account. An
          unknown identity at sign-in is never a login, and an email address never links anything.
        </p>
        {identities.isError ? <Alert>Your linked identities could not be listed.</Alert> : null}
        {identities.isSuccess && identities.data.identities.length === 0 ? (
          <p role="status">No external identity is linked to this account.</p>
        ) : null}
        <ul className="factors">
          {identities.isSuccess ? identities.data.identities.map((identity) => (
            <li className="factor" key={identity.id}>
              <div>
                <strong className="mono">{identity.issuer}</strong>
                <span className="factor__meta mono">
                  subject {identity.subject} · {identity.kind} · linked{' '}
                  {new Date(identity.created_at).toLocaleDateString()}
                </span>
              </div>
              <button
                type="button"
                className="btn"
                aria-label={`Unlink ${identity.issuer}`}
                onClick={() => setProof({ kind: 'unlink', id: identity.id })}
              >
                Unlink
              </button>
            </li>
          )) : null}
        </ul>
        {methods.isPending ? <p role="status">Loading configured identity providers…</p> : null}
        {methods.isError ? <Alert>Configured identity providers could not be loaded.</Alert> : null}
        {methods.isSuccess && methods.data.providers.length === 0 ? (
          <p role="status">
            Linking starts at a configured identity provider. This instance has none enabled, so
            there is nothing to link to.
          </p>
        ) : null}
        {methods.isSuccess && methods.data.providers.length > 0 ? (
          <>
            <div className="panel__actions">
              {methods.data.providers.map((provider) => (
                <button
                  type="button"
                  className="btn"
                  key={provider.slug}
                  disabled={link.isPending || (provider.kind !== 'oidc' && provider.kind !== 'saml')}
                  onClick={() => {
                    if (provider.kind !== 'oidc' && provider.kind !== 'saml') return;
                    setProof({
                      kind: 'link',
                      provider: provider.slug,
                      providerKind: provider.kind,
                    });
                  }}
                >
                  Link {provider.display_name}
                </button>
              ))}
            </div>
            <p className="field__hint">
              Linking verifies your existing credential here, then redirects with purpose = link.
              A normal sign-in uses purpose = login and cannot create this binding.
            </p>
          </>
        ) : null}
        </>}
      </Panel>

      <Panel id="account-preferences" title="Preferences">
        <ThemePreference />
        {prototypeMode ? <>
          <div className="settings-row">
            <div className="settings-row__copy">
              <span className="settings-row__title">Credential-expiry warnings</span>
              <span className="settings-row__detail">in-product, always on (#17); email is an added transport, not the mechanism</span>
            </div>
            <span className="settings-row__spacer" />
            <span className="mono">in-app</span>
            <label className="chk"><input type="checkbox" defaultChecked /> also email</label>
          </div>
          <div className="settings-row">
            <div className="settings-row__copy">
              <span className="settings-row__title">Security alerts</span>
              <span className="settings-row__detail">new session, factor change, identity link: always notified, not disableable</span>
            </div>
            <span className="settings-row__spacer" />
            <span className="mono">always</span>
          </div>
        </> : (
          <p className="field__hint">
            The theme is this browser&apos;s choice and is stored here, not on the account: nothing in
            the contract carries a preference, and a setting that silently failed to follow you would
            be worse than one that never claimed to.
          </p>
        )}
      </Panel>

      {proof === null ? null : (
        <ProofDialog
          request={proof}
          onCancel={() => setProof(null)}
          onSubmit={runProof}
        />
      )}

      {regenerate.codes === null ? null : <RecoveryCodes codes={regenerate.codes} onClose={() => {
        regenerate.dismiss();
        ok('A new batch replaced the old one. The codes are shown exactly once.');
      }} />}
    </div>
  );
}

type ProofRequest =
  | { kind: 'add-passkey' }
  | { kind: 'remove-passkey'; id: string }
  | { kind: 'totp-start' }
  | { kind: 'totp-remove' }
  | { kind: 'recovery' }
  | { kind: 'link'; provider: string; providerKind: 'oidc' | 'saml' }
  | { kind: 'unlink'; id: string };

const PROOF_COPY: Record<ProofRequest['kind'], { title: string; hint: string; label: string }> = {
  'add-passkey': {
    title: 'Confirm it is you',
    hint: 'Enrolling a passkey is an account-security change, so the credential you already have authorises it — the new one never authorises itself.',
    label: 'Password',
  },
  'remove-passkey': {
    title: 'Confirm it is you',
    hint: 'Removing a credential is proved by your password, never by the credential being removed.',
    label: 'Password',
  },
  'totp-start': {
    title: 'Confirm it is you',
    hint: 'Starting an authenticator enrolment stages an inert seed. Nothing changes until you confirm it with a code.',
    label: 'Password',
  },
  'totp-remove': {
    title: 'Confirm it is you',
    hint: 'The factor being removed is excluded from the proof, so a stolen phone alone cannot drop the very factor it is.',
    label: 'Password',
  },
  recovery: {
    title: 'Confirm it is you',
    hint: 'A code from your authenticator where one stands, otherwise your password. Recovery codes never authorise their own regeneration.',
    label: 'Code or password',
  },
  link: {
    title: 'Confirm it is you',
    hint: 'Linking starts from this authenticated account. Your existing password authorises the provider round trip; an ordinary sign-in cannot link identities.',
    label: 'Password',
  },
  unlink: {
    title: 'Confirm it is you',
    hint: 'Unlinking an identity is an account-security change, proved by your password. Removing the last way in is refused.',
    label: 'Password',
  },
};

function ProofDialog({
  request,
  onCancel,
  onSubmit,
}: {
  request: ProofRequest;
  onCancel: () => void;
  onSubmit: (value: string) => void;
}) {
  const dialog = useModalDialog();
  const inputId = useId();
  const [value, setValue] = useSensitiveState('');
  const copy = PROOF_COPY[request.kind];

  return (
    <dialog
      className="ceremony"
      ref={dialog}
      aria-labelledby="proof-title"
      onCancel={(event) => {
        event.preventDefault();
        onCancel();
      }}
    >
      <h2 id="proof-title">{copy.title}</h2>
      <p className="ceremony__lede">{copy.hint}</p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          onSubmit(value);
        }}
      >
        <div className="field">
          <label htmlFor={inputId}>{copy.label}</label>
          <input
            id={inputId}
            type="password"
            autoComplete="current-password"
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
        </div>
        <div className="ceremony__actions">
          <button type="button" className="btn" onClick={onCancel}>
            Cancel
          </button>
          <button type="submit" className="btn btn--primary" disabled={value === ''}>
            Confirm
          </button>
        </div>
      </form>
    </dialog>
  );
}

/**
 * RecoveryCodes is the display-once presentation: the codes exist in exactly
 * this response and nowhere else, so the dialog will not close until the human
 * says they have stored them.
 */
function RecoveryCodes({ codes, onClose }: { codes: readonly string[]; onClose: () => void }) {
  const dialog = useModalDialog();
  const ackId = useId();
  const [stored, setStored] = useState(false);
  const [cancelAttempted, setCancelAttempted] = useState(false);

  return (
    <dialog
      className="ceremony"
      ref={dialog}
      aria-labelledby="codes-title"
      onCancel={(event) => {
        event.preventDefault();
        setCancelAttempted(true);
      }}
    >
      <h2 id="codes-title">Your new recovery codes</h2>
      <p className="ceremony__lede">
        Shown once. They are stored as hashes, so nobody — including this instance — can show them
        to you again. The previous batch is already invalid.
      </p>
      <ul className="codes" aria-label="Recovery codes">
        {codes.map((code) => (
          <li className="mono" key={code}>
            {code}
          </li>
        ))}
      </ul>
      <DisplayOnceCopy
        value={codes.join('\n')}
        success="Recovery codes copied. Store them somewhere safe; clipboard history may retain them."
      />
      {cancelAttempted ? (
        <Alert>
          These codes cannot be dismissed yet. Store them, then acknowledge that below; the
          previous batch is already invalid.
        </Alert>
      ) : null}
      <div className="field chk">
        <input
          id={ackId}
          type="checkbox"
          checked={stored}
          onChange={(event) => setStored(event.target.checked)}
        />
        <label htmlFor={ackId}>I have stored these somewhere safe.</label>
      </div>
      <div className="ceremony__actions">
        <button type="button" className="btn btn--primary" disabled={!stored} onClick={onClose}>
          Done
        </button>
      </div>
    </dialog>
  );
}

/**
 * QrCode renders `value` as a scannable QR built as inline SVG. It is inline
 * and not an `<img src="data:…">` because the CSP's `img-src 'self'` forbids
 * data-URL images. The modules are one `<path>`, painted black on white
 * regardless of theme — a scanner needs the contrast, and `forced-color-adjust`
 * keeps the OS from repainting it into an unscannable pair.
 */
function QrCode({ value, title }: { value: string; title: string }) {
  const { path, count } = useMemo(() => {
    const qr = qrcode(0, 'M');
    qr.addData(value);
    qr.make();
    const modules = qr.getModuleCount();
    let d = '';
    for (let row = 0; row < modules; row += 1) {
      for (let col = 0; col < modules; col += 1) {
        if (qr.isDark(row, col)) {
          d += `M${String(col)} ${String(row)}h1v1h-1z`;
        }
      }
    }
    return { path: d, count: modules };
  }, [value]);

  const quiet = 4; // the spec's four-module quiet zone
  const box = count + quiet * 2;
  return (
    <svg
      className="totp-qr"
      viewBox={`0 0 ${String(box)} ${String(box)}`}
      width="176"
      height="176"
      role="img"
      aria-label={title}
      shapeRendering="crispEdges"
    >
      <rect width={box} height={box} fill="#ffffff" />
      <path d={path} transform={`translate(${String(quiet)} ${String(quiet)})`} fill="#000000" />
    </svg>
  );
}

function PrototypeSessions({
  sessions,
  busy,
  onRevoke,
}: {
  readonly sessions: readonly ActiveSession[];
  readonly busy: boolean;
  readonly onRevoke: (session: ActiveSession) => void;
}) {
  const presentations = [
    { title: 'Safari · macOS', detail: '193.28.x.x · Amsterdam · last active now', badge: 'browser' },
    { title: 'Firefox · Fedora', detail: '193.28.x.x · Amsterdam · last active 2 days ago', badge: 'browser' },
    { title: 'hikyo CLI', detail: 'laptop.example · device authorization · last active 25 min ago', badge: 'CLI artifact' },
    { title: 'hikyo CLI', detail: 'example-cluster-0 · device authorization · last active 6 days ago', badge: 'CLI artifact' },
  ];
  if (sessions.length === 0) return <p role="status">Loading active sessions…</p>;
  return <>
    {sessions.map((session, index) => {
      const presentation = presentations[index] ?? {
        title: session.user_agent ?? session.artifact,
        detail: session.source_ip ?? 'active session',
        badge: session.artifact,
      };
      return <div className="settings-row" key={session.id}>
        <div className="settings-row__copy">
          <span className="settings-row__title">{presentation.title}{index === 0 ? ' this session' : ''}</span>
          <span className="settings-row__detail">{presentation.detail}</span>
        </div>
        <span className="settings-row__spacer" />
        <span className="settings-tag">{presentation.badge}</span>
        {index === 0 ? null : (
          <button type="button" className="capability__revoke" aria-label={`Revoke ${presentation.title}`} disabled={busy} onClick={() => onRevoke(session)}>✕</button>
        )}
      </div>;
    })}
    <p className="settings-note">Browser sessions and CLI sessions are distinct artifact types with their own lifetimes; revoking one never touches the other kind.</p>
  </>;
}

function ThemePreference() {
  const id = useId();
  const [choice, setChoice] = useThemeChoice();

  return (
    <div className="field">
      <label htmlFor={id}>{prototypeMode ? 'theme' : 'Theme'}</label>
      <select id={id} value={choice} onChange={(event) => setChoice(themeOf(event.target.value))}>
        <option value="system">{prototypeMode ? 'auto' : themeLabel('system')}</option>
        <option value="dark">{prototypeMode ? 'dark' : themeLabel('dark')}</option>
        <option value="light">{prototypeMode ? 'light' : themeLabel('light')}</option>
      </select>
    </div>
  );
}

function themeOf(value: string): ThemeChoice {
  if (value === 'dark' || value === 'light' || value === 'system') {
    return value;
  }
  throw new Error(`unknown theme choice ${value}`);
}

/**
 * sessionDetail is the row's sentence. The requesting origin is stated first
 * for a workspace session because it is the thing being judged.
 */
function sessionDetail(session: ActiveSession): string {
  const seen = `last seen ${new Date(session.last_seen_at).toLocaleString()}`;
  if (session.requesting_origin !== undefined) {
    return `Issued to ${session.requesting_origin} — ${seen}.`;
  }
  const where = session.source_ip === undefined ? '' : ` from ${session.source_ip}`;
  return `Signed in with ${session.auth_method}${where} — ${seen}.`;
}
