import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { installSessionFence, settleSessionEpoch } from '../api/sessionEpoch.ts';
import {
  type ShellReauthRequest,
  ContinueInBrowserError,
  ShellError,
  continueInBrowserReason,
  dismissContinueInBrowser,
  instanceOrigin,
  isAppMode,
  knownShellOrigin,
  requireBrowser,
  shellFailureText,
  shellClipboardCopied,
  shellDownloadBytes,
  shellDownloadPath,
  shellOpenInBrowser,
  shellOrigin,
  shellReauth,
  shellSignIn,
} from './shell.ts';

const fetcher = vi.fn<(route: string, init: RequestInit) => Promise<Response>>();

function appMode(protocol = 'wails:') {
  vi.stubGlobal('location', { protocol, origin: `${protocol}//wails`, pathname: '/orgs/o', search: '?q=1' });
}
function body(init: RequestInit | undefined): unknown {
  return typeof init?.body === 'string' ? JSON.parse(init.body) : undefined;
}
const noContent = () => new Response(null, { status: 204 });
const refused = (error: string, status = 409) =>
  new Response(JSON.stringify({ error, message: 'no' }), { status, headers: { 'Content-Type': 'application/json' } });

let uninstall: () => void;
beforeEach(() => {
  fetcher.mockReset();
  vi.stubGlobal('fetch', fetcher);
  appMode();
  // A reauth proves the owner afterwards; the identity check here always finds it unchanged.
  uninstall = installSessionFence(() => undefined, () => undefined, async () => { settleSessionEpoch(); });
});
afterEach(() => {
  uninstall();
  vi.unstubAllGlobals();
  dismissContinueInBrowser();
});

describe('detection', () => {
  it('is app mode under wails: only', () => {
    expect(isAppMode()).toBe(true);
    for (const protocol of ['https:', 'http:', 'file:']) {
      appMode(protocol);
      expect(isAppMode()).toBe(false);
    }
  });

  it('is not app mode where there is no location', () => {
    vi.unstubAllGlobals();
    expect(isAppMode()).toBe(false);
  });
});

describe('reauth', () => {
  const reveal: ShellReauthRequest = { purpose: 'reveal', operation: 'value.reveal', environment_ids: ['env'], key_ids: ['k'] };

  it('posts exactly the contract body and resolves on 204', async () => {
    fetcher.mockResolvedValue(noContent());
    await shellReauth(reveal);
    expect(fetcher).toHaveBeenCalledWith('/__shell/reauth', expect.objectContaining({ method: 'POST' }));
    expect(body(fetcher.mock.calls[0]?.[1])).toEqual(reveal);
  });

  it('carries a self-config intent with no environments', async () => {
    fetcher.mockResolvedValue(noContent());
    const self_config = {
      action: 'apply', owner_instance_id: 'inst', revision: 3, schema_version: 1, expected_generation: 2,
      preview_token: '', to: '', confirm_restored_credentials: false,
    } as const;
    await shellReauth({ purpose: 'self-config', operation: 'self-config.apply', environment_ids: [], self_config });
    expect(body(fetcher.mock.calls[0]?.[1])).toEqual({ purpose: 'self-config', operation: 'self-config.apply', environment_ids: [], self_config });
  });

  it('refuses a self-config request that names an environment, or an environment request that carries an intent', async () => {
    const self_config = {
      action: 'apply', owner_instance_id: 'inst', revision: 3, schema_version: 1, expected_generation: 2,
      preview_token: '', to: '', confirm_restored_credentials: false,
    } as const;
    await expect(shellReauth({ purpose: 'self-config', operation: 'self-config.apply', environment_ids: ['env'], self_config })).rejects.toThrow();
    await expect(shellReauth({ ...reveal, self_config })).rejects.toThrow();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('sends the whole environment set for an adapter, with no key_ids', async () => {
    fetcher.mockResolvedValue(noContent());
    await shellReauth({ purpose: 'adapter', operation: 'adapter.sync', environment_ids: ['a', 'b'] });
    expect(body(fetcher.mock.calls[0]?.[1])).toEqual({ purpose: 'adapter', operation: 'adapter.sync', environment_ids: ['a', 'b'] });
  });

  it('never sends developer-credential', async () => {
    // @ts-expect-error the type refuses it, and so does the schema
    await expect(shellReauth({ purpose: 'developer-credential', operation: 'developer-credential.mint', environment_ids: ['e'] })).rejects.toThrow();
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('refuses more than 500 keys with a Continue in browser offer, without calling the shell', async () => {
    const keys = Array.from({ length: 501 }, (_, index) => `k${index}`);
    await expect(shellReauth({ ...reveal, key_ids: keys })).rejects.toBeInstanceOf(ContinueInBrowserError);
    expect(continueInBrowserReason()).not.toBeNull();
    expect(fetcher).not.toHaveBeenCalled();
    fetcher.mockResolvedValue(noContent());
    await expect(shellReauth({ ...reveal, key_ids: keys.slice(0, 500) })).resolves.toBeUndefined();
  });

  it('refuses a request outside the per-purpose shape before calling the shell', async () => {
    const adapter: ShellReauthRequest = { purpose: 'adapter', operation: 'adapter.sync', environment_ids: ['a', 'b'] };
    const bad: ShellReauthRequest[] = [
      { ...reveal, key_ids: [] },
      { ...reveal, key_ids: undefined },
      { ...reveal, environment_ids: ['a', 'b'] },
      { ...adapter, key_ids: ['k'] },
      { ...adapter, environment_ids: [] },
    ];
    for (const request of bad) await expect(shellReauth(request)).rejects.toThrow();
    expect(fetcher).not.toHaveBeenCalled();
    fetcher.mockResolvedValue(noContent());
    await expect(shellReauth(adapter)).resolves.toBeUndefined();
  });

  it('refuses more than 1000 environments with a Continue in browser offer', async () => {
    const environments = Array.from({ length: 1001 }, (_, index) => `e${index}`);
    await expect(shellReauth({ purpose: 'adapter', operation: 'adapter.sync', environment_ids: environments }))
      .rejects.toBeInstanceOf(ContinueInBrowserError);
    expect(fetcher).not.toHaveBeenCalled();
    fetcher.mockResolvedValue(noContent());
    await expect(shellReauth({ purpose: 'adapter', operation: 'adapter.sync', environment_ids: environments.slice(0, 1000) })).resolves.toBeUndefined();
  });

  it.each(['busy', 'rate_limited', 'cancelled', 'failed', 'invalid'] as const)('maps the %s error body', async (code) => {
    fetcher.mockResolvedValue(refused(code));
    const error = await shellReauth(reveal).catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(ShellError);
    expect(error).toMatchObject({ code });
  });

  it('offers Continue in browser when the shell refuses a reauth', async () => {
    fetcher.mockResolvedValue(refused('refused'));
    await expect(shellReauth(reveal)).rejects.toBeInstanceOf(ContinueInBrowserError);
    expect(continueInBrowserReason()).not.toBeNull();
  });

  it('words each shell failure and leaves other errors alone', () => {
    expect(shellFailureText(new ShellError('cancelled', 'x'))).toContain('cancelled');
    expect(shellFailureText(new ShellError('busy', 'x'))).toContain('already open');
    expect(shellFailureText(new Error('x'))).toBeUndefined();
  });

  it('treats a body outside the contract as a failed hop', async () => {
    fetcher.mockResolvedValue(new Response(JSON.stringify({ error: 'nope', message: 'x', extra: 1 }), { status: 500 }));
    await expect(shellReauth(reveal)).rejects.toMatchObject({ code: 'failed' });
  });

  it('treats a 200 where the contract says 204 as a failed hop', async () => {
    fetcher.mockResolvedValue(new Response('{}', { status: 200 }));
    await expect(shellReauth(reveal)).rejects.toMatchObject({ code: 'failed' });
  });

  it('treats an unreachable shell as failed', async () => {
    fetcher.mockRejectedValue(new TypeError('network'));
    await expect(shellReauth(reveal)).rejects.toMatchObject({ code: 'failed' });
  });
});

describe('sign-in', () => {
  it('shares one call among concurrent callers', async () => {
    let finish: (response: Response) => void = () => undefined;
    fetcher.mockReturnValue(new Promise<Response>((resolve) => { finish = resolve; }));
    const first = shellSignIn();
    const second = shellSignIn();
    expect(second).toBe(first);
    finish(noContent());
    await first;
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(body(fetcher.mock.calls[0]?.[1])).toEqual({});
    fetcher.mockResolvedValue(noContent());
    await shellSignIn();
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});

describe('open-in-browser', () => {
  it('sends an absolute path and refuses a URL or protocol-relative path locally', async () => {
    fetcher.mockResolvedValue(noContent());
    await shellOpenInBrowser('/orgs/o?q=1');
    expect(body(fetcher.mock.calls[0]?.[1])).toEqual({ path: '/orgs/o?q=1' });
    await expect(shellOpenInBrowser('https://evil.example/')).rejects.toThrow();
    await expect(shellOpenInBrowser('//evil.example/')).rejects.toThrow();
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
});

describe('download', () => {
  it('sends a proxied path, and refuses any other path', async () => {
    fetcher.mockResolvedValue(noContent());
    await expect(shellDownloadPath('/api/v1/orgs/o/definitions', 'definitions.json')).resolves.toBe(true);
    expect(body(fetcher.mock.calls[0]?.[1])).toEqual({ path: '/api/v1/orgs/o/definitions', filename: 'definitions.json' });
    await expect(shellDownloadPath('/login/cli', 'x')).rejects.toThrow();
  });

  it('sends bytes as base64, including a large body', async () => {
    fetcher.mockResolvedValue(noContent());
    const bytes = new Uint8Array(100_000).fill(65);
    await shellDownloadBytes(bytes, 'audit.jsonl', 'application/x-ndjson');
    const sent = body(fetcher.mock.calls[0]?.[1]);
    expect(sent).toMatchObject({ filename: 'audit.jsonl', content_type: 'application/x-ndjson' });
    expect(atob(String((sent as { bytes_base64: string }).bytes_base64))).toHaveLength(100_000);
  });

  it('refuses a body over 64 MiB with a Continue in browser offer, without calling the shell', async () => {
    await expect(shellDownloadBytes(new Uint8Array(64 * 1024 * 1024 + 1), 'big', 'application/x-ndjson'))
      .rejects.toBeInstanceOf(ContinueInBrowserError);
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('resolves false on cancel and rejects on failure', async () => {
    fetcher.mockResolvedValueOnce(refused('cancelled'));
    await expect(shellDownloadPath('/api/v1/x', 'x')).resolves.toBe(false);
    fetcher.mockResolvedValueOnce(refused('failed', 500));
    await expect(shellDownloadPath('/api/v1/x', 'x')).rejects.toMatchObject({ code: 'failed' });
  });
});

describe('clipboard and origin', () => {
  it('arms clipboard clearing with an empty body', async () => {
    fetcher.mockResolvedValue(noContent());
    await shellClipboardCopied();
    expect(fetcher).toHaveBeenCalledWith('/__shell/clipboard', expect.objectContaining({ method: 'POST' }));
    expect(body(fetcher.mock.calls[0]?.[1])).toEqual({});
  });

  it('requires 200 for the origin, not any 2xx', async () => {
    fetcher.mockResolvedValue(new Response(JSON.stringify({ origin: 'https://hikyo.example' }), { status: 203 }));
    await expect(shellOrigin()).rejects.toMatchObject({ code: 'failed' });
  });

  it('reads the origin once, parses it, and serves it synchronously afterwards', async () => {
    expect(() => instanceOrigin()).toThrow(ShellError);
    fetcher.mockResolvedValue(new Response(JSON.stringify({ origin: 'https://hikyo.example' }), { status: 200 }));
    await expect(shellOrigin()).resolves.toBe('https://hikyo.example');
    await shellOrigin();
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher).toHaveBeenCalledWith('/__shell/origin', { method: 'GET' });
    expect(knownShellOrigin()).toBe('https://hikyo.example');
    expect(instanceOrigin()).toBe('https://hikyo.example');
    appMode('https:');
    expect(instanceOrigin()).toBe('https://wails');
  });
});

describe('requireBrowser', () => {
  it('is a no-op in a browser and an offer in the app', () => {
    appMode('https:');
    expect(() => requireBrowser('x')).not.toThrow();
    expect(continueInBrowserReason()).toBeNull();
    appMode();
    expect(() => requireBrowser('Adding a passkey needs your browser.')).toThrow(ContinueInBrowserError);
    expect(continueInBrowserReason()).toBe('Adding a passkey needs your browser.');
  });
});
