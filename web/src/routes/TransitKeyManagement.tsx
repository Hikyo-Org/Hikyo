import { zTransitAlgorithm, zTransitCaller } from '@hikyo/zod';
import { useState, type FormEvent } from 'react';

import {
  transitRefusalText, useChangeTransitKeyState, useConfigureTransitKey, useCreateTransitKey,
  useRotateTransitKey, useTrimTransitKey, type EnvRef, type TransitKey,
} from '../api/transit.ts';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { Dialog } from '../ui/Dialog.tsx';
import { Input } from '../ui/Input.tsx';
import { Select } from '../ui/Select.tsx';
import { Textarea } from '../ui/Textarea.tsx';

export function transitWholeNumber(raw: string, minimum: number): number {
  const value = Number(raw);
  if (raw.trim() === '' || !Number.isSafeInteger(value) || value < minimum) {
    throw new Error(`Enter a whole number of at least ${minimum}.`);
  }
  return value;
}

export function parseTransitCallers(raw: string) {
  if (raw.trim() === '') return [];
  return zTransitCaller.array().max(64).parse(raw.split('\n').filter((line) => line.trim() !== '').map((line) => {
    const [principal_id, ...operations] = line.trim().split(/\s+/);
    return { principal_id, operations: operations.join('').split(',').filter(Boolean) };
  }));
}

/** Empty caller lists admit all environment crypto-use holders. A populated
 * list only narrows access, so adding the first restriction is never widening. */
export function transitCallerWidening(before: TransitKey['callers'], after: TransitKey['callers']): string[] {
  if (before.length === 0) return [];
  if (after.length === 0) return ['Every principal with crypto-use in this environment'];
  return after.flatMap((caller) => {
    const previous = before.find((entry) => entry.principal_id === caller.principal_id);
    const added = caller.operations.filter((operation) => !previous?.operations.includes(operation));
    return added.length === 0 ? [] : [`${caller.principal_id}: ${added.join(', ')}`];
  });
}

function CallerInput({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  return <Textarea label="Caller entries" value={value} onChange={(event) => onChange(event.target.value)}
    hint="One principal ID and comma-separated operations per line, separated by a space. Empty admits every principal with crypto-use on this environment. Entries only narrow that permission." rows={4} spellCheck={false} />;
}

function rotationSeconds(raw: string) {
  const value = transitWholeNumber(raw, 0);
  if (value !== 0 && value < 3600) throw new Error('Rotation must be zero (off) or at least 3600 seconds.');
  return value;
}

const algorithmOperations: Record<TransitKey['algorithm'], TransitKey['allowed_operations']> = {
  'xchacha20-poly1305': ['encrypt', 'decrypt', 'rewrap', 'datakey'],
  ed25519: ['sign', 'verify'],
  'hmac-sha256': ['hmac', 'hmac-verify'],
};

export function TransitCreateKey({ env, onClose }: { env: EnvRef; onClose: () => void }) {
  const create = useCreateTransitKey(env);
  const [name, setName] = useState('');
  const [algorithm, setAlgorithm] = useState<TransitKey['algorithm']>('xchacha20-poly1305');
  const [rotation, setRotation] = useState('0');
  const [callers, setCallers] = useState('');
  const [allowed, setAllowed] = useState<TransitKey['allowed_operations']>(algorithmOperations['xchacha20-poly1305']);
  const purposeOperations = algorithmOperations[algorithm];
  const [plaintext, setPlaintext] = useState(false);
  const [error, setError] = useState('');
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError('');
    try {
      const selected = zTransitAlgorithm.parse(algorithm);
      await create.mutateAsync({ name, algorithm: selected, custody: 'software', exportable: false,
        rotation_period_seconds: rotationSeconds(rotation), callers: parseTransitCallers(callers),
        allowed_operations: selected === 'xchacha20-poly1305' && plaintext ? [...allowed, 'datakey-plaintext'] : allowed,
      });
      onClose();
    } catch (cause) {
      setError(cause instanceof Error && (cause.message.startsWith('Enter ') || cause.message.startsWith('Rotation ')) ? cause.message : transitRefusalText(cause instanceof Error ? cause : null));
    }
  };
  return <Dialog title="Create transit key" size="wide" onCancel={(event) => { if (create.isPending) event.preventDefault(); else onClose(); }}>
    <form onSubmit={(event) => { void submit(event); }}>
      <p>Creates a software-custody key. Its private material remains with Hikyo. Requires crypto-manage on this project.</p>
      {error !== '' ? <Alert>{error}</Alert> : null}
      <Input label="Key name" required pattern="[a-z0-9][a-z0-9._-]{0,62}" maxLength={63} value={name} onChange={(event) => setName(event.target.value)} autoComplete="off" />
      <Select label="Algorithm" value={algorithm} onChange={(event) => { const next = zTransitAlgorithm.parse(event.target.value); setAlgorithm(next); setPlaintext(false); setAllowed(algorithmOperations[next]); }}>
        <option value="xchacha20-poly1305">XChaCha20-Poly1305 (encryption)</option><option value="ed25519">Ed25519 (signing)</option><option value="hmac-sha256">HMAC-SHA256 (message authentication)</option>
      </Select>
      <Input label="Rotation period (seconds)" type="number" min={0} step={1} value={rotation} onChange={(event) => setRotation(event.target.value)} hint="Zero disables automatic rotation; otherwise at least 3600 seconds." />
      <CallerInput value={callers} onChange={setCallers} />
      <fieldset><legend>Allowed operations</legend><p className="field__hint">The algorithm and allowed operations cannot be changed after creation.</p>{purposeOperations.map((operation) => <Checkbox key={operation} label={`Allow ${operation}`} checked={allowed.includes(operation)} onChange={(event) => setAllowed(event.target.checked ? [...allowed, operation] : allowed.filter((item) => item !== operation))} />)}</fieldset>
      {algorithm === 'xchacha20-poly1305' ? <Checkbox label="Allow plaintext data keys" checked={plaintext} onChange={(event) => setPlaintext(event.target.checked)} /> : null}
      <div className="dialog__actions"><Button type="button" disabled={create.isPending} onClick={onClose}>Cancel</Button><Button type="submit" variant="primary" disabled={create.isPending || (allowed.length === 0 && !plaintext)}>{create.isPending ? 'Creating…' : 'Create key'}</Button></div>
    </form>
  </Dialog>;
}

type ManagementAction = 'configure' | 'rotate' | 'disable' | 'enable' | 'retire' | 'compromise' | 'schedule-deletion' | 'cancel-deletion' | 'trim';
const actions: readonly { action: ManagementAction; label: string }[] = [
  { action: 'configure', label: 'Configure key' }, { action: 'rotate', label: 'Rotate key' },
  { action: 'disable', label: 'Disable' }, { action: 'enable', label: 'Enable' },
  { action: 'retire', label: 'Retire' }, { action: 'compromise', label: 'Compromise' },
  { action: 'schedule-deletion', label: 'Schedule deletion' }, { action: 'cancel-deletion', label: 'Cancel deletion' },
  { action: 'trim', label: 'Trim versions' },
];

export function TransitKeyManagement({ env, transitKey, canAct }: { env: EnvRef; transitKey: TransitKey; canAct: boolean }) {
  const configure = useConfigureTransitKey(env);
  const rotate = useRotateTransitKey(env);
  const state = useChangeTransitKeyState(env);
  const trim = useTrimTransitKey(env);
  const [action, setAction] = useState<ManagementAction | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [encrypt, setEncrypt] = useState(String(transitKey.min_encrypt_version));
  const [decrypt, setDecrypt] = useState(String(transitKey.min_decrypt_version));
  const [rotation, setRotation] = useState(String(transitKey.rotation_period_seconds));
  const [callers, setCallers] = useState(transitKey.callers.map((caller) => `${caller.principal_id} ${caller.operations.join(',')}`).join('\n'));
  const [delay, setDelay] = useState('604800');
  const [acknowledged, setAcknowledged] = useState(false);
  let proposedCallers: TransitKey['callers'] | null = null;
  try { proposedCallers = parseTransitCallers(callers); } catch { /* The submit path reports invalid caller syntax without raw input. */ }
  const widenedCallers = proposedCallers === null ? [] : transitCallerWidening(transitKey.callers, proposedCallers);
  const lowerVersionFloor = Number(decrypt) < transitKey.min_decrypt_version || Number(encrypt) < transitKey.min_encrypt_version;
  const requiresAccessApproval = action === 'configure' && (widenedCallers.length > 0 || lowerVersionFloor);
  const busy = configure.isPending || rotate.isPending || state.isPending || trim.isPending;
  const label = actions.find((entry) => entry.action === action)?.label ?? '';
  const close = () => { if (!busy) { setAction(null); setError(''); } };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError('');
    if (action === null || busy) return;
    try {
      if (action === 'configure') {
        if (requiresAccessApproval && !acknowledged) return;
        const minEncrypt = transitWholeNumber(encrypt, 1);
        const minDecrypt = transitWholeNumber(decrypt, 1);
        if (minDecrypt > minEncrypt || BigInt(minEncrypt) > transitKey.latest_version) throw new Error('Enter a version window with minimum decrypt ≤ minimum encrypt ≤ latest version.');
        await configure.mutateAsync({ key: transitKey.name, body: { min_encrypt_version: minEncrypt, min_decrypt_version: minDecrypt, rotation_period_seconds: rotationSeconds(rotation), callers: parseTransitCallers(callers) } });
      } else if (action === 'rotate') await rotate.mutateAsync(transitKey.name);
      else if (action === 'trim') { if (!acknowledged) return; await trim.mutateAsync(transitKey.name); }
      else {
        const seconds = action === 'schedule-deletion' ? transitWholeNumber(delay, 86400) : undefined;
        if (seconds !== undefined && seconds > 7776000) throw new Error('Enter a deletion delay of at most 7776000 seconds (90 days).');
        await state.mutateAsync({ key: transitKey.name, body: { action, ...(seconds === undefined ? {} : { delay_seconds: seconds }) } });
      }
      setNotice(`${label} completed.`);
      setAction(null);
    } catch (cause) {
      setError(cause instanceof Error && (cause.message.startsWith('Enter ') || cause.message.startsWith('Rotation ')) ? cause.message : transitRefusalText(cause instanceof Error ? cause : null));
    }
  };
  return <section aria-label="Key management">
    <h3>Key management</h3><p className="field__hint">Requires crypto-manage on this project. The server checks permissions and current key policy for every request.</p>
    {notice !== '' ? <p role="status">{notice}</p> : null}
    <div className="actions">{actions.map((entry) => <Button key={entry.action} type="button" disabled={!canAct || busy} onClick={() => { setAction(entry.action); setError(''); setNotice(''); setAcknowledged(false); }} variant={entry.action === 'trim' || entry.action === 'compromise' || entry.action === 'schedule-deletion' ? 'danger' : 'secondary'}>{entry.label}</Button>)}</div>
    {action === null ? null : <Dialog title={label} size={action === 'configure' ? 'wide' : 'narrow'} onCancel={(event) => { if (busy) event.preventDefault(); else close(); }}>
      <form onSubmit={(event) => { void submit(event); }}>
        <p>Key: <code>{transitKey.name}</code></p>
        {error !== '' ? <Alert>{error}</Alert> : null}
        {action === 'configure' ? <>
          <Input label="Minimum encrypt version" type="number" min={1} step={1} value={encrypt} onChange={(event) => { setEncrypt(event.target.value); setAcknowledged(false); }} required />
          <Input label="Minimum decrypt version" type="number" min={1} step={1} value={decrypt} onChange={(event) => { setDecrypt(event.target.value); setAcknowledged(false); }} required />
          <Input label="Rotation period (seconds)" type="number" min={0} step={1} value={rotation} onChange={(event) => setRotation(event.target.value)} required />
          <CallerInput value={callers} onChange={(value) => { setCallers(value); setAcknowledged(false); }} />
          {requiresAccessApproval ? <>
            <Alert tone="warn">This change expands access to {env.environment}. Review the affected callers and version floors.</Alert>
            {widenedCallers.length === 0 ? null : <ul aria-label="Expanded caller access">{widenedCallers.map((entry) => <li key={entry}>{entry}</li>)}</ul>}
            {lowerVersionFloor ? <p>Lower version floors admit older versions: minimum encrypt {encrypt}, minimum decrypt {decrypt}.</p> : null}
            <Checkbox label="I approve this expanded access" checked={acknowledged} onChange={(event) => setAcknowledged(event.target.checked)} />
          </> : null}
        </> : null}
        {action === 'schedule-deletion' ? <><p>All operations stop immediately. Destruction is irreversible once the delay expires.</p><Input label="Deletion delay (seconds)" type="number" min={86400} max={7776000} step={1} value={delay} onChange={(event) => setDelay(event.target.value)} required /></> : null}
        {action === 'cancel-deletion' ? <p>Cancellation returns the key to disabled. Enable it separately before use. Cancellation is refused after the destruction deadline.</p> : null}
        {action === 'compromise' ? <p>Marks every current version compromised. Producing operations require a new version. Decrypt and rewrap remain available for recovery.</p> : null}
        {action === 'trim' ? <><p>Permanently erases every version below minimum decrypt version {String(transitKey.min_decrypt_version)}. Existing backups may still contain those versions.</p><Checkbox label="I understand these versions cannot be recovered" checked={acknowledged} onChange={(event) => setAcknowledged(event.target.checked)} /></> : null}
        {action === 'rotate' ? <p>Appends a new version. Existing ciphertext retains its original version until rewrapped.</p> : null}
        {action === 'disable' ? <p>Disabling refuses every cryptographic operation immediately.</p> : null}
        {action === 'enable' ? <p>Enabling returns the key to active; version and compromise restrictions still apply.</p> : null}
        {action === 'retire' ? <p>Retiring refuses producing operations while preserving consuming operations.</p> : null}
        <div className="dialog__actions"><Button type="button" disabled={busy} onClick={close}>Cancel</Button><Button type="submit" variant={action === 'trim' || action === 'compromise' || action === 'schedule-deletion' ? 'danger' : 'primary'} disabled={busy || ((action === 'trim' || requiresAccessApproval) && !acknowledged)}>{busy ? 'Working…' : label}</Button></div>
      </form>
    </Dialog>}
  </section>;
}
