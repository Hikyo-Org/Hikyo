import { z } from 'zod';

/** Per-document ownership fence. Cross-tab notifications carry no credentials. */
export const SESSION_CHANNEL = 'hikyo-root-auth';
export const SESSION_MESSAGE = 'session-changed';
const sender = crypto.randomUUID();
const sessionMessage = z.object({ type: z.literal(SESSION_MESSAGE), sender: z.string() });
const STORAGE_KEY = 'hikyo-session-change';

let epoch = 0;
let controller = new AbortController();
let blocked = false;
let cookie = '';
let onReplacement: (() => void) | undefined;
let onRefusal: (() => void) | undefined;
export type ExpectedSessionRotation = {
  readonly session: string;
  readonly principal: string;
};

let onRotation: ((expected?: ExpectedSessionRotation) => Promise<void>) | undefined;
let reconciliation: Promise<void> | undefined;

function companion(): string {
  if (typeof document === 'undefined') return '';
  return document.cookie.split(';')
    .map((part) => part.trim())
    .find((part) => part.startsWith('__Host-hikyo-csrf=')) ?? '';
}

export class SessionChangedError extends Error {
  constructor() {
    super('The browser session changed. Retry after sign-in completes.');
    this.name = 'SessionChangedError';
  }
}

export function installSessionFence(
  replaced: () => void, refused: () => void,
  rotated?: (expected?: ExpectedSessionRotation) => Promise<void>,
): () => void {
  onReplacement = replaced;
  onRefusal = refused;
  onRotation = rotated;
  cookie = companion();
  blocked = false;
  return () => {
    onReplacement = undefined;
    onRefusal = undefined;
    onRotation = undefined;
    reconciliation = undefined;
    blockSessionEpoch();
    blocked = false;
  };
}

/** Called synchronously before a peer-tab identity check begins. */
export function blockSessionEpoch(): void {
  controller.abort();
  controller = new AbortController();
  epoch += 1;
  blocked = true;
}

export function settleSessionEpoch(): void {
  cookie = companion();
  blocked = false;
}

/** Detect cookie replacement even before a queued BroadcastChannel event runs. */
export function checkSessionCookie(): void {
  if (onReplacement !== undefined && !blocked && cookie !== companion()) {
    onReplacement();
  }
}

export function captureSessionEpoch(): number {
  checkSessionCookie();
  if (blocked) throw new SessionChangedError();
  return epoch;
}

export function assertSessionEpoch(captured: number): void {
  checkSessionCookie();
  if (blocked || captured !== epoch) throw new SessionChangedError();
}

/** A proof 401 is ambiguous. Only a fresh whoami may declare session loss. */
export function checkSessionRefusal(): void {
  onRefusal?.();
}

/** OIDC callbacks also use this without needing an authenticated component. */
export function announceSessionChange(): void {
  if (typeof BroadcastChannel === 'function') {
    const channel = new BroadcastChannel(SESSION_CHANNEL);
    channel.postMessage({ type: SESSION_MESSAGE, sender });
    channel.close();
  }
  // Storage is a notification fallback, never an identity or bearer store.
  try {
    window.localStorage.setItem(STORAGE_KEY, crypto.randomUUID());
  } catch {
    // BroadcastChannel and request-time cookie checks also work without storage.
  }
}

export function isSessionStorageEvent(event: StorageEvent): boolean {
  return event.key === STORAGE_KEY;
}

/** Some successful operations rotate cookies in-place. Before releasing their
 * display-once result, whoami must prove the session is still the same owner.
 * New requests stay fenced during that check. No endpoint gets an exemption. */
export async function reconcileSessionResponse(
  captured: number, expected?: ExpectedSessionRotation,
): Promise<void> {
  if (captured !== epoch) throw new SessionChangedError();
  // Each account response owns its proof. Waiting for another response's
  // reconciliation must not authorize a different returned session identity.
  if (reconciliation !== undefined) await reconciliation;
  if (captured !== epoch) throw new SessionChangedError();
  if (onRotation !== undefined && (expected !== undefined || cookie !== companion()) && !blocked) {
    blocked = true;
    reconciliation = onRotation(expected).finally(() => { reconciliation = undefined; });
    await reconciliation;
  }
  assertSessionEpoch(captured);
}

export function isPeerSessionMessage(event: MessageEvent): boolean {
  const parsed = sessionMessage.safeParse(event.data);
  return parsed.success && parsed.data.sender !== sender;
}

export function sessionEpochSignal(captured: number): AbortSignal {
  assertSessionEpoch(captured);
  return controller.signal;
}

/** Unknown cookie ownership must survive overlapping identity checks. */
export function isSessionEpochBlocked(): boolean {
  return blocked;
}
