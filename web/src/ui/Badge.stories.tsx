import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect } from 'storybook/test';

import { inkContrast, tokenPx } from '../../.storybook/tokens.ts';
import { CAPABILITY_TAG, CapabilityTag } from '../routes/capabilityTags.tsx';
import { LIFETIME_TAG } from '../routes/lifetimeTag.ts';
import { Badge, type BadgeTone } from './Badge.tsx';
import type { GlyphName } from './Glyph.tsx';

const meta = {
  title: 'Design system/Badge',
  id: 'ui-badge',
  component: Badge,
  parameters: { docs: { description: { component: 'Compact, noninteractive status tag: a soft fill of its tone, the word in bold tracked capitals (CSS only, the source keeps its case) and an icon that echoes the word. `mono` is an identifier (key name, revision, id, URL): exact case, no default icon.' } } },
  tags: ['ai-generated'],
  args: { children: 'Active', tone: 'neutral' },
  argTypes: { tone: { control: 'select', options: ['neutral', 'danger', 'changed', 'ok'] } },
} satisfies Meta<typeof Badge>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Neutral: Story = {};

const TONES: readonly BadgeTone[] = ['neutral', 'ok', 'changed', 'danger'];
/** Every icon a tag carries somewhere in the app (the mapping in DESIGN.md, Badge). */
const TAG_ICONS: readonly GlyphName[] = [
  'check', 'alert', 'warn', 'denied', 'cross', 'question', 'info', 'history', 'wait', 'sync', 'pause',
  'own-draft', 'plus', 'edit', 'minus', 'lock', 'reveal', 'arrow-right', 'passkey', 'link',
  'workload', 'automation', 'draft', 'settings', 'disconnected',
];

/**
 * The gallery: every tone with every tag icon, then identifiers. Runs in both
 * themes (`test-storybook` dark, `test-storybook:light`).
 */
export const AllTones: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: 'grid', gap: 16 }}>
      {TONES.map((tone) => (
        <section key={tone} aria-label={tone} style={{ display: 'grid', gap: 8 }}>
          <h3 style={{ margin: 0 }}>{tone}</h3>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
            <Badge tone={tone} data-default>{`${tone} default`}</Badge>
            {TAG_ICONS.map((icon) => (
              <Badge key={icon} tone={tone} icon={icon}>{icon}</Badge>
            ))}
          </div>
        </section>
      ))}
      <section aria-label="identifiers" style={{ display: 'grid', gap: 8 }}>
        <h3 style={{ margin: 0 }}>mono (identifiers)</h3>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          <Badge mono>DATABASE_url</Badge>
          <Badge mono>r12</Badge>
          <Badge mono>https://Vault.example/v1</Badge>
          <Badge mono tone="danger">disabled</Badge>
          <Badge icon="own-draft">current <span className="mono">r4</span></Badge>
        </div>
      </section>
    </div>
  ),
  play: async ({ canvasElement }) => {
    const fills = new Set<string>();
    for (const tone of TONES) {
      const tags = [...canvasElement.querySelectorAll(`section[aria-label="${tone}"] .badge`)];
      await expect(tags).toHaveLength(TAG_ICONS.length + 1);
      for (const tag of tags) {
        const style = getComputedStyle(tag);
        // Capitals by CSS only: the DOM keeps the source word.
        await expect(style.textTransform).toBe('uppercase');
        await expect(tag.textContent).toBe(tag.textContent?.toLowerCase());
        await expect(Number(style.fontWeight)).toBe(700);
        await expect(parseFloat(style.letterSpacing)).toBeGreaterThan(0);
        await expect(inkContrast(tag)).toBeGreaterThanOrEqual(4.5);
      }
      const [first] = tags;
      if (first === undefined) throw new Error(`no ${tone} tag`);
      fills.add(getComputedStyle(first).backgroundColor);
      // Every tag here has exactly one decorative icon before its word.
      for (const tag of tags) {
        await expect(tag.querySelectorAll('svg.glyph[aria-hidden="true"]')).toHaveLength(tone === 'neutral' && tag.hasAttribute('data-default') ? 0 : 1);
      }
    }
    // Each tone is its own fill.
    await expect(fills.size).toBe(TONES.length);

    const ids = [...canvasElement.querySelectorAll('section[aria-label="identifiers"] .badge.mono')];
    for (const id of ids) {
      const style = getComputedStyle(id);
      await expect(style.textTransform).toBe('none');
      await expect(id.querySelector('svg')).toBeNull();
    }
    const current = canvasElement.querySelector('section[aria-label="identifiers"] .badge:not(.mono) .mono');
    await expect(current === null ? '' : getComputedStyle(current).textTransform).toBe('none');
  },
};

/**
 * The tag's anatomy: icon before the word at caption size, `--space-1` between
 * them and `--space-2` either side. A tone's default icon can be dropped with
 * `icon={false}`, and an explicit icon replaces the tone's default.
 */
export const IconAndSpacing: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
      <Badge tone="ok" data-ref="default">healthy</Badge>
      <Badge tone="ok" icon={false} data-ref="dropped">healthy</Badge>
      <Badge tone="danger" icon="denied" data-ref="explicit">denied</Badge>
    </div>
  ),
  play: async ({ canvasElement }) => {
    const tag = (ref: string) => {
      const element = canvasElement.querySelector(`[data-ref="${ref}"]`);
      if (element === null) throw new Error(`${ref} tag missing`);
      return element;
    };
    const withIcon = tag('default');
    const glyph = withIcon.querySelector('svg.glyph');
    if (glyph === null) throw new Error('ok tag has no default icon');
    await expect(glyph).toHaveAttribute('data-glyph', 'check');
    // The icon leads the word.
    await expect(withIcon.firstElementChild).toBe(glyph);
    const style = getComputedStyle(withIcon);
    await expect(Number.parseFloat(style.columnGap)).toBe(tokenPx('--space-1'));
    await expect(Number.parseFloat(style.paddingLeft)).toBe(tokenPx('--space-2'));
    await expect(Number.parseFloat(style.paddingRight)).toBe(tokenPx('--space-2'));
    await expect(Math.round(glyph.getBoundingClientRect().height)).toBe(Math.round(tokenPx('--fs-sm')));
    // The word is the accessible text, in its source case.
    await expect(withIcon.textContent).toBe('healthy');

    await expect(tag('dropped').querySelector('svg')).toBeNull();
    await expect(tag('explicit').querySelectorAll('svg.glyph')).toHaveLength(1);
    await expect(tag('explicit').querySelector('svg.glyph')).toHaveAttribute('data-glyph', 'denied');
  },
};

const toneClass = (tone: BadgeTone) => (tone === 'neutral' ? null : `badge--${tone}`);

/**
 * The two tag vocabularies the routes share: every capability id as its
 * CapabilityTag (permission lists, the Audit outcome filter's neighbours) and
 * every credential lifetime (instance connections, SCIM credentials). Each tag
 * wears its table's tone and icon, and its word is the source text.
 */
export const Vocabularies: Story = {
  parameters: { controls: { disable: true } },
  render: () => (
    <div style={{ display: 'grid', gap: 16 }}>
      <section aria-label="capabilities" style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
        {Object.keys(CAPABILITY_TAG).map((id) => (
          <CapabilityTag key={id} id={id} />
        ))}
      </section>
      <section aria-label="lifetimes" style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
        {Object.entries(LIFETIME_TAG).map(([state, tag]) => (
          <Badge key={state} data-state={state} {...tag}>{state}</Badge>
        ))}
      </section>
    </div>
  ),
  play: async ({ canvasElement }) => {
    const check = async (tag: Element | undefined, word: string, { tone, icon }: { tone: BadgeTone; icon: GlyphName }) => {
      if (tag === undefined) throw new Error(`no tag for ${word}`);
      await expect(tag.textContent).toBe(word);
      await expect(getComputedStyle(tag).textTransform).toBe('uppercase');
      await expect(tag.querySelectorAll('svg.glyph')).toHaveLength(1);
      await expect(tag.querySelector('svg.glyph')).toHaveAttribute('data-glyph', icon);
      const modifier = toneClass(tone);
      for (const other of ['badge--ok', 'badge--changed', 'badge--danger']) {
        await expect(tag.classList.contains(other)).toBe(other === modifier);
      }
      await expect(inkContrast(tag)).toBeGreaterThanOrEqual(4.5);
    };
    const capabilities = [...canvasElement.querySelectorAll('section[aria-label="capabilities"] .badge')];
    const ids = Object.keys(CAPABILITY_TAG);
    await expect(capabilities).toHaveLength(ids.length);
    for (const [index, id] of ids.entries()) {
      await check(capabilities[index], id, CAPABILITY_TAG[id]!);
    }
    for (const [state, tag] of Object.entries(LIFETIME_TAG)) {
      await check(canvasElement.querySelector(`section[aria-label="lifetimes"] [data-state="${state}"]`) ?? undefined, state, tag);
    }
  },
};
