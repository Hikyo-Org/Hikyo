import { randomBytes } from 'node:crypto';
import type { Browser, Page } from '@playwright/test';
import { zInvitationResult, zLoginChallenge, zLoginResult, zTotpEnrolStartResult } from '@hikyo/zod';

import { browserApi, fixtureApiCall } from './api.ts';
import { BASE_URL, readSeed, STORAGE_STATE } from './instance.ts';
import { totpCode } from './seed.ts';

const TOTP_PERIOD = 30;

/** One TOTP step, in milliseconds: the unit a flow's time budget adds. */
export const TOTP_STEP_MS = TOTP_PERIOD * 1000;

/**
 * TotpLedger is ONE account's spent-step ledger, owned by the flow that
 * created the account. The shared administrator's ledger is drawn by the
 * seeding session, fixture step-ups and both projects at once, so a flow that
 * signs in or proves repeatedly gets its own account instead.
 *
 * The server accepts a code for the step before, at, or after now, strictly
 * beyond the last step it consumed, and a fresh enrolment starts with the
 * step before its creation as consumed. So a new account presents two codes
 * without waiting: the creation step, then the next one. A third inside the
 * same step waits for the next boundary (at most one step), which only a flow
 * needing three codes on one account meets, and budgets for.
 */
export class TotpLedger {
  private last: number;

  constructor(readonly otpauth: string, createdAt: Date) {
    this.last = Math.floor(createdAt.getTime() / TOTP_STEP_MS) - 1;
  }

  /** Return the next unspent TOTP code, waiting for a new step if necessary. */
  async next(): Promise<string> {
    const now = () => Math.floor(Date.now() / TOTP_STEP_MS);
    const want = Math.max(this.last + 1, now() - 1);
    while (want > now() + 1) {
      await new Promise((resolve) => setTimeout(resolve, (now() + 1) * TOTP_STEP_MS - Date.now() + 50));
    }
    this.last = want;
    return totpCode(this.otpauth, new Date(want * TOTP_STEP_MS));
  }
}

/** A throwaway human with its own password and authenticator. */
export type EnrolledAccount = {
  readonly username: string;
  readonly password: string;
  readonly principal: string;
  readonly ledger: TotpLedger;
  /** A CLI session, stepped up with the account's second code when asked for, else `''`. */
  readonly bearer: string;
};

/**
 * Where an enrolled account lands: the seeded org with no template (it signs
 * in and sees nothing), the seeded org as an `admin` (it administers members
 * and registration there), or the instance with the `operator` template (an
 * instance operator that can administer registration).
 */
export type AccountGrant = 'org' | 'org-admin' | 'instance';

/** Run `work` on a page holding the suite's shared, stepped-up administrator session. */
export async function withSharedAdmin<T>(browser: Browser, work: (page: Page) => Promise<T>): Promise<T> {
  const admin = await browser.newContext({ storageState: STORAGE_STATE });
  try {
    return await work(await admin.newPage());
  } finally {
    await admin.close();
  }
}

/**
 * enrolledAccount invites a fresh human (through the suite's stored,
 * stepped-up administrator session: no administrator code is drawn),
 * establishes its password and enrols an authenticator. `stepUp` steps its CLI
 * session up with the second code, for a flow that acts as a bearer; a flow
 * that drives the account in a browser leaves it off and spends the second
 * code on `signInAs` instead, so its proofs start at the third.
 */
export async function enrolledAccount(
  browser: Browser,
  label: string,
  grant: AccountGrant,
  stepUp = grant === 'instance',
): Promise<EnrolledAccount> {
  const username = `${label}-${Date.now().toString(36)}-${randomBytes(4).toString('hex')}`;
  const password = randomBytes(24).toString('base64url');
  const invitation = await withSharedAdmin(browser, (page) =>
    grant === 'instance'
      ? browserApi(page, 'POST', '/api/v1/instance/invitations', zInvitationResult, { username, template: 'operator' })
      : browserApi(page, 'POST', `/api/v1/orgs/${readSeed().org}/invitations`, zInvitationResult, {
          username,
          ...(grant === 'org-admin' ? { template: 'admin' } : {}),
        }),
  );
  const established = await fetch(`${BASE_URL}/api/v1/auth/credential/establish`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ authority: invitation.authority, password }),
  });
  if (established.status !== 204) {
    throw new Error(`establishing ${username} answered ${String(established.status)}`);
  }
  const login = await fetch(`${BASE_URL}/api/v1/auth/local/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password, artifact: 'cli' }),
  });
  if (login.status !== 200) {
    throw new Error(`signing ${username} in answered ${String(login.status)}`);
  }
  const session = zLoginResult.parse(await login.json()).session_token ?? '';
  const enrolled = await fixtureApiCall(session, 'POST', '/api/v1/auth/totp/enrol/start', zTotpEnrolStartResult, { password });
  // Read after the answer: the server stamped the enrolment at or before now,
  // so a boundary crossed in flight only makes the first code later, never
  // one the server already counts as spent.
  const ledger = new TotpLedger(enrolled.otpauth_uri, new Date());
  const confirmed = await fixtureApiCall(session, 'POST', '/api/v1/auth/totp/enrol/confirm', zLoginResult, { code: await ledger.next() });
  let bearer = '';
  if (stepUp) {
    const stepped = await fixtureApiCall(confirmed.session_token ?? '', 'POST', '/api/v1/auth/totp/step-up', zLoginResult, { code: await ledger.next() });
    bearer = stepped.session_token ?? '';
  }
  return { username, password, principal: invitation.principal_id, ledger, bearer };
}

/**
 * signInAs replaces the page's session with a browser session for `account`,
 * answering its login challenge (#760) from the account's own ledger. The
 * session records `[password, totp]`, adequate for every MFA-mandatory surface.
 */
export async function signInAs(page: Page, account: EnrolledAccount): Promise<void> {
  await page.context().clearCookies();
  const login = await page.request.post(`${BASE_URL}/api/v1/auth/local/login`, {
    data: { username: account.username, password: account.password, artifact: 'browser' },
  });
  if (login.status() !== 202) {
    throw new Error(`signing ${account.username} in answered ${String(login.status())}, want a login challenge`);
  }
  const challenge = zLoginChallenge.parse(await login.json());
  const answered = await page.request.post(`${BASE_URL}/api/v1/auth/login/challenge/${challenge.challenge_id}/totp`, {
    data: { code: await account.ledger.next() },
  });
  if (!answered.ok()) {
    throw new Error(`${account.username}'s login challenge answered ${String(answered.status())}`);
  }
}
