import { expect, userEvent, waitFor, within } from 'storybook/test'

/**
 * Selects a page tab (routes/Sections.tsx SectionTabs) and waits until it is
 * selected. A click can land on the tree AuthProvider is about to replace when
 * the session binds; that click reaches no handler, so this clicks again on the
 * live tab until the selection holds. The selection is in `?tab=`, which the
 * story router keeps across that remount (withApp.tsx).
 */
export async function openTab(canvas: ReturnType<typeof within>, name: string): Promise<void> {
  await waitFor(async () => {
    const tab = canvas.getByRole('tab', { name })
    if (tab.getAttribute('aria-selected') !== 'true') await userEvent.click(tab)
    await expect(canvas.getByRole('tab', { name })).toHaveAttribute('aria-selected', 'true')
  }, { timeout: 3000 })
}
