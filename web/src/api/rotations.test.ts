import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { zCredentialRotation, zRotationRun } from '@hikyo/zod';
import { expect, it } from 'vitest';

import { ApiError } from './client.ts';
import {
  owedRunText,
  owesDrop,
  releaseHoldCommand,
  restoreHoldText,
  rfc3339,
  rotationRefusalText,
} from './rotations.ts';
import { haltedRotation, rotationList, owedRuns, settledRuns, HELD_ROTATION, QUARANTINED_RUN, REFUSED_RUN, VERSIONS_CHANGED_RUN } from '../testkit/rotations.ts';

const runs = zRotationRun.array().parse(owedRuns.items);
const [versionsChanged, refused, quarantined, settled] = runs;
const held = zCredentialRotation.parse(rotationList().items[1]);

it('says the published_versions_changed caution exactly as the CLI does', () => {
  expect(owedRunText(versionsChanged!)).toBe(
    `ATTENTION: run ${VERSIONS_CHANGED_RUN} is revoke_refused (published_versions_changed): a restore put an earlier value back, so the keys may be serving the role it owes a drop again (stays valid until 2026-10-30T00:00:00Z); do NOT drop it at the provider until the keys have moved off it`,
  );
});

it('says a lower bound as at least, and possibly longer', () => {
  expect(owedRunText(refused!)).toBe(
    `ATTENTION: run ${REFUSED_RUN} is revoke_refused and a provider role it owes a drop is still live (stays valid at least until 2026-10-29T12:30:45Z, and possibly longer: an extend of it is unsettled); before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back`,
  );
});

it('says no recorded expiry when none was recorded', () => {
  expect(owedRunText(quarantined!)).toBe(
    `ATTENTION: run ${QUARANTINED_RUN} is quarantine and a provider role it owes a drop is still live (no recorded expiry); before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back`,
  );
});

it('flags only the phases that owe a drop', () => {
  expect(runs.filter(owesDrop).map((run) => run.id)).toEqual([VERSIONS_CHANGED_RUN, REFUSED_RUN, QUARANTINED_RUN]);
  expect(owesDrop(settled!)).toBe(false);
});

it('names the restore hold, its deadline and the release command', () => {
  expect(restoreHoldText(held)).toBe(
    `RESTORE HOLD: rotation ${HELD_ROTATION} is held after a restore and starts no run (the delivered credential lapses at 2099-01-31T00:00:00Z); reconcile its grants and provider credential, then run: hikyo rotation release-hold ${HELD_ROTATION}`,
  );
  expect(releaseHoldCommand(held)).toBe(`hikyo rotation release-hold ${HELD_ROTATION}`);
  expect(restoreHoldText({ ...held, credential_expires_at: null })).toContain('starts no run (no recorded expiry);');
});

it('prints instants as the CLI does: UTC, whole seconds, unparsable input untouched', () => {
  expect(rfc3339('2026-10-29T14:30:45.123+02:00')).toBe('2026-10-29T12:30:45Z');
  expect(rfc3339('not a time')).toBe('not a time');
});

// The sentences are one text in three places (the CLI, the docs and this page).
// The CLI is the source: every fixed fragment below must still be in its code.
it('keeps the CLI wording this page repeats', () => {
  const source = readFileSync(resolve(import.meta.dirname, '../../../internal/cli/credential_rotation.go'), 'utf8');
  for (const fragment of [
    'a restore put an earlier value back, so the keys may be serving the role it owes a drop again',
    'do NOT drop it at the provider until the keys have moved off it',
    'a provider role it owes a drop is still live',
    'before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back',
    'stays valid at least until ',
    ', and possibly longer: an extend of it is unsettled',
    'no recorded expiry',
    'the delivered credential lapses at ',
    'is held after a restore and starts no run',
    'reconcile its grants and provider credential, then run: hikyo rotation release-hold',
  ]) {
    expect(source).toContain(fragment);
  }
});

it('words a refused read without naming a cause it does not know', () => {
  const denied = new ApiError(404, 'not found');
  expect(rotationRefusalText(denied, 'rotations')).toBe('The rotations could not be listed. Listing them needs read on this environment.');
  expect(rotationRefusalText(new ApiError(500, 'internal'), 'runs')).toBe('The runs could not be listed. Reload to try again.');
  expect(rotationRefusalText(new Error('offline'), 'runs')).toBe('The runs could not be listed. Reload to try again.');
});

it('keeps fixtures honest: they parse through the contract', () => {
  expect(zCredentialRotation.parse(haltedRotation()).state).toBe('halted');
  expect(zRotationRun.array().parse(settledRuns.items)).toHaveLength(2);
});
