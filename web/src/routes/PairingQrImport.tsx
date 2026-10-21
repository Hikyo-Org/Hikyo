import { useCallback, useEffect, useRef, useState, type ChangeEvent } from 'react';

import { Alert } from '../ui/Alert.tsx';
import { Button } from '../ui/Button.tsx';
import { Input } from '../ui/Input.tsx';
import { readPairingInvitation } from './pairingInvitation.ts';
import { decodeQrPixels, qrFrame, readQrImage } from './pairingQr.ts';

/** Pixels and decoded invitations stay in this surface only. No URLs, uploads,
 * automatic redemption, or browser-native BarcodeDetector dependency. */
export function PairingQrImport({ onDecoded, revision, disabled }: {
  onDecoded: (value: string) => void;
  revision: string;
  disabled: boolean;
}) {
  const video = useRef<HTMLVideoElement>(null);
  const operation = useRef(0);
  const stream = useRef<MediaStream | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const mounted = useRef(false);
  const [mode, setMode] = useState<'idle' | 'camera' | 'image'>('idle');
  const [notice, setNotice] = useState('');
  const stop = useCallback(() => {
    operation.current += 1;
    window.clearTimeout(timer.current);
    timer.current = undefined;
    stream.current?.getTracks().forEach((track) => track.stop());
    stream.current = null;
    if (video.current !== null) video.current.srcObject = null;
    if (mounted.current) setMode('idle');
  }, []);
  useEffect(() => {
    mounted.current = true;
    const hide = () => { if (document.hidden) { stop(); setNotice('Scanning stopped. Start the camera again, import an image, or paste the invitation.'); } };
    document.addEventListener('visibilitychange', hide);
    window.addEventListener('pagehide', stop);
    return () => { mounted.current = false; stop(); document.removeEventListener('visibilitychange', hide); window.removeEventListener('pagehide', stop); };
  }, [stop]);
  useEffect(() => { stop(); }, [revision, disabled, stop]);
  const accept = (value: string | null) => {
    if (value === null) throw new Error('No QR code found. Use a clear, uncropped PNG or JPEG image, or paste the invitation.');
    const invitation = readPairingInvitation(value);
    if (invitation === null) throw new Error('This QR code is not a valid Hikyo pairing invitation. Ask the administrator for the invitation QR code.');
    if (Date.parse(invitation.expires_at) <= Date.now()) throw new Error('This invitation expired. Ask the administrator for a new one.');
    stop();
    onDecoded(value);
    setNotice('Invitation read locally. Review the destination below, then connect.');
  };
  const startCamera = async () => {
    stop(); setNotice('');
    if (!window.isSecureContext || navigator.mediaDevices?.getUserMedia === undefined) {
      setNotice('Camera scanning needs HTTPS and camera support. Import a QR image or paste the invitation instead.'); return;
    }
    const token = operation.current;
    const current = () => mounted.current && token === operation.current && !document.hidden;
    setMode('camera');
    try {
      const acquired = await navigator.mediaDevices.getUserMedia({ audio: false, video: { facingMode: { ideal: 'environment' }, width: { ideal: 1_280 }, height: { ideal: 720 } } });
      if (!current()) { acquired.getTracks().forEach((track) => track.stop()); return; }
      stream.current = acquired;
      const element = video.current;
      if (element === null) { stop(); return; }
      element.srcObject = acquired;
      await element.play();
      if (!current()) return;
      const scan = async () => {
        if (!current()) return;
        try {
          if (element.readyState >= 2 && element.videoWidth > 0 && element.videoHeight > 0) {
            const pixels = qrFrame(element, element.videoWidth, element.videoHeight);
            const value = await decodeQrPixels(pixels.data, pixels.width, pixels.height);
            if (!current()) return;
            if (value !== null) { accept(value); return; }
          }
          timer.current = window.setTimeout(() => void scan(), 250);
        } catch (error) {
          if (!current()) return;
          stop(); setNotice(error instanceof Error ? error.message : 'Scanning failed. Import an image or paste the invitation.');
        }
      };
      void scan();
    } catch (error) {
      if (!current()) return;
      stop();
      setNotice(error instanceof DOMException && error.name === 'NotAllowedError'
        ? 'Camera permission was refused. Allow the camera in your browser, import a QR image, or paste the invitation.'
        : 'The camera could not start. Check that it is connected and available, import a QR image, or paste the invitation.');
    }
  };
  const importImage = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    stop(); setNotice('');
    if (file === undefined) return;
    const token = operation.current;
    const current = () => mounted.current && token === operation.current && !document.hidden;
    setMode('image');
    try {
      const value = await readQrImage(file, current);
      if (current()) accept(value);
    } catch (error) {
      if (current()) { stop(); setNotice(error instanceof Error ? error.message : 'The image could not be read. Paste the invitation instead.'); }
    }
  };
  return (
    <div className="remote-pairing__qr-import">
      <p className="field__hint">Scan the invitation QR code or read a saved image. Images stay in this browser.</p>
      <div className="actions">
        {mode === 'idle' ? <Button type="button" disabled={disabled} onClick={() => void startCamera()}>Scan QR with camera</Button>
          : <Button type="button" onClick={stop}>{mode === 'camera' ? 'Stop camera' : 'Cancel image import'}</Button>}
      </div>
      <Input id="pairing-qr-image" label="Import QR image" type="file" accept="image/png,image/jpeg" disabled={disabled} onChange={(event) => void importImage(event)} hint="PNG or JPEG, up to 8 MB and 16 million pixels." />
      <video ref={video} className="remote-pairing__camera" hidden={mode !== 'camera'} autoPlay muted playsInline aria-label="Invitation camera preview" />
      {mode === 'camera' ? <p role="status">Point the camera at the administrator's invitation QR code. Nothing connects until you confirm.</p> : null}
      {mode === 'image' ? <p role="status">Reading QR image locally…</p> : null}
      {notice === '' ? null : <Alert tone="info">{notice}</Alert>}
    </div>
  );
}
