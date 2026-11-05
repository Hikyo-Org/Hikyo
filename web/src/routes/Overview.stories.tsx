import type { Meta, StoryObj } from '@storybook/react-vite';
import { expect, userEvent, waitFor, within } from 'storybook/test';

import { phone, safeArea, safeAreaIsWiredToEnv } from '../../.storybook/layout.ts';
import { isManualDocsFrame } from '../../.storybook/manualDocsFrame.ts';
import { appearancePass, interactionOnce } from '../../.storybook/interactionCoverage.ts';
import { topLayerDocs } from '../../.storybook/topLayerDocs.ts';
import { AppRoutes } from '../app/App.tsx';
import { Overview } from './Placeholder.tsx';
import { createOverviewFixture, overviewIdentity, overviewProject } from './Overview.story-fixtures.ts';
import { dottedText } from '../testkit/dottedText.ts';

const fixture = createOverviewFixture();
// Overview lists projects only when there is a choice: with exactly one, the
// route opens that project's matrix instead.
const secondProject = { ...overviewProject, id: `${overviewProject.id.slice(0, -1)}2`, name: 'Payments' };

const meta = {
  title: 'Pages/Overview',
  component: Overview,
  tags: ['ai-generated'],
  render: () => <AppRoutes />,
  beforeEach: async () => {
    fixture.reset();
    // The journey explores navigation and query refresh, not a cold module
    // download. Await the actual lazy group's readiness so full-suite
    // transform contention cannot consume the interaction assertion window.
    // AppRoutes still owns and renders the real lazy route elements.
    await import('../app/route-groups/workspace.ts');
    return () => fixture.reset();
  },
  parameters: {
    ...topLayerDocs,
    docs: {
      ...topLayerDocs.docs,
      description: {
        component: 'The real application route and navigation shell. The journey creates a project through its actual form and verifies that the overview and project list share the updated query cache. API storage is simulated; authentication, route policy, components, and providers are real.',
      },
      story: { ...topLayerDocs.docs.story, autoplay: false },
    },
    app: { auth: true, identity: overviewIdentity, path: '/', routeTree: true, responses: fixture.responses },
  },
} satisfies Meta<typeof Overview>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = {
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    // The lede offers the way to a project list only once the organisation's
    // projects have been read: while the read is pending the page says so
    // instead, so the link arrives with the settled query.
    await expect(await canvas.findByRole('link', { name: 'Choose a project' })).toBeVisible();
    await expect(await canvas.findByText('Acme operations')).toBeVisible();
    await expect(canvas.queryByRole('link', { name: 'Settings for Billing platform' })).toBeNull();
  },
};

export const Populated: Story = {
  beforeEach: () => fixture.reset([overviewProject, secondProject]),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('link', { name: 'Settings for Billing platform' })).toBeVisible();
    await expect(canvas.getByRole('link', { name: 'Settings for Payments' })).toBeVisible();
    await expect(canvas.getAllByRole('link', { name: 'Open matrix' })).toHaveLength(2);
    // The theme toggle shows the painted theme by shape: moon in dark, sun in light.
    const toggle = canvas.getByRole('button', { name: /^Switch to (light|dark) theme$/ });
    const painted = toggle.getAttribute('aria-label') === 'Switch to light theme' ? 'moon' : 'sun';
    await expect(toggle.querySelector('svg.glyph')).toHaveAttribute('data-glyph', painted);
  },
};

// One project: the landing opens its matrix (the matrix reads are not
// stubbed here, so it shows its own error state), and the sidebar keeps
// Overview greyed rather than dropping it. At a phone width the drawer is
// opened to show the same row inside it.
export const SingleProject: Story = {
  // A phone width, so the story also shows the menu drawer open.
  globals: { viewport: { value: 'phone', isRotated: false } },
  beforeEach: () => fixture.reset([overviewProject]),
  play: async ({ canvas }) => {
    // Landed on the project's matrix (its breadcrumb), not on Overview.
    const crumbs = await canvas.findByRole('list', { name: 'Breadcrumb' }, { timeout: 5000 });
    await expect(await canvas.findByText('Environment matrix', { selector: '.header__crumbs span' })).toBeVisible();
    if (phone()) {
      const menu = canvas.getByRole('button', { name: 'Menu' });
      // Icon-only on a phone: no visible label, square like any other
      // icon button, "Menu" still its full accessible name.
      await expect(menu.textContent?.trim()).toBe('');
      const box = menu.getBoundingClientRect();
      await expect(Math.round(box.width)).toBe(Math.round(box.height));
      // The breadcrumb stays inside the header (it was never moved out),
      // just small and tertiary-grey, with its org and project segments
      // still real links.
      await expect(crumbs.closest('.header')).not.toBeNull();
      await expect(Number.parseFloat(getComputedStyle(crumbs).fontSize)).toBe(
        Number.parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--fs-xs')),
      );
      const tertiary = document.createElement('span');
      tertiary.style.color = 'var(--tx-faint)';
      document.body.append(tertiary);
      const tertiaryColor = getComputedStyle(tertiary).color;
      tertiary.remove();
      await expect(getComputedStyle(crumbs).color).toBe(tertiaryColor);
      // Compact spacing, not a larger touch target: the gap between
      // segments is tight, and the hit area for a scope link is the link's
      // own `--control` floor (.header__crumb-link), which a name longer
      // than the floor never notices.
      await expect(getComputedStyle(crumbs).columnGap).toBe(getComputedStyle(document.documentElement).getPropertyValue('--space-1').trim());
      // The trail stays on one line, never wrapping onto a second: every
      // segment's vertical range overlaps every other's (a touch-sized link
      // is taller than the plain-text current page beside it, so they are
      // not all the same height, just centred on the same row).
      const items = within(crumbs).getAllByRole('listitem');
      const rects = items.map((item) => item.getBoundingClientRect());
      for (const [index, rect] of rects.entries()) {
        for (const other of rects.slice(index + 1)) {
          await expect(rect.top < other.bottom && other.top < rect.bottom).toBe(true);
        }
      }
      // At this width the segments overflow their row, so at least one
      // truncates with an ellipsis rather than wrapping or clipping bare,
      // and keeps its full text reachable via `title`.
      const segments = items.map((item) => item.lastElementChild as HTMLElement);
      const truncated = segments.filter((segment) => segment.scrollWidth > segment.clientWidth);
      await expect(truncated.length).toBeGreaterThan(0);
      for (const segment of truncated) {
        await expect(getComputedStyle(segment).textOverflow).toBe('ellipsis');
        await expect(segment.title).toBe(segment.textContent);
      }
      // Org and project segments are still real links with their full
      // accessible name, even while visually truncated.
      const rootLink = within(crumbs).getByRole('link', { name: 'Acme operations' });
      await expect(rootLink).toBeVisible();
      await expect(within(crumbs).getByRole('link', { name: 'Billing platform' })).toBeVisible();
      // A long root crumb's own box is no wider than its text; the gap
      // before the next separator is the ordinary crumb gap, not a
      // touch-floor remainder.
      const nextSeparator = items[1]?.querySelector('.header__crumb-separator');
      if (nextSeparator === null || nextSeparator === undefined) throw new Error('next separator missing');
      const observedGap = nextSeparator.getBoundingClientRect().left - rootLink.getBoundingClientRect().right;
      const standardGap = Number.parseFloat(getComputedStyle(crumbs).columnGap);
      await expect(Math.abs(observedGap - standardGap)).toBeLessThanOrEqual(1);
      await userEvent.click(menu);
      await expect(canvas.getByRole('button', { name: 'Menu' })).toHaveAttribute('aria-expanded', 'true');
    }
    const navigation = canvas.getByRole('navigation', { name: 'Sections' });
    await expect(within(navigation).getByRole('heading', { name: /Billing platform/ })).toBeVisible();
    const overview = within(navigation).getByText(dottedText(/^Overview\s*·\s*one project$/));
    await expect(overview).toHaveAttribute('aria-disabled', 'true');
    await expect(overview).toBeVisible();
  },
};

// The control floor of a touch screen. A story's browser has a fine pointer
// (`--control` 36px, tokens.css), and the crumb floor is the 44px a finger needs:
// at 36px the trail has room the bug never lacked, so the story runs at touch size.
const touchControl = () => {
  const root = document.documentElement;
  const saved = root.style.getPropertyValue('--control');
  root.style.setProperty('--control', 'var(--touch)');
  return () => root.style.setProperty('--control', saved);
};

// A phone: the trail shares one row, so the scope crumbs give up room. They stop
// at their separator, gap and `--control` hit floor; below that the link spilled
// out of its item over the next crumb. The root crumb is hidden, so the first
// crumb left carries no separator and no room kept for one.
export const PhoneCrumbsStayClear: Story = {
  globals: { viewport: { value: 'phoneNarrow', isRotated: false } },
  beforeEach: () => {
    fixture.reset([overviewProject]);
    return touchControl();
  },
  play: async ({ canvas }) => {
    const crumbs = await canvas.findByRole('list', { name: 'Breadcrumb' }, { timeout: 5000 });
    await expect(await canvas.findByText('Environment matrix', { selector: '.header__crumbs span' })).toBeVisible();
    const items = within(crumbs).getAllByRole('listitem').filter((item) => getComputedStyle(item).display !== 'none');
    await expect(items.length).toBeGreaterThanOrEqual(3);
    const row = crumbs.getBoundingClientRect();
    for (const [index, item] of items.entries()) {
      const box = item.getBoundingClientRect();
      // Content stays inside its own item, the item stays inside the row, and
      // an item starts where the one before it ends.
      await expect(item.lastElementChild!.getBoundingClientRect().right).toBeLessThanOrEqual(box.right + 0.5);
      await expect(box.right).toBeLessThanOrEqual(row.right + 0.5);
      if (index > 0) await expect(box.left).toBeGreaterThanOrEqual(items[index - 1]!.getBoundingClientRect().right - 0.5);
    }
    // No leading slash: the first crumb left is where the trail starts.
    await expect(getComputedStyle(items[0]!.querySelector('.header__crumb-separator')!).display).toBe('none');
    // The current page keeps room to be read, not just a separator.
    await expect(items.at(-1)!.lastElementChild!.getBoundingClientRect().width).toBeGreaterThanOrEqual(48);
  },
};

// `viewport-fit=cover` lets the page draw under a notch and the home indicator.
// A browser tab here has no insets, so the story gives a notched phone's to the
// `--safe-*` properties and asserts every shell surface that touches a viewport
// edge moves clear: the header, the account avatar and menu, the drawer, the
// skip link, a toast, and the strips left empty.
const NOTCH = { top: 47, right: 20, bottom: 34, left: 30 };

export const PhoneClearsSafeArea: Story = {
  globals: { viewport: { value: 'phone', isRotated: false } },
  beforeEach: () => {
    fixture.reset([overviewProject]);
    return safeArea(NOTCH);
  },
  play: async ({ canvas }) => {
    await expect(safeAreaIsWiredToEnv()).toBe(true);
    // A single project opens its matrix: wait for the redirect, which closes a menu opened before it.
    await canvas.findByRole('list', { name: 'Breadcrumb' }, { timeout: 5000 });
    await canvas.findByText('Environment matrix', { selector: '.header__crumbs span' });
    const px = (element: Element, property: 'paddingTop' | 'paddingBottom' | 'paddingLeft') => Number.parseFloat(getComputedStyle(element)[property]);
    // The skip link is the first tab stop and shows under the notch otherwise.
    await userEvent.tab();
    const skip = canvas.getByRole('link', { name: 'Skip to content' });
    await expect(skip).toHaveFocus();
    await expect(skip.getBoundingClientRect().top).toBeGreaterThanOrEqual(NOTCH.top);
    await expect(skip.getBoundingClientRect().left).toBeGreaterThanOrEqual(NOTCH.left);
    // The header covers the top strip itself: its content sits below the notch.
    const header = document.querySelector<HTMLElement>('.header')!;
    await expect(px(header, 'paddingTop')).toBe(8 + NOTCH.top);
    await expect(canvas.getByRole('button', { name: 'Menu' }).getBoundingClientRect().top).toBeGreaterThanOrEqual(NOTCH.top);
    const avatar = document.querySelector<HTMLElement>('.chrome__account')!;
    await expect(avatar.getBoundingClientRect().top).toBeGreaterThanOrEqual(NOTCH.top);
    // The shell keeps clear of the side strips and the home indicator, and the
    // strips are painted as chrome, the side and bottom ones only.
    const chrome = document.querySelector<HTMLElement>('.chrome')!;
    const main = document.querySelector<HTMLElement>('.main')!.getBoundingClientRect();
    const box = chrome.getBoundingClientRect();
    await expect(main.left).toBe(box.left + NOTCH.left);
    await expect(main.right).toBe(box.right - NOTCH.right);
    await expect(main.bottom).toBe(box.bottom - NOTCH.bottom);
    const strips = getComputedStyle(chrome, '::after');
    await expect([strips.borderTopWidth, strips.borderRightWidth, strips.borderBottomWidth, strips.borderLeftWidth]).toEqual(['0px', `${String(NOTCH.right)}px`, `${String(NOTCH.bottom)}px`, `${String(NOTCH.left)}px`]);
    // The account menu opens below the notch and inside the right inset.
    await userEvent.click(canvas.getByRole('button', { name: /^Account/ }));
    const menu = await canvas.findByRole('menu', { name: 'Account' });
    // It opens under the avatar that opened it, which is itself below the notch.
    await expect(menu.getBoundingClientRect().top).toBeGreaterThanOrEqual(avatar.getBoundingClientRect().bottom);
    await expect(window.innerWidth - menu.getBoundingClientRect().right).toBeGreaterThanOrEqual(NOTCH.right);
    await userEvent.keyboard('{Escape}');
    // The drawer's content starts below the notch and clear of the left strip.
    await userEvent.click(canvas.getByRole('button', { name: 'Menu' }));
    const drawer = document.querySelector<HTMLElement>('.sidebar')!;
    await expect(px(drawer, 'paddingTop')).toBe(6 + NOTCH.top);
    await expect(px(drawer, 'paddingBottom')).toBe(20 + NOTCH.bottom);
    await expect(px(drawer, 'paddingLeft')).toBe(NOTCH.left);
  },
};

// Beside the side menu the strip above the whole chrome is empty, painted as
// chrome, and the rail, the sidebar and the header start below it; the menu
// stops short of the left strip, and the account avatar clears the corner.
export const DesktopClearsSafeArea: Story = {
  parameters: { viewport: { options: { desktop1024: { name: 'Desktop 1024', styles: { width: '1024px', height: '768px' } } } } },
  globals: { viewport: { value: 'desktop1024', isRotated: false } },
  beforeEach: () => {
    fixture.reset([overviewProject]);
    return safeArea(NOTCH);
  },
  play: async ({ canvas }) => {
    // A single project opens its matrix: wait for the redirect, which closes a menu opened before it.
    await canvas.findByRole('list', { name: 'Breadcrumb' }, { timeout: 5000 });
    await canvas.findByText('Environment matrix', { selector: '.header__crumbs span' });
    const top = (selector: string) => document.querySelector<HTMLElement>(selector)!.getBoundingClientRect().top;
    await expect(top('.rail')).toBe(NOTCH.top);
    await expect(top('.sidebar')).toBe(NOTCH.top);
    await expect(top('.header')).toBe(NOTCH.top);
    // The header is its usual height: the strip is not counted twice.
    await expect(Number.parseFloat(getComputedStyle(document.querySelector('.header')!).paddingTop)).toBe(8);
    const chrome = document.querySelector<HTMLElement>('.chrome')!;
    await expect(document.querySelector<HTMLElement>('.rail')!.getBoundingClientRect().left).toBe(NOTCH.left);
    const strips = getComputedStyle(chrome, '::after');
    await expect([strips.borderTopWidth, strips.borderRightWidth, strips.borderBottomWidth, strips.borderLeftWidth]).toEqual([`${String(NOTCH.top)}px`, `${String(NOTCH.right)}px`, `${String(NOTCH.bottom)}px`, `${String(NOTCH.left)}px`]);
    // `--chrome-menu` is where the side menu ends, left strip included: a side
    // panel opens beside it.
    const probe = document.createElement('div');
    probe.style.width = 'var(--chrome-menu)';
    chrome.append(probe);
    await expect(probe.getBoundingClientRect().width).toBe(document.querySelector<HTMLElement>('.sidebar')!.getBoundingClientRect().right);
    probe.remove();
    // The avatar in the rail's foot clears the home indicator and the left strip.
    const avatar = document.querySelector<HTMLElement>('.chrome__account')!.getBoundingClientRect();
    await expect(avatar.left).toBeGreaterThanOrEqual(NOTCH.left);
    await expect(window.innerHeight - avatar.bottom).toBeGreaterThanOrEqual(NOTCH.bottom);
  },
};

const openDrawer = async (canvas: ReturnType<typeof within>) => {
  if (phone()) {
    await userEvent.click(await canvas.findByRole('button', { name: 'Menu' }));
  }
};

// An organisation page before any project was opened in it: the sidebar's
// project block is the organisation's project list.
export const SidebarListsProjects: Story = {
  beforeEach: () => {
    sessionStorage.removeItem('hikyo.lastProject');
    fixture.reset([overviewProject, secondProject]);
  },
  play: async ({ canvas }) => {
    await canvas.findByRole('link', { name: 'Settings for Payments' });
    await openDrawer(canvas);
    const navigation = canvas.getByRole('navigation', { name: 'Sections' });
    const list = within(navigation).getByRole('region', { name: 'Projects' });
    await expect(within(list).getByRole('link', { name: 'Billing platform' })).toBeVisible();
    await expect(within(list).getByRole('link', { name: 'Payments' })).toBeVisible();
    await expect(within(navigation).queryByRole('heading', { name: /^Project(?!s)/ })).toBeNull();
  },
};

// After a project was opened in this organisation, its block stays in the
// sidebar on the organisation's pages.
export const SidebarKeepsProject: Story = {
  beforeEach: () => {
    sessionStorage.setItem('hikyo.lastProject', JSON.stringify({ [overviewProject.org_id]: secondProject.id }));
    fixture.reset([overviewProject, secondProject]);
    return () => sessionStorage.removeItem('hikyo.lastProject');
  },
  play: async ({ canvas }) => {
    await canvas.findByRole('link', { name: 'Settings for Payments' });
    await openDrawer(canvas);
    const navigation = canvas.getByRole('navigation', { name: 'Sections' });
    await expect(await within(navigation).findByRole('heading', { name: /^Project\W*Payments$/ })).toBeVisible();
    await expect(within(navigation).getByRole('link', { name: 'Environment matrix' })).toBeVisible();
    await expect(within(navigation).queryByRole('region', { name: 'Projects' })).toBeNull();
  },
};

// Account settings has no project context block and no org-page project
// list: the sidebar's one back-link is the way back to
// a project, here the org's last-opened one.
export const SidebarBackLinkOnAccountSettings: Story = {
  parameters: { app: { path: '/settings' } },
  beforeEach: () => {
    sessionStorage.setItem('hikyo.lastProject', JSON.stringify({ [overviewProject.org_id]: secondProject.id }));
    fixture.reset([overviewProject, secondProject]);
    return () => sessionStorage.removeItem('hikyo.lastProject');
  },
  play: async ({ canvas }) => {
    await canvas.findByRole('heading', { name: 'Account & security', level: 1 }, { timeout: 5000 });
    await openDrawer(canvas);
    // The sidebar can remount once the last-opened project resolves: look the
    // link up afresh until it is there, then read its current navigation.
    const back = await waitFor(() => within(canvas.getByRole('navigation', { name: 'Sections' })).getByRole('link', { name: 'Payments' }));
    const navigation = canvas.getByRole('navigation', { name: 'Sections' });
    await expect(back).toHaveClass('sidebar__back');
    await expect(back).toHaveAttribute('href', expect.stringContaining('/matrix'));
    await expect(within(navigation).queryByRole('region', { name: 'Projects' })).toBeNull();
    await expect(within(navigation).queryByRole('heading', { name: /^Project\W/ })).toBeNull();
  },
};

export const LongNames: Story = {
  beforeEach: () => fixture.reset([{ ...overviewProject, name: 'Customer billing and reconciliation platform with an intentionally long operational project name' }, secondProject]),
  play: async ({ canvas }) => {
    await expect(await canvas.findByRole('link', { name: /Settings for Customer billing and reconciliation/ })).toBeVisible();
  },
};

/** Real navigation, form submission and cache refresh, with an explorable Docs starting state. */
export const CreateProjectJourney: Story = {
  tags: ['interaction-once'],
  beforeEach: ({ globals }) => {
    // Seed before rendering so the appearance replacement reaches the same
    // populated Projects route, providers and navigation shell as the journey.
    if (appearancePass(globals)) fixture.reset([overviewProject, secondProject]);
  },
  play: interactionOnce('pages-overview--create-project-journey', async ({ canvas, step }) => {
    if (isManualDocsFrame()) return;
    await step('Start in the authenticated overview', async () => {
      await expect(await canvas.findByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
      await expect(await canvas.findByText('Acme operations')).toBeVisible();
      await expect(canvas.queryByRole('link', { name: 'Settings for Billing platform' })).toBeNull();
    });
    await step('Follow the real Projects route', async () => {
      await userEvent.click(await canvas.findByRole('link', { name: 'Choose a project' }));
      await expect(await canvas.findByRole('heading', { name: 'Projects', level: 1 })).toBeVisible();
      await expect(await canvas.findByText('No projects yet. Create the first one below.')).toBeVisible();
      await expect(canvas.getByRole('button', { name: 'Create project' })).toBeDisabled();
    });
    await step('Create a project and wait for its list refresh', async () => {
      await userEvent.type(canvas.getByLabelText('Project name'), 'Billing platform');
      await userEvent.click(canvas.getByRole('button', { name: 'Create project' }));
      await expect(await canvas.findByText('Project Billing platform created.')).toBeVisible();
      await expect(canvas.getByRole('link', { name: 'Settings for Billing platform' })).toBeVisible();
      await expect(canvas.getByLabelText('Project name')).toHaveValue('');
      await expect(fixture.submittedNames()).toEqual(['Billing platform']);
    });
    await step('Return through the sidebar and revisit the shared project list', async () => {
      const narrow = phone();
      if (narrow) await userEvent.click(canvas.getByRole('button', { name: 'Menu' }));
      const navigation = canvas.getByRole('navigation', { name: 'Sections' });
      // One project now: Overview is greyed out, not a link, and says why.
      await waitFor(() => expect(within(navigation).queryByRole('link', { name: 'Overview' })).toBeNull());
      await expect(within(navigation).getByText(dottedText(/^Overview\s*·\s*one project$/))).toHaveAttribute('aria-disabled', 'true');
      await userEvent.click(within(navigation).getByRole('link', { name: 'Projects' }));
      await expect(await canvas.findByRole('heading', { name: 'Projects', level: 1 })).toBeVisible();
      if (narrow) await expect(canvas.getByRole('button', { name: 'Menu' })).toHaveAttribute('aria-expanded', 'false');
      await expect(canvas.getByRole('link', { name: 'Settings for Billing platform' })).toBeVisible();
      await expect(canvas.queryByText('No projects yet. Create the first one below.')).toBeNull();
    });
  }, async ({ canvas }) => {
    // Render the same final real-route composition without repeating creation
    // and cache-refresh behavior. Async readiness still precedes the a11y scan.
    await expect(await canvas.findByRole('heading', { name: 'Overview', level: 1 })).toBeVisible();
    await expect(await canvas.findByRole('link', { name: 'Settings for Billing platform' })).toBeVisible();
    await userEvent.click(canvas.getByRole('link', { name: 'Choose a project' }));
    await expect(await canvas.findByRole('heading', { name: 'Projects', level: 1 })).toBeVisible();
    await expect(await canvas.findByRole('link', { name: 'Settings for Billing platform' })).toBeVisible();
    await expect(canvas.getByLabelText('Project name')).toHaveValue('');
    await expect(canvas.queryByText('No projects yet. Create the first one below.')).toBeNull();
  }),
};
