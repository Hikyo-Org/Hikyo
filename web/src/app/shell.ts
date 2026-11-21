import { z } from 'zod';

import { captureSessionEpoch, reconcileAfterShellHop } from '../api/sessionEpoch.ts';

/**
 * The desktop shell's window, seen from the SPA (desktop-app ADR D8 and D9;
 * wire contract in docs/spec/desktop-shell-protocol.md).
 *
 * This module owns app-mode detection and every `/__shell/*` call. Nothing else
 * in the SPA fetches a shell route or tests the protocol. Every response is
 * parsed with Zod before use, and every request is built from a strict schema,
 * so a drifting shell fails loudly here rather than three frames later.
 */

/** App mode is `wails:`. No browser serves that scheme, so browser builds never branch. */
export function isAppMode(): boolean {
  return globalThis.location?.protocol === 'wails:';
}

/** The handoff's limit on `key_ids` for a non-adapter purpose (`CLIReauthStartRequest`). */
export const SHELL_REAUTH_KEY_LIMIT = 500;
/** The most environments one reauth names (an `adapter` set); a larger set continues in the browser. */
export const SHELL_REAUTH_ENVIRONMENT_LIMIT = 1000;
/** The most decoded bytes one `download` carries; the audit export's own cap (`web/src/api/audit.ts`). */
export const SHELL_DOWNLOAD_BYTES_LIMIT = 64 * 1024 * 1024;

const errorCode = z.enum(['refused', 'busy', 'rate_limited', 'cancelled', 'failed', 'invalid']);
export type ShellErrorCode = z.infer<typeof errorCode>;
const zShellError = z.strictObject({ error: errorCode, message: z.string() });

/** A refusal or failure the shell reported, or a reply that broke the contract (`failed`). */
export class ShellError extends Error {
  readonly code: ShellErrorCode;
  constructor(code: ShellErrorCode, message: string) {
    super(message);
    this.name = 'ShellError';
    this.code = code;
  }
}

// -- "Continue in browser" ----------------------------------------------------

/**
 * What the app window cannot do on its own origin: the passkey ceremonies the
 * handoff has no purpose for, enrolment, identity linking, and a unit too large
 * for the handoff. The caller throws this; `ContinueInBrowser` renders the offer.
 */
export class ContinueInBrowserError extends Error {
  constructor(readonly reason: string) {
    super(reason);
    this.name = 'ContinueInBrowserError';
  }
}

let offer: string | null = null;
const offerListeners = new Set<() => void>();

function setOffer(next: string | null): void {
  offer = next;
  for (const listener of offerListeners) listener();
}

/** Show the offer and return the error for the caller to throw: `throw offerContinueInBrowser(...)`. */
export function offerContinueInBrowser(reason: string): ContinueInBrowserError {
  setOffer(reason);
  return new ContinueInBrowserError(reason);
}

/** Refuse a flow the app window cannot run, before it spends a proof or starts a ceremony. */
export function requireBrowser(reason: string): void {
  if (isAppMode()) throw offerContinueInBrowser(reason);
}

export function subscribeContinueInBrowser(listener: () => void): () => void {
  offerListeners.add(listener);
  return () => { offerListeners.delete(listener); };
}
export function continueInBrowserReason(): string | null {
  return offer;
}
export function dismissContinueInBrowser(): void {
  if (offer !== null) setOffer(null);
}

// -- Transport ----------------------------------------------------------------

async function shellFetch(route: string, init: RequestInit): Promise<Response> {
  try {
    return await fetch(`/__shell/${route}`, init);
  } catch {
    throw new ShellError('failed', 'The desktop app could not be reached.');
  }
}

async function refusal(response: Response): Promise<ShellError> {
  const body = zShellError.safeParse(await response.json().catch(() => undefined));
  return body.success
    ? new ShellError(body.data.error, body.data.message)
    : new ShellError('failed', `The desktop app answered ${response.status} with a body outside its contract.`);
}

/** POST a strict JSON body and require the contract's bare 204. */
async function post(route: string, body: unknown): Promise<void> {
  const response = await shellFetch(route, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!response.ok) throw await refusal(response);
  if (response.status !== 204) throw new ShellError('failed', `The desktop app answered ${response.status}, expected 204.`);
}

// -- Routes -------------------------------------------------------------------

const id = z.string().min(1);

const zSelfConfigWire = z.strictObject({
  plan_digest: z.string().regex(/^[a-f0-9]{64}$/).optional(),
  action: z.enum(['adopt', 'apply', 'mail-test', 'rollout-restore']),
  owner_instance_id: z.string().min(1).max(128),
  revision: z.number().int().nonnegative(),
  schema_version: z.number().int().min(1),
  expected_generation: z.number().int().nonnegative(),
  preview_token: z.string().max(1024),
  to: z.string().max(320),
  confirm_restored_credentials: z.boolean(),
});

/** `developer-credential` is deliberately absent: it stays a terminal action. */
const zReauthRequest = z.strictObject({
  purpose: z.enum(['adapter', 'reveal', 'copy', 'publish', 'approve', 'reject', 'bypass', 'self-config']),
  operation: z.enum([
    'adapter.configure', 'adapter.credential-set', 'adapter.adopt', 'adapter.sync',
    'value.reveal', 'value.copy-source', 'value.copy-destination',
    'approval.vote', 'approval.bypass',
    'self-config.adopt', 'self-config.apply', 'self-config.test',
  ]),
  environment_ids: z.array(id).max(SHELL_REAUTH_ENVIRONMENT_LIMIT),
  key_ids: z.array(id).max(SHELL_REAUTH_KEY_LIMIT).optional(),
  self_config: zSelfConfigWire.optional(),
}).superRefine((request, context) => {
  // Mirrors the handoff: self-config names no environment, every other purpose names at least one.
  const selfConfig = request.purpose === 'self-config';
  const keys = request.key_ids?.length ?? 0;
  if (selfConfig !== (request.self_config !== undefined)) context.addIssue({ code: 'custom', message: 'self_config belongs to the self-config purpose only' });
  if (selfConfig !== (request.environment_ids.length === 0)) context.addIssue({ code: 'custom', message: 'environment_ids must be empty for self-config and present otherwise' });
  // Key-bound purposes decide over one environment and an exact, non-empty key unit.
  const keyBound = !selfConfig && request.purpose !== 'adapter';
  if (keyBound && request.environment_ids.length !== 1) context.addIssue({ code: 'custom', message: 'a key-bound purpose names exactly one environment' });
  if (keyBound !== (keys > 0) || (!keyBound && request.key_ids !== undefined)) context.addIssue({ code: 'custom', message: 'key_ids belong to the key-bound purposes and must be non-empty there' });
});
export type ShellReauthRequest = z.input<typeof zReauthRequest>;

/** Run a reauth in the system browser. Resolves once the ceremony was approved and redeemed. */
export async function shellReauth(request: ShellReauthRequest): Promise<void> {
  if (request.purpose !== 'adapter' && (request.key_ids?.length ?? 0) > SHELL_REAUTH_KEY_LIMIT) {
    throw offerContinueInBrowser('This decision covers more keys than the desktop app can hand to your browser.');
  }
  if (request.environment_ids.length > SHELL_REAUTH_ENVIRONMENT_LIMIT) {
    throw offerContinueInBrowser('This decision covers more environments than the desktop app can hand to your browser.');
  }
  try {
    const body = zReauthRequest.parse(request);
    const epoch = captureSessionEpoch();
    await post('reauth', body);
    // The shell may have changed the session behind the page: prove the owner is the same.
    await reconcileAfterShellHop(epoch);
  } catch (error) {
    if (error instanceof ShellError && error.code === 'refused') throw offerContinueInBrowser('The desktop app will not run this confirmation. Continue in your browser instead.');
    throw error;
  }
}

/** Text for a failure that came from the shell or from refusing to hop; undefined for anything else. */
export function shellFailureText(error: unknown): string | undefined {
  if (error instanceof ContinueInBrowserError) return error.message;
  if (!(error instanceof ShellError)) return undefined;
  switch (error.code) {
    case 'cancelled': return 'The confirmation was cancelled. Nothing was authorised.';
    case 'busy': return 'Another confirmation is already open in your browser. Finish or close it, then try again.';
    case 'rate_limited': return 'Too many confirmations just now. Wait a moment, then try again.';
    default: return 'The confirmation could not be completed in your browser. Try again.';
  }
}

let signingIn: Promise<void> | undefined;
let signedOutHere = false;

/** An explicit sign-out in this window: the next sign-in waits for a click instead of opening the browser. */
export function noteSignedOut(): void {
  signedOutHere = true;
}
export function hasSignedOut(): boolean {
  return signedOutHere;
}

/** Ask the shell for a fresh session. One call at a time, whoever asks. */
export function shellSignIn(): Promise<void> {
  signedOutHere = false;
  signingIn ??= post('sign-in', {}).finally(() => { signingIn = undefined; });
  return signingIn;
}

const zBrowserPath = z.string().regex(/^\/(?!\/)/);

/** Open an absolute path of this instance in the system browser. Never a URL. */
export async function shellOpenInBrowser(path: string): Promise<void> {
  await post('open-in-browser', { path: zBrowserPath.parse(path) });
}

/** The current page, in the system browser. */
export function shellOpenCurrentPage(): Promise<void> {
  return shellOpenInBrowser(`${location.pathname}${location.search}`);
}

const zFilename = z.string().min(1).max(255);
const zProxiedDownload = z.strictObject({ path: z.string().regex(/^\/api\/v1\//), filename: zFilename });
const zBytesDownload = z.strictObject({ bytes_base64: z.string(), filename: zFilename, content_type: z.string().min(1) });

function toBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return btoa(binary);
}

/** Save an API response through the shell's save panel. Resolves false when the user cancelled. */
export async function shellDownloadPath(path: string, filename: string): Promise<boolean> {
  return downloaded(post('download', zProxiedDownload.parse({ path, filename })));
}

/** Save bytes the SPA built itself through the shell's save panel. Resolves false when the user cancelled. */
export async function shellDownloadBytes(bytes: Uint8Array, filename: string, contentType: string): Promise<boolean> {
  if (bytes.length > SHELL_DOWNLOAD_BYTES_LIMIT) {
    throw offerContinueInBrowser('This file is larger than the desktop app can save. Continue in your browser to download it.');
  }
  return downloaded(post('download', zBytesDownload.parse({ bytes_base64: toBase64(bytes), filename, content_type: contentType })));
}

async function downloaded(saving: Promise<void>): Promise<boolean> {
  try {
    await saving;
    return true;
  } catch (error) {
    if (error instanceof ShellError && error.code === 'cancelled') return false;
    throw error;
  }
}

/** Tell the shell a secret was just copied, so it can arm clearing. */
export function shellClipboardCopied(): Promise<void> {
  return post('clipboard', {});
}

const zOrigin = z.strictObject({
  origin: z.url().refine((value) => new URL(value).origin === value, 'not an origin'),
});
let origin: Promise<string> | undefined;
let knownOrigin: string | undefined;

/** The instance's real origin, for links the SPA builds. Read once. */
export function shellOrigin(): Promise<string> {
  origin ??= (async () => {
    const response = await shellFetch('origin', { method: 'GET' });
    if (response.status !== 200) throw await refusal(response);
    const parsed = zOrigin.safeParse(await response.json().catch(() => undefined));
    if (!parsed.success) throw new ShellError('failed', 'The desktop app answered an origin outside its contract.');
    knownOrigin = parsed.data.origin;
    return parsed.data.origin;
  })().catch((error: unknown) => {
    origin = undefined;
    throw error;
  });
  return origin;
}

/** The origin once `shellOrigin` has resolved, for synchronous render paths. */
export function knownShellOrigin(): string | undefined {
  return knownOrigin;
}

/**
 * The origin this page's links and self-checks should name: the instance's real
 * one in app mode, and `location.origin` everywhere else. In app mode it needs
 * `shellOrigin()` to have resolved, which `main.tsx` awaits before rendering.
 */
export function instanceOrigin(): string {
  if (!isAppMode()) return location.origin;
  if (knownOrigin === undefined) throw new ShellError('failed', 'The instance address was not read from the desktop app.');
  return knownOrigin;
}
