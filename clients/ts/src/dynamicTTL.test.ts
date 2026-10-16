import assert from 'node:assert/strict';
import test from 'node:test';

import {
  zDynamicLease,
  zMintLeaseRequest,
  zRenewLeaseRequest,
} from './generated/zod.gen.ts';

test('dynamic TTL schemas share the safe whole-second duration range', () => {
  const maximum = 9_223_372_036n;
  for (const schema of [
    zMintLeaseRequest.shape.max_ttl_seconds,
    zRenewLeaseRequest.shape.max_ttl_seconds,
  ]) {
    for (const seconds of [1n, 3600n, maximum]) {
      assert.equal(schema.parse(seconds), seconds);
      assert.equal(schema.parse(Number(seconds)), seconds);
    }
    for (const seconds of [maximum + 1n, 9_223_372_036_854_775_807n, 0n, -1n]) {
      assert.equal(schema.safeParse(seconds).success, false, `accepted TTL ${seconds}`);
    }
  }
});

test('renewal omission and null retain the existing ceiling', () => {
  assert.deepEqual(zRenewLeaseRequest.parse({}), {});
  assert.deepEqual(zRenewLeaseRequest.parse({ max_ttl_seconds: null }), { max_ttl_seconds: null });
});

// Existing metadata may contain a legacy invalid ceiling. Keep inspection and
// revocation responses readable while new requests enforce the safe range.
test('historical overflowing lease metadata remains readable', () => {
  assert.equal(zDynamicLease.shape.max_ttl_seconds.parse(9_223_372_036_854_775_807n), 9_223_372_036_854_775_807n);
});
