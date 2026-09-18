const RETURN_PREFIX = 'hikyo-oidc-return:';

export function oidcChannelName(state: string): string {
  return `hikyo-oidc:${state}`;
}

export function rememberOIDCReturn(state: string, location: string): void {
  globalThis.sessionStorage.setItem(RETURN_PREFIX + state, location);
}

/** Reads the return target without consuming it, so it is safe during render. */
export function peekOIDCReturn(state: string): string {
  return globalThis.sessionStorage.getItem(RETURN_PREFIX + state) ?? '/';
}

export function takeOIDCReturn(state: string): string {
  const returnTo = peekOIDCReturn(state);
  globalThis.sessionStorage.removeItem(RETURN_PREFIX + state);
  return returnTo;
}
