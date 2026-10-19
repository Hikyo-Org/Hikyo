import { expect } from 'storybook/test'

export const phone = () => globalThis.matchMedia('(max-width: 700px)').matches

/**
 * The page column starts at its container's left edge (DESIGN.md: the
 * left-aligned page column, keeping its max width), never centred.
 */
export async function expectLeftAlignedPage(inside: Element): Promise<void> {
  const page = inside.closest<HTMLElement>('.page--chrome')
  if (page === null) throw new Error('page column missing')
  await expect(getComputedStyle(page).marginLeft).toBe('0px')
  let parent = page.parentElement
  // Auth/session ownership may add boxless wrappers. Alignment belongs to the
  // nearest containing box, rather than display:contents' zero bounding rect.
  while (parent !== null && getComputedStyle(parent).display === 'contents') parent = parent.parentElement
  if (parent === null) throw new Error('page column has no container')
  const style = getComputedStyle(parent)
  const contentLeft = parent.getBoundingClientRect().left + Number.parseFloat(style.borderLeftWidth) + Number.parseFloat(style.paddingLeft)
  await expect(Math.abs(page.getBoundingClientRect().left - contentLeft)).toBeLessThanOrEqual(1)
}

/**
 * A settings row with two or more buttons stacks (`settings-row--stacked`):
 * its copy, then ONE `.actions` row under it, never buttons split around the
 * copy's line. At desktop width the buttons share one line; on a phone a row
 * of two or more buttons is the equal-width two-column grid.
 */
export async function expectStackedRow(row: Element, count: number): Promise<void> {
  await expect(row).toHaveClass('settings-row--stacked')
  const rows = row.querySelectorAll(':scope > .actions')
  await expect(rows).toHaveLength(1)
  const actions = rows[0] as HTMLElement
  const copy = row.querySelector(':scope > .settings-row__copy')
  if (copy === null) throw new Error('settings row copy missing')
  await expect(actions.getBoundingClientRect().top).toBeGreaterThanOrEqual(copy.getBoundingClientRect().bottom - 0.5)
  const buttons = [...actions.querySelectorAll<HTMLElement>(':scope > .btn, :scope > * > .btn')]
  await expect(buttons).toHaveLength(count)
  if (phone()) {
    // The grid takes direct `.btn` children only; a row of wrapped controls
    // (a button with its own status line) wraps instead.
    if (actions.querySelectorAll(':scope > .btn').length < 2) return
    await expect(getComputedStyle(actions).display).toBe('grid')
    await expect(new Set(buttons.map((button) => Math.round(button.getBoundingClientRect().width))).size).toBe(1)
  } else {
    await expect(new Set(buttons.map((button) => Math.round(button.getBoundingClientRect().top))).size).toBe(1)
  }
}
