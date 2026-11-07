import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import { countsMark, NOTICE_CAUSE_TEXT, ownMark, personalSetRefusalText, personalWarningText } from './personal.ts';

const count = (over: Partial<{ holders: number; notices: number; required: boolean }>) => ({
  key_id: 'k1',
  name: 'DATABASE_URL',
  holders: 0,
  notices: 0,
  required: false,
  ...over,
});

describe('countsMark', () => {
  it('reads holders of eligible for a required key', () => {
    expect(countsMark(count({ required: true, holders: 11 }), 14)).toEqual({
      text: 'personal · 11/14',
      words: 'personal value required, 11 of 14 people hold one',
    });
  });

  it('counts holders and notices for an optional key', () => {
    expect(countsMark(count({ holders: 3, notices: 1 }), 14)?.text).toBe('3 personal · 1 notice');
    expect(countsMark(count({ holders: 1 }), 14)?.words).toBe('1 person holds a personal value');
  });

  it('marks nothing when nobody holds or is noticed', () => {
    expect(countsMark(count({}), 14)).toBeNull();
  });
});

describe('ownMark', () => {
  const entry = { name: 'DATABASE_URL', key_id: 'k1', set: false };

  it('says suspended before anything else', () => {
    expect(ownMark({ ...entry, set: true, suspended: true })).toEqual({
      text: 'personal · suspended',
      words: 'your personal value is suspended and not delivered',
    });
  });

  it('names the cause of a removal', () => {
    const mark = ownMark({ ...entry, notice: 'administrator' });
    expect(mark?.text).toBe('personal · removed');
    expect(mark?.words).toContain(NOTICE_CAUSE_TEXT.administrator);
  });

  it('distinguishes set, missing and absent', () => {
    expect(ownMark({ ...entry, set: true })?.text).toBe('personal');
    expect(ownMark({ ...entry, required: true })?.text).toBe('personal · missing');
    expect(ownMark(entry)).toBeNull();
  });
});

describe('personalWarningText', () => {
  it('names the broken rules and says the value is still delivered', () => {
    expect(personalWarningText('API_TOKEN', ['maxLength', 'pattern'])).toBe(
      'Your value for API_TOKEN does not satisfy these rules of the key: maxLength, pattern. This is a warning only: the value is kept and still delivered to you.',
    );
    expect(personalWarningText('API_TOKEN', ['pattern'])).toContain('this rule of the key: pattern.');
  });

  it('is silent without warnings', () => {
    expect(personalWarningText('API_TOKEN', undefined)).toBeNull();
    expect(personalWarningText('API_TOKEN', [])).toBeNull();
  });
});

describe('personalSetRefusalText', () => {
  const facts = { hasFactor: true, federatedSession: false, protectedEnvironment: false };
  const forbidden = new ApiError(403, 'not permitted');

  it('gives the remedy for each local cause of the uniform refusal', () => {
    expect(personalSetRefusalText(forbidden, { ...facts, protectedEnvironment: true })).toBe(
      'Personal values are for unprotected environments only.',
    );
    expect(personalSetRefusalText(forbidden, { ...facts, hasFactor: false })).toContain('Settings › Security');
    expect(personalSetRefusalText(forbidden, { ...facts, federatedSession: true })).toContain(
      'An identity provider approval cannot set one.',
    );
    expect(personalSetRefusalText(forbidden, facts)).toContain('Approve again with your passkey');
  });

  it('maps the other statuses and unknown errors', () => {
    expect(personalSetRefusalText(new ApiError(404, 'not found'), facts)).toContain('not declared');
    expect(personalSetRefusalText(new ApiError(401, 'unauthenticated'), facts)).toContain('Sign in again');
    expect(personalSetRefusalText(new Error('network'), facts)).toBe('The value could not be saved. Nothing was stored.');
  });
});
