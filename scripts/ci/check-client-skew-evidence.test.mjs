import assert from 'node:assert/strict';
import test from 'node:test';
import { verifyExecution } from './check-client-skew-evidence.mjs';

const root = 'TestFrozenGeneratedClientAgainstCurrentServer';
const passed = ['', '/sqlite', '/postgres'].map(suffix => ({ Test: `${root}${suffix}`, Action: 'pass' }));
test('both engine passes are required, not merely a successful go invocation', () => {
  assert.doesNotThrow(() => verifyExecution(passed));
  for (const events of [[], passed.slice(0, 1), passed.slice(0, 2), passed.map(event => ({ ...event, Test: `${event.Test}Renamed` }))]) {
    assert.throws(() => verifyExecution(events));
  }
});
test('a skip or failure cannot be hidden by other passing events', () => {
  for (const Action of ['skip', 'fail']) {
    for (const suffix of ['', '/sqlite', '/postgres']) {
      assert.throws(() => verifyExecution([...passed, { Test: `${root}${suffix}`, Action }]));
    }
  }
});
