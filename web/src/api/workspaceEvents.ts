import { WorkspaceError, WORKSPACE_CONTROL_RESPONSE_MAX_BYTES } from './workspace.ts';

/** Bound each SSE frame, not the lifetime of a healthy advisory stream. */
export function boundedWorkspaceEvents(response: Response, signal: AbortSignal): Response {
  if (response.body === null) return response;
  let frameBytes = 0;
  let previousNewline = false;
  let previousCR = false;
  const frames = new TransformStream<Uint8Array, Uint8Array>({
    transform(chunk, controller) {
      // Normalize before the generated reader: CRLF may straddle fetch chunks.
      let output: number[] = [];
      for (const byte of chunk) {
        if (previousCR && byte === 10) {
          previousCR = false;
          continue;
        }
        previousCR = byte === 13;
        const normalized = previousCR ? 10 : byte;
        frameBytes += 1;
        if (frameBytes > WORKSPACE_CONTROL_RESPONSE_MAX_BYTES) {
          throw new WorkspaceError('The remote event frame is too large.');
        }
        output.push(normalized);
        if (normalized === 10 && previousNewline) frameBytes = 0;
        previousNewline = normalized === 10;
        // Keep the generated reader's intermediate buffer bounded too, even
        // if one network chunk contains many individually valid events.
        if (output.length === 16 * 1024) {
          controller.enqueue(Uint8Array.from(output));
          output = [];
        }
      }
      if (output.length > 0) controller.enqueue(Uint8Array.from(output));
    },
  });
  const headers = new Headers(response.headers);
  headers.delete('Content-Length');
  headers.delete('Content-Encoding');
  return new Response(response.body.pipeThrough(frames, { signal }), {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}
