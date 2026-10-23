function installHikyo158HistoricalDescribeObserverV2(input) {
  'use strict';
  // SOURCE ONLY. Passive native metadata observation. No API sends, auth reads,
  // secret-value operation capture, JSON.parse, redirect control or retry.
  const marker = '__hikyo158HistoricalDescribeObserverV2';
  const account = '099256073061', region = 'eu-central-1';
  const secretARN = 'arn:aws:secretsmanager:eu-central-1:099256073061:secret:hikyo-e2e-20261018b/web-identity/postgres/role-document-vmN1Vj';
  const endpoint = 'https://secretsmanager.eu-central-1.amazonaws.com/';
  const target = 'secretsmanager.DescribeSecret';
  const main = 'https://a.b.cdn.console.awsstatic.com/a/v1/TJSQGUMIVFEENQQX4MGXCXUOCNT774UXHMTEHFLOY3762Z4ZSXUQ/main.js';
  const mainSHA = 'd412372053a5b7a265fa704e4d483522f4cff28e93fcf8d46ab7f18f797c2520';
  const encoder = new TextEncoder(), decoder = new TextDecoder('utf-8', { fatal: true });
  const wire = JSON.stringify({ SecretId: secretARN }), wireBytes = encoder.encode(wire);
  const maxResponseBytes = 16384, maxExportBytes = 60000;
  function fail(code) { throw new Error('hikyo158-historical-describe:' + code); }
  const fields = ['schema', 'account', 'region', 'secretARN', 'publicMainSHA256', 'reviewedPreparationSHA256', 'expiresAtMS', 'captureTimeoutMS'];
  if (!input || Object.getPrototypeOf(input) !== Object.prototype || Reflect.ownKeys(input).length !== fields.length) fail('manifest-fields');
  const descriptors = Object.getOwnPropertyDescriptors(input);
  if (!fields.every(name => Object.hasOwn(descriptors, name) && Object.hasOwn(descriptors[name], 'value') && descriptors[name].enumerable)) fail('manifest-data-fields');
  const manifest = Object.freeze(Object.fromEntries(fields.map(name => [name, descriptors[name].value])));
  if (manifest.schema !== 'hikyo-158/historical-describe-observer-manifest/v2' || manifest.account !== account || manifest.region !== region ||
      manifest.secretARN !== secretARN || manifest.publicMainSHA256 !== mainSHA ||
      typeof manifest.reviewedPreparationSHA256 !== 'string' || !/^[a-f0-9]{64}$/.test(manifest.reviewedPreparationSHA256) ||
      !Number.isInteger(manifest.expiresAtMS) || manifest.expiresAtMS <= Date.now() || manifest.expiresAtMS > Date.now() + 300000 ||
      !Number.isInteger(manifest.captureTimeoutMS) || manifest.captureTimeoutMS < 1000 || manifest.captureTimeoutMS > 20000) fail('manifest-binding');
  const storageKey = 'hikyo158-historical-describe-metadata-' + manifest.reviewedPreparationSHA256;
  if (Object.hasOwn(globalThis, marker) || typeof fetch !== 'function' || typeof Request !== 'function' || typeof XMLHttpRequest !== 'function' || !crypto?.subtle) fail('platform-or-existing-observer');
  function route(detailOnly = false) {
    if (location.origin !== 'https://eu-central-1.console.aws.amazon.com' || location.hash !== '') return false;
    const params = new URLSearchParams(location.search);
    if (location.pathname === '/secretsmanager/secret') return params.size === 2 && params.getAll('name').length === 1 && params.get('name') === secretARN &&
      params.getAll('region').length === 1 && params.get('region') === region;
    return !detailOnly && ['/secretsmanager/listsecrets', '/secretsmanager/home'].includes(location.pathname) &&
      params.size === 1 && params.getAll('region').length === 1 && params.get('region') === region;
  }
  function accountBanner() {
    return Array.from(document.querySelectorAll('header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]')).some(node =>
      [node.textContent, node.getAttribute('aria-label')].some(value => typeof value === 'string' && /(^|[^0-9])(?:099256073061|0992-5607-3061)([^0-9]|$)/.test(value)));
  }
  if (!route() || !accountBanner() || !document.querySelector('#c') || !Array.from(document.scripts).some(script => script.src === main)) fail('native-page-account-source');
  const store = globalThis.sessionStorage;
  if (!store || typeof store.getItem !== 'function' || typeof store.setItem !== 'function' || store.getItem(storageKey) !== null) fail('exclusive-own-storage-key-required');
  const nativeFetch = globalThis.fetch, proto = XMLHttpRequest.prototype;
  const originalOpen = proto.open, originalHeader = proto.setRequestHeader, originalSend = proto.send;
  const xhrRecords = new WeakMap(), readers = new Set();
  let closed = false, claimed = false, attempts = 0, pending = 0, skipped = 0, receipt = null, captureError = null, storageWriteSucceeded = false;
  let stopCapture = null;
  const live = () => !closed && Date.now() < manifest.expiresAtMS;
  const context = () => live() && route() && accountBanner();
  const requestContext = () => context() && route(true);
  const digest = async bytes => Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes)), x => x.toString(16).padStart(2, '0')).join('');
  function status() {
    return Object.freeze({ schema: 'hikyo-158/native-historical-describe-status/v2', claimed, attempts, pending, skipped, captureError,
      captured: receipt !== null, capturedHTTPStatus: receipt?.status ?? null, resourceNotFound: receipt?.resourceNotFound ?? false,
      storageKey, storageWriteSucceeded, expiresAtMS: manifest.expiresAtMS, maxMatchingCalls: 1, maxResponseBytes,
      ownerIdentityVerifiedByObserver: false, publicSourceBytesVerifiedByObserver: false, providerAPIsCalledByObserver: false,
      authenticationReadByObserver: false, secretValueOperationsCapturedByObserver: false, cleanup: 'retain-session-no-key-reset' });
  }
  function read() {
    if (!receipt) fail('no-metadata-receipt');
    if (!live() || Date.now() < receipt.sentAtMS || Date.now() - receipt.sentAtMS > 300000) fail('stale-metadata-receipt');
    return receipt;
  }
  async function boundedStream(stream, limit, active) {
    if (!stream) fail('missing-stream');
    const reader = stream.getReader(); readers.add(reader);
    const chunks = []; let size = 0;
    try {
      while (active() && context()) {
        const item = await reader.read();
        if (item.done) break;
        if (!(item.value instanceof Uint8Array) || (size += item.value.byteLength) > limit) fail('stream-size');
        chunks.push(item.value.slice());
      }
      if (!active() || !context() || size < 2) fail('stream-ended-or-expired');
      const bytes = new Uint8Array(size); let offset = 0;
      for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
      return bytes;
    } finally { readers.delete(reader); void reader.cancel().catch(() => {}); }
  }
  function exactWire(bytes) {
    return bytes.byteLength === wireBytes.byteLength && bytes.every((value, index) => value === wireBytes[index]);
  }
  // Closed JSON grammar for an absence error. Do not parse/display the untrusted
  // message; unknown/duplicate/escaped keys, extra fields and value payloads fail.
  const ws = '[\\x20\\x09\\x0a\\x0d]*';
  const stringToken = '"(?:[^"\\\\\\x00-\\x1f]|\\\\(?:["\\\\/bfnrt]|u[0-9a-fA-F]{4}))*"';
  const typeField = '"__type"' + ws + ':' + ws + '"(?:ResourceNotFoundException|com\\.amazonaws\\.secretsmanager#ResourceNotFoundException)"';
  const messageField = '"(?:Message|message)"' + ws + ':' + ws + stringToken;
  const absencePattern = new RegExp('^' + ws + '\\{' + ws + '(?:' + typeField + '(?:' + ws + ',' + ws + messageField + ')?|' + messageField + ws + ',' + ws + typeField + ')' + ws + '\\}' + ws + '$');
  async function save(bytes, statusCode, sentAtMS, receivedAtMS, active) {
    if (!active() || !context() || bytes.byteLength < 2 || bytes.byteLength > maxResponseBytes || !Number.isInteger(statusCode) || statusCode < 200 || statusCode > 599) fail('response-binding');
    const rawResponse = decoder.decode(bytes);
    const resourceNotFound = statusCode === 400 && absencePattern.test(rawResponse);
    // Success metadata and other DescribeSecret errors are memory-only diagnostics.
    // They are not absence proof and are never written to durable browser storage.
    if (/SecretString|SecretBinary/.test(rawResponse)) fail('unexpected-value-field');
    const [requestBodySHA256, rawResponseSHA256] = await Promise.all([digest(wireBytes), digest(bytes)]);
    if (!active() || !context() || receivedAtMS < sentAtMS || Date.now() < receivedAtMS || Date.now() - sentAtMS > manifest.captureTimeoutMS) fail('capture-expired');
    const storedAtMS = resourceNotFound ? Date.now() : null;
    const record = Object.freeze({ schema: 'hikyo-158/native-historical-describe-response/v1', account, region, service: 'secretsmanager', operation: 'DescribeSecret',
      target, endpoint, selector: Object.freeze({ SecretId: secretARN }), requestBodyBytes: wireBytes.byteLength, requestBodySHA256,
      sentAtMS, receivedAtMS, storedAtMS, status: statusCode, resourceNotFound, rawResponseBytes: bytes.byteLength, rawResponseSHA256, rawResponse,
      publicMainSHA256: mainSHA, reviewedPreparationSHA256: manifest.reviewedPreparationSHA256, accountHeaderObserved: true,
      ownerIdentityVerifiedByObserver: false, publicSourceBytesVerifiedByObserver: false, providerAPIsCalledByObserver: false });
    const serialized = JSON.stringify(record);
    if (encoder.encode(serialized).byteLength > maxExportBytes) fail('export-size');
    if (resourceNotFound) {
      if (store.getItem(storageKey) !== null) fail('own-storage-key-raced');
      store.setItem(storageKey, serialized); storageWriteSucceeded = true;
    }
    receipt = record;
  }
  function captureTask(work) {
    pending++;
    let active = true, expire;
    const end = () => { active = false; expire(null); };
    const expired = new Promise(resolve => { expire = resolve; });
    stopCapture = end;
    const timer = setTimeout(end, Math.max(1, Math.min(manifest.captureTimeoutMS, manifest.expiresAtMS - Date.now())));
    void (async () => {
      try { await Promise.race([work(() => active), expired]); if (!active) captureError = 'bounded-capture-expired'; }
      catch { captureError = 'bounded-metadata-capture-held'; }
      finally {
        active = false; clearTimeout(timer); if (stopCapture === end) stopCapture = null; pending--;
        for (const reader of readers) void reader.cancel().catch(() => {});
      }
    })();
    return end;
  }
  function observedFetch(request) {
    let candidate = null;
    try {
      // Public target is checked before cloning ANY request or response. Unknown
      // operations (including GetSecretValue/GetResourcePolicy) are untouched.
      if (arguments.length === 1 && request instanceof Request && requestContext() && !claimed && attempts < 8 && pending === 0 && request.method === 'POST' &&
          request.url === endpoint && request.headers.get('x-amz-target') === target && request.headers.get('content-type') === 'application/x-amz-json-1.1') candidate = request.clone();
    } catch {}
    const sentAtMS = Date.now();
    const result = Reflect.apply(nativeFetch, this, arguments);
    if (candidate) {
      attempts++;
      captureTask(async active => {
        let scopeReady = false;
        const scope = boundedStream(candidate.body, wireBytes.byteLength, active).then(bytes => {
          if (!active() || !requestContext() || claimed || !exactWire(bytes)) return false;
          claimed = true; scopeReady = true; return true;
        }, () => false);
        const response = result.then(value => {
          // A fast reply before exact-body scope is known is deliberately skipped.
          // Never delay/replace the original promise to wait for classification.
          if (!scopeReady || !active() || !context() || value.url !== endpoint || value.status < 200 || value.status > 599) return null;
          return { response: value.clone(), receivedAtMS: Date.now() };
        }, () => null);
        const [known, captured] = await Promise.all([scope, response]);
        if (!known || !captured || !active()) { skipped++; return; }
        await save(await boundedStream(captured.response.body, maxResponseBytes, active), captured.response.status, sentAtMS, captured.receivedAtMS, active);
      });
    }
    return result;
  }
  function observedOpen(method, url, asynchronous) {
    xhrRecords.set(this, arguments.length <= 3 && (arguments.length < 3 || asynchronous === true) && method === 'POST' && url === endpoint ? { type: null, target: null, sent: false } : null);
    return Reflect.apply(originalOpen, this, arguments);
  }
  function observedHeader(name, value) {
    const record = xhrRecords.get(this);
    if (record && !record.sent && typeof name === 'string') {
      const key = name.toLowerCase() === 'x-amz-target' ? 'target' : name.toLowerCase() === 'content-type' ? 'type' : null;
      if (key) { if (record[key] !== null || typeof value !== 'string' || value.length > 128) xhrRecords.set(this, null); else record[key] = value; }
    }
    return Reflect.apply(originalHeader, this, arguments);
  }
  function observedSend(body) {
    const xhr = this, record = xhrRecords.get(xhr);
    let matches = false;
    try {
      if (record) record.sent = true;
      matches = requestContext() && !claimed && pending === 0 && record?.target === target && record.type === 'application/x-amz-json-1.1' &&
        typeof body === 'string' && body.length === wire.length && body === wire;
    } catch { captureError = 'request-classification-held'; }
    let endCapture = null;
    if (matches) {
      claimed = true; attempts++;
      const sentAtMS = Date.now();
      endCapture = captureTask(active => new Promise(resolve => {
        xhr.addEventListener('loadend', () => {
          void (async () => {
            try {
              if (!active() || !context() || xhrRecords.get(xhr) !== record || xhr.responseURL !== endpoint || xhr.status < 200 || xhr.status > 599) { skipped++; return; }
              const receivedAtMS = Date.now(); let bytes;
              if (xhr.responseType === 'arraybuffer') {
                if (!(xhr.response instanceof ArrayBuffer) || xhr.response.byteLength > maxResponseBytes) fail('response-size');
                bytes = new Uint8Array(xhr.response).slice();
              } else if (xhr.responseType === '' || xhr.responseType === 'text') {
                const text = xhr.responseText;
                if (typeof text !== 'string' || text.length > maxResponseBytes) fail('response-size');
                bytes = encoder.encode(text);
              } else fail('response-type');
              await save(bytes, xhr.status, sentAtMS, receivedAtMS, active);
            } catch { captureError = 'bounded-metadata-capture-held'; }
            finally { resolve(); }
          })();
        }, { once: true });
      }));
    }
    // Preserve native arguments, return value, errors and network behavior.
    try { return Reflect.apply(originalSend, xhr, arguments); }
    catch (error) { if (endCapture) endCapture(); throw error; }
  }
  for (const [name, value] of [['open', observedOpen], ['setRequestHeader', observedHeader], ['send', observedSend]])
    Object.defineProperty(proto, name, { value, configurable: false, writable: false });
  Object.defineProperty(globalThis, 'fetch', { value: observedFetch, configurable: false, writable: false });
  Object.defineProperty(globalThis, marker, { value: Object.freeze({ status, read }), configurable: false, writable: false });
  setTimeout(() => { closed = true; receipt = null; if (stopCapture) stopCapture(); for (const reader of readers) void reader.cancel().catch(() => {}); }, manifest.expiresAtMS - Date.now());
  return status();
}
