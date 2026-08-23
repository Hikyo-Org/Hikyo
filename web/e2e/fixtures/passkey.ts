import { test as base, type Page } from '@playwright/test';

import { installPasskeyAuthenticator, refreshSharedSession } from './instance.ts';

export type PasskeyCredential = 'shared' | 'empty';

type PasskeyFixtures = {
  passkeyCredential: PasskeyCredential;
  passkeyPage: Page;
};

/** Run work on a passkey-bearing page, then persist and repair before it closes. */
export async function withPasskeyPage(
  page: Page,
  credential: PasskeyCredential,
  use: (page: Page) => Promise<void>,
): Promise<void> {
  const persistPasskey = await installPasskeyAuthenticator(page, credential);
  try {
    await use(page);
  } finally {
    await persistPasskey();
    await refreshSharedSession();
  }
}

/** Page fixture that owns passkey counter persistence and shared-session repair. */
export const test = base.extend<PasskeyFixtures>({
  passkeyCredential: ['shared', { option: true }],
  passkeyPage: async ({ page, passkeyCredential }, use) => {
    await withPasskeyPage(page, passkeyCredential, use);
  },
});
