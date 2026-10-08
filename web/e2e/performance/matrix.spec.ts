import { writeFile } from 'node:fs/promises';
import { expect, test, type Page } from '@playwright/test';
import { z } from 'zod';
import type { RenderCondition } from './timing.ts';

const reportSchema = z.object({
  samples: z.array(z.object({ label: z.string(), durationMs: z.number().nonnegative() })),
  events: z.array(z.object({ name: z.string(), durationMs: z.number().nonnegative() })),
  eventTimingSupported: z.boolean(), failures: z.array(z.string()),
});
async function measure(page: Page, label: string, condition: RenderCondition, action: () => Promise<void>) {
  const before = await page.evaluate(() => window.matrixPerformance.read().samples.length);
  await page.evaluate(({ label, condition }) => window.matrixPerformance.arm(label, condition), { label, condition });
  await action();
  await expect.poll(() => page.evaluate(() => window.matrixPerformance.read().samples.length)).toBe(before + 1);
}

test('timing includes blocked input handlers and asynchronous completion', async ({ page }) => {
  await page.goto('/e2e/performance/index.html');
  await expect(page.getByRole('button', { name: /SETTING_0001 in environment-00:/ })).toBeVisible();
  await page.evaluate(() => {
    const button = document.createElement('button');
    button.textContent = 'Timing probe';
    button.style.position = 'fixed';
    button.style.inset = '0 auto auto 0';
    button.style.zIndex = '10000';
    const output = document.createElement('output');
    output.id = 'timing-probe-output';
    document.body.append(button, output);
    button.addEventListener('pointerdown', () => {
      const until = performance.now() + 120;
      while (performance.now() < until) { /* Deliberate blocking negative control. */ }
      setTimeout(() => { output.textContent = 'complete'; }, 100);
    });
  });
  await measure(page, 'timing-probe', { selector: '#timing-probe-output', text: 'complete' }, () => page.getByRole('button', { name: 'Timing probe', exact: true }).click());
  const result = reportSchema.parse(await page.evaluate(() => ({ ...window.matrixPerformance.read(), failures: window.matrixPerformance.failures })));
  expect(result.samples[0]?.durationMs).toBeGreaterThanOrEqual(220);
});

test('large matrix remains usable while filtering, editing and receiving updates', async ({ page }, testInfo) => {
  test.setTimeout(60_000);
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/e2e/performance/index.html');
  await expect(page.getByRole('heading', { name: 'Environment matrix', level: 1 })).toBeVisible();
  await expect(page.getByRole('button', { name: /SETTING_0001 in environment-00:/ })).toBeVisible();
  await expect(page.getByText('Loading environment matrix…')).toHaveCount(0);
  // One warmup followed by five measured cycles. Every save uses new material.
  for (let cycle = 0; cycle < 6; cycle++) {
    const menu = page.getByRole('button', { name: 'Menu', exact: true });
    if (await menu.isVisible()) await menu.click();
    await measure(page, 'filter-problems', { selector: '.matrix__filter', text: 'showing 100 of 1000 keys' }, () => page.getByRole('button', { name: /^problems/ }).click());
    if (await menu.isVisible()) await page.keyboard.press('Escape');
    await measure(page, 'show-all', { selector: '.matrix__filter', absent: true }, () => page.getByRole('button', { name: 'Show all keys' }).click());
    await measure(page, 'open-editor', { selector: 'dialog[open]' }, () => page.getByRole('button', { name: /SETTING_0001 in environment-00:/ }).click());
    const editor = page.getByRole('dialog');
    const input = editor.getByLabel('environment-00 value', { exact: true });
    await input.fill('');
    const value = `changed-${String(cycle)}`;
    await measure(page, 'type-value', { selector: 'dialog[open] textarea', value }, () => input.pressSequentially(value));
    await measure(page, 'save-draft', { selector: 'dialog[open]', absent: true }, () => editor.getByRole('button', { name: 'Save 1 draft', exact: true }).click());
    await expect(page.locator('.notice')).toContainText('1 draft updated for SETTING_0001');
    await expect(editor).toHaveCount(0);
    await measure(page, 'live-update', { selector: '.matrix', text: `live-update-${String(cycle + 1)}` }, () => page.getByRole('button', { name: 'Refresh fixture' }).click());
  }
  const report = reportSchema.parse(await page.evaluate(() => ({ ...window.matrixPerformance.read(), failures: window.matrixPerformance.failures })));
  const samples = report.samples.slice(6); // The six warmup actions.
  const labels = [...new Set(samples.map((sample) => sample.label))];
  const metrics = labels.map((label) => {
    const times = samples.filter((sample) => sample.label === label).map((sample) => sample.durationMs).sort((a, b) => a - b);
    const medianMs = times[Math.floor(times.length / 2)];
    const maxMs = times.at(-1);
    if (medianMs === undefined || maxMs === undefined) throw new Error(`No samples for ${label}`);
    return { label, samples: times.length, medianMs, maxMs };
  });
  const reportPath = testInfo.outputPath('matrix-performance.json');
  await writeFile(reportPath, JSON.stringify({ viewport: testInfo.project.name, keys: 1000, environments: 20, metrics, ...report }, null, 2));
  await testInfo.attach('matrix-performance.json', { path: reportPath, contentType: 'application/json' });
  console.log(JSON.stringify({ viewport: testInfo.project.name, metrics }));
  expect(errors).toEqual([]);
  expect(report.failures).toEqual([]);
  expect(metrics).toHaveLength(6);
  for (const metric of metrics) {
    expect(metric.samples).toBe(5);
    // Provisional lab ceilings for completed actions, not a field INP SLO.
    expect(metric.medianMs, `${metric.label} median`).toBeLessThan(200);
    expect(metric.maxMs, `${metric.label} maximum`).toBeLessThan(500);
  }
});
