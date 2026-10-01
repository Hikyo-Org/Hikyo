import { describe, expect, it } from 'vitest';

import { ApiError } from './client.ts';
import { statusText } from './statusText.ts';

describe('statusText', () => {
  it('renders only explicitly configured statuses', () => {
    expect(statusText(new ApiError(403, 'failed'), { 401: 'sign in' }, 'unknown'))
      .toBe('unknown');
    expect(statusText(new ApiError(401, 'failed'), { 401: 'sign in' }, 'unknown'))
      .toBe('sign in');
  });

  it('preserves caller-safe details, including an empty detail', () => {
    const messages = { 400: (error: ApiError) => error.detail ?? 'invalid' };
    expect(statusText(new ApiError(400, 'failed', ''), messages, 'unknown')).toBe('');
    expect(statusText(new ApiError(400, 'failed', 'safe detail'), messages, 'unknown'))
      .toBe('safe detail');
    expect(statusText(new ApiError(400, 'failed'), messages, 'unknown')).toBe('invalid');
  });

  it('keeps unhandled server statuses distinct from an unconfirmed network failure', () => {
    const server = (error: ApiError) => `server ${String(error.status)}`;
    expect(statusText(new ApiError(503, 'failed'), {}, 'unknown outcome', server))
      .toBe('server 503');
    expect(statusText(new Error('network'), {}, 'unknown outcome', server))
      .toBe('unknown outcome');
  });

  it('does not treat a status-shaped non-API error as an authenticated refusal', () => {
    expect(statusText({ status: 401 }, { 401: 'sign in' }, 'unknown')).toBe('unknown');
  });

  it('preserves intentionally empty feature wording', () => {
    expect(statusText(new ApiError(404, 'failed'), { 404: '' }, 'unknown')).toBe('');
  });
});
