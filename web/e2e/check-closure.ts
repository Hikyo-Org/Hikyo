/**
 * Flow-registry closure check over a MERGED run log.
 *
 * The `web` job splits the flows across matrix legs, so no single leg runs them
 * all and each leg's globalTeardown skips the execution-closure check (its run
 * is a positional-spec partial by design). This script restores that check's
 * full force in the `web-closure` aggregator job: given the concatenation of
 * every leg's `.runs/pinned.log`, it runs both halves of the registry gate , 
 * the declarative closure (every locked surface has a flow, every flow's spec
 * exists) and the execution closure (every flow×surface×theme claim actually
 * ran, for every viewport project present in the merged log).
 *
 * It imports only `registry.ts` and the pure surface list it closes over, so it
 * needs no dependency install: Node's native type stripping runs it directly.
 */
import { readFileSync } from 'node:fs';

import { liveClosureViolations, unexecutedClaims } from './registry.ts';

const logPath = process.argv[2];
if (logPath === undefined) {
  process.stderr.write('usage: node e2e/check-closure.ts <merged-run-log>\n');
  process.exit(2);
}

const problems = [...liveClosureViolations(), ...unexecutedClaims(readFileSync(logPath, 'utf8'))];
if (problems.length > 0) {
  process.stderr.write(`the flow registry is not closed across the sharded run:\n  - ${problems.join('\n  - ')}\n`);
  process.exit(1);
}

process.stdout.write(
  'flow-registry closure: every claim executed for every viewport present in the merged log\n',
);
