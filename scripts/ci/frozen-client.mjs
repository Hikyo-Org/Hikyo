import assert from 'node:assert/strict';
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
const login = z.zLoginResult.parse((await call('localLogin', {
  body: { username: fixture.username, password: fixture.password, artifact: 'cli' },
})).data);
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
const fresh = z.zLoginResult.parse((await call('localLogin', {
  body: { username: fixture.username, password: fixture.password, artifact: 'cli' },
})).data);
assert.ok(fresh.session_token);
client.setConfig({ headers: { Authorization: `Bearer ${fresh.session_token}` } });
assert.equal(z.zOrg.parse((await call('getOrg', { path: { org: org.id } })).data).id, org.id);
const allOrgs = await call('listOrgs', { throwOnError: false });
assert.equal(allOrgs.response.status, 403, 'instance-wide listing requires MFA');
assert.equal(z.zError.parse(allOrgs.error).error.code, 'forbidden');
const list = z.zMyOrgList.parse((await call('listMyOrgs')).data);
assert.ok(list.items.some(item => item.id === org.id));
console.log('frozen generated SDK and Zod: discovery, login, identity, logout, create, grant revocation, re-login, read/list passed');
