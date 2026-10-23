function installHikyo158NextSetupVersionGuardV1(manifest) {
  'use strict';
  // SOURCE ONLY. Separate fresh reviewed once-intent for each exact owner operation. No activation operation.
  const marker = '__hikyo158NextSetupVersionGuardV1';
  const policyARN = 'arn:aws:iam::099256073061:policy/hikyo-158-setup-policy';
  const policyId = 'ANPAROHA7PNSTPQUQEUTR', userName = 'hikyo-158-setup';
  const expected = ['schema', 'operation', 'targetARN', 'expectedPolicyId', 'currentDefaultVersionId',
    'wireBodySHA256', 'wireBodyBytes', 'intentSHA256', 'reviewedPreparationSHA256',
    'publicMainSHA256', 'publicVendorsSHA256', 'expiresAtMS', 'timeoutMS', 'bodyPrefix', 'writeRoute', 'readProbeRoute',
    'targetVersionId', 'ownerFullPolicyProofSHA256', 'archivedV1V5ProofSHA256', 'proofObservedAtMS', 'retirementProofSHA256'];
  function fail(code) { throw new Error('hikyo158-next-setup-version:' + code); }
  const digestLike = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
  if (!manifest || typeof manifest !== 'object' || Array.isArray(manifest) || Reflect.ownKeys(manifest).length !== expected.length)
    fail('manifest-fields');
  const descriptors = Object.getOwnPropertyDescriptors(manifest);
  if (!expected.every(key => Object.hasOwn(descriptors, key) && Object.hasOwn(descriptors[key], 'value') && descriptors[key].enumerable))
    fail('manifest-data-fields');
  if (manifest.schema !== 'hikyo-158/next-setup-version-write-manifest/v1' ||
      !['DeletePolicyVersion', 'SetDefaultPolicyVersion'].includes(manifest.operation)) fail('unsupported-operation');
  const deletion = manifest.operation === 'DeletePolicyVersion';
  if (manifest.targetARN !== policyARN || manifest.expectedPolicyId !== policyId || manifest.currentDefaultVersionId !== (deletion ? 'v5' : 'v6') ||
      manifest.targetVersionId !== (deletion ? 'v1' : 'v5')) fail('target-identity');
  if (!digestLike(manifest.ownerFullPolicyProofSHA256) || manifest.archivedV1V5ProofSHA256 !== 'c794a6c61e131c972ea5e201bc6e6189fdb974acea91be362dfb984d490a6c46' ||
      (deletion ? manifest.retirementProofSHA256 !== null : !digestLike(manifest.retirementProofSHA256)) ||
      !Number.isInteger(manifest.proofObservedAtMS) || manifest.proofObservedAtMS > Date.now() || manifest.proofObservedAtMS < Date.now() - 300000) fail('external-full-proof-binding');
  if (![manifest.wireBodySHA256, manifest.intentSHA256, manifest.reviewedPreparationSHA256,
      manifest.publicMainSHA256, manifest.publicVendorsSHA256].every(digestLike)) fail('manifest-hashes');
  if (manifest.publicMainSHA256 !== '85a9aed5a11dc8d019b95f399a1502b1c8e19affee5c933fc09623d0d359d0f9' ||
      manifest.publicVendorsSHA256 !== 'e4cdd4a45e6d93e7d1e4c512cfa82eaf02bbb040280c93b1b0f20958118b4ba9') fail('public-source-pins');
  if (!Number.isInteger(manifest.wireBodyBytes) || manifest.wireBodyBytes < 32 || manifest.wireBodyBytes > 512 ||
      !Number.isInteger(manifest.expiresAtMS) || manifest.expiresAtMS <= Date.now() || manifest.expiresAtMS > Date.now() + 300000 ||
      !Number.isInteger(manifest.timeoutMS) || manifest.timeoutMS < 1000 || manifest.timeoutMS > 20000) fail('manifest-bounds');
  if (manifest.bodyPrefix !== 'Action=' + manifest.operation + '&Version=2010-05-08&') fail('serializer-prefix');
  if (location.origin !== 'https://us-east-1.console.aws.amazon.com' || location.pathname !== '/iam/home' ||
      !['?region=eu-central-1', '?region=us-east-1', ''].includes(location.search)) fail('document-origin');
  if ([marker, '__hikyo158NativeIAMWriteGuardV1', '__hikyo158RecoveryManagedVersionGuardV1',
      '__hikyo158RecoveryMetadataObserverV1', '__hikyo158RecoveryMetadataObserverV2', '__hikyo158BoundaryDeleteGuardV1'].some(name => Object.hasOwn(globalThis, name))) fail('already-installed');
  if (typeof Request !== 'function' || typeof XMLHttpRequest !== 'function' || typeof fetch !== 'function' ||
      !crypto?.subtle || typeof TextDecoder !== 'function' || typeof AbortController !== 'function') fail('platform');
  const policyRoutes = [policyARN.replace('/', '%2F'), encodeURIComponent(policyARN)].flatMap(encoded =>
    ['', '?section=permissions', '?section=versions', '?section=entities'].map(suffix => '#/policies/details/' + encoded + suffix));
  const versionRoutes = policyRoutes.filter(route => route.endsWith('?section=versions'));
  const allowedWriteRoutes = versionRoutes;
  const ownUserProbeRoutes = ['#/users/details/' + userName, '#/users/details/' + userName + '?section=permissions'];
  if (!allowedWriteRoutes.includes(manifest.writeRoute) || !ownUserProbeRoutes.includes(manifest.readProbeRoute) ||
      location.hash !== manifest.writeRoute) fail('reviewed-route-binding');
  function accountBanner() {
    return Array.from(document.querySelectorAll('header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]'))
      .some(node => [node.textContent, node.getAttribute('aria-label')].some(value => typeof value === 'string' &&
        /(^|[^0-9])(?:099256073061|0992-5607-3061)([^0-9]|$)/.test(value)));
  }
  if (!accountBanner()) fail('public-account-binding');
  const main = 'https://a.b.cdn.console.awsstatic.com/a/v1/EPYETUDGRYAX3B5DPL5Z2CIJYNSNNCGH7C7BCGW564I7W6F6ZYAQ/main.js';
  if (!Array.from(document.scripts).some(s => s.src === main) ||
      !Array.from(document.scripts).some(s => s.src === main.replace('/main.js', '/vendors.js')) ||
      !document.querySelector('#app') || !document.querySelector('[data-testid="iam-v2-vite"]')) fail('loaded-module');
  const approved = Object.freeze({ ...manifest }), installRoute = manifest.writeRoute;
  const nativeFetch = globalThis.fetch;
  const state = { phase: 'observing', claimConsumed: false, forwards: 0, blocked: 0, activeReads: 0,
    observedScopedReads: 0, observedOwnUserReads: 0, lastReadAction: null, responseStatus: null, reason: null };
  const hex = bytes => Array.from(new Uint8Array(bytes), value => value.toString(16).padStart(2, '0')).join('');
  function receipt() {
    return Object.freeze({ schema: 'hikyo-158/next-setup-version-write-status/v1', ...state,
      operation: approved.operation, targetARN: policyARN, expectedPolicyId: policyId, currentDefaultVersionId: approved.currentDefaultVersionId, targetVersionId: approved.targetVersionId,
      ownerFullPolicyProofSHA256: approved.ownerFullPolicyProofSHA256, archivedV1V5ProofSHA256: approved.archivedV1V5ProofSHA256,
      retirementProofSHA256: approved.retirementProofSHA256, proofObservedAtMS: approved.proofObservedAtMS,
      intentSHA256: approved.intentSHA256, wireBodySHA256: approved.wireBodySHA256, armedUntilMS: approved.expiresAtMS,
      reviewedPreparationSHA256: approved.reviewedPreparationSHA256,
      preparationAndFullPolicyProofVerifiedByPage: false, policyIdentityAndUsageVerifiedByPage: false,
      loadedSourceBytesVerifiedByPage: false, ownerIdentityVerifiedByPage: false, durabilityVerifiedByPage: false,
      coversCustomFetchOrOtherRealms: false, cleanup: 'close-controlled-tab' });
  }
  function blocked(code) { state.blocked++; fail(code); }
  function hold(code) { state.phase = 'held'; state.reason = code; }
  function destination(value) {
    const url = new URL(value, location.href);
    if (url.username || url.password || url.hash || url.href.length > 4096) fail('request-url-shape');
    return url;
  }
  async function boundedBody(request, limit, signal) {
    const reader = request.clone().body?.getReader();
    if (!reader) fail('missing-body');
    let count = 0;
    const chunks = [];
    function cancel() { void reader.cancel().catch(() => {}); }
    signal.addEventListener('abort', cancel, { once: true });
    try {
      while (true) {
        if (signal.aborted) fail('body-aborted');
        const result = await reader.read();
        if (signal.aborted) fail('body-aborted');
        if (result.done) break;
        count += result.value.byteLength;
        if (count > limit) fail('body-size');
        chunks.push(result.value);
      }
      const bytes = new Uint8Array(count);
      let offset = 0;
      for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
      return { bytes, text: new TextDecoder('utf-8', { fatal: true }).decode(bytes) };
    } finally {
      signal.removeEventListener('abort', cancel);
      cancel();
    }
  }
  // Decode only public action/key selectors first. Unknown credential/password
  // field names refuse before decoding any associated values.
  function selectors(text, names) {
    const raw = text.split('&');
    const entries = [];
    const seen = new Set();
    for (const pair of raw) {
      const equal = pair.indexOf('=');
      if (equal < 1) fail('query-shape');
      const name = decodeURIComponent(pair.slice(0, equal).replace(/\+/g, ' '));
      if (!names.has(name) || seen.has(name)) fail('query-fields');
      seen.add(name); entries.push([name, pair.slice(equal + 1)]);
    }
    return new Map(entries.map(([name, value]) => [name, decodeURIComponent(value.replace(/\+/g, ' '))]));
  }
  function validateWrite(text) {
    if (text !== approved.bodyPrefix + 'PolicyArn=' + encodeURIComponent(policyARN) + '&VersionId=' + approved.targetVersionId) fail('wire-shape');
    const names = new Set(['Action', 'Version', 'PolicyArn']);
    names.add('VersionId');
    const fields = selectors(text, names);
    if (fields.get('Action') !== approved.operation || fields.get('Version') !== '2010-05-08' || fields.get('PolicyArn') !== policyARN ||
        fields.size !== 4 || fields.get('VersionId') !== approved.targetVersionId) fail('delete-binding');
  }
  function scopedRead(text) {
    const fields = selectors(text, new Set(['Action', 'Version', 'UserName', 'PolicyArn', 'VersionId', 'MaxItems']));
    const action = fields.get('Action');
    if (fields.get('Version') !== '2010-05-08') fail('read-version');
    if (['GetUser', 'ListAttachedUserPolicies', 'ListUserPolicies', 'ListGroupsForUser', 'ListAccessKeys', 'ListMFADevices'].includes(action)) {
      if (fields.size !== 3 || fields.get('UserName') !== userName || !ownUserProbeRoutes.includes(location.hash)) fail('read-user-scope');
    } else if (['GetPolicy', 'ListPolicyVersions', 'GetPolicyVersion'].includes(action)) {
      if (!policyRoutes.includes(location.hash) || fields.get('PolicyArn') !== policyARN ||
          fields.size !== (action === 'GetPolicyVersion' || (action === 'ListPolicyVersions' && fields.has('MaxItems')) ? 4 : 3) ||
          (fields.has('MaxItems') && (action !== 'ListPolicyVersions' || fields.get('MaxItems') !== '1000')) ||
          (action === 'GetPolicyVersion' && !(deletion ? ['v1', 'v2', 'v3', 'v4', 'v5'] : ['v2', 'v3', 'v4', 'v5', 'v6']).includes(fields.get('VersionId')))) fail('read-policy-scope');
    } else fail('unapproved-read-action');
    return action;
  }
  async function guardedFetch(request, init) {
    if (!(request instanceof Request) || init !== undefined) return blocked('request-form');
    let url;
    try { url = destination(request.url); } catch { return blocked('request-url'); }
    if (url.origin !== 'https://iam.amazonaws.com' || url.pathname !== '/' || url.search) return blocked('unapproved-endpoint');
    if (request.method !== 'POST' || request.headers.get('content-type') !== 'application/x-www-form-urlencoded' || request.headers.has('x-amz-target'))
      return blocked('iam-protocol');
    if (!['observing', 'armed'].includes(state.phase)) return blocked('gate-closed');
    const mutation = state.phase === 'armed';
    // Burn before ANY async clone/read/hash. A background read can consume this
    // one attempt; that holds safely and is never automatically refunded.
    if (mutation) { state.claimConsumed = true; state.phase = 'checking'; }
    else state.activeReads++;
    const controller = new AbortController();
    const abort = () => controller.abort();
    request.signal.addEventListener('abort', abort, { once: true });
    if (request.signal.aborted) controller.abort();
    const deadline = Math.min(Date.now() + approved.timeoutMS, approved.expiresAtMS);
    let timeout;
    const timedOut = new Promise((resolve, reject) => {
      timeout = setTimeout(() => {
        if (mutation) hold('deadline');
        controller.abort(); reject(new Error('hikyo158-next-setup-version:deadline'));
      }, Math.max(1, deadline - Date.now()));
    });
    async function perform() {
      try {
        const body = await boundedBody(request, mutation ? approved.wireBodyBytes : 2048, controller.signal);
        if (controller.signal.aborted || Date.now() >= deadline) fail('deadline');
        if (!mutation) {
          const action = scopedRead(body.text);
          if (state.phase !== 'observing' || Date.now() >= approved.expiresAtMS) fail('read-phase-changed');
          const response = await nativeFetch(new Request(request, { redirect: 'error', signal: controller.signal }));
          if (response.status === 200 && !controller.signal.aborted) {
            state.observedScopedReads++; state.lastReadAction = action;
            if (action === 'GetUser' && ownUserProbeRoutes.includes(location.hash)) state.observedOwnUserReads++;
          }
          return response;
        }
        if (Date.now() >= approved.expiresAtMS || location.hash !== installRoute || !accountBanner() || Date.now() - approved.proofObservedAtMS > 300000 || Date.now() < approved.proofObservedAtMS) fail('intent-expired-or-route-changed');
        if (body.bytes.byteLength !== approved.wireBodyBytes) fail('body-size');
        validateWrite(body.text);
        if (hex(await crypto.subtle.digest('SHA-256', body.bytes)) !== approved.wireBodySHA256) fail('wire-hash');
        if (controller.signal.aborted || Date.now() >= deadline ||
            Date.now() >= approved.expiresAtMS || location.hash !== installRoute || !accountBanner() || state.phase !== 'checking' || Date.now() - approved.proofObservedAtMS > 300000 || Date.now() < approved.proofObservedAtMS) fail('deadline');
        state.phase = 'forwarded'; state.forwards++;
        const response = await nativeFetch(new Request(request, { redirect: 'error', signal: controller.signal }));
        if (controller.signal.aborted || state.phase !== 'forwarded') fail('deadline');
        state.responseStatus = response.status; state.phase = 'settled';
        return response;
      } catch {
        if (mutation && state.phase !== 'held') hold('consumed-operation-needs-reconciliation');
        fail('held-operation');
      }
    }
    try { return await Promise.race([perform(), timedOut]); }
    finally {
      clearTimeout(timeout); request.signal.removeEventListener('abort', abort);
      if (!mutation) state.activeReads--;
    }
  }
  function arm(intentSHA256) {
    if (state.phase !== 'observing' || state.claimConsumed || state.observedOwnUserReads < 1 || state.activeReads !== 0 ||
        intentSHA256 !== approved.intentSHA256 || Date.now() >= approved.expiresAtMS ||
        location.hash !== installRoute || !accountBanner() || Date.now() - approved.proofObservedAtMS > 300000 || Date.now() < approved.proofObservedAtMS) fail('cannot-arm');
    state.phase = 'armed';
    return receipt();
  }
  const control = Object.freeze({ status: receipt, arm });
  Object.defineProperty(XMLHttpRequest.prototype, 'send', { configurable: false, writable: false,
    value: function () { return blocked('xhr-denied'); } });
  Object.defineProperty(globalThis, 'fetch', { configurable: false, writable: false, value: guardedFetch });
  Object.defineProperty(globalThis, marker, { configurable: false, writable: false, value: control });
  return receipt();
}
