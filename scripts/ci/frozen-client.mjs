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
console.log('frozen generated SDK and Zod: discovery, login, identity, logout, create, grant revocation, re-login, read/list passed');
