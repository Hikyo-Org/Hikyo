/** Render a stored timestamp in the operator's locale; preserve invalid input. */
export function formatWhen(value: string): string {
  const at = new Date(value);
  return Number.isNaN(at.getTime()) ? value : at.toLocaleString();
}
