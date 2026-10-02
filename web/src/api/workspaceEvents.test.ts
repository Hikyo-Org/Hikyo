import { expect, test, vi } from 'vitest';
import { boundedWorkspaceEvents } from './workspaceEvents.ts';
import { WORKSPACE_CONTROL_RESPONSE_MAX_BYTES } from './workspace.ts';

test('oversized incomplete SSE frame fails and cancels its foreign stream', async () => {
  const cancel = vi.fn();
  const response = new Response(new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new TextEncoder().encode('data:' + 'x'.repeat(WORKSPACE_CONTROL_RESPONSE_MAX_BYTES)));
    },
    cancel,
  }));
  await expect(boundedWorkspaceEvents(response, new AbortController().signal).text())
    .rejects.toThrow('event frame is too large');
  expect(cancel).toHaveBeenCalledOnce();
});

test('many valid frames exceed cumulative limit without ending the stream', async () => {
  const frame = 'data:' + 'x'.repeat(32_000) + '\n\n';
  const bytes = new TextEncoder().encode(frame.repeat(12));
  const response = new Response(bytes);
  expect(bytes.byteLength).toBeGreaterThan(WORKSPACE_CONTROL_RESPONSE_MAX_BYTES);
  await expect(boundedWorkspaceEvents(response, new AbortController().signal).text())
    .resolves.toBe(frame.repeat(12));
});

test('CRLF and CR-only delimiters normalize across network chunk boundaries', async () => {
  const response = new Response(new ReadableStream<Uint8Array>({
    start(controller) {
      for (const text of ['data:one\r', '\n\r', '\ndata:two\r\r']) {
        controller.enqueue(new TextEncoder().encode(text));
      }
      controller.close();
    },
  }));
  await expect(boundedWorkspaceEvents(response, new AbortController().signal).text())
    .resolves.toBe('data:one\n\ndata:two\n\n');
});

test('abort cancels a long-lived idle event stream', async () => {
  const cancel = vi.fn();
  const controller = new AbortController();
  const response = boundedWorkspaceEvents(new Response(new ReadableStream({ cancel })), controller.signal);
  const pending = expect(response.text()).rejects.toThrow();
  controller.abort(new Error('workspace changed'));
  await pending;
  expect(cancel).toHaveBeenCalledOnce();
});
