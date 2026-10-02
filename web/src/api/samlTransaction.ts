const PREFIX = 'hikyo-saml-transaction:';

export type SAMLTransaction = { readonly purpose: 'login'; readonly returnTo: string };

function relativeTarget(target: string): string {
  const url = new URL(target, globalThis.location.origin);
  if (url.origin !== globalThis.location.origin) throw new Error('The sign-in return must be on this instance.');
  return `${url.pathname}${url.search}${url.hash}`;
}

/** Only the initiating tab knows the purpose and continuation for this RelayState. */
export function rememberSAMLTransaction(state: string, returnTo = '/'): void {
  if (state === '') throw new Error('The identity provider did not supply a sign-in transaction.');
  const transaction: SAMLTransaction = { purpose: 'login', returnTo: relativeTarget(returnTo) };
  globalThis.sessionStorage.setItem(PREFIX + state, JSON.stringify(transaction));
}

export function peekSAMLTransaction(state: string): SAMLTransaction | null {
  if (state === '') return null;
  try {
    const raw = globalThis.sessionStorage.getItem(PREFIX + state);
    if (raw === null) return null;
    const transaction: unknown = JSON.parse(raw);
    if (typeof transaction !== 'object' || transaction === null ||
      !('purpose' in transaction) || transaction.purpose !== 'login' ||
      !('returnTo' in transaction) || typeof transaction.returnTo !== 'string') return null;
    const returnTo = relativeTarget(transaction.returnTo);
    // Stored targets must be relative too, not merely currently same-origin.
    if (returnTo !== transaction.returnTo) return null;
    return { purpose: 'login', returnTo };
  } catch {
    return null;
  }
}

export function takeSAMLTransaction(state: string): SAMLTransaction | null {
  const transaction = peekSAMLTransaction(state);
  globalThis.sessionStorage.removeItem(PREFIX + state);
  return transaction;
}
