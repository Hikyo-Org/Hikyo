import type { SettingsOperation } from '../api/settings.ts';
import type { ChromeIdentityActions, ChromeIdentityChange } from './ChromeIdentityControls.tsx';

type IdentityMutations = {
  readonly set: { readonly mutateAsync: (input: ChromeIdentityChange) => Promise<unknown> };
  readonly setImage: { readonly mutateAsync: (input: { blob: Blob; contentType: string }) => Promise<unknown> };
  readonly clearImage: { readonly mutateAsync: () => Promise<unknown> };
};

/**
 * identityActions binds an identity mutation set to its page's feedback. A
 * failure is reported once, here, and re-thrown so the controls revert to the
 * server state; success says so and lets the invalidated reads redraw.
 */
export function identityActions(
  mutations: IdentityMutations,
  operations: { readonly identity: SettingsOperation; readonly image: SettingsOperation },
  feedback: {
    readonly report: (operation: SettingsOperation, error: unknown) => void;
    readonly ok: (text: string) => void;
  },
): ChromeIdentityActions {
  const attempt = async (operation: SettingsOperation, saved: string, change: () => Promise<unknown>) => {
    try {
      await change();
      feedback.ok(saved);
    } catch (error) {
      feedback.report(operation, error);
      throw error;
    }
  };
  return {
    setIdentity: (next) => attempt(operations.identity, 'Identity saved.', () => mutations.set.mutateAsync(next)),
    setImage: (image) => attempt(operations.image, 'Image saved.', () => mutations.setImage.mutateAsync(image)),
    clearImage: () => attempt(operations.image, 'Image removed.', () => mutations.clearImage.mutateAsync()),
  };
}
