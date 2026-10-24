import { createHash } from 'node:crypto';
import { createServer } from 'node:http';
import { request } from '@playwright/test';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { z } from 'zod';

const fixture = vi.hoisted(() => ({ base: '' }));
vi.mock('./instance.ts', () => ({
  ADMIN: { username: 'fixture-admin', password: 'fixture-password' },
  get BASE_URL() { return fixture.base; },
}));

import { BrowserApiError, fixtureApiCall, fixtureBearer } from './api.ts';

const id = '00000000-0000-0000-0000-000000000001';
const expires = '2099-01-01T00:00:00Z';
function loginResult(artifact: 'browser' | 'cli') {
  return {
    ...(artifact === 'cli' ? { session_token: 'fixture-cli-bearer' } : {}),
    session: {
      id: `ses_${id}`, artifact, created_at: expires,
      idle_expires_at: expires, absolute_expires_at: expires,
      assurance: { method: 'local-password', factors: ['password', 'totp'], authenticated_at: expires },
    },
    principal: { id: `prn_${id}`, kind: 'human' },
  };
}

describe('fixtureBearer real HTTP ceremony', () => {
  const servers: ReturnType<typeof createServer>[] = [];
  afterEach(async () => {
    vi.restoreAllMocks();
    await Promise.all(servers.splice(0).map((server) => new Promise<void>((resolve, reject) => {
      server.close((error) => error ? reject(error) : resolve());
      server.closeAllConnections();
    })));
  });

  async function serve(options: { enrolled?: boolean; enrolmentRequired?: boolean; wrongBootstrapArtifact?: boolean; omitBootstrapToken?: boolean; wrongState?: boolean; refuseLogout?: boolean; refuseCode?: boolean; retryCode?: boolean } = {}) {
    const calls: { path: string; body: Record<string, string>; cookie: string; csrf: string | string[] | undefined }[] = [];
    let state = '';
    let challenge = '';
    let redirect = '';
    let codeAttempts = 0;
    const server = createServer(async (req, res) => {
      let raw = '';
      for await (const chunk of req) raw += String(chunk);
      const body = z.record(z.string(), z.string()).parse(raw === '' ? {} : JSON.parse(raw));
      const path = req.url ?? '';
      calls.push({ path, body, cookie: req.headers.cookie ?? '', csrf: req.headers['x-hikyo-csrf'] });
      res.setHeader('Content-Type', 'application/json');
      const browser = () => {
        res.setHeader('Set-Cookie', [
          '__Host-hikyo=fixture-browser-cookie; Secure; HttpOnly; Path=/; SameSite=Strict',
          '__Host-hikyo-csrf=fixture-csrf; Secure; Path=/; SameSite=Strict',
        ]);
        res.end(JSON.stringify(loginResult('browser')));
      };
      if (path === '/api/v1/auth/local/login') {
        if (body['artifact'] === 'cli') {
          if (options.enrolled) {
            res.statusCode = 400;
            res.end('{}');
          } else {
            const result = loginResult(options.wrongBootstrapArtifact ? 'browser' : 'cli');
            if (options.omitBootstrapToken) delete result.session_token;
            result.session.assurance.factors = ['password'];
            res.end(JSON.stringify(result));
          }
        } else if (options.enrolled) {
          res.statusCode = 202;
          res.end(JSON.stringify({ challenge_id: `chl_${id}`, expires_at: expires, factors: ['totp'] }));
        } else browser();
      } else if (path === `/api/v1/auth/login/challenge/chl_${id}/totp`) {
        codeAttempts++;
        if (options.refuseCode || (options.retryCode && codeAttempts === 1)) {
          res.statusCode = options.refuseCode ? 401 : 409;
          res.end('{}');
        } else browser();
      } else if (path === '/api/v1/auth/cli-login/start') {
        const start = z.object({ state: z.string(), pkce_challenge: z.string(), redirect_uri: z.string() }).parse(body);
        state = start.state;
        challenge = start.pkce_challenge;
        redirect = start.redirect_uri;
        res.end(JSON.stringify({ state, device_code: '', user_code: '', verification_uri: '', expires_at: expires, interval: 5 }));
      } else if (path === '/api/v1/auth/cli-login/approve') {
        if (options.enrolmentRequired && !options.enrolled) {
          // The real artifact admission gate hides this operation from a
          // password-only session until required enrolment is complete.
          res.statusCode = 404;
          res.end('{}');
        } else res.end(JSON.stringify({ code: 'fixture-code', state: options.wrongState ? 'wrong-state' : state, redirect_uri: redirect }));
      } else if (path === '/api/v1/auth/cli-login/redeem') {
        const redeem = z.object({ code: z.literal('fixture-code'), pkce_verifier: z.string(), redirect_uri: z.literal(redirect) }).parse(body);
        if (createHash('sha256').update(redeem.pkce_verifier).digest('base64url') !== challenge) res.statusCode = 403;
        res.end(JSON.stringify(loginResult('cli')));
      } else if (path === '/api/v1/auth/logout') {
        res.statusCode = options.refuseLogout ? 500 : 204;
        res.end();
      } else if (path === '/api/v1/auth/whoami') {
        res.end(JSON.stringify({ enrolment_required: options.enrolmentRequired === true }));
      } else { res.statusCode = 404; res.end('{}'); }
    });
    servers.push(server);
    await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
    const address = server.address();
    if (address === null || typeof address === 'string') throw new Error('fixture HTTP listener has no port');
    fixture.base = `http://localhost:${address.port}`;
    return calls;
  }

  it('completes an enrolled browser challenge then approves and redeems a bound CLI handoff', async () => {
    const calls = await serve({ enrolled: true });
    const draw = vi.fn(async () => '123456');
    expect(await fixtureBearer('enrolled fixture', draw)).toBe('fixture-cli-bearer');
    expect(draw).toHaveBeenCalledOnce();
    expect(calls.map((call) => call.path)).toEqual([
      '/api/v1/auth/local/login', `/api/v1/auth/login/challenge/chl_${id}/totp`,
      '/api/v1/auth/cli-login/start', '/api/v1/auth/cli-login/approve',
      '/api/v1/auth/cli-login/redeem', '/api/v1/auth/logout',
    ]);
    expect(calls[0]?.body).toEqual({ username: 'fixture-admin', password: 'fixture-password', artifact: 'browser' });
    expect(calls[1]?.body).toEqual({ code: '123456' });
    expect(calls[3]?.body).toEqual({ reference: calls[2]?.body['state'] });
    for (const call of calls.filter((call) => call.path.endsWith('/approve') || call.path.endsWith('/logout'))) {
      expect(call.cookie).toContain('__Host-hikyo=fixture-browser-cookie');
      expect(call.csrf).toBe('fixture-csrf');
    }
    // Public redemption gets no approving browser cookie or CSRF authority.
    expect(calls[4]?.cookie).toBe('');
    expect(calls[4]?.csrf).toBeUndefined();
  });

  it('supports unenrolled setup without spending a TOTP code', async () => {
    const calls = await serve();
    const draw = vi.fn(async () => '123456');
    expect(await fixtureBearer('unenrolled fixture', draw)).toBe('fixture-cli-bearer');
    expect(draw).not.toHaveBeenCalled();
    expect(calls.some((call) => call.path.includes('/challenge/'))).toBe(false);
  });

  it('preserves the restricted CLI bootstrap session when enrolment is required', async () => {
    const calls = await serve({ enrolmentRequired: true });
    expect(await fixtureBearer('initial setup')).toBe('fixture-cli-bearer');
    expect(await fixtureApiCall('fixture-cli-bearer', 'GET', '/api/v1/auth/whoami',
      z.object({ enrolment_required: z.boolean() }))).toEqual({ enrolment_required: true });
    expect(calls.map((call) => call.path)).toEqual(['/api/v1/auth/local/login', '/api/v1/auth/whoami']);
    expect(calls[0]?.body['artifact']).toBe('cli');
  });

  it.each([{ wrongBootstrapArtifact: true }, { omitBootstrapToken: true }])(
    'rejects a bootstrap response without a CLI bearer: %j', async (options) => {
      const calls = await serve(options);
      await expect(fixtureBearer('invalid bootstrap')).rejects.toThrow('CLI bootstrap did not return a bearer');
      expect(calls).toHaveLength(1);
    },
  );

  it('draws a new ledger code after a replay refusal on the same challenge', async () => {
    const calls = await serve({ enrolled: true, retryCode: true });
    const draw = vi.fn<() => Promise<string>>()
      .mockResolvedValueOnce('123456').mockResolvedValueOnce('654321');
    expect(await fixtureBearer('replayed step', draw)).toBe('fixture-cli-bearer');
    expect(calls.filter((call) => call.path.includes('/challenge/')).map((call) => call.body)).toEqual([
      { code: '123456' }, { code: '654321' },
    ]);
  });

  it('fails clearly if an enrolled caller supplies no TOTP ledger', async () => {
    const calls = await serve({ enrolled: true });
    await expect(fixtureBearer('missing ledger')).rejects.toThrow('enrolled accounts require a TOTP ledger and browser MFA');
    expect(calls).toHaveLength(1);
    expect(calls[0]?.body['artifact']).toBe('cli');
  });

  it('never redeems approval for another state and revokes the temporary browser session', async () => {
    const calls = await serve({ wrongState: true });
    await expect(fixtureBearer('wrong state', async () => '123456')).rejects.toThrow('CLI approval binding');
    expect(calls.some((call) => call.path.endsWith('/redeem'))).toBe(false);
    expect(calls.at(-1)?.path).toBe('/api/v1/auth/logout');
  });

  it('propagates temporary browser logout failures', async () => {
    await serve({ refuseLogout: true });
    await expect(fixtureBearer('logout failure', async () => '123456')).rejects.toThrow('logout answered 500');
  });

  it('bounds refused TOTP attempts and never starts a CLI transaction', async () => {
    const calls = await serve({ enrolled: true, refuseCode: true });
    const draw = vi.fn(async () => '123456');
    await expect(fixtureBearer('refused factor', draw)).rejects.toThrow('TOTP challenge answered 401');
    expect(draw).toHaveBeenCalledTimes(4);
    expect(calls.some((call) => call.path.includes('/cli-login/'))).toBe(false);
  });

  it('disposes the first context when allocating the second one fails', async () => {
    const browser = await request.newContext();
    const originalDispose = browser.dispose.bind(browser);
    const dispose = vi.spyOn(browser, 'dispose');
    vi.spyOn(request, 'newContext').mockResolvedValueOnce(browser)
      .mockRejectedValueOnce(new Error('second context allocation failed'));
    try {
      await expect(fixtureBearer('partial allocation', async () => '123456')).rejects.toThrow('second context allocation failed');
      expect(dispose).toHaveBeenCalledOnce();
    } finally {
      await originalDispose();
    }
  });

  it('waits for every disposal and preserves protocol, logout and disposal failures', async () => {
    await serve({ wrongState: true, refuseLogout: true });
    const browser = await request.newContext({ baseURL: fixture.base });
    const cli = await request.newContext({ baseURL: fixture.base });
    const disposeBrowser = browser.dispose.bind(browser);
    const disposeCLI = cli.dispose.bind(cli);
    let release = () => {};
    const held = new Promise<void>((resolve) => { release = resolve; });
    const browserCleanup = vi.spyOn(browser, 'dispose').mockImplementation(async () => {
      await disposeBrowser();
      throw new Error('browser disposal failed');
    });
    const cliCleanup = vi.spyOn(cli, 'dispose').mockImplementation(async () => {
      await held;
      await disposeCLI();
      throw new Error('CLI disposal failed');
    });
    vi.spyOn(request, 'newContext').mockResolvedValueOnce(browser).mockResolvedValueOnce(cli);
    let settled = false;
    const outcome = fixtureBearer('combined failures', async () => '123456').then(
      (value) => ({ value }), (error) => ({ error }),
    ).finally(() => { settled = true; });
    try {
      await vi.waitFor(() => {
        expect(browserCleanup).toHaveBeenCalledOnce();
        expect(cliCleanup).toHaveBeenCalledOnce();
      });
      expect(settled).toBe(false);
      release();
      expect(await outcome).toMatchObject({ error: {
        errors: [
          { errors: [
            { message: 'combined failures CLI approval binding did not match' },
            { message: 'combined failures browser logout answered 500' },
          ] },
          { message: 'browser disposal failed' },
          { message: 'CLI disposal failed' },
        ],
      } });
    } finally {
      release();
      await outcome;
      await Promise.all([disposeBrowser(), disposeCLI()]);
    }
  });
});

describe('BrowserApiError', () => {
  it('keeps the response status and body as typed error data', () => {
    const error = new BrowserApiError('DELETE', '/api/v1/orgs/missing', 404, '{"error":"gone"}');

    expect(error).toBeInstanceOf(Error);
    expect(error.name).toBe('BrowserApiError');
    expect(error.status).toBe(404);
    expect(error.body).toBe('{"error":"gone"}');
    expect(error.message).toBe(
      'DELETE /api/v1/orgs/missing answered 404: {"error":"gone"}',
    );
  });
});
