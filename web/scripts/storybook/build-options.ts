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
