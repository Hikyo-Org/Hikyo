import { useEffect, useState } from 'react';
import { Button } from '../ui/Button.tsx';
import { onReplayConsentChange, replayAvailable, saveReplayConsent, savedReplayConsent, startReplay, stopReplay } from './replay.ts';

/** Inline in the toolbar, so it never covers the screen a visitor came to try. */
export function ReplayConsentControl() {
  const [consent, setConsent] = useState(savedReplayConsent);
  useEffect(() => onReplayConsentChange(() => setConsent(savedReplayConsent())), []);
  useEffect(() => {
    if (consent !== 'granted') {
      stopReplay();
      return;
    }
    // A blocked or failed chunk must not leave the toolbar claiming to record.
    startReplay().catch(() => setConsent((current) => (current === 'granted' ? null : current)));
  }, [consent]);
  if (!replayAvailable()) return null;
  const choose = (value: 'granted' | 'denied') => {
    saveReplayConsent(value);
    setConsent(value);
  };
  if (consent === 'granted') {
    return (
      <p className="demo-toolbar__replay">
        Recording this demo session.{' '}
        <Button type="button" onClick={() => choose('denied')}>Stop session recording</Button>
      </p>
    );
  }
  if (consent === 'denied') {
    return <p className="demo-toolbar__replay"><Button type="button" onClick={() => choose('granted')}>Allow session recording</Button></p>;
  }
  return (
    <p className="demo-toolbar__replay">
      Help improve Hikyo by sharing a recording of what you click and see in this demo. Values you
      type are hidden, but use sample values only. No cookies; recording goes to PostHog.{' '}
      <Button type="button" onClick={() => choose('granted')}>Allow session recording</Button>{' '}
      <Button type="button" onClick={() => choose('denied')}>No thanks</Button>
    </p>
  );
}
