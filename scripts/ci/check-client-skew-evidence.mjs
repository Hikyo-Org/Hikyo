import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';

export function verifyExecution(events) {
  for (const suffix of ['', '/sqlite', '/postgres']) {
    const name = `TestFrozenGeneratedClientAgainstCurrentServer${suffix}`;
    assert.ok(events.some(event => event.Test === name && event.Action === 'pass'), `missing passing execution: ${name}`);
    assert.ok(!events.some(event => event.Test === name && ['skip', 'fail'].includes(event.Action)), `skipped or failed: ${name}`);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const events = readFileSync(process.argv[2], 'utf8').trim().split('\n').map(JSON.parse);
  verifyExecution(events);
  console.log('client skew: both engine executions verified');
}
