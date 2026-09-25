/**
 * An in-memory `localStorage` for tests. happy-dom does not always put one on
 * the global (the CI runner's Node exposes its own experimental getter, which
 * warns and yields undefined without `--localstorage-file`), so a test that
 * reads or clears storage installs this first, as Shell.update.test.tsx does.
 * Returns the store so a test can assert on it directly.
 */
export function installMemoryStorage(): Storage {
  const values = new Map<string, string>();
  const storage: Storage = {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key: string) => values.get(key) ?? null,
    key: (index: number) => [...values.keys()][index] ?? null,
    removeItem: (key: string) => values.delete(key),
    setItem: (key: string, value: string) => values.set(key, value),
  };
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage });
  return storage;
}
