// Demo-only session replay. Imported only behind `MODE === 'demo'` checks, so the
// normal application build contains neither this module nor the PostHog SDK.

// The landing page's analytics choice promises no session recording, so replay
// needs its own consent under its own key and must never read that choice.
const consentKey = 'hikyo.demo.replay.consent';

export type ReplayConsent = 'granted' | 'denied' | null;

/** The build adds this element only when it was given a PostHog host and token. */
export function replayAvailable(): boolean {
  return replayConfig() !== null;
}

function replayConfig(): { host: string; token: string } | null {
  const element = document.querySelector<HTMLMetaElement>('meta[data-posthog-host][data-posthog-key]');
  const host = element?.dataset['posthogHost'];
  const token = element?.dataset['posthogKey'];
  return host && token ? { host, token } : null;
}

/** The only external origins the demo's fetch adapter forwards, matching connect-src. */
export function replayOrigins(): string[] {
  const element = document.querySelector<HTMLMetaElement>('meta[data-posthog-host][data-posthog-key]');
  return element?.dataset['posthogConnect']?.split(' ') ?? [];
}

export function savedReplayConsent(): ReplayConsent {
  try {
    const saved = localStorage.getItem(consentKey);
    return saved === 'granted' || saved === 'denied' ? saved : null;
  } catch {
    return null;
  }
}

export function saveReplayConsent(value: 'granted' | 'denied'): void {
  try {
    localStorage.setItem(consentKey, value);
  } catch {
    // The current-page choice still applies when storage is unavailable.
  }
}

/** A choice made in another tab applies here too, including a withdrawal. */
export function onReplayConsentChange(listener: () => void): () => void {
  const onStorage = (event: StorageEvent) => {
    // A null key means another tab cleared all storage, including this choice.
    if (event.key === consentKey || event.key === null) listener();
  };
  window.addEventListener('storage', onStorage);
  return () => window.removeEventListener('storage', onStorage);
}

// Input masking hides the field, but publishing renders the same value again as
// cell text and in the cell's aria-label. Keeping each keystroke's prefix also
// covers corrected typos, and word boundaries stop "deve" hiding every "development".
const typedPatterns = new Map<string, RegExp>();

export function typedValuePattern(value: string): RegExp {
  return new RegExp(`(?<!\\w)${value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?!\\w)`);
}

export function containsTypedValue(text: string, patterns: Iterable<RegExp> = typedPatterns.values()): boolean {
  for (const pattern of patterns) {
    if (pattern.test(text)) return true;
  }
  return false;
}

/** Runs before consent too, because a value typed earlier can still be on screen. */
export function watchTypedValues(): void {
  document.addEventListener('input', (event) => {
    const field = event.target;
    const typed = field instanceof HTMLTextAreaElement || (field instanceof HTMLInputElement && field.type !== 'checkbox' && field.type !== 'radio');
    if (!typed) return;
    const value = field.value.trim();
    if (value !== '' && !typedPatterns.has(value)) typedPatterns.set(value, typedValuePattern(value));
  }, true);
}

// Attributes the app owns. Masking these would only break the replay's styling,
// because a short typed value matches a stylesheet nearly every time.
const unmaskedAttributes = new Set(['_cssText', 'class', 'style']);

// Everything else is dropped by omission, as on the landing page, because the SDK
// keeps adding properties such as $referrer that the consent text does not cover.
// $session_id and $window_id link a pageview to its recording.
const keptEventProperties = ['token', 'distinct_id', '$process_person_profile', '$session_id', '$window_id', '$current_url', '$host', '$pathname'];

/**
 * Campaign and click ids must not reach a recording, and the recorder expands every
 * in-page link to a full URL holding the page's query. So remove them from the address
 * itself. The demo reads only `account` from the query; its routes live in the hash.
 * Runs at startup, before the router adds history entries that Back could restore.
 */
export function keepOnlyDemoQuery(): void {
  const url = new URL(window.location.href);
  for (const key of [...url.searchParams.keys()]) {
    if (key !== 'account') url.searchParams.delete(key);
  }
  if (url.href !== window.location.href) window.history.replaceState(window.history.state, '', url);
}

async function loadPostHog() {
  // The no-external bundle carries the recorder, so `script-src 'self'` stays.
  const { default: posthog } = await import('posthog-js/dist/module.full.no-external.js');
  return posthog;
}

let loaded: Awaited<ReturnType<typeof loadPostHog>> | null = null;
// Read by before_send, so withdrawing consent stops every event type, and the SDK
// keeps no opt-out state of its own that could outlive a later "allow".
let recording = false;

/** Without consent this is never called, so the SDK is never downloaded. */
export async function startReplay(): Promise<void> {
  const config = replayConfig();
  if (config === null) return;
  recording = true;
  const posthog = await loadPostHog();
  // The visitor may have withdrawn consent while the chunk was loading.
  if (!recording) return;
  loaded = posthog;
  if (posthog.__loaded) {
    posthog.startSessionRecording();
    return;
  }
  posthog.init(config.token, {
    api_host: config.host,
    defaults: '2026-01-30',
    // This tab only: an account switch reloads the page and stays one recording.
    persistence: 'sessionStorage',
    person_profiles: 'never',
    disable_external_dependency_loading: true,
    // Recording needs the project's remote config, so advanced_disable_flags stays off.
    advanced_disable_feature_flags: true,
    autocapture: false,
    rageclick: false,
    capture_pageview: true,
    capture_pageleave: false,
    capture_performance: false,
    capture_dead_clicks: false,
    capture_heatmaps: false,
    capture_exceptions: false,
    enable_recording_console_log: false,
    disable_surveys: true,
    disable_product_tours: true,
    disable_web_experiments: true,
    disable_conversations: true,
    session_recording: {
      maskAllInputs: true,
      // rrweb asks maskTextFn only about text matching this selector.
      maskTextSelector: '*',
      maskTextFn: (text) => (containsTypedValue(text) ? text.replace(/\S/g, '*') : text),
      maskAttributeFn: (name, value) => (!unmaskedAttributes.has(name) && containsTypedValue(value) ? value.replace(/\S/g, '*') : value),
      // The project setting could otherwise turn request bodies and headers on.
      maskCapturedNetworkRequestFn: (request) => ({ ...request, requestBody: null, responseBody: null, requestHeaders: {}, responseHeaders: {} }),
    },
    before_send: (event) => {
      if (event === null || !recording) return null;
      delete event.$set;
      delete event.$set_once;
      // Snapshot events carry the recording itself in their own properties.
      if (event.event !== '$snapshot') {
        const source = event.properties;
        event.properties = Object.fromEntries(keptEventProperties.filter((key) => key in source).map((key) => [key, source[key]]));
      }
      event.properties['$geoip_disable'] = true;
      return event;
    },
  });
}

export function stopReplay(): void {
  recording = false;
  loaded?.stopSessionRecording();
  loaded?.reset(true);
  loaded = null;
  // Drop this tab's ids, also when stopped before the SDK finished loading, so a later
  // "allow" is not linked to the recording stopped here. A loaded SDK may store new ones.
  try {
    for (const key of Object.keys(sessionStorage)) {
      if (key.startsWith('ph_')) sessionStorage.removeItem(key);
    }
  } catch {
    // Storage that cannot be read holds nothing to remove.
  }
}
