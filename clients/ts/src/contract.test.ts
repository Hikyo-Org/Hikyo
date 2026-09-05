import assert from 'node:assert/strict';
import test from 'node:test';

import type { AdapterProvider, SamlProviderWarning } from './generated/types.gen.ts';

import {
  zAdapterProvider,
  zDynamicProviderKind,
  zSamlProviderWarning,
  zCreateOrgRequest,
  zErrorCode,
  zGrantResult,
  zGrantOrigin,
  zIdentityProviderKind,
  zOidcStartRequest,
  zSamlStartRequest,
  zMeta,
  zProtocolCapability,
  zTotpReauthRequest,
} from './generated/zod.gen.ts';

// The TypeScript half of the bound 3.1 profile (system-architecture ADR,
// 2026-08-07 amendment): the round-trip fixtures must run through the Zod
// consumer as well as the Go one, because the two generators read the same
// document and could still disagree about what it means.

test('nullable members round-trip absent, null and value distinctly', () => {
  // `type: [object, "null"]` - three states, three outcomes, none collapsed.
  assert.deepEqual(zCreateOrgRequest.parse({ name: 'acme' }).metadata, undefined);
  assert.equal(zCreateOrgRequest.parse({ name: 'acme', metadata: null }).metadata, null);
  assert.deepEqual(
    zCreateOrgRequest.parse({ name: 'acme', metadata: { team: 'platform' } }).metadata,
    { team: 'platform' },
  );
});

test('an open enum tolerates a value this client has never heard of', () => {
  // The whole point of x-extensible-enum: an older client must not reject a
  // newer server's response. If this ever throws, every client in the field
  // breaks the day a new auth flow ships.
  assert.equal(zProtocolCapability.parse('local-password'), 'local-password');
  assert.equal(zProtocolCapability.parse('some-flow-from-2030'), 'some-flow-from-2030');

  const meta = zMeta.parse({
    server_version: '1.4.0',
    api_revision: 7,
    protocol_capabilities: ['local-password', 'a-flow-we-do-not-know'],
  });
  assert.deepEqual(meta.protocol_capabilities, ['local-password', 'a-flow-we-do-not-know']);
});

test('a closed enum refuses an unknown value', () => {
  // Closed enums never grow, so tolerating one would hide a server speaking
  // a contract this client does not have.
  assert.equal(zErrorCode.parse('not_found'), 'not_found');
  assert.throws(() => zErrorCode.parse('teapot'));
});

test('grant mutations expose exactly one closed outcome', () => {
  const grantId = 'grt_0198b727-19e3-7c31-a2df-904b89224e4c';
  for (const outcome of ['created', 'origin_added', 'unchanged']) {
    assert.equal(
      zGrantResult.parse({ grant_id: grantId, capability: 'read', outcome }).outcome,
      outcome,
    );
  }
  assert.throws(() =>
    zGrantResult.parse({ grant_id: grantId, capability: 'read', outcome: 'partly_created' }),
  );
  assert.throws(() =>
    zGrantResult.parse({
      grant_id: grantId,
      capability: 'read',
      created: false,
      origin_added: true,
    }),
  );
});

test('a request missing a required member is refused before it is sent', () => {
  assert.throws(() => zCreateOrgRequest.parse({}));
});

test('TOTP reauthentication accepts only one canonical intent variant', () => {
  const environment = 'env_00000000-0000-0000-0000-000000000001';
  assert.throws(() => zTotpReauthRequest.parse({ code: '123456' }));
  assert.throws(() =>
    zTotpReauthRequest.parse({
      code: '123456',
      environment_id: environment,
      purpose: 'adapter',
      operation: 'adapter.sync',
      environment_ids: [environment],
    }),
  );
  assert.throws(() =>
    zTotpReauthRequest.parse({
      code: '123456',
      purpose: 'adapter',
      operation: 'adapter.sync',
    }),
  );
  assert.doesNotThrow(() =>
    zTotpReauthRequest.parse({ code: '123456', environment_id: environment }),
  );
  assert.doesNotThrow(() =>
    zTotpReauthRequest.parse({
      code: '123456',
      purpose: 'adapter',
      operation: 'adapter.sync',
      environment_ids: [environment],
    }),
  );
});

// These fixtures mirror api/pre_freeze_test.go and the Go client's decoder.
test('pre-freeze identity provider kind accepts a future protocol', () => {
  assert.equal(zIdentityProviderKind.parse('future-kind'), 'future-kind');
});
test('pre-freeze OIDC purpose accepts a future purpose', () => {
  assert.equal(zOidcStartRequest.parse({ purpose: 'future-purpose' }).purpose, 'future-purpose');
});
test('pre-freeze SAML purpose accepts a future purpose', () => {
  assert.equal(zSamlStartRequest.parse({ purpose: 'future-purpose' }).purpose, 'future-purpose');
});
test('pre-freeze grant origin accepts a future origin', () => {
  assert.equal(zGrantOrigin.parse({ kind: 'future-origin', subject: 'holder' }).kind, 'future-origin');
});

test('adapter provider preserves an unknown response discriminator', () => {
  const provider: AdapterProvider = 'future-provider';
  assert.equal(zAdapterProvider.parse(provider), provider);
});
test('SAML warning preserves an unknown code and its server diagnostic', () => {
  const warning: SamlProviderWarning = {
    code: 'future-warning', effective_at: '2026-09-01T00:00:00Z',
    message: 'Server diagnostic', severity: 'error',
  };
  assert.deepEqual(zSamlProviderWarning.parse(warning), warning);
  assert.throws(() => zSamlProviderWarning.parse({ ...warning, severity: 'future-severity' }));
});
test('dynamic provider kind keeps the closed PostgreSQL-only contract', () => {
  assert.equal(zDynamicProviderKind.parse('postgres'), 'postgres');
  assert.throws(() => zDynamicProviderKind.parse('future-provider'));
});
