/** A login continuation is an instance-local path, excluding the sign-in doors. */
export function loginReturnTarget(target: string | null | undefined): string | undefined {
  if (target == null || !target.startsWith('/') || target.startsWith('//')) return undefined;
  try {
    const url = new URL(target, globalThis.location.origin);
    if (url.origin !== globalThis.location.origin || url.username !== '' || url.password !== '' ||
      url.pathname.startsWith('//')) return undefined;
    // React Router matches the sign-in doors regardless of case or trailing slash.
    const path = decodeURIComponent(url.pathname).toLowerCase().replace(/\/+$/, '');
    if (path === '/login' || path === '/signup') return undefined;
    return `${url.pathname}${url.search}${url.hash}`;
  } catch {
    return undefined;
  }
}
