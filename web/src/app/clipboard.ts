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

/**
 * Clear only what we put there: if the human has since copied something else,
 * wiping it would destroy their work. A read that throws (permission prompt
 * declined, API absent) is treated as "do not clear": guessing wrong costs the
 * human a clipboard, guessing cautious costs nothing.
 */
async function clearClipboardIfStill(expected: string): Promise<void> {
  let current: string;
  try {
    const readText = navigator.clipboard?.readText;
    if (readText === undefined) return;
    current = await navigator.clipboard.readText();
  } catch {
    return;
  }
  if (current === expected) await writeClipboard('');
}

/** Copy a value with the prototype's honest best-effort expiry microcopy. */
export async function writeExpiringClipboard(text: string, audited: boolean): Promise<string> {
  if ((await writeClipboard(text)) === 'refused') {
    return 'This browser refused clipboard access, so nothing was copied.';
  }
  globalThis.setTimeout(() => {
    if (document.hasFocus()) void clearClipboardIfStill(text);
  }, CLIPBOARD_CLEAR_MS);
  return audited
    ? 'Copied, and recorded as a disclosure. Cleared in 45s if this tab stays focused. The OS may keep clipboard history.'
    : 'Copied. This value is not a secret, so no disclosure was recorded.';
}
