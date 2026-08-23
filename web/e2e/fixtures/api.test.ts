import { describe, expect, it } from 'vitest';

import { BrowserApiError } from './api.ts';

describe('BrowserApiError', () => {
  it('keeps the response status and body as typed error data', () => {
    const error = new BrowserApiError('DELETE', '/api/v1/orgs/missing', 404, '{"error":"gone"}');

    expect(error).toBeInstanceOf(Error);
    expect(error.name).toBe('BrowserApiError');
    expect(error.status).toBe(404);
    expect(error.body).toBe('{"error":"gone"}');
    expect(error.message).toBe(
      'DELETE /api/v1/orgs/missing answered 404: {"error":"gone"}',
    );
  });
});
