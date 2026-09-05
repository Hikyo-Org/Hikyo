// @vitest-environment happy-dom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { ScanWarnDialog, type ScanWarnItem } from './ScanWarnDialog.tsx';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const item: ScanWarnItem = {
  environmentId: 'env_a',
  environmentName: 'development',
  value: 'AKIA-looking-value',
  finding: {
    rule_id: 'aws-access-key',
    surface: 'value_write',
    locator: 'app/API_KEY',
    acknowledgement: 'ack-1',
  },
};

afterEach(() => {
  document.body.innerHTML = '';
});

describe('ScanWarnDialog', () => {
  it('labels the dialog by its heading, shows the locator, and offers exactly two actions', async () => {
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    await act(async () => {
      root.render(
        <ScanWarnDialog
          keyName="API_KEY"
          items={[item]}
          onDismiss={vi.fn().mockResolvedValue([])}
          onReclassify={vi.fn().mockResolvedValue(undefined)}
          onClose={vi.fn()}
        />,
      );
    });
    const dialog = container.querySelector('dialog');
    const heading = dialog?.querySelector('h2');
    expect(dialog?.getAttribute('aria-labelledby')).toBe(heading?.id);
    expect(heading?.id).toBeTruthy();
    expect(container.querySelector('.scan-warn__locator')?.textContent).toBe('app/API_KEY');
    // The value itself never renders.
    expect(container.textContent).not.toContain('AKIA-looking-value');
    const labels = [...container.querySelectorAll('button')].map((node) => node.textContent);
    expect(labels).toEqual(['✕', 'Keep as config', 'Reclassify API_KEY as secret']);
    await act(async () => root.unmount());
  });
});
