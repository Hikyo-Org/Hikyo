import { useCallback, useEffect, useState, type ClipboardEvent, type FormEvent } from 'react';
import { Link } from 'react-router';

import { ApiError } from '../api/client.ts';
import {
  useCreateRemotePairingInvite,
  usePairRemote,
  usePrepareRemotePairing,
  useRemotePairingInvites,
  useRevokeRemotePairingInvite,
  type CreatedPairingInvite,
  type PreparedPairing,
} from '../api/remotePairing.ts';
import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { writeClipboard } from '../app/clipboard.ts';
import { surfaceById } from '../app/navigation.ts';
import { Alert } from '../ui/Alert.tsx';
import { Badge } from '../ui/Badge.tsx';
import { Button } from '../ui/Button.tsx';
import { ChoiceGroup } from '../ui/ChoiceGroup.tsx';
import { Input } from '../ui/Input.tsx';
import { Radio } from '../ui/Radio.tsx';
import { QrCode } from '../ui/auth/QrCode.tsx';
import { PanelForm } from './Sections.tsx';
import { PairingQrImport } from './PairingQrImport.tsx';
import { MAX_INVITATION_LENGTH, readPairingInvitation } from './pairingInvitation.ts';
import { useRemaskOnFocusLoss } from './useRemaskOnFocusLoss.ts';

/** Pairing secrets remain in component memory and never enter a query cache. */
export function RemotePairing() {
  return (
    <>
      <section className="card" aria-labelledby="pair-remote-title">
        <h2 id="pair-remote-title">Pair with a password or phrase</h2>
        <p>
          Generate a phrase or use your own password or phrase, then share it privately with the administrator of another instance.
          They create an invitation on their own instance and send it back to you.
        </p>
        <p>
          Pairing shares directory names only. Opening a workspace still needs your own sign-in
          and that instance's workspace consent.
        </p>
        <PanelForm opener="Pair a remote" variant="primary" readOnlySafe={false}>
          {(close) => <PairRemoteForm onClose={close} />}
        </PanelForm>
      </section>
      <PairingInvitations />
    </>
  );
}

function pairingFailure(error: Error | null): string {
  if (error instanceof ApiError && error.status === 403) {
    return 'Pairing was refused. Confirm your instance-config permission and that the invitation and local continuation are still valid. Ask for a new invitation if needed.';
  }
  if (error instanceof ApiError && error.status === 409) {
    return 'Pairing was refused. Check that the invitation is unused, unrevoked and unexpired, and belongs to this phrase. Start again if the local continuation has expired.';
  }
  return 'Pairing could not be completed. Check the invitation and try again. If it was already used, ask the administrator for a new invitation.';
}

/** Hide displayed secrets on focus loss while preserving the pending local keys.
 * Expiry, navigation, session retirement and closing retire the actual values. */
function usePairingDisclosure() {
  const [shown, setShown] = useState(false);
  const remask = useCallback(() => setShown(false), []);
  const generation = useRemaskOnFocusLoss(remask);
  useEffect(() => {
    if (!shown) return;
    const timer = window.setTimeout(remask, 45_000);
    return () => window.clearTimeout(timer);
  }, [shown, remask]);
  return { shown, generation, reveal: () => setShown(true), remask };
}

function pairingExpired(expiresAt: string) {
  return Date.parse(expiresAt) <= Date.now();
}

function usePairingExpiry(expiresAt: string | undefined, clear: () => void) {
  useEffect(() => {
    if (expiresAt === undefined) return;
    const remaining = Date.parse(expiresAt) - Date.now();
    // Invalid or unreasonably distant expiry cannot extend secret custody.
    if (!Number.isFinite(remaining) || remaining > 2_147_483_647) {
      clear();
      return;
    }
    const timer = window.setTimeout(clear, Math.max(0, remaining));
    return () => window.clearTimeout(timer);
  }, [expiresAt, clear]);
}

// A password input otherwise silently removes pasted CR/LF before onChange.
function rejectMultilinePaste(event: ClipboardEvent<HTMLInputElement>, refuse: () => void) {
  if (/[\r\n]/.test(event.clipboardData.getData('text/plain'))) {
    event.preventDefault();
    refuse();
  }
}

const SINGLE_LINE_NOTICE = 'This field accepts a single line. Use a password or phrase without line breaks.';

export function PairRemoteForm({ onClose }: { onClose?: () => void } = {}) {
  const prepare = usePrepareRemotePairing();
  const pair = usePairRemote();
  const resetPrepare = prepare.reset;
  const resetPair = pair.reset;
  const [prepared, setPrepared] = useSensitiveState<PreparedPairing | null>(null);
  const [bundle, setBundle] = useSensitiveState('');
  const [customSecret, setCustomSecret] = useSensitiveState('');
  const [secretMode, setSecretMode] = useState<'generated' | 'custom'>('generated');
  const [wordCount, setWordCount] = useState('6');
  const [name, setName] = useState('');
  const [notice, setNotice] = useState('');
  const disclosure = usePairingDisclosure();
  const clear = useCallback(() => {
    resetPrepare();
    resetPair();
    setPrepared(null);
    setBundle('');
    setCustomSecret('');
    setNotice('The local continuation expired. Generate a new phrase and ask for a new invitation.');
  }, [setPrepared, setBundle, setCustomSecret, resetPrepare, resetPair]);
  usePairingExpiry(prepared?.expires_at, clear);

  const invitation = readPairingInvitation(bundle.trim());
  const clearInvitation = useCallback(() => { setBundle(''); setNotice('This invitation expired. Ask the administrator for a new one.'); }, [setBundle]);
  usePairingExpiry(invitation?.expires_at, clearInvitation);
  const generatedWords = Number(wordCount);
  const invalidWords = !Number.isInteger(generatedWords) || generatedWords < 1 || generatedWords > 128;
  const generate = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (secretMode === 'custom' ? customSecret.length === 0 : invalidWords) return;
    setNotice('');
    pair.reset();
    const generation = disclosure.generation.current;
    const input = secretMode === 'custom' ? { phrase: customSecret }
      : generatedWords === 6 ? undefined : { word_count: generatedWords };
    prepare.mutate(input, { onSuccess: (value) => {
      setPrepared(value);
      setBundle('');
      if (generation === disclosure.generation.current) disclosure.reveal();
    } });
    setCustomSecret('');
  };
  const handleImport = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (prepared === null || name.trim() === '' || invitation === null) return;
    if (pairingExpired(invitation.expires_at)) { clearInvitation(); return; }
    if (pairingExpired(prepared.expires_at)) { clear(); return; }
    pair.mutate({ name: name.trim(), requester_state: prepared.requester_state, invite_bundle: bundle.trim() }, {
      onSuccess: () => {
        setPrepared(null);
        setBundle('');
        setNotice('Remote paired. Its directory will appear in Remotes.');
        onClose?.();
      },
    });
    setBundle('');
  };

  return (
    <div className="remote-pairing">
      {prepare.isError ? <Alert>{pairingFailure(prepare.error)}</Alert> : null}
      {notice === '' ? null : <p role="status">{notice}</p>}
      {prepared === null ? (
        <form className="form" aria-label="Prepare pairing" onSubmit={generate}>
          <p className="field__hint">Choose a secret to share privately with the remote administrator. Preparing it contacts only this instance.</p>
          <ChoiceGroup legend="Pairing secret">
            <Radio id="pairing-mode-generated" name="pairing-secret-mode" label="Generate a phrase" checked={secretMode === 'generated'} disabled={prepare.isPending} onChange={() => { setSecretMode('generated'); setCustomSecret(''); prepare.reset(); }} />
            <Radio id="pairing-mode-custom" name="pairing-secret-mode" label="Use my own password or phrase" checked={secretMode === 'custom'} disabled={prepare.isPending} onChange={() => { setSecretMode('custom'); setCustomSecret(''); prepare.reset(); }} />
          </ChoiceGroup>
          {secretMode === 'generated' ? (
            <Input id="pairing-word-count" label="Generated words" type="number" min="1" max="128" step="1" required value={wordCount} disabled={prepare.isPending} onChange={(event) => setWordCount(event.target.value)} hint="Choose 1 to 128 words. The default is 6." />
          ) : (
            <Input id="pairing-custom-secret" label="Your password or phrase" type="password" required value={customSecret} disabled={prepare.isPending} onChange={(event) => { setCustomSecret(event.target.value); setNotice(''); }} onPaste={(event) => rejectMultilinePaste(event, () => setNotice(SINGLE_LINE_NOTICE))} autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} hint="Case and spaces are kept exactly. Share the same text with the administrator." />
          )}
          <p className="field__hint">Longer secrets are harder to guess. With a short secret, keeping the invitation private matters more.</p>
          <div className="actions"><Button type="submit" variant="primary" disabled={prepare.isPending || (secretMode === 'custom' ? customSecret.length === 0 : invalidWords)}>
            {prepare.isPending ? 'Preparing…' : secretMode === 'custom' ? 'Prepare with my password or phrase' : 'Generate pairing phrase'}
          </Button></div>
        </form>
      ) : (
        <>
          <section className="remote-pairing__section" aria-labelledby="remote-pairing-share-title">
            <div className="remote-pairing__copy">
              <h3 id="remote-pairing-share-title">Share the phrase</h3>
              <p className="field__hint">Send only this phrase to the administrator, through a private channel.</p>
            </div>
            {disclosure.shown ? <p className="mono remote-pairing__phrase" aria-label="Pairing phrase">{prepared.phrase}</p> : <p role="status">Pairing phrase hidden. Your local continuation is still held.</p>}
            <div className="actions">
              <Button type="button" onClick={disclosure.shown ? disclosure.remask : disclosure.reveal}>
                {disclosure.shown ? 'Hide pairing phrase' : 'Reveal pairing phrase'}
              </Button>
              {disclosure.shown ? <Button type="button" onClick={async () => setNotice(await writeClipboard(prepared.phrase) === 'ok' ? 'Phrase copied. Clipboard managers may retain it; clear it after sharing.' : 'Clipboard access refused. Select and copy the phrase manually.')}>Copy phrase</Button> : null}
            </div>
            <p className="field__hint">Keep this panel open until you import the invitation. Closing it or signing out means starting again. Pairing expires {new Date(prepared.expires_at).toLocaleString()}.</p>
          </section>
          <section className="remote-pairing__section" aria-labelledby="remote-pairing-import-title">
            <div className="remote-pairing__copy">
              <h3 id="remote-pairing-import-title">Import the invitation</h3>
              <p className="field__hint">The administrator creates an invitation on their instance and sends it back to you.</p>
            </div>
            <form className="form" aria-label="Import pairing invitation" onSubmit={(event) => handleImport(event)}>
              <Input id="pair-remote-name" label="Remote name" value={name} onChange={(event) => setName(event.target.value)} required autoComplete="off" />
              <PairingQrImport onDecoded={setBundle} revision={bundle} disabled={pair.isPending} />
              <Input id="pair-remote-invitation" label="Invitation from the administrator" type="password" value={bundle} onChange={(event) => setBundle(event.target.value)} maxLength={MAX_INVITATION_LENGTH} required autoComplete="off" autoCapitalize="none" spellCheck={false} hint="Paste the complete invitation. Keep it separate from the phrase." />
              {invitation !== null ? <section className="remote-pairing__review" aria-label="Invitation destination review">
                <h3>Review the destination</h3>
                <p className="remote-pairing__destination">Connect to <strong className="mono">{invitation.origin}</strong></p>
                <p className="field__hint">Confirm this is the instance you intended to pair. The server verifies the invitation and its identity when you connect.</p>
                <p className="field__hint remote-pairing__destination">Identity fingerprint: <span className="mono">{invitation.spki_pin}</span></p>
                <p className="field__hint">Invitation expires {new Date(invitation.expires_at).toLocaleString()}.</p>
              </section> : bundle.trim() === '' ? null : <Alert>Enter a complete, valid Hikyo pairing invitation. A website address or an unrelated QR code cannot be used.</Alert>}
              {pair.isError ? <Alert>{pairingFailure(pair.error)}</Alert> : null}
              <div className="actions actions--stack">
                <Button type="submit" variant="primary" disabled={pair.isPending || name.trim() === '' || invitation === null}>{pair.isPending ? 'Connecting…' : 'Connect remote'}</Button>
                <Button type="button" variant="quiet" disabled={pair.isPending} onClick={() => { prepare.reset(); pair.reset(); setPrepared(null); setBundle(''); setNotice('Pairing discarded. Generate a new phrase to start again.'); }}>Discard pairing</Button>
              </div>
            </form>
          </section>
        </>
      )}
    </div>
  );
}

export function PairingInvitations() {
  const invitations = useRemotePairingInvites();
  const revoke = useRevokeRemotePairingInvite();
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const deadlines = (invitations.data?.items ?? [])
      .map((invite) => Date.parse(invite.expires_at)).filter((expiry) => expiry > now);
    if (deadlines.length === 0) return;
    const timer = window.setTimeout(() => setNow(Date.now()), Math.max(0, Math.min(...deadlines) - Date.now()));
    return () => window.clearTimeout(timer);
  }, [invitations.data, now]);
  return (
    <section className="card" aria-labelledby="pairing-invites-title">
      <h2 id="pairing-invites-title">Pairing invitations</h2>
      <p>Create an invitation here only after receiving a phrase privately from someone you approve. Invitations can be used once and expire automatically.</p>
      <PanelForm opener="Create pairing invitation" readOnlySafe={false}>
        {() => <CreatePairingInviteForm />}
      </PanelForm>
      {invitations.isPending ? <p role="status">Loading pairing invitations…</p> : null}
      {invitations.isError ? <Alert>Pairing invitations could not be read. You need instance-config permission to manage them.</Alert> : null}
      {revoke.isError ? <Alert>The invitation could not be revoked. Reload its status and try again.</Alert> : null}
      {invitations.data?.items.length === 0 ? <p>No pairing invitations.</p> : null}
      <ul className="remotes">
        {(invitations.data?.items ?? []).map((invite) => {
          const status = invite.revoked_at !== undefined ? 'Revoked' : invite.consumed_at !== undefined ? 'Used' : Date.parse(invite.expires_at) <= now ? 'Expired' : 'Available';
          return (
            <li className="remote" key={invite.id}>
              <div className="remote__head"><h3>{invite.label}</h3><Badge>{status}</Badge></div>
              <p className="mono remote__url">{invite.origin}</p>
              <p>Invitation expires {new Date(invite.expires_at).toLocaleString()}. Directory access ends {new Date(invite.credential_expires_at).toLocaleString()}.</p>
              {status === 'Available' ? <div className="actions"><Button type="button" variant="danger" disabled={revoke.isPending} onClick={() => revoke.mutate(invite.id)}>Revoke invitation</Button></div> : null}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

export function CreatePairingInviteForm() {
  const create = useCreateRemotePairingInvite();
  const configuration = useRemotePairingInvites();
  const resetCreate = create.reset;
  const [phrase, setPhrase] = useSensitiveState('');
  const [created, setCreated] = useSensitiveState<CreatedPairingInvite | null>(null);
  const [label, setLabel] = useState('');
  const [inviteMinutes, setInviteMinutes] = useState('10');
  const [credentialDays, setCredentialDays] = useState('30');
  const [notice, setNotice] = useState('');
  const disclosure = usePairingDisclosure();
  const clear = useCallback(() => {
    resetCreate();
    setCreated(null);
    setPhrase('');
    setNotice('This invitation expired. Create a new invitation if pairing is still needed.');
  }, [setCreated, setPhrase, resetCreate]);
  usePairingExpiry(created?.invite.expires_at, clear);
  const configurationPending = configuration.isPending || configuration.isFetching;
  const origin = configuration.isSuccess && !configurationPending &&
    configuration.data.configuration_error === undefined && configuration.data.origin !== ''
    ? configuration.data.origin : null;
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const minutes = Number(inviteMinutes);
    const days = Number(credentialDays);
    if (phrase.length === 0 || label.trim() === '' || origin === null || !Number.isInteger(minutes) || !Number.isInteger(days) || minutes <= 0 || minutes > 60 || days <= 0 || days > 365) return;
    const generation = disclosure.generation.current;
    create.mutate({ phrase, label: label.trim(), invite_lifetime_seconds: minutes * 60, credential_lifetime_seconds: days * 86_400 }, { onSuccess: (value) => {
      setCreated(value);
      if (generation === disclosure.generation.current) disclosure.reveal();
      setNotice('');
    } });
    setPhrase('');
  };
  if (created !== null) return (
    <div className="remote-pairing">
      <div className="remote-pairing__copy">
        <h3>Send the invitation</h3>
        <p className="field__hint">Send it back to the requester, separately from the phrase. It can be used once, before {new Date(created.invite.expires_at).toLocaleString()}.</p>
      </div>
      {disclosure.shown ? <>
        <Input label="Pairing invitation" value={created.invite_bundle} readOnly mono />
        {new TextEncoder().encode(created.invite_bundle).length <= 2_000
          ? <QrCode value={created.invite_bundle} title="Pairing invitation QR code" />
          : <p>This invitation is too long for a QR code. Copy the invitation instead.</p>}
      </> : <p role="status">Invitation hidden. Reveal it again while this panel remains open.</p>}
      <div className="actions">
        <Button type="button" onClick={disclosure.shown ? disclosure.remask : disclosure.reveal}>{disclosure.shown ? 'Hide invitation' : 'Reveal invitation'}</Button>
        {disclosure.shown ? <Button type="button" onClick={async () => setNotice(await writeClipboard(created.invite_bundle) === 'ok' ? 'Invitation copied. Send it separately from the phrase and clear your clipboard afterward.' : 'Clipboard access refused. Select and copy the invitation manually.')}>Copy invitation</Button> : null}
      </div>
      <p className="field__hint">Directory access ends {new Date(created.invite.credential_expires_at).toLocaleString()}. Opening a workspace still requires the requester's own sign-in.</p>
      {notice === '' ? null : <p role="status">{notice}</p>}
    </div>
  );
  return (
    <form className="form remote-pairing" aria-label="Create pairing invitation" onSubmit={submit}>
      {notice === '' ? null : <p role="status">{notice}</p>}
      {configurationPending ? <p role="status">Loading this instance's pairing address…</p> : configuration.isError ? (
        <Alert action={<Button type="button" variant="quiet" onClick={() => void configuration.refetch()}>Retry pairing configuration</Button>}>
          The pairing address could not be read. Your phrase is still here.
        </Alert>
      ) : origin === null ? (
        <Alert>
          Configure this instance's public HTTPS address before creating an invitation.{' '}
          <Link to={surfaceById('instance-config').path}>Open instance configuration</Link>
        </Alert>
      ) : <p className="remote-pairing__destination">Connects to <span className="mono">{origin}</span></p>}
      <section className="remote-pairing__section" aria-labelledby="remote-pairing-requester-title">
        <div className="remote-pairing__copy">
          <h3 id="remote-pairing-requester-title">Approve the requester</h3>
          <p className="field__hint">Only approve someone you recognise. They will be able to list this instance's organisations and projects.</p>
        </div>
        <Input id="invite-pairing-phrase" label="Phrase received privately" type="password" value={phrase} onChange={(event) => { setPhrase(event.target.value); setNotice(''); }} onPaste={(event) => rejectMultilinePaste(event, () => setNotice(SINGLE_LINE_NOTICE))} required autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} hint="Keep the password or phrase exactly as received, including case and spaces." />
        <Input id="invite-pairing-label" label="Invitation label" value={label} onChange={(event) => setLabel(event.target.value)} required hint="Who or which instance you are approving." />
      </section>
      <section className="remote-pairing__section" aria-labelledby="remote-pairing-expiry-title">
        <h3 id="remote-pairing-expiry-title">Expiry</h3>
        <Input id="invite-pairing-minutes" label="Invitation lifetime (minutes)" type="number" min="1" max="60" step="1" value={inviteMinutes} onChange={(event) => setInviteMinutes(event.target.value)} required />
        <Input id="invite-pairing-days" label="Directory access lifetime (days)" type="number" min="1" max="365" step="1" value={credentialDays} onChange={(event) => setCredentialDays(event.target.value)} required hint="The instance policy may shorten this. The invitation shows the approved expiry." />
      </section>
      {create.isError ? <Alert>{create.error instanceof ApiError && create.error.detail !== undefined ? create.error.detail : pairingFailure(create.error)}</Alert> : null}
      <div className="actions"><Button type="submit" variant="primary" disabled={create.isPending || phrase.length === 0 || label.trim() === '' || origin === null}>{create.isPending ? 'Creating…' : 'Approve and create invitation'}</Button></div>
    </form>
  );
}
