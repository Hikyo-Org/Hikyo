// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { RefusalFinding } from '../api/client.ts';
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
  it('renders only the redacted findings, never a value or excerpt', async () => {
    const view = await render([withToken], vi.fn().mockResolvedValue(undefined));
    const text = view.container.textContent ?? '';
    expect(text).toContain('aws-access-key');
    expect(text).toContain('app/API_KEY');
    expect(text).toContain('value_write');
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

  it('offers no override when a finding has no token — a hard block', async () => {
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
});
