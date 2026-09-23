import type { zAuthMethodProvider } from '@hikyo/zod';
import type { JSX } from 'react';
import type { z } from 'zod';

import { Badge } from '../Badge.tsx';
import { cx } from '../cx.ts';

/** One configured identity provider, as `GET /auth/methods` describes it. */
export type SignInProvider = z.infer<typeof zAuthMethodProvider>;

/**
 * The vendor whose brand rules the button follows. Absent for a generic OIDC
 * or SAML row, which reads "Continue with <display_name>" in the house style.
 * Not on the wire yet: `docs/spec/social-signin.md` section 3.1 pins only
 * `profile: [github]`, so the route offers no brand until #607/#609 decide.
 */
export type ProviderBrand = 'google' | 'microsoft' | 'github';

export type LoginProvider = SignInProvider & { brand?: ProviderBrand };

/** Whether the row starts a sign-in or, from the sign-up door, a sign-up. */
export type SignInIntent = 'sign-in' | 'sign-up';

// The marks as each vendor publishes them (docs/research/social-providers.md
// sections 2.7, 3.7, 4.7). Google's G keeps its four colours on either theme;
// Microsoft's four squares likewise; GitHub's Invertocat takes the text
// colour, so it is white on the dark fill and near-black on the light one.
const MARKS: Record<ProviderBrand, JSX.Element> = {
  google: (
    <svg viewBox="0 0 48 48" aria-hidden="true">
      <path fill="#EA4335" d="M24 9.5c3.54 0 6.71 1.22 9.21 3.6l6.85-6.85C35.9 2.38 30.47 0 24 0 14.62 0 6.51 5.38 2.56 13.22l7.98 6.19C12.43 13.72 17.74 9.5 24 9.5z" />
      <path fill="#4285F4" d="M46.98 24.55c0-1.57-.15-3.09-.38-4.55H24v9.02h12.94c-.58 2.96-2.26 5.48-4.78 7.18l7.73 6c4.51-4.18 7.09-10.36 7.09-17.65z" />
      <path fill="#FBBC05" d="M10.53 28.59c-.48-1.45-.76-2.99-.76-4.59s.27-3.14.76-4.59l-7.98-6.19C.92 16.46 0 20.12 0 24c0 3.88.92 7.54 2.56 10.78l7.97-6.19z" />
      <path fill="#34A853" d="M24 48c6.48 0 11.93-2.13 15.89-5.81l-7.73-6c-2.15 1.45-4.92 2.3-8.16 2.3-6.26 0-11.57-4.22-13.47-9.91l-7.98 6.19C6.51 42.62 14.62 48 24 48z" />
    </svg>
  ),
  microsoft: (
    <svg viewBox="0 0 21 21" aria-hidden="true">
      <rect x="1" y="1" width="9" height="9" fill="#F25022" />
      <rect x="11" y="1" width="9" height="9" fill="#7FBA00" />
      <rect x="1" y="11" width="9" height="9" fill="#00A4EF" />
      <rect x="11" y="11" width="9" height="9" fill="#FFB900" />
    </svg>
  ),
  github: (
    <svg viewBox="0 0 16 16" aria-hidden="true" fill="currentColor">
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z" />
    </svg>
  ),
};

/**
 * The wording each vendor permits (research sections 2.7, 3.7, 4.7; #587 Q3).
 * Google and GitHub bless a sign-up form; Microsoft blesses only "Sign in
 * with Microsoft", so that text stands under the "Create an account" heading
 * too. Entra is one row per tenant (#588 d1), so the tenant's display name
 * follows the fixed text instead of replacing it.
 */
function providerLabel(provider: LoginProvider, intent: SignInIntent): string {
  if (provider.brand === undefined) return `Continue with ${provider.display_name}`;
  const verb = intent === 'sign-up' ? 'Sign up' : 'Continue';
  const label: Record<ProviderBrand, string> = {
    google: `${verb} with Google`,
    github: `${verb} with GitHub`,
    microsoft: 'Sign in with Microsoft',
  };
  return label[provider.brand];
}

/**
 * One identity-provider row on the sign-in and sign-up doors. A branded row
 * wears the vendor's fill, hairline and mark; a plain row is the house
 * secondary button. `busy` swaps the text for the in-flight label while the
 * mark stays, so the row keeps its identity while it waits.
 */
export function ProviderButton({
  provider,
  intent,
  busy,
  disabled,
  lastUsed = false,
  onClick,
}: {
  provider: LoginProvider;
  intent: SignInIntent;
  /** This row's own ceremony is in flight. */
  busy: boolean;
  disabled: boolean;
  /** This browser signed in through this row last time. */
  lastUsed?: boolean;
  onClick: () => void;
}) {
  const { brand } = provider;
  return (
    <button
      type="button"
      className={cx('login__brand', brand === undefined ? 'btn' : `login__brand--${brand}`)}
      disabled={disabled}
      onClick={onClick}
    >
      {brand === undefined ? null : MARKS[brand]}
      <span>
        {busy ? 'Contacting identity provider…' : providerLabel(provider, intent)}
        {brand === 'microsoft' && !busy ? (
          <span className="login__brand-tenant">· {provider.display_name}</span>
        ) : null}
      </span>
      {lastUsed ? <Badge className="login__last-used">Last used</Badge> : null}
    </button>
  );
}
