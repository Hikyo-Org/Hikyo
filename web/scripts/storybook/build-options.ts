/** Keep the Docs guard pointed at the same immutable directory as Storybook. */
export function storybookOutputDirectory(args: readonly string[]): string {
  let directory = 'storybook-static';
  for (const [index, argument] of args.entries()) {
    if (argument === '-o' || argument === '--output-dir') {
      const value = args[index + 1];
      if (value === undefined || value.startsWith('-')) throw new Error(`${argument} requires an output directory`);
      directory = value;
    } else if (argument.startsWith('-o') && !argument.startsWith('--')) {
      directory = argument.slice(2);
    } else if (argument.startsWith('--output-dir=')) {
      directory = argument.slice('--output-dir='.length);
      if (directory === '') throw new Error('--output-dir requires an output directory');
    }
  }
  return directory;
}

/** pnpm may forward its separator; Node's --run consumes it itself. */
export function storybookIndexDirectory(args: readonly string[]): string {
  const paths = args.filter((argument) => argument !== '--');
  if (paths.length > 1) throw new Error('Docs check accepts one build directory');
  return paths[0] ?? 'storybook-static';
}
