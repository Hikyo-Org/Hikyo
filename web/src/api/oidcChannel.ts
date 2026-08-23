const RETURN_PREFIX = 'hikyo-oidc-return:';

export function oidcChannelName(state: string): string {
  return `hikyo-oidc:${state}`;
}

export function rememberOIDCReturn(state: string, location: string): void {
  globalThis.sessionStorage.setItem(RETURN_PREFIX + state, location);
}

export function takeOIDCReturn(state: string): string {
  const returnTo = globalThis.sessionStorage.getItem(RETURN_PREFIX + state) ?? '/';
  globalThis.sessionStorage.removeItem(RETURN_PREFIX + state);
  return returnTo;
}
