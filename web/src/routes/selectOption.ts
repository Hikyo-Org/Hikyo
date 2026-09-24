/**
 * selectOption narrows a `<select>` value to the option it was rendered from.
 * A select only ever reports one of the values it was given, so the lookup is
 * the parse, no cast; a miss is a programming error, not user input, and fails
 * loud rather than falling back to a value the operator did not pick.
 */
export function selectOption<T extends string>(options: readonly T[], value: string): T {
  const match = options.find((option) => option === value);
  if (match === undefined) {
    throw new Error(`unknown select option ${value}`);
  }
  return match;
}
