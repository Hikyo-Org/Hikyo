// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ApiError, type RefusalFinding } from '../api/client.ts';
import { ScanBlockDialog } from './ScanBlockDialog.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const withToken: RefusalFinding = {
  rule_id: 'aws-access-key',
  surface: 'value_write',
  locator: 'app/API_KEY',
  acknowledgement: 'ack-token-1',
};
const withoutToken: RefusalFinding = {
  rule_id: 'high-entropy',
  surface: 'edit',
  locator: 'app/SECRET',
};

afterEach(() => {
  document.body.innerHTML = '';
});

async function render(
  findings: readonly RefusalFinding[],
  onOverride: ((tokens: readonly string[]) => Promise<void>) | null,
) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  await act(async () => {
    root.render(
      <ScanBlockDialog
        title="Declaration blocked by secret scanning"
        intro="Declaring API_KEY was refused."
        findings={findings}
        onOverride={onOverride}
        onClose={vi.fn()}
      />,
    );
  });
  return { container, unmount: () => act(async () => root.unmount()) };
}

function button(container: HTMLElement, label: string): HTMLButtonElement | undefined {
  return [...container.querySelectorAll('button')].find((candidate) =>
    (candidate.textContent ?? '').includes(label),
  );
}

describe('ScanBlockDialog', () => {
  it('renders only the rule id and locator, never the surface, a value, or excerpt', async () => {
    const view = await render([withToken], vi.fn().mockResolvedValue(undefined));
    const text = view.container.textContent ?? '';
    expect(text).toContain('aws-access-key');
    expect(text).toContain('app/API_KEY');
    // The ADR §4 shape is rule id + locator ONLY; the surface enum is not shown.
    expect(text).not.toContain('value_write');
    await view.unmount();
  });

  it('overrides with every acknowledgement token when all findings carry one', async () => {
    const onOverride = vi.fn<(tokens: readonly string[]) => Promise<void>>().mockResolvedValue(undefined);
    const view = await render([withToken], onOverride);
    const acknowledge = button(view.container, 'Acknowledge and continue');
    expect(acknowledge).toBeDefined();
    await act(async () => acknowledge!.click());
    expect(onOverride).toHaveBeenCalledWith(['ack-token-1']);
    await view.unmount();
  });

  it('offers no override when a finding has no token, a hard block', async () => {
    const onOverride = vi.fn<(tokens: readonly string[]) => Promise<void>>().mockResolvedValue(undefined);
    const view = await render([withToken, withoutToken], onOverride);
    expect(button(view.container, 'Acknowledge and continue')).toBeUndefined();
    expect(view.container.textContent ?? '').toContain('cannot be overridden');
    await view.unmount();
  });

  it('offers no override when the caller supplies none', async () => {
    const view = await render([withToken], null);
    expect(button(view.container, 'Acknowledge and continue')).toBeUndefined();
    await view.unmount();
  });

  it("surfaces the server's named refusal verbatim when an override is rejected", async () => {
    // A content-bound token the field outran is rejected BY NAME (#183, ADR §4):
    // stale / version-skew / surplus / expired, carried on the caller-safe
    // detail, never as matched text. The dialog must show that reason, not a
    // generic line, so the operator learns why the token no longer holds.
    const named = 'token #1 (key.description/aws-access-key): stale, the field content changed since the token was minted';
    const onOverride = vi
      .fn<(tokens: readonly string[]) => Promise<void>>()
      .mockRejectedValue(new ApiError(400, 'refused', named));
    const view = await render([withToken], onOverride);
    await act(async () => button(view.container, 'Acknowledge and continue')!.click());
    const alert = view.container.querySelector('[role="alert"]');
    expect(alert?.textContent ?? '').toContain(named);
    // The value/excerpt still never appears, only the redacted reason.
    expect(view.container.textContent ?? '').not.toContain('AKIA');
    await view.unmount();
  });

  it('falls back to a generic message when a rejection carries no safe detail', async () => {
    const onOverride = vi
      .fn<(tokens: readonly string[]) => Promise<void>>()
      .mockRejectedValue(new Error('network'));
    const view = await render([withToken], onOverride);
    await act(async () => button(view.container, 'Acknowledge and continue')!.click());
    const alert = view.container.querySelector('[role="alert"]');
    expect(alert?.textContent ?? '').toContain('The override was refused');
    await view.unmount();
  });
});
