import { spawnSync } from 'node:child_process';
import { resolve } from 'node:path';

import { storybookOutputDirectory } from './build-options.ts';

const web = resolve(import.meta.dirname, '../..');
const args = process.argv.slice(2).filter((argument) => argument !== '--');
const directory = storybookOutputDirectory(args);
function run(command: string, arguments_: string[]): void {
  const result = spawnSync(command, arguments_, { cwd: web, stdio: 'inherit' });
  if (result.error !== undefined) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

if (args.includes('--help') || args.includes('-h')) {
  run('pnpm', ['exec', 'storybook', 'build', ...args]);
  process.exit(0);
}

run('pnpm', ['run', 'design:export']);
run('pnpm', ['exec', 'storybook', 'build', ...args]);
run(process.execPath, [resolve(web, 'scripts/storybook/check-docs.ts'), directory]);
