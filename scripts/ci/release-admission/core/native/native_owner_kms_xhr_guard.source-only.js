function installHikyo158KMSXHRGuard(manifest) {
  'use strict';
  // SOURCE ONLY. No authentication/signing/configuration access or SDK call.
  // Guarantees at most one native CreateKey send invocation, NOT at most one
  // HTTP hop. Native metadata read sends are separate and can occur repeatedly.
  const marker = '__hikyo158NativeKMSXHRWriteGuardV1';
  function fail(code) { throw new Error('hikyo158-kms-guard:' + code); }
  const keys = ['schema', 'endpoint', 'wireBodySHA256', 'wireBodyBytes', 'intentSHA256',
    'publicMainSHA256', 'expiresAtMS', 'timeoutMS', 'redirectLimitationReviewed'].sort();
  if (!manifest || typeof manifest !== 'object' || Array.isArray(manifest) ||
      Object.keys(manifest).sort().join('|') !== keys.join('|')) fail('manifest-fields');
  if (manifest.schema !== 'hikyo-158/native-kms-xhr-write-manifest/v1' ||
      manifest.endpoint !== 'https://kms.eu-central-1.amazonaws.com/' ||
      manifest.publicMainSHA256 !== '20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2' ||
      manifest.redirectLimitationReviewed !== true) fail('manifest-binding');
  for (const value of [manifest.wireBodySHA256, manifest.intentSHA256])
    if (typeof value !== 'string' || !/^[a-f0-9]{64}$/.test(value)) fail('manifest-hash');
  if (!Number.isInteger(manifest.wireBodyBytes) || manifest.wireBodyBytes < 2 ||
      manifest.wireBodyBytes > 32768 || !Number.isInteger(manifest.timeoutMS) ||
      manifest.timeoutMS < 1000 || manifest.timeoutMS > 20000 ||
      !Number.isInteger(manifest.expiresAtMS) || manifest.expiresAtMS <= Date.now() ||
      manifest.expiresAtMS > Date.now() + 300000) fail('manifest-bounds');
  if (location.origin !== 'https://eu-central-1.console.aws.amazon.com' ||
      location.pathname !== '/kms/home' || location.search !== '?region=eu-central-1') fail('document-origin');
  if (globalThis.__isTangerineBoxHosted__ !== true) fail('proxy-variant-unsupported');
  if (!Array.from(document.scripts).some(script => script.src ===
      'https://a.b.cdn.console.awsstatic.com/a/v1/B35BNLNKUKKD7F5DTSPWUSOXQ4VAIKKIFWAKR2KD5MB4MCZXQ4RQ/main.js'))
    fail('loaded-module');
  if (Object.prototype.hasOwnProperty.call(globalThis, marker)) fail('already-installed');
  if (typeof XMLHttpRequest !== 'function' || !crypto?.subtle || typeof TextEncoder !== 'function')
    fail('platform-features');
  const approved = Object.freeze({ ...manifest });
  // Native SPA read probes may visit the key list first. Mutation arm and
  // forwarding remain bound to the exact current source's create-wizard route.
  const route = '#/kms/keys/create';
  const originalOpen = XMLHttpRequest.prototype.open;
  const originalSend = XMLHttpRequest.prototype.send;
  const originalHeader = XMLHttpRequest.prototype.setRequestHeader;
  const originalAbort = XMLHttpRequest.prototype.abort;
  const originalFetch = globalThis.fetch;
  const records = new WeakMap();
  const encoder = new TextEncoder();
  const hex = bytes => Array.from(new Uint8Array(bytes), value => value.toString(16).padStart(2, '0')).join('');
  const state = { phase: 'observing', claimConsumed: false, nativeSendInvocations: 0,
    blocked: 0, activeReads: 0, successfulReadProbes: 0, lastReadTarget: null,
    responseStatus: null, reason: null, responseEndpointMatched: null };
  function status() {
    return Object.freeze({ schema: 'hikyo-158/native-kms-xhr-write-status/v1', ...state,
      intentSHA256: approved.intentSHA256, wireBodySHA256: approved.wireBodySHA256,
      endpoint: approved.endpoint, durabilityVerifiedByPage: false, ownerIdentityVerifiedByPage: false,
      nativeSendCounterScope: 'CreateKey-only-excludes-metadata-reads',
      loadedSourceBytesVerifiedByPage: false, redirectErrorSupported: false,
      physicalHTTPHopBoundEstablished: false, coversOtherRealmsOrCapturedMethods: false,
      cleanup: 'close-controlled-tab' });
  }
  function reject(code) { state.blocked++; fail(code); }
  function hold(code) { state.phase = 'held'; state.reason = code; }
  function publicEndpoint(value) {
    const url = new URL(value, location.href);
    if (url.username || url.password || url.search || url.hash) fail('endpoint-shape');
    return url.href;
  }
  function finishError(xhr, code) {
    hold(code);
    // No body/response/auth data is read. Notify the unmodified SDK's own error
    // handling; its retry attempts encounter the permanently closed gate.
    try { originalAbort.call(xhr); } catch {}
    try { xhr.dispatchEvent(new Event('error')); } catch {}
  }
  function arm(intentSHA256) {
    if (state.phase !== 'observing' || state.claimConsumed || state.activeReads !== 0 ||
        state.successfulReadProbes < 1 || intentSHA256 !== approved.intentSHA256 ||
        Date.now() >= approved.expiresAtMS || location.hash !== route) fail('cannot-arm');
    state.phase = 'armed'; return status();
  }
  function guardedOpen(method, url, async) {
    if (arguments.length > 3 || async === false) return reject('open-form');
    const endpoint = publicEndpoint(String(url));
    const previous = records.get(this);
    if (previous?.sent) return reject('xhr-reuse');
    records.set(this, { method: String(method).toUpperCase(), endpoint,
      type: null, target: null, sent: false });
    return Reflect.apply(originalOpen, this, arguments);
  }
  function guardedHeader(name, value) {
    const record = records.get(this);
    if (!record || record.sent) return reject('header-phase');
    // Authentication/CSRF/signature headers pass through opaquely. Their values
    // are neither inspected, copied, recorded nor exposed by this hook.
    const selector = String(name).toLowerCase();
    if (selector === 'content-type' || selector === 'x-amz-target') {
      const key = selector === 'content-type' ? 'type' : 'target';
      if (record[key] !== null || typeof value !== 'string' || value.length > 128)
        return reject('public-selector-shape');
      record[key] = value;
    }
    return Reflect.apply(originalHeader, this, arguments);
  }
  function guardedSend(body) {
    const xhr = this;
    const record = records.get(xhr);
    if (!record || record.sent) return reject('send-phase');
    record.sent = true;
    if (record.method !== 'POST' || record.endpoint !== approved.endpoint ||
        record.type !== 'application/x-amz-json-1.1') return reject('kms-protocol');
    if (!['observing', 'armed', 'settled', 'held'].includes(state.phase)) return reject('gate-closed');
    const mutation = state.phase === 'armed';
    if (mutation) { state.claimConsumed = true; state.phase = 'checking'; }
    const reads = ['TrentService.ListKeys', 'TrentService.ListKeysForService', 'TrentService.ListAliases',
      'TrentService.DescribeKey', 'TrentService.GetKeyPolicy', 'TrentService.GetKeyRotationStatus',
      'TrentService.ListResourceTags'];
    if (!mutation) {
      if (!reads.includes(record.target)) return reject(state.claimConsumed ? 'gate-closed' : 'unapproved-read-target');
      // Body is entirely opaque on the read path. Existing official UI controls
      // must scope the read; this interception probe is not an owner/resource proof.
      state.activeReads++;
      let ended = false;
      const finish = () => {
        if (ended) return;
        ended = true; state.activeReads--;
        if (xhr.status === 200 && xhr.responseURL === approved.endpoint) {
          state.successfulReadProbes++; state.lastReadTarget = record.target;
        }
      };
      xhr.addEventListener('loadend', finish, { once: true });
      try { return originalSend.call(xhr, body); }
      catch (error) { finish(); throw error; }
    }
    // The claim has already burned. No async body/hash work precedes it.
    if (record.target !== 'TrentService.CreateKey' || typeof body !== 'string') {
      finishError(xhr, 'create-key-shape'); return;
    }
    const bytes = encoder.encode(body);
    if (bytes.byteLength !== approved.wireBodyBytes) {
      finishError(xhr, 'wire-size'); return;
    }
    let settled = false;
    const deadline = Math.min(Date.now() + approved.timeoutMS, approved.expiresAtMS);
    const timer = setTimeout(() => {
      if (!settled) { settled = true; finishError(xhr, 'deadline'); }
    }, Math.max(1, deadline - Date.now()));
    function done() {
      if (settled) return;
      settled = true; clearTimeout(timer);
      state.responseStatus = xhr.status;
      state.responseEndpointMatched = xhr.responseURL === approved.endpoint;
      if (!state.responseEndpointMatched) hold('response-endpoint-mismatch-or-absent');
      else state.phase = 'settled';
    }
    xhr.addEventListener('loadend', done, { once: true });
    void crypto.subtle.digest('SHA-256', bytes).then(digest => {
      if (settled) return;
      if (hex(digest) !== approved.wireBodySHA256 || location.hash !== route ||
          Date.now() >= deadline || state.phase !== 'checking') {
        settled = true; clearTimeout(timer); finishError(xhr, 'wire-binding'); return;
      }
      try {
        // Preserve the exact original XHR, original signed headers, and body.
        // A browser-managed redirect remains outside this send-call bound.
        state.phase = 'forwarded'; state.nativeSendInvocations++;
        originalSend.call(xhr, body);
      } catch {
        settled = true; clearTimeout(timer); finishError(xhr, 'send-failed-reconcile');
      }
    }, () => {
      if (!settled) { settled = true; clearTimeout(timer); finishError(xhr, 'hash-failed'); }
    });
    return undefined;
  }
  function guardedFetch(request, init) {
    // KMS's proven SDK path is XHR. Fetch is not an alternate mutation bridge.
    let url;
    try { url = new URL(typeof request === 'string' ? request : request.url, location.href); }
    catch { return reject('fetch-form'); }
    const method = init?.method ?? (typeof request === 'string' ? 'GET' : request.method);
    if (method === 'GET' && url.origin === location.origin && state.phase === 'observing')
      return originalFetch(request, init);
    return reject('fetch-denied');
  }
  for (const [name, value] of [['open', guardedOpen], ['setRequestHeader', guardedHeader], ['send', guardedSend]])
    Object.defineProperty(XMLHttpRequest.prototype, name, { configurable: false, writable: false, value });
  Object.defineProperty(globalThis, 'fetch', { configurable: false, writable: false, value: guardedFetch });
  Object.defineProperty(globalThis, marker, { configurable: false, writable: false,
    value: Object.freeze({ status, arm }) });
  return status();
}
