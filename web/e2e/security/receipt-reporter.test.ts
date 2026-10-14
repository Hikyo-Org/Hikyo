import { expect, it } from 'vitest';
import { completeSecurityProfile } from './receipt-reporter.ts';

const complete = ['stored-content', 'hostile-origin', 'oracle-negative-controls']
  .map((id) => ({ id, status: 'passed', duration_ms: 1 }));

it('accepts exactly the completed production and oracle-control profile', () => {
  expect(completeSecurityProfile('passed', false, complete)).toBe(true);
});

it('refuses filtered, duplicated and undeclared observations even when the runner says passed', () => {
  expect(completeSecurityProfile('passed', false, complete.slice(1))).toBe(false);
  expect(completeSecurityProfile('passed', false, [complete[0], complete[0], complete[2]].filter((test) => test !== undefined))).toBe(false);
  expect(completeSecurityProfile('passed', false, [...complete, { id: 'undeclared', status: 'passed', duration_ms: 1 }])).toBe(false);
});

it.each(['skipped', 'failed', 'timedOut', 'interrupted'])('refuses a %s production test', (status) => {
  const tests = complete.map((test) => test.id === 'stored-content' ? { ...test, status } : test);
  expect(completeSecurityProfile('passed', false, tests)).toBe(false);
});

it('refuses setup errors or a failed run even if all observed tests passed', () => {
  expect(completeSecurityProfile('passed', true, complete)).toBe(false);
  expect(completeSecurityProfile('failed', false, complete)).toBe(false);
});
