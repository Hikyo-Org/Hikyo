// @vitest-environment happy-dom
import { act, useState, type ReactNode } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { RetentionDayState, RetentionPolicy } from '../api/settings.ts';
import { renderForm, typeInto } from '../testkit/renderForm.tsx';
import { RetentionEditor } from './OrgSettings.tsx';
import { ProjectRetentionEditor } from './ProjectSettings.tsx';
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

async function choose(select: Element, value: string): Promise<void> {
  if (!(select instanceof HTMLSelectElement)) {
    throw new Error('expected a select');
  }
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value')?.set;
  if (setter === undefined) {
    throw new Error('HTMLSelectElement exposes no value setter');
  }
  await act(async () => {
    setter.call(select, value);
    select.dispatchEvent(new Event('change', { bubbles: true }));
  });
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

const boundedPolicy: RetentionPolicy = {
  mode: 'keep-if-either',
  max_age_seconds: 30 * 86_400,
  last_revisions: 5,
};

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

describe('RetentionEditor', () => {
  it('clears a validation refusal when a bound is edited', async () => {
    const container = await render(
      <RetentionEditor
        scope="org_a"
        policy={{ ...boundedPolicy, last_revisions: null }}
        busy={false}
        onSave={vi.fn()}
      />,
    );

    const save = [...container.querySelectorAll('button')].find(
      (candidate) => candidate.textContent === 'Save retention',
    );
    if (save === undefined) {
      throw new Error('save action is missing');
    }
    await click(save);
    expect(container.querySelector('[role="alert"]')).not.toBeNull();

    const count = controlByLabel(container, 'Revisions kept per environment');
    if (!(count instanceof HTMLInputElement)) {
      throw new Error('revision bound is not an input');
    }
    await act(async () => typeInto(count, '7'));

    expect(container.querySelector('[role="alert"]')).toBeNull();
  });

  it('submits the exact bounded payload shown by the shared fields', async () => {
    const onSave = vi.fn<(next: RetentionPolicy) => void>();
    const container = await render(
      <RetentionEditor scope="org_a" policy={boundedPolicy} busy={false} onSave={onSave} />,
    );

    const age = controlByLabel(container, 'Maximum age, in days');
    const count = controlByLabel(container, 'Revisions kept per environment');
    if (!(age instanceof HTMLInputElement) || !(count instanceof HTMLInputElement)) {
      throw new Error('retention bounds are not inputs');
    }
    await act(async () => {
      typeInto(age, '45');
      typeInto(count, '7');
    });

    const save = [...container.querySelectorAll('button')].find(
      (candidate) => candidate.textContent === 'Save retention',
    );
    if (save === undefined) {
      throw new Error('save action is missing');
    }
    await click(save);

    expect(onSave).toHaveBeenCalledWith({
      mode: 'keep-if-either',
      max_age_seconds: 45 * 86_400,
      last_revisions: 7,
    });
  });

  it('resets an unsaved draft when the organisation scope changes', async () => {
    function ScopeHarness() {
      const [scope, setScope] = useState('org_a');
      return (
        <>
          <RetentionEditor scope={scope} policy={boundedPolicy} busy={false} onSave={vi.fn()} />
          <button type="button" onClick={() => setScope('org_b')}>
            Change organisation
          </button>
        </>
      );
    }

    const container = await render(<ScopeHarness />);
    const count = controlByLabel(container, 'Revisions kept per environment');
    if (!(count instanceof HTMLInputElement)) {
      throw new Error('revision bound is not an input');
    }
    await act(async () => typeInto(count, '99'));
    expect(count.value).toBe('99');

    const change = [...container.querySelectorAll('button')].find(
      (candidate) => candidate.textContent === 'Change organisation',
    );
    if (change === undefined) {
      throw new Error('scope change action is missing');
    }
    await click(change);

    expect(count.value).toBe('5');
  });

  it('submits null bounds after switching the organisation policy to unlimited', async () => {
    const onSave = vi.fn<(next: RetentionPolicy) => void>();
    const container = await render(
      <RetentionEditor scope="org_a" policy={boundedPolicy} busy={false} onSave={onSave} />,
    );

    await choose(controlByLabel(container, 'Mode'), 'unlimited');
    const save = [...container.querySelectorAll('button')].find(
      (candidate) => candidate.textContent === 'Save retention',
    );
    if (save === undefined) {
      throw new Error('save action is missing');
    }
    await click(save);

    expect(onSave).toHaveBeenCalledWith({
      mode: 'unlimited',
      max_age_seconds: null,
      last_revisions: null,
    });
  });
});

describe('ProjectRetentionEditor', () => {
  it('submits null bounds after switching the project policy to inherit', async () => {
    const onSave = vi.fn();
    const container = await render(
      <ProjectRetentionEditor
        scope="org_a/project_a"
        policy={{ ...boundedPolicy, inherited: false }}
        busy={false}
        onSave={onSave}
      />,
    );

    await choose(controlByLabel(container, 'Policy'), 'inherit');
    const save = [...container.querySelectorAll('button')].find(
      (candidate) => candidate.textContent === 'Save retention',
    );
    if (save === undefined) {
      throw new Error('save action is missing');
    }
    await click(save);

    expect(onSave).toHaveBeenCalledWith({
      inherited: true,
      maxAgeSeconds: null,
      lastRevisions: null,
    });
  });

  it('resets an unsaved draft when the project scope changes', async () => {
    function ScopeHarness() {
      const [scope, setScope] = useState('org_a/project_a');
      return (
        <>
          <ProjectRetentionEditor
            scope={scope}
            policy={{ ...boundedPolicy, inherited: false }}
            busy={false}
            onSave={vi.fn()}
          />
          <button type="button" onClick={() => setScope('org_a/project_b')}>
            Change project
          </button>
        </>
      );
    }

    const container = await render(<ScopeHarness />);
    const age = controlByLabel(container, 'Maximum age, in days');
    if (!(age instanceof HTMLInputElement)) {
      throw new Error('maximum age is not an input');
    }
    await act(async () => typeInto(age, '99'));
    expect(age.value).toBe('99');

    const change = [...container.querySelectorAll('button')].find(
      (candidate) => candidate.textContent === 'Change project',
    );
    if (change === undefined) {
      throw new Error('scope change action is missing');
    }
    await click(change);

    expect(age.value).toBe('30');
  });
});
