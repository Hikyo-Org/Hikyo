import assert from 'node:assert/strict';
import { withBrowserLogout } from './frozen-client-cleanup.mjs';
import { createHash, randomBytes } from 'node:crypto';
import { existsSync, readFileSync, realpathSync } from 'node:fs';
import { registerHooks } from 'node:module';
import { resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const root = pathToFileURL(`${realpathSync(resolve(process.argv[2]))}/`).href;
// The frozen generator emits extensionless relative imports for bundlers.
// Resolve those at runtime without rewriting or regenerating the old client.
registerHooks({
  resolve(specifier, context, nextResolve) {
    if (context.parentURL?.startsWith(root) && specifier.startsWith('.')) {
      const url = new URL(specifier, context.parentURL);
      for (const suffix of ['.ts', '/index.ts']) {
        const candidate = `${url.href}${suffix}`;
        if (existsSync(fileURLToPath(candidate))) {
          return nextResolve(candidate, context);
        }
      }
    }
    return nextResolve(specifier, context);
  },
});
const sdk = await import(`${root}src/generated/sdk.gen.ts`);
async function call(operation, options) {
  try { return await sdk[operation](options); }
  catch (error) { throw new Error(`frozen ${operation} failed: ${error?.error?.code ?? 'transport or decode'}`); }
}
const z = await import(`${root}src/generated/zod.gen.ts`);
const { client } = await import(`${root}src/generated/client.gen.ts`);
// Fixture credentials travel over stdin, never argv or diagnostic output.
const fixture = JSON.parse(readFileSync(0, 'utf8'));
client.setConfig({ baseUrl: fixture.origin, throwOnError: true });
const meta = z.zMeta.parse((await call('getMeta')).data);
assert.equal(meta.server_version, 'current-server-skew-fixture');
assert.ok(meta.api_revision >= 1);
async function enrolledLogin() {
  client.setConfig({ headers: { Authorization: null } });
  const refused = await call('localLogin', {
    body: { username: fixture.username, password: fixture.password, artifact: 'cli' }, throwOnError: false,
  });
  assert.equal(refused.response.status, 400, 'an enrolled factor cannot be skipped');
  assert.equal(z.zError.parse(refused.error).error.code, 'bad_request');
  const challengeResponse = await call('localLogin', {
    body: { username: fixture.username, password: fixture.password, artifact: 'browser' },
  });
  assert.equal(challengeResponse.response.status, 202);
  assert.equal(challengeResponse.response.headers.getSetCookie().length, 0);
  const challenge = z.zLoginChallenge.parse(challengeResponse.data);
  assert.ok(challenge.factors.includes('totp'));
  const completed = await call('loginChallengeTotp', {
    path: { challenge: challenge.challenge_id }, body: { code: fixture.codes.shift() },
  });
  const browser = z.zLoginResult.parse(completed.data);
  assert.equal(browser.session.artifact, 'browser');
  assert.ok(browser.session_token === undefined, 'browser login must not return a bearer');
  assert.equal(browser.principal.id, fixture.principal);
  const cookies = completed.response.headers.getSetCookie().map(value => value.split(';', 1)[0]);
  const csrf = cookies.find(value => value.startsWith('__Host-hikyo-csrf='))?.slice('__Host-hikyo-csrf='.length);
  assert.ok(csrf);
  assert.ok(cookies.some(value => value.startsWith('__Host-hikyo=')));
  const browserHeaders = { Cookie: cookies.join('; '), 'X-Hikyo-CSRF': csrf };
  return withBrowserLogout(async () => {
    const state = randomBytes(32).toString('base64url');
    const verifier = randomBytes(32).toString('base64url');
    const redirect = 'http://127.0.0.1:41234/callback';
    const started = z.zCliLoginStart.parse((await call('startCliLogin', {
      body: { transport: 'loopback', state, pkce_challenge: createHash('sha256').update(verifier).digest('base64url'), redirect_uri: redirect },
    })).data);
    assert.equal(started.state, state);
    const approved = z.zCliLoginApproved.parse((await call('approveCliLogin', {
      headers: browserHeaders, body: { reference: state },
    })).data);
    assert.equal(approved.state, state);
    assert.equal(approved.redirect_uri, redirect);
    assert.ok(approved.code);
    const result = z.zLoginResult.parse((await call('redeemCliLogin', {
      body: { code: approved.code, pkce_verifier: verifier, redirect_uri: redirect },
    })).data);
    assert.equal(result.session.artifact, 'cli');
    assert.equal(result.principal.id, browser.principal.id);
    assert.ok(result.session_token);
    assert.notEqual(result.session.id, browser.session.id);
    return result;
  }, async () => {
    await call('logout', { headers: browserHeaders });
  });
}
const login = await enrolledLogin();
assert.equal(login.principal.id, fixture.principal);
assert.ok(login.session_token);
client.setConfig({ headers: { Authorization: `Bearer ${login.session_token}` } });
const who = z.zWhoAmI.parse((await call('whoami')).data);
assert.equal(who.principal.id, fixture.principal);
await call('logout');
const revoked = await call('whoami', { throwOnError: false });
assert.equal(revoked.response.status, 401, 'logout must revoke the session');
assert.equal(z.zError.parse(revoked.error).error.code, 'unauthenticated');

client.setConfig({ headers: { Authorization: `Bearer ${fixture.elevated}` } });
const org = z.zOrg.parse((await call('createOrg', { body: { name: 'frozen-client-org' } })).data);
assert.equal(org.name, 'frozen-client-org');
// Creating an org also changes grants. Existing sessions deliberately become
// stale; the frozen consumer must observe revocation and authenticate again.
const stale = await call('whoami', { throwOnError: false });
assert.equal(stale.response.status, 401);
assert.equal(z.zError.parse(stale.error).error.code, 'unauthenticated');
const fresh = await enrolledLogin();
assert.ok(fresh.session_token);
client.setConfig({ headers: { Authorization: `Bearer ${fresh.session_token}` } });
assert.equal(z.zOrg.parse((await call('getOrg', { path: { org: org.id } })).data).id, org.id);
assert.equal((await call('listOrgs')).response.status, 200, 'verified login assurance crosses the handoff');
const list = z.zMyOrgList.parse((await call('listMyOrgs')).data);
assert.ok(list.items.some(item => item.id === org.id));
// Transit as the v1.0.0 consumer sees it. The environment holds an mldsa65 key
// (#1129) and the closed v1.0.0 Zod enum cannot name it: the whole list fails to
// parse, Ed25519 entries included, and so does the single key. This is the
// one-time skew break of the 2026-11-04 api-cli-surface amendment, asserted here
// as the real behavior rather than avoided. The v1.0.0 SDK has no response
// validator, so its calls keep working against the same server and the same key.
// PKI cannot mint mldsa65 here while service.PKI.MLDSAEnabled is false.
const transitList = (await call('listTransitKeys', { path: fixture.scope })).data;
assert.deepEqual(transitList.items.map(key => key.algorithm).sort(), ['ed25519', 'mldsa65', 'xchacha20-poly1305']);
const refusedList = z.zTransitKeyList.safeParse(transitList);
assert.equal(refusedList.success, false, 'the v1.0.0 Zod list schema must reject a list holding mldsa65');
const mldsaIndex = transitList.items.findIndex(key => key.algorithm === 'mldsa65');
assert.deepEqual(
  refusedList.error.issues.map(issue => issue.path.join('.')),
  [`items.${mldsaIndex}.algorithm`],
  'only the mldsa65 algorithm is the failure, not any other field',
);
const classicalOnly = z.zTransitKeyList.parse({ items: transitList.items.filter(key => key.algorithm !== 'mldsa65') });
assert.deepEqual(classicalOnly.items.map(key => key.algorithm).sort(), ['ed25519', 'xchacha20-poly1305']);
const signing = classicalOnly.items.find(key => key.algorithm === 'ed25519');
assert.ok(signing.versions[0].public_key, 'an ed25519 key publishes its public key');
const pqShown = (await call('showTransitKey', { path: { ...fixture.scope, transit_key: 'skew-pq' } })).data;
assert.equal(z.zTransitKey.safeParse(pqShown).success, false, 'the v1.0.0 Zod key schema rejects a single mldsa65 key');
assert.equal(z.zTransitKey.safeParse((await call('showTransitKey', { path: { ...fixture.scope, transit_key: 'skew-signing' } })).data).success, true);
// The v1.0.0 SDK still signs and verifies with it; its public key is a non-empty SPKI.
assert.ok(pqShown.versions[0].public_key.length > 2000, 'the published mldsa65 key is a large SPKI value');
const pqMessage = Buffer.from('frozen client signs with mldsa65').toString('base64');
const pqSigned = z.zTransitSignatureResult.parse((await call('transitSign', { path: { ...fixture.scope, transit_key: 'skew-pq' }, body: { message: pqMessage } })).data);
assert.ok(pqSigned.signature.startsWith('hikyo:v1:'));
const pqVerified = z.zTransitVerifyResult.parse((await call('transitVerify', { path: { ...fixture.scope, transit_key: 'skew-pq' }, body: { message: pqMessage, signature: pqSigned.signature } })).data);
assert.equal(pqVerified.valid, true);
// The whole lifecycle through the v1.0.0 SDK: create, sign, verify, rotate, list.
// Its request types do not name mldsa65, but the SDK sends the body as given.
const lifecycle = { ...fixture.scope, transit_key: 'skew-pq-sdk' };
const created = (await call('createTransitKey', { path: fixture.scope, body: { name: 'skew-pq-sdk', algorithm: 'mldsa65', allowed_operations: ['sign', 'verify'] } })).data;
assert.equal(created.algorithm, 'mldsa65');
assert.equal(created.latest_version, 1);
const first = (await call('transitSign', { path: lifecycle, body: { message: pqMessage } })).data;
const rotated = (await call('rotateTransitKey', { path: lifecycle })).data;
assert.equal(rotated.latest_version, 2);
assert.equal(rotated.versions.length, 2);
assert.notEqual(rotated.versions[0].public_key, rotated.versions[1].public_key, 'rotation makes a new signing key');
const second = (await call('transitSign', { path: lifecycle, body: { message: pqMessage } })).data;
assert.ok(first.signature.startsWith('hikyo:v1:') && second.signature.startsWith('hikyo:v2:'));
for (const signature of [first.signature, second.signature]) {
  assert.equal((await call('transitVerify', { path: lifecycle, body: { message: pqMessage, signature } })).data.valid, true);
}
assert.equal((await call('transitVerify', { path: lifecycle, body: { message: Buffer.from('another message').toString('base64'), signature: first.signature } })).data.valid, false);
const afterList = (await call('listTransitKeys', { path: fixture.scope })).data;
assert.deepEqual(afterList.items.filter(key => key.algorithm === 'mldsa65').map(key => key.name).sort(), ['skew-pq', 'skew-pq-sdk']);
assert.equal(z.zTransitKeyList.safeParse(afterList).success, false);
const pkiProfiles = z.zPkiProfileList.parse((await call('listPkiProfiles')).data);
assert.deepEqual(pkiProfiles.profiles.map(profile => profile.name), ['skew-web']);
assert.ok(pkiProfiles.profiles[0].policy.key_algorithms.includes('ed25519'));
const pkiIssuers = z.zPkiIssuerList.parse((await call('listPkiIssuers')).data);
assert.ok(pkiIssuers.issuers.some(issuer => issuer.name === 'skew-root'), 'the seeded skew-root issuer is listed');
client.setConfig({ headers: { Authorization: null } });
const weak = z.zLoginResult.parse((await call('localLogin', {
  body: { username: fixture.weak_username, password: fixture.password, artifact: 'cli' },
})).data);
assert.equal(weak.session.artifact, 'cli');
assert.ok(weak.session_token);
client.setConfig({ headers: { Authorization: `Bearer ${weak.session_token}` } });
const allOrgs = await call('listOrgs', { throwOnError: false });
assert.equal(allOrgs.response.status, 403, 'instance-wide listing requires MFA');
assert.equal(z.zError.parse(allOrgs.error).error.code, 'forbidden');
await call('logout');
console.log('frozen generated SDK and Zod: discovery, login, identity, logout, create, grant revocation, re-login, read/list, transit mldsa65 list rejected by the v1.0.0 Zod schemas while the SDK signs and verifies, pki profile and issuer lists passed');
