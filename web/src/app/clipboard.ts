// Serialize app writes so retiring a pending disclosure cannot erase a newer
// app-owned copy. Start the idle write synchronously, preserving user activation.
const writes: Array<() => void> = [];
let writing = false;
let writeGeneration = 0;
function ownedWrite<T>(run: () => Promise<T>): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    writes.push(() => {
      writing = true;
      void run().then(resolve, reject).finally(() => {
        writing = false;
        writes.shift()?.();
      });
    });
    if (!writing) writes.shift()?.();
  });
}

async function nativeWrite(text: string): Promise<"ok" | "refused"> {
  try {
    const clipboard = navigator.clipboard;
    if (clipboard?.writeText === undefined) {
      return "refused";
    }
    await clipboard.writeText(text);
    writeGeneration += 1;
    return "ok";
  } catch {
    return "refused";
  }
}

export function writeClipboard(text: string): Promise<"ok" | "refused"> {
  return ownedWrite(() => nativeWrite(text));
}

const CLIPBOARD_CLEAR_MS = 45_000;
const CLIPBOARD_FOCUS_RETRY_MS = 120_000;

/**
 * Clear only what we put there: if the human has since copied something else,
 * wiping it would destroy their work. A read that throws (permission prompt
 * declined, API absent) is treated as "do not clear": guessing wrong costs the
 * human a clipboard, guessing cautious costs nothing.
 */
export function clearClipboardIfStill(
  expected: string,
  generation?: number,
  eligible: () => boolean = () => true,
): Promise<void> {
  return ownedWrite(async () => {
    if (!eligible() || (generation !== undefined && generation !== writeGeneration)) return;
    let current: string;
    try {
      const readText = navigator.clipboard?.readText;
      if (readText === undefined) return;
      current = await navigator.clipboard.readText();
    } catch {
      return;
    }
    if (eligible() && current === expected && (generation === undefined || generation === writeGeneration)) await nativeWrite("");
  });
}

// Background tabs often cannot read the clipboard. Give the operator one
// focused attempt within a bounded window, then retire every listener/timer.
function scheduleClipboardClear(text: string, generation: number): void {
  const deadline = Date.now() + CLIPBOARD_CLEAR_MS + CLIPBOARD_FOCUS_RETRY_MS;
  let deadlineTimer: ReturnType<typeof globalThis.setTimeout> | undefined;
  let listening = false;
  let attempted = false;
  const eligible = () => Date.now() < deadline && document.hasFocus() && document.visibilityState !== 'hidden';
  const cleanup = () => {
    if (deadlineTimer !== undefined) globalThis.clearTimeout(deadlineTimer);
    if (listening) {
      window.removeEventListener('focus', onFocus);
      document.removeEventListener('visibilitychange', onFocus);
      listening = false;
    }
  };
  const onFocus = () => {
    if (Date.now() >= deadline) {
      cleanup();
      return;
    }
    if (attempted || !eligible()) return;
    attempted = true;
    cleanup();
    // Check eligibility again when the serialized operation dispatches and
    // after its async read. A queued/slow read cannot clear past the deadline.
    void clearClipboardIfStill(text, generation, eligible);
  };
  globalThis.setTimeout(() => {
    if (Date.now() >= deadline) return;
    if (eligible()) {
      onFocus();
      return;
    }
    listening = true;
    window.addEventListener('focus', onFocus);
    document.addEventListener('visibilitychange', onFocus);
    deadlineTimer = globalThis.setTimeout(cleanup, deadline - Date.now());
  }, CLIPBOARD_CLEAR_MS);
}

/**
 * Copy a value with the prototype's honest best-effort expiry microcopy.
 *
 * Expiry is a SECRET measure: only an audited (secret) copy schedules the
 * clear, so the microcopy below stays truthful for both branches. An ordinary
 * config value is already on screen under plain read, and wiping it from the
 * clipboard 45 seconds later would surprise the human for no protection.
 */
export async function writeExpiringClipboard(
  text: string,
  audited: boolean,
  isCurrent: () => boolean = () => true,
): Promise<string> {
  let generation = 0;
  const result = await ownedWrite(async () => {
    if (!isCurrent()) return 'retired';
    const written = await nativeWrite(text);
    generation = writeGeneration;
    if (written === 'ok' && !isCurrent()) {
      // The native API cannot cancel a pending write. Retire its result within
      // the same owned slot, before any newer app write, even without readText.
      return await nativeWrite('') === 'ok' ? 'retired' : 'retirement-refused';
    }
    return written;
  });
  if (result === 'retired') return 'The copy was canceled.';
  if (result === 'retirement-refused') {
    const message = 'The browser refused to clear a canceled copy. Clear your clipboard manually.';
    // The disclosure owner may already be unmounted. A global, value-free
    // warning must survive it when native cleanup is denied.
    notifyFailure(message);
    return message;
  }
  if (result === "refused") {
    return "This browser refused clipboard access, so nothing was copied.";
  }
  if (audited) {
    scheduleClipboardClear(text, generation);
  }
  return audited
    ? "Copied, and recorded as a disclosure. Attempts to clear after 45s. If unfocused, retries once on return within 2 minutes. Clipboard managers may keep this browser copy. On macOS or Windows, use hikyo values get KEY --reveal --clipboard to request exclusion from clipboard history."
    : "Copied. This value is not a secret, so no disclosure was recorded.";
}
import { notifyFailure } from './notifications.tsx';
