// @vitest-environment happy-dom
import { act, useRef, useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle } from '../testkit/renderForm.tsx';
import { MintDialog } from './MachineAccess.tsx';
import {
  transitionMintLifecycle,
  type MintLifecycle,
  type MintLifecycleEvent,
  type MintRequest,
} from './mintLifecycle.ts';

const SENTINEL = 'hik_1_wl_SENTINEL_PLAINTEXT';

afterEach(() => {
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
        new Response(JSON.stringify({ value: SENTINEL, clamped: false }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);

    const { container, client } = await renderForm(<MintHarness />);
    const button = container.querySelector('button');
    if (!(button instanceof HTMLButtonElement)) {
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
        new Response(JSON.stringify({ value: SENTINEL, clamped: false }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('navigator', clipboard === undefined ? {} : { clipboard });

    const { container } = await renderForm(<MintHarness />);
    // Sentence case, as every other dialog title.
    expect(container.querySelector('#mint-title')?.textContent).toMatch(/^Mint credential · /);
    const mintButton = Array.from(container.querySelectorAll('button')).find(
      (button) => button.textContent === 'Mint credential',
    );
    if (mintButton === undefined) {
      throw new Error('the mint dialog has no mint button');
    }
    await act(async () => mintButton.click());
    await settle();

    // The mint result is narrowed to value and clamped, so the panel states the
    // instance default rather than an instant it does not hold.
    expect(container.textContent).toContain('Expiry: instance default.');

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
