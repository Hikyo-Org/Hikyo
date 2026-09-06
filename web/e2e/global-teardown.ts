import type { FullConfig } from '@playwright/test';

import { stopInstance } from './fixtures/instance.ts';
import { readRunLog, unexecutedClaims } from './registry.ts';

/**
 * Teardown closes the half of the registry a declaration cannot: every surface
 * a flow CLAIMS must have had the pinned assertion set actually run on it.
 * Without this, adding a surface to the table, an element to the router and an
 * entry to a flow's claim list passes the closure check while asserting
 * nothing about the new page.
 *
 * It is skipped under `--grep` or a positional spec filter: a filtered run is
 * deliberately partial, and failing it would make the check something people
 * work around instead of with.
 *
 * CI RELIES ON THAT SKIP. The `web` job splits the flows across matrix legs and
 * runs each leg with a positional spec list (see .github/workflows/ci.yml), so
 * each leg is a partial run and skips here, the `web-closure` aggregator job
 * then merges every leg's run log and runs this same closure over the whole set
 * (e2e/check-closure.ts). A local unfiltered `pnpm run e2e` still closes here.
 *
 * `--shard` is REFUSED rather than skipped. It would split the flows the same
 * way, but WITHOUT the positional-filter marker, so the log would be partial
 * while the run still looked complete, a wall of "claims more than it runs"
 * lines that say nothing about the real cause. The CI split uses positional
 * specs precisely so the skip is honest; `--shard` is not, so it says so.
 */
export default function globalTeardown(config: FullConfig): void {
  stopInstance();

  if (config.shard !== null) {
    throw new Error(
      'the flow suite cannot be sharded with --shard: each shard runs a subset of the flows, ' +
        'so no shard can check the registry\'s claims and none of them would notice. ' +
        'Parallelise by project instead (--project=desktop / --project=mobile, as the `web` ' +
        'job does): both projects run every flow, so every shard checks every claim.',
    );
  }

  // `config.grep` is NOT where a CLI `--grep` lands: Playwright leaves the
  // resolved config's `grep` at `/.*/` and applies the CLI filter separately, so
  // reading it alone made this check fire on every filtered run, after the
  // tests passed, with a wall of lines about flows nobody asked to run. That is
  // exactly the "check people work around instead of with" the note above warns
  // against. The CLI is read from `process.argv` (global teardown runs in
  // Playwright's own process), and the config field is still consulted because a
  // `grep` set in `playwright.config.ts` does land there.
  const grepped = process.argv.some(
    (arg) =>
      arg === '--grep' ||
      arg === '-g' ||
      arg.startsWith('--grep=') ||
      arg === '--grep-invert' ||
      arg.startsWith('--grep-invert='),
  );
  const specFiltered = process.argv.some(
    (arg) => arg.endsWith('.spec.ts') || arg.includes('/flows/'),
  );
  const filtered = grepped || specFiltered || String(config.grep) !== '/.*/';
  if (filtered) {
    process.stdout.write('flow-registry execution check skipped: this run was filtered\n');
    return;
  }
  const missing = unexecutedClaims(readRunLog());
  if (missing.length > 0) {
    throw new Error(`the flow registry claims more than it runs:\n  - ${missing.join('\n  - ')}`);
  }
}
