async (page, input) => {
  'use strict';
  const fail = () => { throw new Error('aws-operator-held'); };
  let cdp;
  try {
    const ROUTES = {"iam-home":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/home","iam-users":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/users","iam-roles":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/roles","iam-providers":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/identity_providers","iam-setup":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/users/details/hikyo-158-setup","iam-setup-permissions":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/users/details/hikyo-158-setup?section=permissions","iam-setup-groups":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/users/details/hikyo-158-setup?section=groups","iam-setup-security":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/users/details/hikyo-158-setup?section=security_credentials","iam-boundary":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/arn%3Aaws%3Aiam%3A%3A099256073061%3Apolicy%2Fhikyo-158-role-boundary","iam-policies":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies","iam-policy":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/arn%3Aaws%3Aiam%3A%3A099256073061%3Apolicy%2Fhikyo-158-setup-policy","iam-policy-versions":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/arn%3Aaws%3Aiam%3A%3A099256073061%3Apolicy%2Fhikyo-158-setup-policy?section=versions","iam-policy-permissions":"https://us-east-1.console.aws.amazon.com/iam/home?region=eu-central-1#/policies/details/arn%3Aaws%3Aiam%3A%3A099256073061%3Apolicy%2Fhikyo-158-setup-policy?section=permissions","kms-home":"https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/home","kms-keys":"https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys","kms-create":"https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create","sm-list":"https://eu-central-1.console.aws.amazon.com/secretsmanager/listsecrets?region=eu-central-1","sm-home":"https://eu-central-1.console.aws.amazon.com/secretsmanager/home?region=eu-central-1","sm-historical":"https://eu-central-1.console.aws.amazon.com/secretsmanager/secret?name=arn%3Aaws%3Asecretsmanager%3Aeu-central-1%3A099256073061%3Asecret%3Ahikyo-e2e-20261018b%2Fweb-identity%2Fpostgres%2Frole-document-vmN1Vj&region=eu-central-1","billing-bills":"https://us-east-1.console.aws.amazon.com/billing/home?region=eu-central-1#/bills","kms-old1":"https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/10908b8d-fe54-40e3-9210-a3eefea709b6","kms-old2":"https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/990c479f-d673-4bc9-8bca-7b9b86f4b205","kms-old3":"https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/fa374267-a456-4e79-b5b1-6cdf356b7d0e","kms-old4":"https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1"};
    const w=Object.freeze({notBeforeMS:input.window.notBeforeMS,expiresAtMS:input.window.expiresAtMS});
    const expected = input.expected;
    const routeURL = ROUTES[input.routeId];
    if (!routeURL || !expected || expected.targetURL !== routeURL) fail();
    const fields = ['targetId','targetType','targetURL','browserContextId','mainFrameId','loaderId','frameURL','frameURLRelation'];
    if (Object.keys(expected).length !== fields.length || fields.some(k=>!Object.hasOwn(expected,k))) fail();
    cdp = await page.context().newCDPSession(page);
    async function identity(url) {
      const {targetInfo:t} = await cdp.send('Target.getTargetInfo');
      const {frameTree:tree} = await cdp.send('Page.getFrameTree');
      const f = tree.frame;
      const relation = f.url === t.url ? 'same-url' : t.url.includes('#') && f.url === t.url.split('#',1)[0] ? 'target-url-without-fragment' : 'unrelated';
      if (f.parentId || t.type !== 'page' || t.url !== url || t.targetId !== expected.targetId ||
          t.browserContextId !== expected.browserContextId || f.id !== expected.mainFrameId || f.loaderId !== expected.loaderId ||
          !['same-url','target-url-without-fragment'].includes(relation)) fail();
      const value={targetId:t.targetId,targetType:t.type,targetURL:t.url,browserContextId:t.browserContextId,
        mainFrameId:f.id,loaderId:f.loaderId,frameURL:f.url,frameURLRelation:relation};
      if(url===routeURL && fields.some(k=>value[k]!==expected[k])) fail();
      return value;
    }
    // This is the only pre-account DOM access. URL is checked in the same realm
    // before querying the single public account header. Never export its text.
    async function account(url) {
      const okay = await page.evaluate(({url,sharedWindow}) => {
        function checkSharedWindow(globalObject, sharedWindow, minimumRemainingMS = 0) {
  const now = globalObject.Date.now();
  if (!sharedWindow || Object.keys(sharedWindow).length !== 2 ||
      !Number.isSafeInteger(sharedWindow.notBeforeMS) || !Number.isSafeInteger(sharedWindow.expiresAtMS) ||
      !Number.isSafeInteger(now) || now < sharedWindow.notBeforeMS || now >= sharedWindow.expiresAtMS ||
      sharedWindow.expiresAtMS > sharedWindow.notBeforeMS + 300000 ||
      ![0, 60000, 120000].includes(minimumRemainingMS) || sharedWindow.expiresAtMS - now < minimumRemainingMS)
    throw new Error('aws-operator-held');
  return now;
}
        checkSharedWindow(globalThis,sharedWindow);
        if(location.href!==url)return false;
        const n=document.querySelector('#nav-usernameMenu');
        if(!n || n.getClientRects().length===0)return false;
        const text=n.textContent;
        const matched=typeof text==='string' && text.length<=512 && /(^|[^0-9])099256073061([^0-9]|$)/.test(text);
        checkSharedWindow(globalThis,sharedWindow);return matched;
      },{url,sharedWindow:w});
      if(okay!==true)fail();
    }
    async function checked(url=routeURL) { const value=await identity(url); await account(url); return value; }
    const before = await checked();
    const clock=await page.evaluate(({url,sharedWindow})=>{
      function checkSharedWindow(globalObject, sharedWindow, minimumRemainingMS = 0) {
  const now = globalObject.Date.now();
  if (!sharedWindow || Object.keys(sharedWindow).length !== 2 ||
      !Number.isSafeInteger(sharedWindow.notBeforeMS) || !Number.isSafeInteger(sharedWindow.expiresAtMS) ||
      !Number.isSafeInteger(now) || now < sharedWindow.notBeforeMS || now >= sharedWindow.expiresAtMS ||
      sharedWindow.expiresAtMS > sharedWindow.notBeforeMS + 300000 ||
      ![0, 60000, 120000].includes(minimumRemainingMS) || sharedWindow.expiresAtMS - now < minimumRemainingMS)
    throw new Error('aws-operator-held');
  return now;
}
      const now=checkSharedWindow(globalThis,sharedWindow);
      if(location.href!==url)throw new Error('aws-operator-held');
      return now;
    },{url:routeURL,sharedWindow:w});
    let remainingMS=w.expiresAtMS-clock;
    async function reserve(ms) {
      remainingMS=await page.evaluate(({sharedWindow,ms})=>{
        function checkSharedWindow(globalObject, sharedWindow, minimumRemainingMS = 0) {
  const now = globalObject.Date.now();
  if (!sharedWindow || Object.keys(sharedWindow).length !== 2 ||
      !Number.isSafeInteger(sharedWindow.notBeforeMS) || !Number.isSafeInteger(sharedWindow.expiresAtMS) ||
      !Number.isSafeInteger(now) || now < sharedWindow.notBeforeMS || now >= sharedWindow.expiresAtMS ||
      sharedWindow.expiresAtMS > sharedWindow.notBeforeMS + 300000 ||
      ![0, 60000, 120000].includes(minimumRemainingMS) || sharedWindow.expiresAtMS - now < minimumRemainingMS)
    throw new Error('aws-operator-held');
  return now;
}
        return sharedWindow.expiresAtMS-checkSharedWindow(globalThis,sharedWindow,ms);
      },{sharedWindow:w,ms});
    }
    let value;
    let afterURL=routeURL;

 await reserve(60000);if(input.manifest.expiresAtMS!==w.expiresAtMS)fail();
 await checked();await page.evaluate(({url,sharedWindow,m})=>{
 function checkSharedWindow(globalObject, sharedWindow, minimumRemainingMS = 0) {
  const now = globalObject.Date.now();
  if (!sharedWindow || Object.keys(sharedWindow).length !== 2 ||
      !Number.isSafeInteger(sharedWindow.notBeforeMS) || !Number.isSafeInteger(sharedWindow.expiresAtMS) ||
      !Number.isSafeInteger(now) || now < sharedWindow.notBeforeMS || now >= sharedWindow.expiresAtMS ||
      sharedWindow.expiresAtMS > sharedWindow.notBeforeMS + 300000 ||
      ![0, 60000, 120000].includes(minimumRemainingMS) || sharedWindow.expiresAtMS - now < minimumRemainingMS)
    throw new Error('aws-operator-held');
  return now;
}
 checkSharedWindow(globalThis,sharedWindow);
 if(location.href!==url)throw new Error('aws-operator-held');
 const banner=document.querySelector('#nav-usernameMenu');
 if(!banner||banner.getClientRects().length===0||typeof banner.textContent!=='string'||banner.textContent.length>512||!/(^|[^0-9])099256073061([^0-9]|$)/.test(banner.textContent))throw new Error('aws-operator-held');
 function createGuardedDocument(globalObject, allowedURLs, sharedWindow) {
  const nativeDocument = globalObject.document;
  const fail = () => { throw new Error('aws-operator-held'); };
  const one = new Set(['#nav-usernameMenu', '#app', '#c', '[data-testid="iam-v2-vite"]']);
  const many = 'header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]';
  function guard() {
    checkSharedWindow(globalObject, sharedWindow);
    // The URL check MUST precede every real document/node/collection access.
    if (!allowedURLs.includes(globalObject.location.href)) fail();
    if (globalObject.document !== nativeDocument) fail();
    const banner = Reflect.apply(nativeDocument.querySelector, nativeDocument, ['#nav-usernameMenu']);
    if (!banner || Reflect.apply(banner.getClientRects, banner, []).length === 0) fail();
    const text = banner.textContent;
    if (typeof text !== 'string' || text.length > 512 || !/(^|[^0-9])099256073061([^0-9]|$)/.test(text)) fail();
  }
  function node(value) {
    if (value === null) return null;
    const attribute = name => {
      guard();
      if (name !== 'aria-label') fail();
      return Reflect.apply(value.getAttribute, value, [name]);
    };
    // Returned nodes cannot expose real DOM objects through arbitrary members.
    return Object.freeze(Object.defineProperties(Object.create(null), {
      textContent: { enumerable: true, get() { guard(); return value.textContent; } },
      src: { enumerable: true, get() { guard(); return value.src; } },
      getAttribute: { enumerable: true, get() { guard(); return attribute; } }
    }));
  }
  function collection(value) {
    function* iterate() {
      guard();
      const iterator = Reflect.apply(value[Symbol.iterator], value, []);
      while (true) {
        guard();
        const part = Reflect.apply(iterator.next, iterator, []);
        if (part.done) return;
        yield node(part.value);
      }
    }
    return Object.freeze(Object.defineProperties(Object.create(null), {
      length: { enumerable: true, get() { guard(); return value.length; } },
      [Symbol.iterator]: { get() { guard(); return iterate; } }
    }));
  }
  const queryOne = selector => {
    guard();
    if (!one.has(selector)) fail();
    return node(Reflect.apply(nativeDocument.querySelector, nativeDocument, [selector]));
  };
  const queryMany = selector => {
    guard();
    if (selector !== many) fail();
    return collection(Reflect.apply(nativeDocument.querySelectorAll, nativeDocument, [selector]));
  };
  return Object.freeze(Object.defineProperties(Object.create(null), {
    querySelector: { enumerable: true, get() { guard(); return queryOne; } },
    querySelectorAll: { enumerable: true, get() { guard(); return queryMany; } },
    scripts: { enumerable: true, get() { guard(); return collection(nativeDocument.scripts); } }
  }));
}
 { const document=createGuardedDocument(globalThis,["https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/home","https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys","https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/create","https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/10908b8d-fe54-40e3-9210-a3eefea709b6","https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/990c479f-d673-4bc9-8bca-7b9b86f4b205","https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/fa374267-a456-4e79-b5b1-6cdf356b7d0e","https://eu-central-1.console.aws.amazon.com/kms/home?region=eu-central-1#/kms/keys/bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1"],sharedWindow);
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

 const d=Object.getOwnPropertyDescriptor(globalThis,'__hikyo158NextKeyCustodyObserverV1');
 if(!d||!Object.hasOwn(d,'value')||!Object.isFrozen(d.value))throw new Error('aws-operator-held');
 const pre=d.value.status();
 if(pre.schema!=='hikyo-158/next-key-observer-status/v1'||pre.mode!=='create'||pre.pendingCaptures!==0||pre.readySlots.length!==0||pre.error!==null)throw new Error('aws-operator-held');
 checkSharedWindow(globalThis,sharedWindow,60000);
 installHikyo158KMSXHRGuard(m);
 }
 },{url:routeURL,sharedWindow:w,m:input.manifest});
 await checked();const s=await page.evaluate(({url,sharedWindow})=>{
 function checkSharedWindow(globalObject, sharedWindow, minimumRemainingMS = 0) {
  const now = globalObject.Date.now();
  if (!sharedWindow || Object.keys(sharedWindow).length !== 2 ||
      !Number.isSafeInteger(sharedWindow.notBeforeMS) || !Number.isSafeInteger(sharedWindow.expiresAtMS) ||
      !Number.isSafeInteger(now) || now < sharedWindow.notBeforeMS || now >= sharedWindow.expiresAtMS ||
      sharedWindow.expiresAtMS > sharedWindow.notBeforeMS + 300000 ||
      ![0, 60000, 120000].includes(minimumRemainingMS) || sharedWindow.expiresAtMS - now < minimumRemainingMS)
    throw new Error('aws-operator-held');
  return now;
}
 checkSharedWindow(globalThis,sharedWindow);
 if(location.href!==url)throw new Error('aws-operator-held');
 const banner=document.querySelector('#nav-usernameMenu');
 if(!banner||banner.getClientRects().length===0||typeof banner.textContent!=='string'||banner.textContent.length>512||!/(^|[^0-9])099256073061([^0-9]|$)/.test(banner.textContent))throw new Error('aws-operator-held');
 return globalThis.__hikyo158NativeKMSXHRWriteGuardV1.status();
 },{url:routeURL,sharedWindow:w});
 if(s.phase!=='observing'||s.claimConsumed!==false||s.nativeSendInvocations!==0||s.intentSHA256!==input.manifest.intentSHA256||s.wireBodySHA256!==input.manifest.wireBodySHA256)fail();
 value={installedUnarmed:true,phase:'observing',claimConsumed:false,nativeSendInvocations:0,finishHelperProvided:false,rootPhysicalGuardRequired:true};
    const after=await checked(afterURL);
    // Detach first: expiry during protocol cleanup must not produce success.
    await cdp.detach();cdp=null;
    await account(afterURL);
    return {schema:'hikyo-158/aws-public-operator/v1',before,after,readAtMS:clock,value,
      controlLeaseClaimed:false,exclusiveControlVerifiedByAdapter:false,writeAuthorityGranted:false,providerOutcomeProven:false};
  } catch { throw new Error('aws-operator-held'); }
  finally { if(cdp) { try { await cdp.detach(); } catch { throw new Error('aws-operator-held'); } } }
}
