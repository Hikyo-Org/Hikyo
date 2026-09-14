import type { FullConfig } from '@playwright/test';

import { startInstance, stopInstance } from './fixtures/instance.ts';
import { liveClosureViolations, resetRunLog } from './registry.ts';

/**
 * Global setup runs the registry's closure check FIRST, before a browser
 * starts. A suite that is missing a flow should say so in two seconds, not
 * after it has finished passing every flow it does have.
 *
 * ONE instance per invocation, and therefore one instance shared by every
 * project a single invocation runs. That matters: flows mutate the instance —
 * `instance-admin` creates an organisation, so a run covering both viewport
 * projects at once has the first project's writes visible to the second, and
 * an assertion about how many organisations exist fails for a reason that is
 * not the code (e.g. `history.spec.ts` 559 stages a draft the second project's
 * editor then sees). `pnpm run e2e` therefore invokes each project separately,
 * the same shape CI's per-viewport sharding gives it — and a multi-project
 * invocation is refused below, not just discouraged in this comment.
 */
export default async function globalSetup(config: FullConfig): Promise<void> {
  // The one-instance-per-invocation contract above is load-bearing, so enforce
  // it. Playwright's `config.projects` does not reflect the `--project` filter
  // (it lists every configured project even when one is selected), so the
  // selection is read from argv instead — the only place the filter survives.
  const selected = selectedProjectCount(process.argv, config.projects.length);
  if (selected > 1) {
    throw new Error(
      `this suite shares one seeded instance per invocation, so run one project ` +
        `at a time: \`pnpm run e2e\`, or \`playwright test --project=<desktop|mobile>\`. ` +
        `Running ${selected} projects together lets the first project's writes ` +
        `pollute the second (e.g. history.spec.ts 559's staged draft).`,
    );
  }

  const problems = liveClosureViolations();
  if (problems.length > 0) {
    throw new Error(`the flow registry is not closed:\n  - ${problems.join('\n  - ')}`);
  }
  // A previous run's log must not vouch for this one.
  resetRunLog();
  try {
    await startInstance();
  } catch (err) {
    // A setup failure means Playwright never runs globalTeardown; without this
    // the just-spawned servers outlive the runner and poison the next run's
    // port preflight.
    stopInstance();
    throw err;
  }
}

/**
 * How many distinct projects this invocation will run, read from argv because
 * `config.projects` is unfiltered. No `--project` flag means every configured
 * project runs (`total`); otherwise it is the set of names passed, in either
 * `--project=name` or `--project name` form.
 */
function selectedProjectCount(argv: readonly string[], total: number): number {
  const names = new Set<string>();
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === undefined) continue;
    if (arg === '--project') {
      const value = argv[i + 1];
      if (value !== undefined) names.add(value);
    } else if (arg.startsWith('--project=')) {
      names.add(arg.slice('--project='.length));
    }
  }
  return names.size === 0 ? total : names.size;
}
