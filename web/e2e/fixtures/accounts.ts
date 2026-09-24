import { randomBytes } from 'node:crypto';
import type { Browser } from '@playwright/test';
import { zInvitationResult, zLoginResult, zTotpEnrolStartResult } from '@hikyo/zod';

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
  readonly ledger: TotpLedger;
  /** A CLI session stepped up with the account's second code (instance scope only). */
  readonly bearer: string;
};

/**
 * enrolledAccount invites a fresh human (through the suite's stored,
 * stepped-up administrator session: no administrator code is drawn),
 * establishes its password and enrols an authenticator. `scope` names where
 * the invitation lands: the seeded org with no template (it signs in and sees
 * nothing), or the instance with the `operator` template (an instance
 * operator that can administer registration). The operator's CLI session is
 * stepped up with its second code; a proof is its third.
 */
export async function enrolledAccount(browser: Browser, label: string, scope: 'org' | 'instance'): Promise<EnrolledAccount> {
  const username = `${label}-${Date.now().toString(36)}-${randomBytes(4).toString('hex')}`;
  const password = randomBytes(24).toString('base64url');
  const admin = await browser.newContext({ storageState: STORAGE_STATE });
  const invitation = await (async () => {
    try {
      const page = await admin.newPage();
      return scope === 'org'
        ? await browserApi(page, 'POST', `/api/v1/orgs/${readSeed().org}/invitations`, zInvitationResult, { username })
        : await browserApi(page, 'POST', '/api/v1/instance/invitations', zInvitationResult, { username, template: 'operator' });
    } finally {
      await admin.close();
    }
  })();
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
  let bearer = confirmed.session_token ?? '';
  if (scope === 'instance') {
    const stepped = await fixtureApiCall(bearer, 'POST', '/api/v1/auth/totp/step-up', zLoginResult, { code: await ledger.next() });
    bearer = stepped.session_token ?? '';
  }
  return { username, password, ledger, bearer };
}
