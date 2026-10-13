import { test as base, type Page } from '@playwright/test';

import { installPasskeyAuthenticator, refreshSharedSession, restoreSharedSession } from './instance.ts';

export type PasskeyCredential = 'shared' | 'empty';

type PasskeyFixtures = {
  passkeyCredential: PasskeyCredential;
  passkeyPage: Page;
  sharedSession: boolean;
};

/** Prepare before loading the counter; a stale-session repair advances it. */
export async function withPasskeyLifecycle(
  prepare: () => Promise<void>,
  install: () => Promise<() => Promise<void>>,
  use: () => Promise<void>,
  repair: () => Promise<void>,
): Promise<void> {
  await prepare();
  const persist = await install();
  try {
    await use();
  } finally {
    await persist();
    await repair();
  }
}

/** Run work on a passkey-bearing page, then persist and repair before it closes. */
export async function withPasskeyPage(
  page: Page,
  credential: PasskeyCredential,
  use: (page: Page) => Promise<void>,
  sharedSession = false,
): Promise<void> {
  if (sharedSession && credential !== 'shared') {
    throw new Error('a shared session requires the shared passkey');
  }
  await withPasskeyLifecycle(
    async () => { if (sharedSession) await restoreSharedSession(page); },
    () => installPasskeyAuthenticator(page, credential),
    () => use(page),
    refreshSharedSession,
  );
}

/** Page fixture that owns passkey counter persistence and shared-session repair. */
export const test = base.extend<PasskeyFixtures>({
  passkeyCredential: ['shared', { option: true }],
  sharedSession: [false, { option: true }],
  passkeyPage: async ({ page, passkeyCredential, sharedSession }, use) => {
    await withPasskeyPage(page, passkeyCredential, use, sharedSession);
  },
});
