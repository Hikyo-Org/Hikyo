/** `1 day`, `2 days`: a count with its unit spelled out and pluralised. */
export function plural(count: number, unit: string): string {
  return `${String(count)} ${unit}${count === 1 ? '' : 's'}`;
}

const UNITS = [
  [86_400, 'day'],
  [3_600, 'hour'],
  [60, 'minute'],
] as const;

/**
 * duration is the one way UI text states a length of time: units spelled
 * out, never `30 d`, `8h` or `45s`. It names a whole
 * number of seconds in the largest unit that divides it exactly, so 7776000
 * reads "90 days" and an odd value stays in seconds rather than rounding to a
 * lie.
 */
export function duration(seconds: number): string {
  for (const [size, unit] of UNITS) {
    if (seconds >= size && seconds % size === 0) return plural(seconds / size, unit);
  }
  return plural(seconds, 'second');
}
