import type { Browser } from '@playwright/test';
import { zInvitationResult, zLoginResult, zTotpEnrolStartResult } from '@hikyo/zod';

import { browserApi, fixtureApiCall } from './api.ts';
import { BASE_URL, readSeed, STORAGE_STATE } from './instance.ts';
import { totpCode } from './seed.ts';

const TOTP_PERIOD = 30;

/**
 * TotpLedger is ONE account's spent-step ledger, owned by the flow that
 * created the account. The shared administrator's ledger is drawn by the
 * seeding session, fixture step-ups and both projects at once, so a third
 * draw inside one 30-second step has to wait out a boundary; a flow that
 * signs in repeatedly therefore gets its own account instead.
 *
 * The server accepts a code for the step before, at, or after now, strictly
 * beyond the last step it consumed. So a fresh account presents three codes
 * inside any one step without waiting: the previous step, the current one,
 * then the next. Flows keep to three draws per account.
 */
export class TotpLedger {
  private last = Number.NEGATIVE_INFINITY;

  constructor(readonly otpauth: string) {}

  next(): string {
    const now = Math.floor(Date.now() / 1000 / TOTP_PERIOD);
    const want = Math.max(now - 1, this.last + 1);
    if (want > now + 1) {
      throw new Error('TotpLedger: a fourth code inside one step; give the flow another account instead of waiting');
    }
    this.last = want;
    return totpCode(this.otpauth, new Date(want * TOTP_PERIOD * 1000));
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
 * stepped up with its second code, leaving it one code for a proof.
 */
export async function enrolledAccount(browser: Browser, label: string, scope: 'org' | 'instance'): Promise<EnrolledAccount> {
  const username = `${label}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;
  const password = `a password for ${username}`;
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
  const ledger = new TotpLedger(enrolled.otpauth_uri);
  const confirmed = await fixtureApiCall(session, 'POST', '/api/v1/auth/totp/enrol/confirm', zLoginResult, { code: ledger.next() });
  let bearer = confirmed.session_token ?? '';
  if (scope === 'instance') {
    const stepped = await fixtureApiCall(bearer, 'POST', '/api/v1/auth/totp/step-up', zLoginResult, { code: ledger.next() });
    bearer = stepped.session_token ?? '';
  }
  return { username, password, ledger, bearer };
}
