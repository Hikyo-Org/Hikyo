import assert from 'node:assert/strict';
import test from 'node:test';
import { withBrowserLogout } from './frozen-client-cleanup.mjs';

test('preserves PKCE failure and logout failure in order', async () => {
  const protocolError = new Error('PKCE binding refused');
  const logoutError = new Error('browser logout failed');
  await assert.rejects(withBrowserLogout(
    async () => { throw protocolError; },
    async () => { throw logoutError; },
  ), error => {
    assert.ok(error instanceof AggregateError);
    assert.deepEqual(error.errors, [protocolError, logoutError]);
    return true;
  });
});

test('retires the browser before returning the CLI result', async () => {
  const order = [];
  const result = await withBrowserLogout(
    async () => { order.push('redeem'); return 'validated result'; },
    async () => { order.push('logout'); },
  );
  assert.equal(result, 'validated result');
  assert.deepEqual(order, ['redeem', 'logout']);
});

test('propagates each single failure without replacement', async () => {
  for (const stage of ['protocol', 'logout']) {
    const failure = new Error(stage);
    await assert.rejects(withBrowserLogout(
      async () => { if (stage === 'protocol') throw failure; },
      async () => { if (stage === 'logout') throw failure; },
    ), error => error === failure);
  }
});
