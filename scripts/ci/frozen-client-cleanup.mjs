// Keep the protocol failure visible when retiring its browser also fails.
export async function withBrowserLogout(protocol, logout) {
  const [result] = await Promise.allSettled([protocol()]);
  try {
    await logout();
  } catch (cleanupError) {
    if (result.status === 'rejected') {
      throw new AggregateError([result.reason, cleanupError], 'frozen login protocol and browser logout failed');
    }
    throw cleanupError;
  }
  if (result.status === 'rejected') throw result.reason;
  return result.value;
}
