// @vitest-environment happy-dom
import { act, useState, type ReactNode } from 'react';
import { afterEach, describe, expect, it } from 'vitest';

import type { RetentionDayState } from '../api/settings.ts';
import { renderForm } from '../testkit/renderForm.tsx';
import { RetentionBoundsFields } from './RetentionBoundsFields.tsx';

const cleanups: Array<() => Promise<void>> = [];

afterEach(async () => {
  for (const cleanup of cleanups.splice(0)) {
    await cleanup();
  }
});

async function render(node: ReactNode): Promise<HTMLElement> {
  const rendered = await renderForm(node);
  cleanups.push(rendered.unmount);
  return rendered.container;
}

function controlByLabel(container: HTMLElement, text: string): HTMLElement {
  const label = [...container.querySelectorAll('label')].find(
    (candidate) => candidate.textContent === text,
  );
  const control = label?.control;
  if (!(control instanceof HTMLElement)) {
    throw new Error(`no control labelled ${text}`);
  }
  return control;
}

async function click(button: Element): Promise<void> {
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error('expected a button');
  }
  await act(async () => button.click());
}

function BoundsHarness() {
  const [age, setAge] = useState<RetentionDayState>({ kind: 'exact', seconds: 90 });
  const [count, setCount] = useState('5');
  return (
    <RetentionBoundsFields
      age={age}
      count={count}
      onAgeChange={setAge}
      onCountChange={setCount}
    />
  );
}

describe('RetentionBoundsFields', () => {
  it('protects an exact-seconds age until it is deliberately replaced with days', async () => {
    const container = await render(<BoundsHarness />);

    expect(container.querySelector('[role="alert"]')?.textContent).toContain(
      'Current maximum age is exact (90 seconds)',
    );
    const age = controlByLabel(container, 'Maximum age, in days');
    if (!(age instanceof HTMLInputElement)) {
      throw new Error('maximum age is not an input');
    }
    expect(age.disabled).toBe(true);

    const replace = [...container.querySelectorAll('button')].find(
      (candidate) => candidate.textContent === 'Replace with whole days',
    );
    if (replace === undefined) {
      throw new Error('replace action is missing');
    }
    await click(replace);

    expect(age.disabled).toBe(false);
    expect(age.value).toBe('');
    expect(container.querySelector('[role="alert"]')).toBeNull();
  });
});
