// @vitest-environment happy-dom
import { act, useRef, useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle } from '../testkit/renderForm.tsx';
import { MintDialog } from './machineAccess/Credentials.tsx';
import {
  transitionMintLifecycle,
  type MintLifecycle,
  type MintLifecycleEvent,
  type MintRequest,
} from './mintLifecycle.ts';

const SENTINEL = 'hik_1_wl_SENTINEL_PLAINTEXT';

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const REQUEST: MintRequest = {
  id: 1,
  sessionId: 'ses_first',
  org: 'org_acme',
  project: 'prj_payments',
  accountId: 'mch_worker',
  accountName: 'worker',
  rotating: false,
  lifetimeId: 'default',
  reach: [],
};

function MintHarness() {
  const [lifecycle, setLifecycle] = useState<MintLifecycle>({
    kind: 'reviewing',
    request: REQUEST,
  });
  const lifecycleRef = useRef<MintLifecycle>(lifecycle);
  const move = (event: MintLifecycleEvent) => {
    const current = lifecycleRef.current;
    const next = transitionMintLifecycle(current, event);
    lifecycleRef.current = next;
    if (next !== current) {
      setLifecycle(next);
    }
    return { state: next, accepted: next !== current };
  };
  const isSubmitting = (requestId: number) =>
    lifecycleRef.current.kind === 'submitting' && lifecycleRef.current.request.id === requestId;

  return lifecycle.kind === 'idle' ? null : (
    <MintDialog lifecycle={lifecycle} move={move} isSubmitting={isSubmitting} />
  );
}

describe('MintDialog', () => {
  it('renders display-once plaintext without entering its QueryCache or MutationCache', async () => {
    const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) =>
      Promise.resolve(
        new Response(JSON.stringify({ value: SENTINEL, clamped: false, expires_at: null }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const { container, client } = await renderForm(<MintHarness />);
    // By NAME, never by position: the action row puts the primary last, so the
    // first button in the dialog is Cancel.
    const button = Array.from(container.querySelectorAll('button')).find(
      (candidate) => candidate.textContent === 'Mint credential',
    );
    if (button === undefined) {
      throw new Error('the mint dialog has no submit button');
    }
    await act(async () => button.click());
    await settle();

    expect(container.querySelector('.machine__token')?.textContent).toBe(SENTINEL);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(client.getQueryCache().getAll()).toEqual([]);
    expect(client.getMutationCache().getAll()).toEqual([]);
  });

  it.each([
    ['default', undefined, undefined],
    ['90d', '90d', 90 * 24 * 60 * 60],
  ])('posts the chosen lifetime (%s) and none for the default', async (_name, choice, seconds) => {
    const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) =>
      Promise.resolve(
        new Response(JSON.stringify({ value: SENTINEL, clamped: false, expires_at: null }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    // Only Date is faked: the dialog freezes its open time, and timers stay real.
    vi.useFakeTimers({ toFake: ['Date'] });
    const opened = Date.UTC(2030, 0, 1);
    vi.setSystemTime(opened);
    const { container } = await renderForm(<MintHarness />);
    const expiry = () => container.querySelector('.cred__meta time')?.getAttribute('datetime');
    expect(expiry()).toBe(new Date(opened + 30 * 24 * 60 * 60 * 1000).toISOString());
    const select = container.querySelector<HTMLSelectElement>('#mint-lifetime');
    if (select === null) {
      throw new Error('the mint dialog has no lifetime field');
    }
    expect(container.textContent).toContain('The instance lifetime ceiling may shorten it');
    if (choice !== undefined) {
      await act(async () => {
        select.value = choice;
        select.dispatchEvent(new Event('change', { bubbles: true }));
      });
    }
    expect(expiry()).toBe(
      new Date(opened + (seconds ?? 30 * 24 * 60 * 60) * 1000).toISOString(),
    );
    const button = Array.from(container.querySelectorAll('button')).find(
      (candidate) => candidate.textContent === 'Mint credential',
    );
    await act(async () => button?.click());
    await settle();
    const sent = fetchMock.mock.calls[0]?.[0];
    const body = sent instanceof Request ? await sent.text() : String(fetchMock.mock.calls[0]?.[1]?.body);
    expect(body).toBe(
      JSON.stringify(seconds === undefined ? {} : { lifetime_seconds: seconds }),
    );
  });

  it.each([
    ['the clipboard API is absent', undefined],
    [
      'clipboard access throws synchronously',
      {
        writeText: vi.fn(() => {
          throw new TypeError('clipboard unavailable');
        }),
      },
    ],
  ])('keeps the display-once value guarded when %s', async (_case, clipboard) => {
    const fetchMock = vi.fn((..._args: Parameters<typeof fetch>) =>
      Promise.resolve(
        new Response(JSON.stringify({ value: SENTINEL, clamped: false, expires_at: null }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('navigator', clipboard === undefined ? {} : { clipboard });

    const { container } = await renderForm(<MintHarness />);
    // Sentence case, as every other dialog title. The title id belongs to the
    // atom now, so the title is read off the dialog's own title element.
    expect(container.querySelector('dialog[open] .dialog__title')?.textContent).toMatch(
      /^Mint credential · /,
    );
    const mintButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'Mint credential',
    );
    if (mintButton === undefined) {
      throw new Error('the mint dialog has no mint button');
    }
    await act(async () => mintButton.click());
    await settle();

    // Explicit null is an indefinite lifetime, not missing expiry metadata.
    expect(container.textContent).toContain('Expiry: indefinite.');

    const copyButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'Copy to clipboard',
    );
    if (copyButton === undefined) {
      throw new Error('the disclosed mint dialog has no copy button');
    }
    await act(async () => copyButton.click());
    await settle();

    expect(container.querySelector('.machine__token')?.textContent).toBe(SENTINEL);
    expect(container.textContent).toContain(
      'This browser refused clipboard access, so nothing was copied.',
    );
    expect(container.querySelector<HTMLInputElement>('#mint-stored')?.checked).toBe(false);

    const doneButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'Done',
    );
    if (doneButton === undefined) {
      throw new Error('the disclosed mint dialog has no done button');
    }
    await act(async () => doneButton.click());
    await settle();

    expect(container.querySelector('.machine__token')?.textContent).toBe(SENTINEL);
    expect(container.textContent).toContain(
      'Confirm you have stored it: there is no second look at this value.',
    );
  });
});
