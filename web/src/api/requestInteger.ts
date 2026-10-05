/** Refuse rounding when a bigint response value becomes a JSON request number. */
export function requestInteger(value: bigint, field: string, minimum: bigint): number {
  if (value < minimum || value > BigInt(Number.MAX_SAFE_INTEGER)) {
    throw new Error(`${field} ${String(value)} is outside the browser request range. Use the CLI.`);
  }
  return Number(value);
}
