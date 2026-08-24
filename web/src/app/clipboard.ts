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
