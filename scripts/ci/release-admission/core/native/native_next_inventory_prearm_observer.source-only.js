async function installHikyo158NextInventoryPrearmObserverV1(input) {
  'use strict';
  // SOURCE ONLY. Prearm on native IAM/KMS home; capture only original metadata routes.
  // Passive native metadata observation, never an API caller or
  // write authorizer. No JSON.parse, SDK/client, auth/config or secret values.
  const marker = '__hikyo158NextInventoryPrearmObserverV1';
  const account = '099256073061', region = 'eu-central-1';
  const keyPrefix = 'arn:aws:kms:' + region + ':' + account + ':key/';
  const oldIds = ['10908b8d-fe54-40e3-9210-a3eefea709b6','990c479f-d673-4bc9-8bca-7b9b86f4b205','fa374267-a456-4e79-b5b1-6cdf356b7d0e','bfa44dc1-ee8a-43b8-aec7-d8a7fbb38db1'];
  const roles = ['static', 'ambient', 'assume', 'web', 'kms-denied', 'source', 'oracle', 'cleanup'].map(n => 'hikyo-158-' + n);
  const setup = 'arn:aws:iam::' + account + ':policy/hikyo-158-setup-policy';
  const boundary = 'arn:aws:iam::' + account + ':policy/hikyo-158-role-boundary';
  const provider = 'arn:aws:iam::' + account + ':oidc-provider/token.actions.githubusercontent.com';
  const sources = {
    iam: { main: 'https://a.b.cdn.console.awsstatic.com/a/v1/EPYETUDGRYAX3B5DPL5Z2CIJYNSNNCGH7C7BCGW564I7W6F6ZYAQ/main.js',
      mainSHA: '85a9aed5a11dc8d019b95f399a1502b1c8e19affee5c933fc09623d0d359d0f9',
      vendors: 'https://a.b.cdn.console.awsstatic.com/a/v1/EPYETUDGRYAX3B5DPL5Z2CIJYNSNNCGH7C7BCGW564I7W6F6ZYAQ/vendors.js',
      vendorsSHA: 'e4cdd4a45e6d93e7d1e4c512cfa82eaf02bbb040280c93b1b0f20958118b4ba9' },
    kms: { main: 'https://a.b.cdn.console.awsstatic.com/a/v1/B35BNLNKUKKD7F5DTSPWUSOXQ4VAIKKIFWAKR2KD5MB4MCZXQ4RQ/main.js',
      mainSHA: '20a50d3f9e11cdae453b0f764689073f2adc6c644e6ac2a08660be5b1b2227a2', vendors: null, vendorsSHA: null }
  };
  const encoder = new TextEncoder(), decoder = new TextDecoder('utf-8', { fatal: true });
  const maxRequestBytes = 8192, maxResponseBytes = 32768, maxStoredBytes = 524288, maxExportBytes = 60000;
  const fail = code => { throw new Error('hikyo158-next-inventory:' + code); };
  const isSHA = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
  function exact(object, names) {
    if (!object || Object.getPrototypeOf(object) !== Object.prototype || Reflect.ownKeys(object).length !== names.length) fail('schema');
    const descriptors = Object.getOwnPropertyDescriptors(object);
    if (!names.every(name => Object.hasOwn(descriptors, name) && Object.hasOwn(descriptors[name], 'value')
      && descriptors[name].enumerable === true)) fail('data-fields');
    return Object.fromEntries(names.map(name => [name, descriptors[name].value]));
  }
  const manifest = exact(input, ['schema', 'service', 'account', 'region', 'reviewedPreparationSHA256',
    'publicMainSHA256','publicVendorsSHA256','expiresAtMS','notBeforeMS','stage','newKeyARN','boundaryPolicyId','roleIds','boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256','ownerContextSHA256']);
  if (manifest.schema !== 'hikyo-158/next-inventory-observer-manifest/v1' || !['iam', 'kms'].includes(manifest.service)
    || manifest.account !== account || manifest.region !== region || !isSHA(manifest.reviewedPreparationSHA256)
    || !Number.isInteger(manifest.expiresAtMS) || manifest.expiresAtMS <= Date.now()
    || manifest.expiresAtMS > manifest.notBeforeMS + 300000 || !Number.isInteger(manifest.notBeforeMS) || manifest.notBeforeMS>Date.now() || Date.now()-manifest.notBeforeMS>300000
    || !['baseline','inventory','restored-setup'].includes(manifest.stage) || manifest.service==='kms'&&manifest.stage==='restored-setup'
    || !isSHA(manifest.ownerContextSHA256)) fail('manifest');
  const baseline=manifest.stage==='baseline';
  if(baseline){
    if(!['newKeyARN','boundaryPolicyId','roleIds','boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256'].every(k=>manifest[k]===null))fail('baseline-no-created-identities');
  }else{
    if(typeof manifest.newKeyARN!=='string'||!new RegExp('^'+keyPrefix+'[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$').test(manifest.newKeyARN)
      ||oldIds.some(id=>manifest.newKeyARN===keyPrefix+id)||typeof manifest.boundaryPolicyId!=='string'||!/^ANPA[A-Z0-9]{8,64}$/.test(manifest.boundaryPolicyId)
      ||['ANPAROHA7PNSTPQUQEUTR','ANPAROHA7PNS5QVDJKPIN'].includes(manifest.boundaryPolicyId)
      ||!['boundPlanSHA256','creationCustodySHA256','newKeyCustodySHA256'].every(k=>isSHA(manifest[k])))fail('manifest');
    manifest.roleIds=Object.freeze(exact(manifest.roleIds,roles));
    if(!roles.every(r=>typeof manifest.roleIds[r]==='string'&&/^AROA[A-Z0-9]{8,64}$/.test(manifest.roleIds[r]))||new Set(Object.values(manifest.roleIds)).size!==8)fail('role-identities');
  }
  Object.freeze(manifest);
  const keys=baseline?[...oldIds]:[manifest.newKeyARN.slice(keyPrefix.length),...oldIds];
  function canonical(v){return v&&typeof v==='object'?'{'+Object.keys(v).sort().map(k=>JSON.stringify(k)+':'+canonical(v[k])).join(',')+'}':JSON.stringify(v);}
  if(!crypto?.subtle)fail('native-platform');
  const manifestSHA=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',encoder.encode(canonical(manifest)))),v=>v.toString(16).padStart(2,'0')).join('');
  const source = sources[manifest.service];
  if (manifest.publicMainSHA256 !== source.mainSHA || manifest.publicVendorsSHA256 !== source.vendorsSHA) fail('source-pins');
  if (Object.hasOwn(globalThis, marker) || ['__hikyo158NextInventoryObserverV1','__hikyo158NextSetupWholeReadPrearmObserverV1','__hikyo158NativeIAMWriteGuardV1', '__hikyo158RecoveryManagedVersionGuardV1',
    '__hikyo158NativeKMSXHRWriteGuardV1','__hikyo158NextFixtureIAMGuardV1','__hikyo158NextSetupVersionGuardV1','__hikyo158NextSetupWholeReadObserverV1','__hikyo158RecoveryMetadataObserverV1','__hikyo158RecoveryMetadataObserverV2'].some(name => Object.hasOwn(globalThis, name))) fail('fresh-readonly-document-required');
  if (!crypto?.subtle) fail('native-platform');
  if (manifest.service === 'iam') {
    if (!document.querySelector('#app') || !document.querySelector('[data-testid="iam-v2-vite"]')) fail('native-iam-anchors');
  } else if (globalThis.__isTangerineBoxHosted__ !== true) fail('native-kms-platform');
  function page() {
    if (manifest.service === 'iam') return location.origin === 'https://us-east-1.console.aws.amazon.com'
      && location.pathname === '/iam/home' && location.search === '?region=eu-central-1';
    return location.origin === 'https://eu-central-1.console.aws.amazon.com'
      && location.pathname === '/kms/home' && location.search === '?region=eu-central-1';
  }
  function accountBanner() {
    // Read only public navigation/account controls, never body/form/input text.
    return Array.from(document.querySelectorAll('header, nav, #awsc-nav-account-menu-button, [data-testid="awsc-nav-account-menu-button"]'))
      .some(node => [node.textContent, node.getAttribute('aria-label')].some(value =>
        typeof value === 'string' && new RegExp('(^|[^0-9])' + account + '([^0-9]|$)').test(value)));
  }
  if (!page() || !accountBanner() || !Array.from(document.scripts).some(s => s.src === source.main)
    || (source.vendors !== null && !Array.from(document.scripts).some(s => s.src === source.vendors))) fail('native-page-account-source');
  const endpoint = manifest.service === 'iam' ? 'https://iam.amazonaws.com/' : 'https://kms.eu-central-1.amazonaws.com/';
  const receipts = new Map(), claimed = new Set(), readers = new Set(), xhrSettlers = new Set();
  let expireObservation;
  const expired = new Promise(resolve => { expireObservation = resolve; });
  let closed = false, pending = 0, storedBytes = 0, skipped = 0, attempts = 0, captureError = null;
  const live = () => !closed && Date.now() < manifest.expiresAtMS;
  const hash = async bytes => Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes)),
    value => value.toString(16).padStart(2, '0')).join('');
  function route() {
    if (!page()) return false;
    let path;
    try { path = decodeURIComponent(location.hash); } catch { return false; }
    if (manifest.service === 'kms') return keys.some(id => [id, keyPrefix + id].some(value =>
      path === '#/kms/keys/' + value || ['keyPolicy', 'cryptographicConfiguration', 'keyRotation', 'aliases', 'tags', 'keyMaterial']
        .some(tab => path === '#/kms/keys/' + value + '/' + tab)));
    if(manifest.stage==='restored-setup')return /^#\/users\/details\/hikyo-158-setup(?:\?section=(?:permissions|security_credentials|groups|tags))?$/.test(path);
    if (baseline && path === '#/roles') return true;
    if (path === '#/identity_providers') return true;
    if (/^#\/users\/details\/hikyo-158-setup(?:\?section=(?:permissions|security_credentials|groups|tags))?$/.test(path)) return true;
    if ([boundary].some(arn => path === '#/policies/details/' + arn
      || ['permissions', 'versions', 'entities', 'tags'].some(tab => path === '#/policies/details/' + arn + '?section=' + tab))) return true;
    if (roles.some(role => path === '#/roles/details/' + role
      || ['permissions', 'trust_relationships', 'tags'].some(tab => path === '#/roles/details/' + role + '?' + 'section=' + tab))) return true;
    return path === '#/identity_providers/details/OPENID/' + provider;
  }
  function prearmRoute() { return page() && location.hash === (manifest.service === 'iam' ? '#/home' : '#/kms/home'); }
  if (!prearmRoute()||Date.now()>=manifest.expiresAtMS) fail('readonly-route-or-expiry');
  const context = () => live() && Date.now()>=manifest.notBeforeMS && route() && accountBanner();
  const schemas = {
    ListRoles: ['PathPrefix','MaxItems'], GetUser: ['UserName'], ListMFADevices: ['UserName', 'MaxItems'],
    ListAttachedUserPolicies: ['UserName', 'PathPrefix', 'MaxItems'],
    ListUserPolicies: ['UserName', 'MaxItems'], ListGroupsForUser: ['UserName', 'MaxItems'],
    ListAccessKeys: ['UserName', 'MaxItems'],
    GetPolicy: ['PolicyArn'], ListPolicyVersions: ['PolicyArn', 'MaxItems'], GetPolicyVersion: ['PolicyArn', 'VersionId'],
    GetRole: ['RoleName'], GetRolePolicy: ['RoleName', 'PolicyName'], ListRolePolicies: ['RoleName', 'MaxItems'],
    ListAttachedRolePolicies: ['RoleName', 'PathPrefix', 'MaxItems'], ListRoleTags: ['RoleName', 'MaxItems'],
    ListOpenIDConnectProviders: [], GetOpenIDConnectProvider: ['OpenIDConnectProviderArn']
  };
  async function iamScope(request,active,ownedReaders) {
    if (!request.body) return null;
    const reader = request.body.getReader();
    readers.add(reader);ownedReaders.add(reader);
    const pairs = new Map(), bytes = [];
    let key = [], value = [], inValue = false, action = null, allowed = ['Action'], total = 0;
    function finish() {
      const name = decoder.decode(Uint8Array.from(key));
      if (!inValue || pairs.has(name) || !allowed.includes(name)) fail('query-shape');
      const text = decoder.decode(Uint8Array.from(value));
      const decoded = decodeURIComponent(text.replace(/\+/g, ' '));
      if (name === 'Action') {
        // This is the only decoded value before an exact metadata Action is
        // known. A password-bearing write stops here, before later values.
        if (!Object.hasOwn(schemas, decoded)) fail('not-metadata-action');
        action = decoded;
        allowed = ['Action', 'Version', ...schemas[action]];
      }
      pairs.set(name, decoded); key = []; value = []; inValue = false;
    }
    try {
      while (context()&&active()) {
        const item = await reader.read();
        if (item.done) break;
        if (!(item.value instanceof Uint8Array)) fail('query-bytes');
        for (const byte of item.value) {
          if (++total > maxRequestBytes) fail('query-size');
          // Buffer only bytes belonging to a known field. Unknown fields stop
          // at '=' before their values are inspected or decoded.
          if (byte === 61 && !inValue) {
            const name = decoder.decode(Uint8Array.from(key));
            if (!allowed.includes(name) || pairs.has(name) || (pairs.size === 0 && name !== 'Action')) fail('query-field');
            inValue = true;
          } else if (byte === 38) finish();
          else if (inValue) value.push(byte);
          else { if (key.length >= 64) fail('query-key-size'); key.push(byte); }
          bytes.push(byte);
        }
      }
      if (!context()||!active()) return null;
      finish();
      if (!action || pairs.get('Version') !== '2010-05-08') return null;
      const selectors = Object.fromEntries([...pairs].filter(([name]) => !['Action', 'Version'].includes(name)));
      if ('UserName' in selectors && selectors.UserName !== 'hikyo-158-setup') return null;
      if (action.includes('User') || ['ListMFADevices', 'ListAccessKeys', 'ListGroupsForUser'].includes(action))
        if (selectors.UserName !== 'hikyo-158-setup') return null;
      if ('RoleName' in selectors && !roles.includes(selectors.RoleName)) return null;
      if (action.includes('Role') && action!=='ListRoles' && !roles.includes(selectors.RoleName)) return null;
      if ('PolicyName' in selectors && selectors.PolicyName !== 'hikyo-158-fixture') return null;
      if (action === 'GetRolePolicy' && selectors.PolicyName !== 'hikyo-158-fixture') return null;
      if(action==='ListRoles'&&!baseline)return null;
      if(baseline&&!['GetUser','ListMFADevices','ListAttachedUserPolicies','ListUserPolicies','ListGroupsForUser','ListAccessKeys','ListRoles','GetPolicy','ListOpenIDConnectProviders'].includes(action))return null;
      if(manifest.stage==='restored-setup'&&!['GetUser','ListMFADevices','ListAttachedUserPolicies','ListUserPolicies','ListGroupsForUser','ListAccessKeys'].includes(action))return null;
      if (['GetPolicy','ListPolicyVersions','GetPolicyVersion'].includes(action)&&selectors.PolicyArn!==boundary)return null;
      if (action === 'GetPolicyVersion' && !['v1'].includes(selectors.VersionId)) return null;
      if (action === 'GetOpenIDConnectProvider' && selectors.OpenIDConnectProviderArn !== provider) return null;
      if ('PathPrefix' in selectors && selectors.PathPrefix !== '/') return null;
      if ('MaxItems' in selectors && !['100','1000'].includes(selectors.MaxItems)) return null;
      const body = Uint8Array.from(bytes), bodySHA256 = await hash(body);
      let slot = action==='ListRoles'?'roles':selectors.RoleName ? 'role.' + selectors.RoleName : selectors.PolicyArn === setup ? 'setup'
        : selectors.PolicyArn === boundary ? 'boundary' : action.includes('OpenIDConnect') ? 'provider' : 'user';
      slot += '.' + action + (selectors.VersionId ? '.' + selectors.VersionId : '');

      if(claimed.has(slot))return null;claimed.add(slot);
      return { slot, action, requestBodySHA256: bodySHA256, requestBodyBytes: body.byteLength,
        selectors, pageMarkerPresent:false, pageMarkerSHA256:null };
    } catch { return null; }
    finally { readers.delete(reader);ownedReaders.delete(reader);void reader.cancel().catch(() => {}); }
  }
  function kmsScope(record, body) {
    if (record.target !== 'TrentService.DescribeKey' && !['TrentService.GetKeyPolicy', 'TrentService.GetKeyRotationStatus', 'TrentService.ListAliases'].includes(record.target)) return null;
    if (typeof body !== 'string' || body.length > maxRequestBytes || encoder.encode(body).byteLength > maxRequestBytes) return null;
    for (let index = 0; index < keys.length; index++) {
      for (const key of [keys[index], keyPrefix + keys[index]]) {
        const action = record.target.slice('TrentService.'.length);
        if ((baseline || index !== 0) && action !== 'DescribeKey') continue;
        const variants = action === 'GetKeyPolicy' ? [JSON.stringify({ KeyId: key, PolicyName: 'default' })]
          : action === 'ListAliases' ? [JSON.stringify({ KeyId: key }), JSON.stringify({ KeyId: key, Limit: 100 }), JSON.stringify({ Limit: 100, KeyId: key })]
            : [JSON.stringify({ KeyId: key })];
        if (variants.includes(body)) return { slot: (baseline?'old'+(index+1):index === 0 ? 'new' : 'old' + index) + '.' + action,
          action, selectors: { keyARN: keyPrefix + keys[index] }, pageMarkerPresent: false, pageMarkerSHA256: null };
      }
    }
    return null;
  }
  async function responseBytes(response,active,ownedReaders) {
    if (!response.body) fail('empty-response');
    const reader = response.body.getReader();readers.add(reader);ownedReaders.add(reader);
    const chunks = []; let size = 0;
    try {
      while (context()&&active()) {
        const item = await reader.read();
        if (item.done) break;
        if (!(item.value instanceof Uint8Array) || (size += item.value.byteLength) > maxResponseBytes) fail('response-size');
        chunks.push(item.value.slice());
      }
      if (!context()||!active()||size<2) fail('response-expired-or-empty');
      const result = new Uint8Array(size); let offset = 0;
      for (const chunk of chunks) { result.set(chunk, offset); offset += chunk.byteLength; }
      return result;
    } finally { readers.delete(reader);ownedReaders.delete(reader);void reader.cancel().catch(() => {}); }
  }
  async function save(scope, bytes, sentAtMS, receivedAtMS, responseStatus=200) {
    if (!context() || receipts.has(scope.slot) || bytes.byteLength < 2 || bytes.byteLength > maxResponseBytes) return;
    const rawResponse = decoder.decode(bytes);
    if(/SecretString|SecretBinary|SecretAccessKey|AccessKeyId|SessionToken/.test(rawResponse))fail('unexpected-value-or-credential-field');
    const responseSHA256 = await hash(bytes);
    if(receivedAtMS<sentAtMS||Date.now()<receivedAtMS||Date.now()-sentAtMS>20000||sentAtMS<manifest.notBeforeMS)fail('capture-time');
    const receipt = Object.freeze({ schema:'hikyo-158/next-inventory-native-response/v1',stage:manifest.stage,account,region,manifestSHA256:manifestSHA,notBeforeMS:manifest.notBeforeMS,expiresAtMS:manifest.expiresAtMS,
      boundPlanSHA256:manifest.boundPlanSHA256,creationCustodySHA256:manifest.creationCustodySHA256,newKeyCustodySHA256:manifest.newKeyCustodySHA256,ownerContextSHA256:manifest.ownerContextSHA256, service: manifest.service,
      ...scope, selectors: Object.freeze({ ...scope.selectors }), status: responseStatus, responseEndpoint: endpoint, sentAtMS, receivedAtMS,
      rawResponseBytes: bytes.byteLength, rawResponseSHA256: responseSHA256, rawResponse,
      reviewedPreparationSHA256: manifest.reviewedPreparationSHA256, publicMainSHA256: source.mainSHA,
      publicVendorsSHA256: source.vendorsSHA, JSONParsedByThisCode: false, providerAPIsCalledByThisCode: false,
      ownerProofComplete: false, nativeWriteAuthorizationOwnedByObserver: false });
    if (!context() || storedBytes + bytes.byteLength > maxStoredBytes || encoder.encode(JSON.stringify(receipt)).byteLength > maxExportBytes) fail('storage-or-export-size');
    storedBytes += bytes.byteLength; receipts.set(scope.slot, receipt);
  }
  function status() {
    return Object.freeze({ schema:'hikyo-158/next-inventory-observer-status/v1', service: manifest.service,
      phase: live() ? 'observing-readonly' : 'expired', readySlots: [...receipts.keys()].sort(), pendingCaptures: pending,
      storedBytes, skipped, captureError, expiresAtMS: manifest.expiresAtMS,
      maxResponseBytes, maxStoredBytes, maxExportBytes, reviewedPreparationSHA256: manifest.reviewedPreparationSHA256,
      providerAPIsCalledByThisCode: false, JSONParsedByThisCode: false, ownerProofComplete: false,
      ownerIdentityVerifiedByThisCode: false, publicSourceBytesVerifiedByThisCode: false,
      nativeWriteAuthorizationOwnedByObserver:false,coversCustomFetchOrOtherRealms:false,redirectOrHTTPHopBoundEstablished:false,cleanup:'full-navigation-controlled-tab-preserves-session' });
  }
  function read(slot) {
    if (typeof slot !== 'string' || !receipts.has(slot)) fail('unknown-receipt-slot');
    if (!live()) return { status: 'expired', ownerProofComplete: false };
    const receipt = receipts.get(slot);
    if (Date.now() - receipt.sentAtMS > 300000 || Date.now() < receipt.sentAtMS) return { status: 'stale', ownerProofComplete: false };
    return receipt;
  }
  if (manifest.service === 'iam') {
    if (typeof fetch !== 'function' || typeof Request !== 'function') fail('fetch-platform');
    const originalFetch = globalThis.fetch;
    function observedFetch(request) {
      let candidate = null;
      try {
        if (arguments.length === 1 && request instanceof Request && context() && request.method === 'POST'
          && request.url === endpoint && request.headers.get('content-type') === 'application/x-www-form-urlencoded'
          && request.headers.get('x-amz-target') === null && pending < 16 && attempts < 256) candidate = request.clone();
      } catch {}
      const sentAtMS = Date.now();
      const result = Reflect.apply(originalFetch, this, arguments);
      if (candidate) {
        attempts++; pending++;
        let scopeResolved=false,knownScope=null,active=true;const ownedReaders=new Set();
        let stop;const timedOut=new Promise(resolve=>{stop=()=>{active=false;for(const reader of ownedReaders)void reader.cancel().catch(()=>{});resolve(null);};});const timer=setTimeout(stop,Math.min(20000,manifest.expiresAtMS-Date.now()));
        const scopePromise = iamScope(candidate,()=>active,ownedReaders).then(scope => { knownScope = scope; scopeResolved = true; return scope; });
        // Never copy an unknown response, including possible write credentials.
        // A fast response before classification is deliberately not captured.
        // Do not delay the original native promise to wait for classification.
        const responsePromise = result.then(response => {
          try {
            if (!scopeResolved || !knownScope || !active || !context() || !(response.status===200 || baseline && knownScope.slot==='boundary.GetPolicy' && response.status===404) || response.url !== endpoint) return null;
            return {response:response.clone(),receivedAtMS:Date.now(),status:response.status};
          } catch { captureError = 'native-response-clone-held'; return null; }
        }, () => null);
        void (async () => {
          let response = null;
          try {
            const scope = await Promise.race([scopePromise,expired,timedOut]);
            response = await Promise.race([responsePromise,expired,timedOut]);
            if (!scope || !response || !active || !context()) { skipped++; return; }
            const bytes=await responseBytes(response.response,()=>active,ownedReaders);
            if(active)await save(scope,bytes,sentAtMS,response.receivedAtMS,response.status);
          } catch { captureError = 'bounded-metadata-capture-held'; }
          finally { if(response?.response?.body&&!response.response.body.locked)void response.response.body.cancel().catch(()=>{});for(const reader of ownedReaders)void reader.cancel().catch(()=>{}); pending--;active=false;clearTimeout(timer); }
        })();
      }
      // Exact original native promise identity, arguments and Request retained.
      return result;
    }
    Object.defineProperty(globalThis, 'fetch', { value: observedFetch, configurable: false, writable: false });
  } else {
    if (typeof XMLHttpRequest !== 'function') fail('xhr-platform');
    const proto = XMLHttpRequest.prototype, records = new WeakMap();
    const originalOpen = proto.open, originalHeader = proto.setRequestHeader, originalSend = proto.send;
    function observedOpen(method, url, asynchronous) {
      let record = null;
      if (arguments.length <= 3 && (arguments.length < 3 || asynchronous === true)
        && method === 'POST' && url === endpoint) record = { type: null, target: null, sent: false };
      records.set(this, record);
      return Reflect.apply(originalOpen, this, arguments);
    }
    function observedHeader(name, value) {
      const record = records.get(this);
      if (record && !record.sent && typeof name === 'string') {
        const key = name.toLowerCase() === 'content-type' ? 'type' : name.toLowerCase() === 'x-amz-target' ? 'target' : null;
        if (key) { if (record[key] !== null || typeof value !== 'string' || value.length > 128) records.set(this, null); else record[key] = value; }
      }
      return Reflect.apply(originalHeader, this, arguments);
    }
    function observedSend(body) {
      const xhr = this, record = records.get(this);
      let scope = null;
      try {
        if (context() && record && !record.sent && record.type === 'application/x-amz-json-1.1' && pending < 16 && attempts < 256) {
          record.sent = true; scope = kmsScope(record, body);
        }
      } catch { captureError = 'request-classification-held'; }
      let settleCapture = null;
      if (scope && !claimed.has(scope.slot)) {
        attempts++; claimed.add(scope.slot); pending++;
        const sentAtMS = Date.now();
        let settled=false;const timer=setTimeout(()=>settleCapture(),Math.min(20000,manifest.expiresAtMS-Date.now()));
        settleCapture = () => { if (!settled) { settled=true;clearTimeout(timer);pending--;xhrSettlers.delete(settleCapture); } };
        xhrSettlers.add(settleCapture);
        xhr.addEventListener('loadend', () => {
          if (settled) return;
          void (async () => {
            try {
              if (!context() || xhr.status !== 200 || xhr.responseURL !== endpoint) { skipped++; return; }
              let bytes;
              if (xhr.responseType === 'arraybuffer') {
                const value=xhr.response;if(!(value instanceof ArrayBuffer)||value.byteLength>maxResponseBytes)fail('xhr-size');
                bytes=new Uint8Array(value).slice();
              } else if (xhr.responseType === '' || xhr.responseType === 'text') {
                const value=xhr.responseText;if(typeof value!=='string'||value.length>maxResponseBytes)fail('xhr-text-size');
                bytes=encoder.encode(value);
              } else fail('xhr-response-type');
              await save({ ...scope, requestBodySHA256: await hash(encoder.encode(body)), requestBodyBytes: encoder.encode(body).byteLength }, bytes, sentAtMS, Date.now());
            } catch { captureError = 'bounded-metadata-capture-held'; }
            finally { settleCapture(); }
          })();
        }, { once: true });
      }
      try { return Reflect.apply(originalSend, xhr, arguments); }
      catch (error) { if (settleCapture) settleCapture(); throw error; }
    }
    for (const [name, value] of [['open', observedOpen], ['setRequestHeader', observedHeader], ['send', observedSend]])
      Object.defineProperty(proto, name, { value, configurable: false, writable: false });
  }
  Object.defineProperty(globalThis, marker, { value: Object.freeze({ status, read }), configurable: false, writable: false });
  setTimeout(() => {
    closed = true; receipts.clear(); claimed.clear(); storedBytes = 0;
    expireObservation(null);
    for (const settle of xhrSettlers) settle();
    xhrSettlers.clear();
    for (const reader of readers) void reader.cancel().catch(() => {});
    readers.clear();
  }, manifest.expiresAtMS - Date.now());
  return status();
}
