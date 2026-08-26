export async function writeClipboard(text: string): Promise<'ok' | 'refused'> {
  try {
    const clipboard = navigator.clipboard;
    if (clipboard?.writeText === undefined) {
      return 'refused';
    }
    await clipboard.writeText(text);
    return 'ok';
  } catch {
    return 'refused';
  }
}

const CLIPBOARD_CLEAR_MS = 45_000;

/** Copy a value with the prototype's honest best-effort expiry microcopy. */
export async function writeExpiringClipboard(text: string, audited: boolean): Promise<string> {
  if ((await writeClipboard(text)) === 'refused') {
    return 'This browser refused clipboard access, so nothing was copied.';
  }
  globalThis.setTimeout(() => {
    if (document.hasFocus()) void writeClipboard('');
  }, CLIPBOARD_CLEAR_MS);
  return audited
    ? 'Copied, and recorded as a disclosure. Cleared in 45s if this tab stays focused — the OS may keep clipboard history.'
    : 'Copied. This value is not a secret, so no disclosure was recorded.';
}
