import { join } from 'node:path';

import { expect, test, type Locator, type Page } from '@playwright/test';
import { z } from 'zod';

/**
 * Delivery-target condition reporting from a real controller to the browser
 * (#791; k8s-condition-reporting ADR D3 to D6, D11).
 *
 * Each test asks the Go side for one named step, which arranges the cluster
 * and the server and asserts the wire (statuses, rows, captured bodies), then
 * asserts here what a human reading the Kubernetes tab is shown. No state may
 * read as healthy unless it is `reported`.
 *
 * Every page load is proved by a row that must be there, its control, before
 * anything is asserted absent: a listing that failed, is still loading or is
 * unreadable renders no rows either.
 */

function required(name: string): string {
  const value = process.env[name];
  if (value === undefined || value === '') {
    throw new Error(`${name} is not set: run scripts/ci/reporting-e2e.sh`);
  }
  return value;
}

const CONTROL = required('HIKYO_REPORTING_E2E_CONTROL');
const SCREENSHOTS = required('HIKYO_REPORTING_E2E_SCREENSHOTS');

const zFacts = z.object({
  namespace: z.string(),
  username: z.string(),
  password: z.string(),
  org: z.string(),
  project: z.string(),
  orgB: z.string(),
  projectB: z.string(),
});
/** What the payload audit looked at, by kind of body and category of denied string. */
const zAudit = z.record(z.string(), z.number());
const AUDITED = [
  'reports',
  'tombstones',
  'secret value',
  'config value',
  'key name',
  'bearer',
  'condition message',
  'cursor',
  'cursor binding',
  'stamp',
  'managed Secret UID',
];

/**
 * How far the server's delivery clock is ahead of real time. The Go side moves
 * that clock to reach staleness, and the browser's follows it, so the ages on
 * the page are measured against the instant the states were derived at.
 */
let clockOffsetMs = 0;

/** step runs one named arrangement on the Go side and parses what it answers. */
async function step<T>(name: string, schema: z.ZodType<T>): Promise<T> {
  const response = await fetch(`${CONTROL}/step/${name}`, { method: 'POST' });
  if (!response.ok) {
    throw new Error(`step ${name} answered ${String(response.status)}`);
  }
  const answer = z.object({ result: schema, clock_offset_ms: z.number() }).parse(await response.json());
  clockOffsetMs = answer.clock_offset_ms;
  return answer.result;
}

const arrange = (name: string) => step(name, z.null());

test.describe.configure({ mode: 'serial' });

let page: Page;
let facts: z.infer<typeof zFacts>;

test.beforeAll(async ({ browser }) => {
  facts = await step('facts', zFacts);
  page = await browser.newPage();
  await page.goto('/login');
  await page.getByRole('button', { name: /^Password\b/ }).click();
  await page.getByLabel('Username').fill(facts.username);
  await page.getByLabel('Password').fill(facts.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page).not.toHaveURL(/\/login/);
});

test.afterAll(async () => {
  await page.close();
});

/**
 * openTargets loads a project's Kubernetes tab afresh, so every state is read,
 * never cached, and returns only once the listing is on the page: the control
 * row is rendered, nothing is still being read and no listing failed.
 */
async function openTargets(org: string, project: string, control: string): Promise<void> {
  await page.clock.setFixedTime(Date.now() + clockOffsetMs);
  await page.goto(`/orgs/${org}/projects/${project}/machine-access`);
  await page.getByRole('tab', { name: /^Kubernetes targets/ }).click();
  await expect(page.getByRole('heading', { name: 'Reported by controllers' })).toBeVisible();
  await expect(row(control)).toHaveCount(1);
  await expect(page.getByText('Reading delivery-target reports…')).toHaveCount(0);
  await expect(page.getByText(/could not be read, so they are unknown here/)).toHaveCount(0);
  await expect(page.getByText(/capabilities could not be read/)).toHaveCount(0);
}

/** Project A's control is the happy CR, which holds a row from the first test to the last. */
const openA = () => openTargets(facts.org, facts.project, 'happy');

function row(name: string): Locator {
  return page
    .locator('tr[data-state]')
    .filter({ has: page.getByRole('cell', { name, exact: true }) });
}

async function expectState(name: string, state: string): Promise<void> {
  await expect(row(name)).toHaveCount(1);
  await expect(row(name)).toHaveAttribute('data-state', state);
  await expect(row(name).locator('.badge, [data-state]').first()).toBeVisible();
}

async function shot(name: string): Promise<void> {
  await page.screenshot({ path: join(SCREENSHOTS, `${name}.png`) });
}

test('1 happy path: a reconciled CR is reported with its namespace and name', async () => {
  await arrange('happy-path');
  await openA();
  await expectState('happy', 'reported');
  await expect(page.getByRole('heading', { name: new RegExp(`namespace ${facts.namespace}`) })).toBeVisible();
  await expect(row('happy')).toContainText('reported by the controller');
  await expect(row('happy')).toContainText('Ready=True/Reconciled');
  await shot('01-reported');
});

test('2 cross-tenant: a report naming another tenant is refused and shows nowhere', async () => {
  await arrange('cross-tenant');
  // Tenant B's own target is listed, so B is readable and its listing loaded.
  await openTargets(facts.orgB, facts.projectB, 'tenant-b');
  await expectState('tenant-b', 'reported');
  await expect(page.locator('tr[data-state]')).toHaveCount(1);
  await expect(row('cross-tenant')).toHaveCount(0);
  await shot('02-cross-tenant-project-b');
  await openA();
  await expectState('happy', 'reported');
  await expect(row('cross-tenant')).toHaveCount(0);
  await expect(row('tenant-b')).toHaveCount(0);
});

test('3 revoked grant: the row turns reporter-revoked', async () => {
  await arrange('grant-revoked');
  await openA();
  await expectState('grant-revoked', 'reporter-revoked');
  await expect(row('grant-revoked')).not.toContainText('reported by the controller');
  await shot('03-grant-revoked');
});

test('3 revoked credential: its row is reporter-revoked while other CRs keep reporting', async () => {
  await arrange('credential-revoked');
  await openA();
  await expectState('credential-revoked', 'reporter-revoked');
  await expectState('happy', 'reported');
  await shot('04-credential-revoked');
});

test('3 deleted principal: its rows are gone', async () => {
  await arrange('principal-deleted');
  await openA();
  await expect(row('credential-revoked')).toHaveCount(0);
  await expectState('happy', 'reported');
  await shot('05-principal-deleted');
});

test('4 out-of-order reports are refused 409 and never shown as refused', async () => {
  await arrange('out-of-order');
  await openA();
  await expectState('happy', 'reported');
  await shot('06-out-of-order-unchanged');
});

test('4 unknown reason: refused 422 and the row says refused', async () => {
  await arrange('unknown-reason');
  await openA();
  await expectState('happy', 'refused');
  await expect(row('happy')).toContainText('the latest report was refused (vocabulary)');
  await shot('07-refused');
});

test('5 stale: after the operator stops, the row is stale', async () => {
  await arrange('operator-stopped');
  await openA();
  await expectState('happy', 'stale');
  await expect(row('happy')).toContainText('no report within its heartbeat');
  await shot('08-stale');
});

test('5 never reported: a CR created while reporting is disabled has no row', async () => {
  await arrange('reporting-disabled');
  await openA();
  await expect(row('never-reported')).toHaveCount(0);
  await expectState('happy', 'reported');
  await shot('09-never-reported-tab');
  await page.getByRole('tab', { name: /^Service accounts/ }).click();
  await page.getByRole('button', { name: /never-reported-reporter/ }).first().click();
  await expect(page.getByText('No reports from this account in the environments you can read. No report is not health.')).toBeVisible();
  await shot('10-never-reported-account');
});

test('6 deletion: the tombstone removes the row', async () => {
  await arrange('tombstone');
  await openA();
  await expectState('happy', 'reported');
  await expect(row('tombstoned')).toHaveCount(0);
  await shot('11-tombstoned');
});

test('6 deletion with the server unreachable: the row stays, goes stale, and is purged', async () => {
  await arrange('unreachable-delete');
  await openA();
  await expectState('orphaned', 'reported');
  await shot('12-orphaned-reported');

  await arrange('stale-after-delete');
  await openA();
  await expectState('orphaned', 'stale');
  await shot('13-orphaned-stale');

  // The happy CR reported an hour short of the purge threshold, so it is spared.
  await arrange('purge');
  await openA();
  await expectState('happy', 'reported');
  await expect(row('orphaned')).toHaveCount(0);
  await shot('14-orphaned-purged');
});

test('7 secret-safe payload: every body on the wire is value-free', async () => {
  const audit = await step('wire-audit', zAudit);
  for (const audited of AUDITED) {
    expect(audit[audited], audited).toBeGreaterThan(0);
  }
});
