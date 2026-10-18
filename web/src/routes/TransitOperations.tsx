import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { flushSync } from 'react-dom';

import { useSensitiveState } from '../api/sensitiveMutation.ts';
import { runTransit, transitRefusalText, type EnvRef, type TransitInput, type TransitKey, type TransitOperation, type TransitResult } from '../api/transit.ts';
import { useTransport } from '../api/transport.tsx';
import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Checkbox } from '../ui/Checkbox.tsx';
import { Input } from '../ui/Input.tsx';
import { Select } from '../ui/Select.tsx';
import { Textarea } from '../ui/Textarea.tsx';
import { transitWholeNumber } from './TransitKeyManagement.tsx';
import { useRemaskOnFocusLoss } from './useRemaskOnFocusLoss.ts';

const operations: readonly { operation: TransitOperation; label: string }[] = [
  { operation: 'encrypt', label: 'Encrypt' }, { operation: 'decrypt', label: 'Decrypt' },
  { operation: 'rewrap', label: 'Rewrap' }, { operation: 'datakey', label: 'Generate data key' },
  { operation: 'sign', label: 'Sign' }, { operation: 'verify', label: 'Verify signature' },
  { operation: 'hmac', label: 'HMAC' }, { operation: 'hmac-verify', label: 'Verify HMAC' },
];
type SensitiveForm = { value: string; context: string; proof: string; result: TransitResult | null };
const emptyForm: SensitiveForm = { value: '', context: '', proof: '', result: null };

export function transitPlaintext(result: TransitResult | null): string | null {
  if (result?.operation === 'decrypt') return result.output.plaintext;
  if (result?.operation === 'datakey') return result.output.plaintext ?? null;
  return null;
}

/** This component is keyed by route, origin, session and public-key revision.
 * Inputs and output never enter a query, mutation cache, URL or browser storage.
 * Every focus loss revokes the request before clearing its component state. */
export function TransitOperations({ env, transitKey, canAct }: { env: EnvRef; transitKey: TransitKey; canAct: boolean }) {
  const transport = useTransport();
  const available = operations.filter((entry) => transitKey.allowed_operations.includes(entry.operation) || (entry.operation === 'datakey' && transitKey.allowed_operations.includes('datakey-plaintext')));
  const [operation, setOperation] = useState<TransitOperation>(available[0]?.operation ?? 'encrypt');
  const [form, setForm] = useSensitiveState<SensitiveForm>(emptyForm);
  const [version, setVersion] = useState('');
  const [bits, setBits] = useState('256');
  const [plaintext, setPlaintext] = useState(!transitKey.allowed_operations.includes('datakey') && transitKey.allowed_operations.includes('datakey-plaintext'));
  const [stored, setStored] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const epoch = useRef(0);
  const inFlight = useRef<AbortController | null>(null);
  const clear = useCallback(() => {
    epoch.current += 1;
    inFlight.current?.abort();
    inFlight.current = null;
    setForm(emptyForm);
    setPending(false);
    setStored(false);
  }, [setForm]);
  useRemaskOnFocusLoss(useCallback(() => {
    clear();
    setError('');
    setNotice('The window lost focus. Sensitive inputs and results were cleared. Run the operation again if needed.');
  }, [clear]));
  useEffect(() => {
    // BFCache may suspend pending React work. Wipe the committed controls
    // before the browser captures this document, even if it is frozen next.
    const pagehide = () => { flushSync(clear); };
    window.addEventListener('pagehide', pagehide);
    return () => {
      window.removeEventListener('pagehide', pagehide);
      epoch.current += 1;
      inFlight.current?.abort();
    };
  }, [clear]);
  const disclosed = transitPlaintext(form.result);
  const mustStore = disclosed !== null && !stored;
  const hasContext = ['encrypt', 'decrypt', 'rewrap', 'datakey'].includes(operation);
  const hasVersion = ['encrypt', 'sign', 'hmac'].includes(operation);
  const producing = ['encrypt', 'datakey', 'sign', 'hmac'].includes(operation);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (inFlight.current !== null || mustStore || !canAct) return;
    setError('');
    setNotice('');
    let keyVersion: number | undefined;
    try { keyVersion = version === '' ? undefined : transitWholeNumber(version, 1); }
    catch { setError('Enter a positive whole key version, or leave it empty for latest.'); return; }
    const context = form.context === '' ? {} : { context: form.context };
    const requestedVersion = keyVersion === undefined ? {} : { key_version: keyVersion };
    let input: TransitInput;
    switch (operation) {
      case 'encrypt': input = { operation, body: { plaintext: form.value, ...context, ...requestedVersion } }; break;
      case 'decrypt': case 'rewrap': input = { operation, body: { ciphertext: form.value, ...context } }; break;
      case 'datakey': input = { operation, body: { bits: bits === '128' ? 128 : bits === '512' ? 512 : 256, plaintext, ...context } }; break;
      case 'sign': case 'hmac': input = { operation, body: { message: form.value, ...requestedVersion } }; break;
      case 'verify': input = { operation, body: { message: form.value, signature: form.proof } }; break;
      case 'hmac-verify': input = { operation, body: { message: form.value, mac: form.proof } }; break;
    }
    const controller = new AbortController();
    const request = ++epoch.current;
    inFlight.current = controller;
    setForm(emptyForm);
    setStored(false);
    setPending(true);
    try {
      const result = await runTransit(env, transitKey.name, input, transport, controller.signal);
      if (controller.signal.aborted || request !== epoch.current) return;
      setForm({ ...emptyForm, result });
    } catch (cause) {
      if (controller.signal.aborted || request !== epoch.current) return;
      setError(transitRefusalText(cause instanceof Error ? cause : null));
    } finally {
      if (request === epoch.current) { inFlight.current = null; setPending(false); }
    }
  };
  return <section aria-label="Cryptographic operations">
    <h3>Cryptographic operations</h3>
    <p className="field__hint">Requires crypto-use on this environment and an allowed caller entry. Binary inputs and plaintext outputs use standard base64. Sensitive inputs clear when submitted. Plaintext is shown once; store it before clearing. All fields and results clear when this window loses focus.</p>
    {transitKey.state === 'disabled' || transitKey.state === 'pending-deletion' ? <Alert>This key is {transitKey.state}. Cryptographic operations are refused.</Alert> : null}
    {transitKey.compromised_through_version > 0n ? <Alert>Versions through {String(transitKey.compromised_through_version)} are compromised. Producing and verification operations refuse those versions. Rotate before producing new values; decrypt and rewrap remain available for recovery.</Alert> : null}
    {transitKey.state === 'retired' ? <Alert>This key is retired. Producing operations are refused; consuming operations remain available.</Alert> : null}
    {notice !== '' ? <p role="status">{notice}</p> : null}
    {error !== '' ? <Alert>{error}</Alert> : null}
    {available.length === 0 ? <p>No interactive operations are allowed for this key.</p> : <form method="post" onSubmit={(event) => { void submit(event); }} autoComplete="off">
      <Select label="Operation" value={operation} disabled={pending || mustStore} onChange={(event) => {
        const next = available.find((entry) => entry.operation === event.target.value);
        if (next === undefined) return;
        clear(); setError(''); setNotice(''); setVersion(''); setPlaintext(!transitKey.allowed_operations.includes('datakey') && transitKey.allowed_operations.includes('datakey-plaintext')); setOperation(next.operation);
      }}>{available.map((entry) => <option key={entry.operation} value={entry.operation}>{entry.label}</option>)}</Select>
      {operation !== 'datakey' ? <Textarea label={operation === 'encrypt' ? 'Plaintext (base64)' : operation === 'decrypt' || operation === 'rewrap' ? 'Ciphertext' : 'Message (base64)'} value={form.value} maxLength={operation === 'decrypt' || operation === 'rewrap' ? 131072 : 87384} required={operation === 'decrypt' || operation === 'rewrap'} disabled={pending || mustStore} onChange={(event) => setForm({ ...form, value: event.target.value })} spellCheck={false} /> : null}
      {hasContext ? <Textarea label="Context (base64)" hint="Optional associated data; decrypt and rewrap require the same context used for encryption." maxLength={1368} value={form.context} disabled={pending || mustStore} onChange={(event) => setForm({ ...form, context: event.target.value })} spellCheck={false} /> : null}
      {hasVersion ? <Input label="Key version" type="number" min={1} step={1} hint="Leave empty for the latest version." value={version} disabled={pending || mustStore} onChange={(event) => setVersion(event.target.value)} /> : null}
      {operation === 'verify' || operation === 'hmac-verify' ? <Textarea label={operation === 'verify' ? 'Signature' : 'MAC'} value={form.proof} required maxLength={131072} disabled={pending || mustStore} onChange={(event) => setForm({ ...form, proof: event.target.value })} spellCheck={false} /> : null}
      {operation === 'datakey' ? <><Select label="Data key size" value={bits} disabled={pending || mustStore} onChange={(event) => setBits(event.target.value)}><option value="128">128 bits</option><option value="256">256 bits</option><option value="512">512 bits</option></Select><Checkbox label="Return plaintext data key" checked={plaintext} disabled={pending || mustStore || !transitKey.allowed_operations.includes('datakey-plaintext') || !transitKey.allowed_operations.includes('datakey')} onChange={(event) => setPlaintext(event.target.checked)} /></> : null}
      {producing && transitKey.state === 'retired' ? <p className="field__hint">This producing operation will be refused while the key is retired.</p> : null}
      <div className="actions"><Button type="submit" variant="primary" disabled={!canAct || pending || mustStore}>{pending ? 'Running…' : 'Run operation'}</Button><Button type="button" disabled={mustStore} onClick={() => { clear(); setError(''); setNotice('Inputs and results cleared.'); }}>Clear inputs and result</Button></div>
    </form>}
    {form.result === null ? null : <section aria-label="Operation result">
      <h4>Operation result</h4><p>Key version {String(form.result.output.key_version)}</p>
      {'ciphertext' in form.result.output ? <Textarea label="Ciphertext output" value={form.result.output.ciphertext} readOnly mono spellCheck={false} /> : null}
      {'signature' in form.result.output ? <Textarea label="Signature output" value={form.result.output.signature} readOnly mono spellCheck={false} /> : null}
      {'mac' in form.result.output ? <Textarea label="MAC output" value={form.result.output.mac} readOnly mono spellCheck={false} /> : null}
      {'valid' in form.result.output ? <p role="status">{form.result.output.valid ? 'Verification succeeded.' : 'Verification failed.'}</p> : null}
      {disclosed === null ? null : <><Alert tone="warn">Plaintext is displayed once. Save it securely now. It will be cleared on focus loss or when this key changes.</Alert><Textarea label="Plaintext output (base64)" value={disclosed} readOnly mono spellCheck={false} /><Checkbox label="I have stored this plaintext" checked={stored} onChange={(event) => setStored(event.target.checked)} /></>}
      <Button type="button" disabled={mustStore} onClick={() => { clear(); setNotice('Result cleared.'); }}>Clear result</Button>
    </section>}
  </section>;
}
