import {
  enrolPasskeyFinishOp, enrolTotpConfirmOp, regenerateRecoveryCodesOp,
  removePasskeyOp, removeTotpOp, unlinkIdentityOp, whoamiOp,
} from '@hikyo/operations';
import { assertSessionEpoch, captureSessionEpoch, checkSessionRefusal, reconcileSessionResponse, sessionEpochSignal } from './sessionEpoch.ts';

import type { ScanFinding } from '@hikyo/client';
import type { BodylessOperation, BodyOperation, Options, TDataShape } from '@hikyo/operations';
import { client } from '@hikyo/runtime';
import { zError, zLoginResult, zRecoveryCodesResult } from '@hikyo/zod';
import type { z, ZodObject, ZodRawShape, ZodType } from 'zod';

import type { ExpectedSessionRotation } from './sessionEpoch.ts';

// These authenticated security changes remint the acting session. Login and
// logout never establish continuity with a previous logical session owner.
const ACCOUNT_SESSION_ROTATIONS = new Set<object>([
  enrolPasskeyFinishOp, enrolTotpConfirmOp, removePasskeyOp, removeTotpOp, unlinkIdentityOp,
]);

function expectedAccountRotation<T>(operation: object, data: T): ExpectedSessionRotation | undefined {
  const recovered = operation === regenerateRecoveryCodesOp
    ? zRecoveryCodesResult.safeParse(data) : undefined;
  const rotated = ACCOUNT_SESSION_ROTATIONS.has(operation) ? zLoginResult.safeParse(data) : undefined;
  const login = recovered?.success ? recovered.data.login : rotated?.success ? rotated.data : undefined;
  // A malformed proof grants no continuity. Still reconcile the cookie before
  // the operation's normal parser reports its contract error to the caller.
  if (login === undefined || login.session.artifact !== 'browser') return undefined;
  return { session: login.session.id, principal: login.principal.id };
}

/**
 * A redacted secret-scanning finding as it rides a REFUSAL body (#74, #183).
 *
 * The contract guarantees this shape never carries the matched text, an
 * offset, a length, or an excerpt — only a rule id, the surface it fired on,
 * an immutable locator, and (where an acknowledgement is possible) an opaque
 * token. That guarantee is why the block dialog can render it verbatim.
 */
export type RefusalFinding = ScanFinding;

/**
 * The one place the SPA talks to the server.
 *
 * Three rules live here so no caller has to remember them:
 *
 * 1. **Parse, do not cast.** Every response crosses a generated Zod schema
 *    before any component sees it. TypeScript types vanish at build time; a
 *    server that answers a shape the contract does not describe must fail
 *    HERE, naming the member, not three frames later as `undefined`.
 * 2. **Cookies, always.** The browser session is an HttpOnly `__Host-hikyo`
 *    cookie the SPA can neither read nor set. `credentials: 'same-origin'`
 *    is what makes it travel.
 * 3. **The synchronizer token on every mutation.** It arrives on the
 *    readable `__Host-hikyo-csrf` cookie and is echoed on `X-Hikyo-CSRF`;
 *    without it the server refuses a state-changing cookie request (#56).
 */

function ownedOptions<TData extends TDataShape>(
  options: Options<TData, false>,
  epoch: number | undefined,
): Options<TData, false> {
  if (epoch === undefined) return options;
  const signal = sessionEpochSignal(epoch);
  return {
    ...options,
    signal: options.signal == null ? signal : AbortSignal.any([options.signal, signal]),
    fetch: (input, init) => {
      // SDK preparation is asynchronous. Fence the actual network dispatch too.
      assertSessionEpoch(epoch);
      return (options.fetch ?? globalThis.fetch)(input, init);
    },
  };
}

function isIdentityCheck(operation: object): boolean {
  return operation === whoamiOp;
}

const CSRF_COOKIE = '__Host-hikyo-csrf';
const CSRF_HEADER = 'X-Hikyo-CSRF';

/** readCsrfToken returns the synchronizer token, or '' when there is none. */
export function readCsrfToken(cookieString: string = document.cookie): string {
  for (const part of cookieString.split(';')) {
    const [name, ...rest] = part.trim().split('=');
    if (name === CSRF_COOKIE) {
      return rest.join('=');
    }
  }
  return '';
}

const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS']);

client.setConfig({
  // Root-only, same-origin: the SPA is served by the instance it talks to, so
  // a base URL would be a second place for the origin to be wrong.
  baseUrl: '',
  credentials: 'same-origin',
});

client.interceptors.request.use((request: Request) => {
  if (SAFE_METHODS.has(request.method.toUpperCase())) {
    return request;
  }
  const token = readCsrfToken();
  if (token !== '') {
    request.headers.set(CSRF_HEADER, token);
  }
  return request;
});

/**
 * ApiError is every refusal the SPA can render. Most callers branch only on
 * `status`. `detail` is present only when the generated error contract parsed
 * an explicitly caller-safe detail from the server; malformed bodies and
 * uniform refusals cannot smuggle arbitrary prose through this boundary.
 */
export class ApiError extends Error {
  readonly status: number;
  readonly detail: string | undefined;
  readonly retryAfterMs: number | undefined;
  /**
   * Redacted scanner findings the refusal carried, when it was a scanner block
   * (#183, Surface 2). Empty for every other refusal. These are the ONLY thing
   * the block dialog renders — the contract keeps them free of matched text.
   */
  readonly findings: readonly RefusalFinding[];

  constructor(
    status: number,
    message: string,
    detail?: string,
    retryAfterMs?: number,
    findings: readonly RefusalFinding[] = [],
  ) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.detail = detail;
    this.retryAfterMs = retryAfterMs;
    this.findings = findings;
  }
}

/**
 * TransportError is the one failure that is NOT a refusal: no HTTP response ever
 * came back. A dropped connection, DNS failure, or a fetch that rejected all
 * land here. It exists so `transportRefusalText` can tell "the server could not
 * be reached" apart from "the server answered something this client cannot
 * understand" — the conflation #452 exists to end. Everything the client cannot
 * trust that is NOT this class is a contract violation.
 */
class TransportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'TransportError';
  }
}

const MAX_RETRY_AFTER_MS = 30_000;

function retryAfterMilliseconds(response: Response): number | undefined {
  const value = response.headers.get('Retry-After');
  if (value === null || !/^\d+$/.test(value)) {
    return undefined;
  }
  const milliseconds = Number(value) * 1_000;
  if (!Number.isSafeInteger(milliseconds)) {
    return undefined;
  }
  return Math.min(milliseconds, MAX_RETRY_AFTER_MS);
}

function requireResponse(result: { response?: Response | undefined }): Response {
  if (result.response === undefined) {
    throw new TransportError('SDK call completed without an HTTP response');
  }
  return result.response;
}

/**
 * refusal turns any non-2xx into the ApiError the SPA renders. `detail` survives
 * only when the generated error contract parsed an explicitly caller-safe detail
 * from the body; a malformed or uniform refusal carries none. Both `parsed` and
 * `ok` route their non-2xx here so a bodyless call surfaces the same safe detail
 * a body-bearing one does.
 */
function refusal(response: Response, error: unknown): ApiError {
  const parsed = zError.safeParse(error);
  return new ApiError(
    response.status,
    `request failed with ${response.status}`,
    parsed.success ? parsed.data.error.detail ?? undefined : undefined,
    retryAfterMilliseconds(response),
    parsed.success ? parsed.data.error.findings ?? [] : [],
  );
}

/**
 * parsed runs a body-bearing operation and returns its response parsed by the
 * operation's OWN generated schema. The descriptor binds the call and that
 * schema together, so no caller can pair an operation with another's model. A
 * non-2xx becomes an ApiError carrying the status; a 2xx whose body does not
 * satisfy the contract throws from Zod, loudly, because a silently-accepted
 * wrong shape is the bug this whole chain exists to stop.
 */
export async function parsed<TData extends TDataShape, TSchema extends ZodType>(
  operation: BodyOperation<TData, TSchema>,
  options: Options<TData, false>,
): Promise<z.infer<TSchema>> {
  // whoami is the authority that reopens the fence.
  const epoch = isIdentityCheck(operation) ? undefined : captureSessionEpoch();
  const result = await operation.call(ownedOptions(options, epoch));
  if (epoch !== undefined) {
    await reconcileSessionResponse(epoch, options.client === undefined && result.response !== undefined &&
      result.response.ok && operation.successStatuses.includes(result.response.status)
      ? expectedAccountRotation(operation, result.data) : undefined);
  }
  const response = requireResponse(result);
  if (!response.ok) {
    if (response.status === 401 && !isIdentityCheck(operation) && options.client === undefined) {
      checkSessionRefusal();
    }
    throw refusal(response, result.error);
  }
  if (!operation.successStatuses.includes(response.status)) {
    throw new Error(
      `expected a body-bearing ${operation.successStatuses.join(' or ')}, got ${response.status}: refuse an unbound success response`,
    );
  }
  return operation.response.parse(result.data);
}

/**
 * parsedPick validates a narrow projection of an operation's OWN object schema.
 * Display-once responses use this when unrelated metadata drift must not hide
 * an irretrievable value. The caller supplies keys, never another schema, so
 * operation-to-parser binding remains closed.
 */
export async function parsedPick<
  TData extends TDataShape,
  TShape extends ZodRawShape,
  TMask extends z.util.Mask<keyof TShape>,
>(
  operation: BodyOperation<TData, ZodObject<TShape>>,
  options: Options<TData, false>,
  mask: TMask & Record<Exclude<keyof TMask, keyof TShape>, never>,
) {
  // whoami is the authority that reopens the fence.
  const epoch = isIdentityCheck(operation) ? undefined : captureSessionEpoch();
  const result = await operation.call(ownedOptions(options, epoch));
  if (epoch !== undefined) {
    await reconcileSessionResponse(epoch, options.client === undefined && result.response !== undefined &&
      result.response.ok && operation.successStatuses.includes(result.response.status)
      ? expectedAccountRotation(operation, result.data) : undefined);
  }
  const response = requireResponse(result);
  if (!response.ok) {
    if (response.status === 401 && !isIdentityCheck(operation) && options.client === undefined) {
      checkSessionRefusal();
    }
    throw refusal(response, result.error);
  }
  if (!operation.successStatuses.includes(response.status)) {
    throw new Error(
      `expected a body-bearing ${operation.successStatuses.join(' or ')}, got ${response.status}: refuse an unbound success response`,
    );
  }
  return operation.response.pick(mask).parse(result.data);
}

/**
 * ok runs a bodyless operation - one whose success is a status alone.
 *
 * It is deliberately narrow: anything with a body must go through `parsed` so
 * the contract's schema sees it. A success status other than the descriptor's
 * bodyless one means the contract grew a body this caller is ignoring, which is
 * a bug in the caller and is refused as loudly as a failed request rather than
 * silently discarded.
 */
export async function ok<TData extends TDataShape>(
  operation: BodylessOperation<TData>,
  options: Options<TData, false>,
): Promise<void> {
  // whoami is the authority that reopens the fence.
  const epoch = isIdentityCheck(operation) ? undefined : captureSessionEpoch();
  const result = await operation.call(ownedOptions(options, epoch));
  if (epoch !== undefined) await reconcileSessionResponse(epoch);
  const response = requireResponse(result);
  if (!response.ok) {
    if (response.status === 401 && !isIdentityCheck(operation) && options.client === undefined) {
      checkSessionRefusal();
    }
    throw refusal(response, result.error);
  }
  if (!operation.successStatuses.includes(response.status)) {
    throw new Error(
      `expected a bodyless ${operation.successStatuses.join(' or ')}, got ${response.status}: parse this response instead of discarding it`,
    );
  }
}

/**
 * The domain tier — the statuses whose message every feature voices for itself.
 * `transportRefusalText` returns `null` for these so a feature's own `switch`
 * keeps saying the deliberately-different thing its subsystem needs (an auth
 * 401 is not a disclosure 401). Everything outside this set is generic, so the
 * feature can delegate it here and stop re-typing the same drifting sentence.
 */
const DOMAIN_STATUSES = new Set([401, 403, 404, 409]);

/**
 * transportRefusalText is the shared voice of the GENERIC error tier (#452).
 *
 * It answers the four cases every feature was hand-rolling with drifting
 * wording, and — the reason it exists — keeps two of them apart that the old
 * catch-all fallback fused: "the server could not be reached" (a
 * `TransportError`: your network, retry) is now a different sentence from "the
 * server answered something this client cannot understand" (a contract
 * violation: a bug worth reporting). It returns `null` for a domain `ApiError`
 * (401/403/404/409), the feature's own to describe.
 *
 * Note the fall-through: a `TransportError` is the ONLY non-refusal treated as
 * transport. A Zod parse failure, an unbound-status `Error`, or any other junk
 * the client cannot trust is a contract violation by default — fail loud, not
 * "check your network" for a server that broke the contract. Classifying by an
 * owned class rather than `instanceof ZodError` also sidesteps the cross-copy
 * `instanceof` hazard a bundled third-party error would carry.
 */
export function transportRefusalText(error: unknown): string | null {
  if (error instanceof ApiError) {
    if (error.status === 429) {
      return 'Too many requests right now. Wait a moment and try again.';
    }
    if (DOMAIN_STATUSES.has(error.status)) {
      return null;
    }
    return `The request could not be completed (server error ${error.status}). Try again shortly.`;
  }
  if (error instanceof Error && error.name === 'NotAllowedError') {
    // A dismissed or timed-out WebAuthn prompt never sent anything, so the
    // reassurance is honest here in a way it would not be for a transport drop.
    return 'The passkey prompt was dismissed or timed out. Nothing was sent.';
  }
  if (error instanceof TransportError) {
    // No effect claim: a fetch can reject after the request reached the server,
    // so "nothing was sent" would be a lie in the mid-flight case.
    return typeof navigator !== 'undefined' && navigator.onLine === false
      ? 'You appear to be offline. The server could not be reached — check your connection and try again.'
      : 'The server could not be reached. Try again shortly.';
  }
  return 'The server answered something this client cannot understand. This is likely a bug worth reporting.';
}
