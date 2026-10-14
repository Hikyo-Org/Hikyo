import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, fn, userEvent, waitFor } from 'storybook/test';

import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { MatrixRowEditor } from './MatrixRowEditor.tsx';
import { dottedText } from '../testkit/dottedText.ts';

type Props = Parameters<typeof MatrixRowEditor>[0];

const environmentId = 'env_01989abc-def0-7123-8123-123456789abc';
const environment: Props['rows'][number]['environment'] = {
  id: environmentId,
  org_id: 'org_01989abc-def0-7123-8123-123456789abc',
  project_id: 'prj_01989abc-def0-7123-8123-123456789abc',
  name: 'development',
  display_order: 0,
  created_at: '2026-08-23T08:00:00Z',
};
const keyRecord: Props['keyRecord'] = {
  id: 'key_01989abc-def0-7123-8123-123456789abc',
  org_id: environment.org_id,
  project_id: environment.project_id,
  name: 'LOG_LEVEL',
  folder_path: '',
  classification: 'config',
  description: '',
  deprecated: false,
  deprecation_note: '',
  declaration: { rule: { type: 'string', allow_empty: true } },
  presence: {
    required_in: { mode: 'none' },
    forbidden_in: { mode: 'none' },
  },
  group_id: '',
  created_at: '2026-08-23T08:00:00Z',
};
const rows: Props['rows'] = [
  {
    environmentId,
    environment,
    protected: false,
    degraded: false,
    cell: {
      key_id: keyRecord.id,
      name: keyRecord.name,
      classification: 'config',
      set: true,
      revealed: true,
      value: 'published',
    },
    signal: undefined,
    draftPreview: undefined,
    problems: [],
  },
];

const meta = {
  title: 'Features/Values/MatrixRowEditor',
  id: 'routes-matrixroweditor',
  component: MatrixRowEditor,
  tags: ['ai-generated'],
  // A config key needs no reveal window, so an empty response table is enough:
  // the harness supplies the QueryClientProvider and Router the editor mounts
  // under, and any stray fetch 404s loud.
  parameters: { app: { responses: [] }, ...topLayerDocs },
  args: {
    refData: { org: 'org-a', project: 'project-a' },
    keyRecord,
    environmentId,
    rows,
    busy: false,
    mutationError: null,
    onClose: fn(),
    onApply: fn(async () => undefined),
    onCopy: fn(),
  },
} satisfies Meta<typeof MatrixRowEditor>;

export default meta;
type Story = StoryObj<typeof meta>;

// The editor opens on the published value with Save disabled: nothing has
// changed yet.
export const Default: Story = {
  play: async ({ canvas, canvasElement }) => {
    // Named, not just present: the value control is labelled through ui/Field.
    await expect(canvas.getByRole('textbox', { name: 'development value' })).toHaveValue(
      'published',
    );
    await expect(canvas.getByRole('button', { name: /^Save/ })).toBeDisabled();
    // The eyebrow names environment and classification with the shared separator.
    await expect(canvas.getByText(dottedText(/^development\s*·\s*config$/))).toHaveClass('matrix-editor__eyebrow');
    // Both nested disclosures wear the button skin, not a bare label with the
    // native triangle (ProjectSettings' environment-lifecycle pattern).
    for (const detailsSelector of ['.matrix-editor__schema', '.matrix-editor__raw']) {
      const summary = canvasElement.querySelector(`${detailsSelector} > summary`);
      if (!(summary instanceof HTMLElement)) throw new Error(`${detailsSelector} summary missing`);
      await expect(summary).toHaveClass('btn');
      await expect(getComputedStyle(summary).listStyle).toContain('none');
    }
    // Opening the outer one: the chevron, not a native marker, carries the
    // open state. (The inner "Raw declaration" stays closed here: its `<pre>`
    // has its own pre-existing scrollable-region focus gap, out of scope for
    // this disclosure-styling pass.)
    const schemaSummary = canvasElement.querySelector('.matrix-editor__schema > summary');
    if (!(schemaSummary instanceof HTMLElement)) throw new Error('schema summary missing');
    const chevron = schemaSummary.querySelector('.glyph[data-glyph="chevron"]');
    if (chevron === null) throw new Error('chevron missing');
    await userEvent.click(schemaSummary);
    await expect(schemaSummary.closest('details')).toHaveAttribute('open');
    await expect(getComputedStyle(chevron).transform).not.toBe('none');
  },
};

// The copy panel opens from a toggle that sits beside it and names it, not from
// the action row: `aria-controls` resolves to the panel that appears.
export const CopyDisclosure: Story = {
  play: async ({ canvas }) => {
    const toggle = canvas.getByRole('button', { name: 'Copy published development value to\u2026' });
    await expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(toggle);
    await expect(toggle).toHaveAttribute('aria-expanded', 'true');

    const panelId = toggle.getAttribute('aria-controls') ?? '';
    const panel = canvas.getByRole('group', { name: 'Copy independent published value to' });
    await expect(panel.closest(`#${panelId}`)).not.toBeNull();
  },
};

// Editing the value enables Save and submits the change through onApply.
export const Edits: Story = {
  play: async ({ args, canvas }) => {
    const textarea = canvas.getByRole('textbox');
    await userEvent.clear(textarea);
    await userEvent.type(textarea, 'debug');
    const save = canvas.getByRole('button', { name: /^Save/ });
    await expect(save).toBeEnabled();
    await userEvent.click(save);
    await waitFor(() =>
      expect(args.onApply).toHaveBeenCalledWith([
        { environmentId, operation: 'set', value: 'debug' },
      ]),
    );
  },
};

// An unbroken value (a token, a masked secret's run of dots) wraps inside the
// dialog: the textarea sizes to its content, so without a break opportunity it
// grew to the value's full length and the dialog scrolled sideways.
const longValue = 'x'.repeat(400);

/** Fails when the value control is wider than the dialog it sits in. */
async function expectNoHorizontalOverflow(textarea: HTMLElement) {
  const dialog = textarea.closest('dialog');
  await expect(dialog).not.toBeNull();
  await expect(dialog?.scrollWidth).toBeLessThanOrEqual(dialog?.clientWidth ?? 0);
  await expect(textarea.getBoundingClientRect().width).toBeLessThanOrEqual(
    dialog?.getBoundingClientRect().width ?? 0,
  );
}

export const LongValue: Story = {
  args: {
    rows: rows.map((row) => ({
      ...row,
      cell: row.cell === undefined ? undefined : { ...row.cell, value: longValue },
    })),
  },
  play: async ({ canvas }) => {
    const textarea = canvas.getByRole('textbox', { name: 'development value' });
    await expect(textarea).toHaveValue(longValue);
    await expectNoHorizontalOverflow(textarea);
  },
};

// The screenshot case: a secret is masked while typing, one long run of dots.
export const LongSecretValue: Story = {
  args: {
    keyRecord: { ...keyRecord, name: 'API_TOKEN', classification: 'secret' },
    rows: rows.map((row) => ({
      ...row,
      cell: {
        key_id: keyRecord.id,
        name: 'API_TOKEN',
        classification: 'secret',
        set: false,
        revealed: false,
      },
    })),
  },
  play: async ({ canvas }) => {
    const textarea = canvas.getByRole('textbox', { name: 'development value' });
    await expect(textarea).toHaveClass('matrix-editor__value--masked');
    await userEvent.click(textarea);
    await userEvent.paste(longValue);
    await expect(textarea).toHaveValue(longValue);
    await expectNoHorizontalOverflow(textarea);
  },
};

// A key required everywhere with an own pending draft: the row head marks the
// draft with the own-draft glyph, and the schema summary joins its parts with
// the shared separator.
export const PendingDraft: Story = {
  args: {
    keyRecord: { ...keyRecord, presence: { required_in: { mode: 'all' }, forbidden_in: { mode: 'none' } } },
    rows: rows.map((row) => ({
      ...row,
      signal: {
        key_id: keyRecord.id,
        name: keyRecord.name,
        classification: 'config',
        pending_by_others: false,
        pending: { versionId: 'ver_01989abc-def0-7123-8123-123456789abc', operation: 'set' },
      },
    })),
  },
  play: async ({ canvas, canvasElement }) => {
    const pending = canvas.getByText('pending set');
    await expect(pending.querySelector('.glyph')).toHaveAttribute('data-glyph', 'own-draft');
    const schema = canvasElement.querySelector('.matrix-editor__schema');
    const summary = schema?.querySelector(':scope > summary');
    if (!(summary instanceof HTMLElement)) throw new Error('schema summary missing');
    await userEvent.click(summary);
    const rules = schema?.querySelector(':scope > p');
    await expect(rules).toHaveTextContent(/^string\s*·\s*required everywhere$/);
    await expect(rules?.querySelector('.sep')).not.toBeNull();
  },
};

const SECRET_ENV_URL = `/api/v1/orgs/org-a/projects/project-a/environments/${environmentId}`;

// A secret revealed under a live window: the plaintext shows with its
// countdown in spelled-out seconds (R12), and the status region announces it.
export const RevealedSecret: Story = {
  args: {
    keyRecord: { ...keyRecord, name: 'API_TOKEN', classification: 'secret' },
    rows: rows.map((row) => ({
      ...row,
      cell: { key_id: keyRecord.id, name: 'API_TOKEN', classification: 'secret', set: true, revealed: false },
    })),
  },
  parameters: {
    app: {
      responses: [
        {
          url: `${SECRET_ENV_URL}/reveal-window`,
          body: {
            effective_window_seconds: 600,
            protected: false,
            totp_offered: false,
            live: true,
            single_decision: false,
            can_reveal: true,
            expires_at: new Date(Date.now() + 9 * 60_000).toISOString(),
          },
        },
        {
          url: `${SECRET_ENV_URL}/values/API_TOKEN/reveal`,
          method: 'POST',
          body: { key_id: keyRecord.id, name: 'API_TOKEN', classification: 'secret', set: true, revealed: true, value: 'tok_live_revealed' },
        },
      ],
    },
  },
  play: async ({ canvas, canvasElement }) => {
    await userEvent.click(await canvas.findByRole('button', { name: 'Reveal API_TOKEN' }));
    await expect(await canvas.findByText('tok_live_revealed')).toBeVisible();
    const countdown = canvasElement.querySelector('.matrix-editor__revealed small');
    await expect(countdown?.textContent).toMatch(/^re-masks in \d+ seconds?$/);
    await expect(canvasElement.querySelector('.visually-hidden[role="status"]')).toHaveTextContent('API_TOKEN revealed, re-masks in 10 seconds');
  },
};
