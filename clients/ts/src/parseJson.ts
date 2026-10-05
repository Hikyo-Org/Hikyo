import BigNumber from 'bignumber.js';
import { parse } from '@ungap/raw-json';

// Keep other consumers' mutable BigNumber configuration outside this boundary.
const ExactNumber = BigNumber.clone({ RANGE: [-1_000_000_000, 1_000_000_000] });

/** Preserve Go's decimal int64 tokens before the generated Zod schema runs. */
export function parseJson(text: string): unknown {
  // The fallback quotes numeric tokens and can thereby accept unquoted keys.
  // Validate the original syntax first; discard these potentially rounded values.
  JSON.parse(text);
  // The ponyfill supplies context.source on older browsers without globally
  // patching JSON. Object construction and syntax retain native JSON semantics.
  return parse(text, (_key: string, value: unknown, context?: { source?: string }): unknown => {
    if (typeof value !== 'number') return value;
    if (context?.source === undefined || !Number.isFinite(value)) {
      throw new SyntaxError('JSON number has no finite, trustworthy source token');
    }
    // Reject underflow using the original mantissa, even when its exponent is
    // beyond the decimal library's own range. Literal signed zero is allowed.
    if (value === 0 && /[1-9]/.test(context.source.split(/[eE]/, 1)[0])) {
      throw new SyntaxError('JSON number underflows to zero');
    }
    const exact = new ExactNumber(context.source);
    if (exact.isInteger()) {
      if (Number.isSafeInteger(value) && exact.eq(value)) return value;
      return BigInt(exact.toFixed());
    }
    // Includes underflow: a fractional token must never become revision 0.
    if (Number.isInteger(value)) {
      throw new SyntaxError('JSON fraction cannot be represented without becoming an integer');
    }
    return value;
  });
}
