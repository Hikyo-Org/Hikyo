// @vitest-environment happy-dom
import type { RetentionConsequence } from '@hikyo/client';
import { act, createRef } from 'react';
import { MemoryRouter } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { renderForm, settle, typeInto } from '../testkit/renderForm.tsx';
import { HistoryDrawer, PinReleaseOutcome, shortPrincipal } from './HistoryDrawer.tsx';

type HistoryDrawerMocks = {
  preview: RetentionConsequence;
  schemaOverride: boolean;
  ceremonyRun: ReturnType<typeof vi.fn>;
  releaseMutate: ReturnType<typeof vi.fn>;
  setPinMutate: ReturnType<typeof vi.fn>;
  restoreMutate: ReturnType<typeof vi.fn>;
};

const mocks = vi.hoisted<HistoryDrawerMocks>(() => ({
  preview: 'retained',
  schemaOverride: false,
  ceremonyRun: vi.fn(),
  releaseMutate: vi.fn(),
  setPinMutate: vi.fn(),
  restoreMutate: vi.fn(),
}));

vi.mock('../api/history.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/history.ts')>();
  return {
    ...actual,
    useRevisionHistory: () => ({
      data: {
        items: [4n, 3n].map((revision) => ({
          revision,
          schema_revision: 1n,
          published_by: 'usr_admin',
          published_by_name: 'Alex Lee',
          published_at: '2026-08-01T00:00:00Z',
          changed_keys: [],
          payload_present: true,
        })),
        count: 2,
      },
      isSuccess: true,
    }),
    useRevisionPins: () => ({
      data: {
        items: [{
          id: 'pin_a',
          workload_principal_id: 'mch_workload',
          revision: 3n,
          authority_principal_id: 'usr_admin',
          expires_at: '2026-09-01T00:00:00Z',
          created_at: '2026-08-01T00:00:00Z',
          authorized_at: '2026-08-01T00:00:00Z',
          history_authorized: true,
          schema_override: mocks.schemaOverride,
          expired: false,
          release_retention_consequence: mocks.preview,
        }],
        count: 1,
      },
    }),
    useProjectRetention: () => ({
      data: { inherited: false, mode: 'keep-if-either', max_age_seconds: 3600, last_revisions: 1 },
      isError: false,
    }),
    useRevisionDetail: () => ({
      data: {
        keys: [{ key_id: 'key_secret', name: 'TOKEN', classification: 'secret' }],
      },
      isSuccess: true,
      isError: false,
    }),
    useRestoreRevision: () => ({ mutate: mocks.restoreMutate, isPending: false }),
    useSetRevisionPin: () => ({ mutate: mocks.setPinMutate, isPending: false }),
    useReleaseRevisionPin: () => ({ mutate: mocks.releaseMutate, isPending: false }),
  };
});

vi.mock('../api/identities.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/identities.ts')>();
  return {
    ...actual,
    useServiceAccounts: () => ({
      data: {
        items: [{
          id: 'svc_a',
          principal_id: 'mch_workload',
          name: 'worker',
          kind: 'workload',
          created_at: '2026-08-01T00:00:00Z',
          created_by: 'usr_admin',
          live_credentials: 1,
        }],
        count: 1,
      },
    }),
  };
});

vi.mock('../api/matrix.ts', async (importActual) => {
  const actual = await importActual<typeof import('../api/matrix.ts')>();
  return { ...actual, usePublishMatrix: () => ({ mutate: vi.fn(), isPending: false }) };
});

vi.mock('./useProtectedPublishCeremony.ts', () => ({
  useProtectedPublishCeremony: () => ({
    request: null,
    error: null,
    run: mocks.ceremonyRun,
    onAuthorised: vi.fn(),
    onCancel: vi.fn(),
  }),
}));

beforeEach(() => {
  mocks.preview = 'retained';
  mocks.schemaOverride = false;
  mocks.ceremonyRun.mockReset();
  mocks.releaseMutate.mockReset();
  mocks.setPinMutate.mockReset();
  mocks.restoreMutate.mockReset();
});

describe('PinReleaseOutcome', () => {
  const cases: ReadonlyArray<readonly [RetentionConsequence, string]> = [
    ['retained', "r3's values remain retained"],
    ['collection_eligible', "r3's values became eligible for collection"],
    ['already_collected', "r3's values were already collected"],
  ];

  for (const [consequence, expected] of cases) {
    it(`renders server consequence ${consequence}`, async () => {
      const { container } = await renderForm(
        <PinReleaseOutcome
          consequence={consequence}
          revision={3n}
        />,
      );

      expect(container.textContent).toContain(expected);
      expect(container.textContent).toContain('resumes latest on its next fetch');
    });
  }
});

function drawer(
  initialEntry = '/orgs/org_a/projects/prj_a/matrix/history?env=env_a&rev=4',
  pendingByOthers = 0,
) {
  return (
    <MemoryRouter initialEntries={[initialEntry]}>
      <HistoryDrawer
        refData={{ org: 'org_a', project: 'prj_a' }}
        environments={[
          {
            id: 'env_a',
            org_id: 'org_a',
            project_id: 'prj_a',
            name: 'production',
            display_order: 0,
            created_at: '2026-08-01T00:00:00Z',
          },
          {
            id: 'env_b',
            org_id: 'org_a',
            project_id: 'prj_a',
            name: 'staging',
            display_order: 1,
            created_at: '2026-08-01T00:00:00Z',
          },
        ]}
        keys={[]}
        currentRevisions={new Map([['env_a', 7n], ['env_b', 2n]])}
        protectedEnvironmentIds={[]}
        cellsByEnvironment={new Map()}
        pendingByEnvironment={new Map()}
        pendingByOthersByEnvironment={new Map([['env_a', pendingByOthers]])}
        currentValuesByEnvironment={new Map()}
        openerRef={createRef<HTMLAnchorElement>()}
      />
    </MemoryRouter>
  );
}

function buttonNamed(container: HTMLElement, name: string): HTMLButtonElement {
  const button = [...container.querySelectorAll('button')].find((candidate) => candidate.textContent === name);
  if (button === undefined) {
    throw new Error(`button ${name} is missing`);
  }
  return button;
}

describe('HistoryDrawer pin release flow', () => {
  it('refuses a cleared expiry before starting disclosure or pinning', async () => {
    const { container } = await renderForm(drawer());

    await act(async () => buttonNamed(container, 'Pin r4…').click());
    const expiry = container.querySelector<HTMLInputElement>('#history-pin-expiry');
    const submit = container.querySelector<HTMLButtonElement>('#history-pin-submit');
    if (expiry === null || submit === null) {
      throw new Error('pin expiry controls are missing');
    }

    expect(expiry.min).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(expiry.max).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(expiry.max > expiry.min).toBe(true);
    await act(async () => typeInto(expiry, ''));
    await settle();

    expect(submit.disabled).toBe(true);
    expect(container.querySelector('#history-pin-refusal')?.textContent).toContain(
      'Invalid pin expiry date',
    );
    expect(mocks.ceremonyRun).not.toHaveBeenCalled();
    expect(mocks.setPinMutate).not.toHaveBeenCalled();
  });

  it('passes the server response into the outcome instead of stale drawer state', async () => {
    mocks.releaseMutate.mockImplementation((_workload, options) => {
      options.onSuccess({ revision: 11n, retention_consequence: 'already_collected' });
    });
    const { container } = await renderForm(drawer());

    await act(async () => buttonNamed(container, 'Release').click());
    await settle();

    expect(mocks.releaseMutate).toHaveBeenCalledTimes(1);
    expect(container.querySelector('[role="status"]')?.textContent).toContain(
      "r11's values were already collected",
    );
    expect(container.textContent).not.toContain("r3's values were already collected");
  });

  it('keeps confirmation only for a server-previewed collection risk', async () => {
    mocks.preview = 'collection_eligible';
    mocks.releaseMutate.mockImplementation((_workload, options) => {
      options.onSuccess({ revision: 3n, retention_consequence: 'collection_eligible' });
    });
    const { container } = await renderForm(drawer());

    // Sole keeper: the tier is spelled beside the glyph, never colour-only.
    expect(container.querySelector('.history__warn')?.textContent).toContain('sole keeper');
    expect(container.textContent).toContain(
      'r3 is past normal retention: its values survive only because of this pin.',
    );
    await act(async () => buttonNamed(container, 'Release').click());
    expect(mocks.releaseMutate).not.toHaveBeenCalled();

    // The confirmation states the known consequence plainly and names it on the button.
    expect(container.textContent).toContain(
      "This pin is the only thing keeping r3's values. Releasing it makes them collection-eligible: no diff by value, no restore, no reveal once collected.",
    );
    await act(async () => buttonNamed(container, 'Release and allow collection of r3').click());
    await settle();
    expect(mocks.releaseMutate).toHaveBeenCalledTimes(1);
    expect(container.querySelector('[role="status"]')?.textContent).toContain(
      "r3's values became eligible for collection",
    );
  });

  it('warns and confirms when moving a server-previewed sole-keeper pin', async () => {
    mocks.preview = 'collection_eligible';
    const { container } = await renderForm(drawer());

    await act(async () => buttonNamed(container, 'Pin r4…').click());

    expect(container.querySelector('#history-pin-move-collection-warning')?.textContent).toContain(
      "make r3's values eligible for immediate collection",
    );
    expect(buttonNamed(container, 'Move pin from r3 to r4, old values may be collected')).toBeTruthy();
  });
});

describe('HistoryDrawer head', () => {
  it('opens on the detail pane when the deep link names a revision', async () => {
    const { container } = await renderForm(drawer());
    expect(container.querySelector('aside')?.className).toContain('history--detail');
    const { container: list } = await renderForm(
      drawer('/orgs/org_a/projects/prj_a/matrix/history?env=env_a'),
    );
    expect(list.querySelector('aside')?.className).not.toContain('history--detail');
  });

  it('links the retention pointer to the project-settings Policy panel', async () => {
    const { container } = await renderForm(drawer());
    const pointer = container.querySelector<HTMLAnchorElement>('a.history__settings-pointer');
    expect(pointer?.getAttribute('href')).toBe('/orgs/org_a/projects/prj_a/settings#project-policy');
    expect(pointer?.textContent).toContain('change it in project settings › Policy');
  });

  it('renders the pin quota against the pins held', async () => {
    const { container } = await renderForm(drawer());
    await act(async () => buttonNamed(container, 'Pin r4…').click());
    expect(container.textContent).toContain('1 pinned in this environment; the project quota is 100 and expiry is mandatory.');
  });

  it('marks schema drift by its own class, not the sole-keeper warning', async () => {
    mocks.schemaOverride = true;
    const { container } = await renderForm(drawer());
    expect(container.querySelector('.history__pin .history__drift')?.textContent).toBe('Δ schema drift');
    expect(container.querySelector('.history__pin .history__warn')).toBeNull();
  });

  it('prefers the publisher name and retains the full principal ID in title', async () => {
    expect(shortPrincipal('usr_0192b4c1-7a2e-7f3b-9c11-3f2a1b')).toBe('usr_0192b4c1…');
    expect(shortPrincipal('usr_admin')).toBe('usr_admin');
    const { container } = await renderForm(drawer());
    expect(container.querySelector('dd > span.mono[title="usr_admin"]')?.textContent).toBe('Alex Lee');
  });
});

describe('HistoryDrawer restore sheet', () => {
  it('counts changes pending by others without inventing names', async () => {
    const { container } = await renderForm(drawer(undefined, 2));
    expect(container.querySelector('.history__pending')?.textContent).toBe('2 changes pending by others');
  });

  it('groups the impact per environment, spells absence, and names every environment on publish', async () => {
    mocks.ceremonyRun.mockImplementation((_units: unknown, act: () => void) => {
      act();
      return Promise.resolve();
    });
    const change = {
      version_id: 'ver_1',
      key_id: 'key_cfg',
      name: 'LOG_LEVEL',
      classification: 'config',
      operation: 'set',
      staged_from_revision: 4n,
      created_at: '2026-08-01T00:00:00Z',
    };
    mocks.restoreMutate.mockImplementation((_input, options) => {
      options.onSuccess({
        changes: [change],
        preview: {
          token: 'tok',
          environments: [
            {
              environment_id: 'env_a',
              base_revision: 7n,
              schema_revision: 1n,
              protected: true,
              changes: [{ ...change, status: 'edited', after: 'debug' }],
            },
            {
              environment_id: 'env_b',
              base_revision: 2n,
              schema_revision: 1n,
              protected: false,
              changes: [{ ...change, operation: 'unset', status: 'removed', before: 'info' }],
            },
          ],
        },
      });
    });
    const { container } = await renderForm(drawer());

    await act(async () => buttonNamed(container, 'Restore r4…').click());
    await act(async () => buttonNamed(container, 'Stage the restore from r4').click());
    await settle();

    const headings = [...container.querySelectorAll('.history__impact-heading')].map((h) => h.textContent);
    expect(headings).toEqual(['production · r7 → r8PROTECTED', 'staging · r2 → r3']);
    const absent = [...container.querySelectorAll('.history__impact .values__absent')].map((a) => a.textContent);
    expect(absent).toEqual(['absent', 'cleared']);
    expect(container.textContent).toContain('Drafts are staged; they are also visible on the matrix.');
    expect(container.textContent).not.toContain('Staged as ordinary drafts');
    expect(
      buttonNamed(container, 'Publish this restore (r4 → production r8, staging r3)'),
    ).toBeTruthy();
  });
});

describe('HistoryDrawer key filter', () => {
  it('renders the empty state for an unknown deleted key', async () => {
    const { container } = await renderForm(
      drawer('/orgs/org_a/projects/prj_a/matrix/history?env=env_a&key=key_deleted'),
    );

    expect(container.querySelector('.history__empty')?.textContent).toBe(
      'No revision has moved key_deleted (unknown key) in this environment.',
    );
  });
});
