import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

import { zCredentialRotation, zRotationRun } from '@hikyo/zod';
import { expect, it } from 'vitest';

import { ApiError } from './client.ts';
import {
  haltCause,
  haltReasonText,
  owedBadgeText,
  owedRunText,
  owedSummaryText,
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
    'the retry window ran out before a run could publish or extend the credential',
    'the recorded person no longer holds the access the rotation needs, or one of its keys or its environment is gone',
    'a pre-publish gate refused: the environment became protected, a key changed or a draft appeared',
    'the server recorded a reason this client does not know',
    'it starts no run and does not extend the delivered credential until a person resumes it: hikyo rotation resume',
    'run(s) that owe a provider role a drop, %d of them refused after a restore (%s): the keys may be serving that role again, so do NOT drop it at the provider until the keys have moved off it; read: hikyo rotation runs',
    'run(s) that owe a provider role a drop and the role is still live; before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back; read: hikyo rotation runs',
    'run(s) that owe a provider role a drop and a restore holds the rotation: the keys may be serving that role again, so do NOT drop it at the provider until you have checked that neither key serves it; read: hikyo rotation runs',
    'the server halted the rotation without a reason it can name',
  ]) {
    expect(source).toContain(fragment);
  }
});

// The halt sentences are one text in three places: this page, the CLI and the user docs.
it('keeps every halt sentence equal in the CLI, the docs and this page', () => {
  const cli = readFileSync(resolve(import.meta.dirname, '../../../internal/cli/credential_rotation.go'), 'utf8');
  const docs = readFileSync(
    resolve(import.meta.dirname, '../../../docs/site/src/content/docs/docs/rotation.mdx'),
    'utf8',
  );
  for (const word of ['retries_exhausted', 'authority_lost', 'gate_failed', 'incident_contained', 'unknown']) {
    const cause = haltCause({ ...halted, halt_reason: word });
    expect(cause.startsWith(`halted (${word}): `)).toBe(true);
    const sentence = cause.slice(`halted (${word}): `.length);
    expect(cli).toContain(sentence);
    expect(docs).toContain(`\`${word}\`: ${sentence}`);
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

const [activeRotation] = zCredentialRotation.array().parse(rotationList().items);
const halted = zCredentialRotation.parse(haltedRotation());

it('words a halt as the CLI does: fixed words, an unknown one quoted', () => {
  expect(haltReasonText(halted)).toBe(
    `HALTED: rotation ${halted.id} is halted (retries_exhausted): the retry window ran out before a run could publish or extend the credential; it starts no run and does not extend the delivered credential until a person resumes it: hikyo rotation resume ${halted.id}`,
  );
  expect(haltCause({ ...halted, halt_reason: 'authority_lost' })).toContain('(authority_lost): the recorded person no longer holds');
  expect(haltCause({ ...halted, halt_reason: 'gate_failed' })).toContain('(gate_failed): a pre-publish gate refused');
  expect(haltCause({ ...halted, halt_reason: undefined })).toBe('halted (unknown): the server halted the rotation without a reason it can name');
  expect(haltCause({ ...halted, halt_reason: 'unknown' })).toBe('halted (unknown): the server halted the rotation without a reason it can name');
  // A word on Object.prototype is an unrecognised word, never a sentence.
  for (const word of ['toString', 'constructor', '__proto__']) {
    expect(haltCause({ ...halted, halt_reason: word })).toBe(`halted ("${word}"): the server recorded a reason this client does not know`);
  }
  expect(haltCause({ ...halted, halt_reason: 'password=hunter2' })).toBe('halted ("password=hunter2"): the server recorded a reason this client does not know');
});

it('words the owed-drop summary as the CLI does, and says drop only when no restore refused one', () => {
  expect(owedSummaryText(activeRotation!)).toBe(
    `ATTENTION: rotation ${activeRotation!.id} has 3 run(s) that owe a provider role a drop, 1 of them refused after a restore (published_versions_changed): the keys may be serving that role again, so do NOT drop it at the provider until the keys have moved off it; read: hikyo rotation runs ${activeRotation!.id}`,
  );
  expect(owedBadgeText(activeRotation!)).toBe('Do not drop: restore');
  const plain = { ...activeRotation!, owed_drop_runs: 2n, owed_drop_runs_versions_changed: 0n };
  expect(owedSummaryText(plain)).toBe(
    `ATTENTION: rotation ${plain.id} has 2 run(s) that owe a provider role a drop and the role is still live; before you drop it at the provider, check that neither key serves that role, because a revision restore can put it back; read: hikyo rotation runs ${plain.id}`,
  );
  expect(owedBadgeText(plain)).toBe('Owes a drop');
  // A restore hold that is still on is never worded as a plain drop.
  const held = { ...plain, restore_hold: true };
  expect(owedSummaryText(held)).toBe(
    `ATTENTION: rotation ${held.id} has 2 run(s) that owe a provider role a drop and a restore holds the rotation: the keys may be serving that role again, so do NOT drop it at the provider until you have checked that neither key serves it; read: hikyo rotation runs ${held.id}`,
  );
  expect(owedBadgeText(held)).toBe('Do not drop: restore');
  // A deleted rotation says it is deleted.
  expect(owedSummaryText({ ...plain, state: 'disabled' })).toContain(`rotation ${plain.id} (deleted) has 2 run(s)`);
});
