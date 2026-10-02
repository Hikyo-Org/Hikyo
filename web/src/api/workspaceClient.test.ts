import { listValues } from "@hikyo/client";
import { getMetaOp, logoutOp, watchProjectEventsOp } from '@hikyo/operations';
import { ok, parsed, parsedPick } from './client.ts';
import { afterEach, expect, test, vi } from "vitest";

import { createWorkspaceClient } from "./workspaceClient.ts";
import {
  forgetWorkspace,
  HANDOFF_REQUEST_TIMEOUT_MS,
  rememberWorkspace,
  workspaceBearer,
  WORKSPACE_DATA_RESPONSE_MAX_BYTES,
  WorkspaceError,
} from "./workspace.ts";

const ORIGIN = "https://remote.example";

function seed(value: string, session: string): void {
  rememberWorkspace({
    origin: ORIGIN,
    value,
    session,
    idleExpiresAt: "2099-01-01T00:00:00Z",
    absoluteExpiresAt: "2099-01-01T00:00:00Z",
  });
}

afterEach(() => {
  forgetWorkspace(ORIGIN);
  vi.useRealTimers();
  vi.restoreAllMocks();
});

test("the bearer rides an Authorization header and nothing ambient travels", async () => {
  seed("secret-1", "ses_1");
  let seen: Request | undefined;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    seen = input as Request;
    return new Response("{}", {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });

  await createWorkspaceClient(ORIGIN).get({ url: "/api/v1/me/sessions" });

  expect(seen?.headers.get("Authorization")).toBe("Bearer secret-1");
  // credentials: 'omit' is load-bearing, it keeps the remote's CORS out of
  // credentials mode and cookies from ever crossing the origin.
  expect(seen?.credentials).toBe("omit");
  expect(seen?.url).toBe("https://remote.example/api/v1/me/sessions");
});

test("a real generated call routes to the remote with the bearer and no cookies", async () => {
  seed("secret-1", "ses_1");
  let seen: Request | undefined;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    seen = input as Request;
    return new Response('{"values":[]}', {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });

  // The whole seam as the wrappers use it: a generated SDK fn, its path
  // templated, its `security: bearer` resolved, pointed at the remote by the
  // per-call client. Not `client.get`, that skips the parts a real call hits.
  await listValues({
    path: { org: "org_a", project: "proj_b", environment: "env_c" },
    client: createWorkspaceClient(ORIGIN),
  });

  expect(seen?.url).toBe(
    "https://remote.example/api/v1/orgs/org_a/projects/proj_b/environments/env_c/values",
  );
  expect(seen?.headers.get("Authorization")).toBe("Bearer secret-1");
  expect(seen?.credentials).toBe("omit");
});

test("the bearer is read LIVE per request, not frozen at client creation", async () => {
  seed("secret-1", "ses_1");
  const client = createWorkspaceClient(ORIGIN);
  const seen: string[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    seen.push((input as Request).headers.get("Authorization") ?? "");
    return new Response("{}", {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  });

  await client.get({ url: "/api/v1/x" });
  // A step-up rotates the bearer in place under the same origin; the very next
  // call must carry the new value, not the one held when the client was built.
  seed("secret-2", "ses_1");
  await client.get({ url: "/api/v1/x" });

  expect(seen).toEqual(["Bearer secret-1", "Bearer secret-2"]);
});

test("no bearer fails closed rather than leaking an anonymous cross-origin call", async () => {
  const fetchSpy = vi.spyOn(globalThis, "fetch");
  // The client resolves the refusal into a result (throwOnError is off, so an
  // interceptor throw becomes `{ error }` with no response) rather than
  // rejecting, the guarantee that matters is that NO request left the browser.
  const result = await createWorkspaceClient(ORIGIN).get({ url: "/api/v1/x" });
  expect(result.error).toBeInstanceOf(WorkspaceError);
  expect(result.response).toBeUndefined();
  expect(fetchSpy).not.toHaveBeenCalled();
});

test("a 401 drops the bearer at once, the kill switch does not wait for the poll", async () => {
  seed("secret-1", "ses_1");
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response("{}", {
      status: 401,
      headers: { "Content-Type": "application/json" },
    }),
  );

  await createWorkspaceClient(ORIGIN).get({ url: "/api/v1/x" });

  expect(workspaceBearer(ORIGIN)).toBeUndefined();
});

test("a 401 for a value that was already rotated leaves the replacement alone", async () => {
  seed("secret-1", "ses_1");
  // Reseed INSIDE fetch, which runs only after the request interceptor has
  // already captured secret-1, so this is the real ordering: the credential is
  // rotated (a step-up: same session id, new value) while its old value's 401
  // is in flight. The stale 401 is for secret-1; the live secret-2 must survive.
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    expect((input as Request).headers.get("Authorization")).toBe(
      "Bearer secret-1",
    );
    seed("secret-2", "ses_1");
    return new Response("{}", {
      status: 401,
      headers: { "Content-Type": "application/json" },
    });
  });

  await createWorkspaceClient(ORIGIN).get({ url: "/api/v1/x" });

  expect(workspaceBearer(ORIGIN)?.session).toBe("ses_1");
  expect(workspaceBearer(ORIGIN)?.value).toBe("secret-2");
});

test("a stale 401 cannot drop a replacement epoch even if bearer text matches", async () => {
  seed("secret-1", "ses_1");
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    if (!(input instanceof Request)) {
      throw new Error("workspace client did not issue a Request");
    }
    expect(input.headers.get("Authorization")).toBe("Bearer secret-1");
    seed("secret-1", "ses_2");
    return new Response("{}", {
      status: 401,
      headers: { "Content-Type": "application/json" },
    });
  });

  await createWorkspaceClient(ORIGIN).get({ url: "/api/v1/x" });

  expect(workspaceBearer(ORIGIN)?.session).toBe("ses_2");
  expect(workspaceBearer(ORIGIN)?.value).toBe("secret-1");
});

test("ordinary remote calls stop at the fixed request deadline", async () => {
  vi.useFakeTimers();
  seed("secret-1", "ses_1");
  vi.spyOn(globalThis, "fetch").mockImplementation(
    (input) =>
      new Promise<Response>((_resolve, reject) => {
        const request = input as Request;
        request.signal.addEventListener(
          "abort",
          () => reject(request.signal.reason),
          { once: true },
        );
      }),
  );

  const pending = createWorkspaceClient(ORIGIN).get({ url: "/api/v1/x" });
  await vi.advanceTimersByTimeAsync(HANDOFF_REQUEST_TIMEOUT_MS);
  const result = await pending;

  expect(result.error).toBeInstanceOf(WorkspaceError);
});

test("ordinary remote calls reject an oversized declared response before parsing", async () => {
  seed("secret-1", "ses_1");
  vi.spyOn(globalThis, "fetch").mockResolvedValue(
    new Response("{}", {
      status: 200,
      headers: {
        "Content-Type": "application/json",
        "Content-Length": String(WORKSPACE_DATA_RESPONSE_MAX_BYTES + 1),
      },
    }),
  );

  const result = await createWorkspaceClient(ORIGIN).get({ url: "/api/v1/x" });

  expect(result.error).toBeInstanceOf(WorkspaceError);
});

test.each(['parsed', 'parsedPick', 'ok'])("%s preserves workspace response limits", async (wrapper) => {
  seed('secret-1', 'ses_1');
  const cancel = vi.fn();
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(new ReadableStream({ cancel }), {
    status: 400,
    headers: { 'Content-Length': String(WORKSPACE_DATA_RESPONSE_MAX_BYTES + 1) },
  }));
  const options = { client: createWorkspaceClient(ORIGIN) };
  const pending = wrapper === 'ok' ? ok(logoutOp, options)
    : wrapper === 'parsedPick' ? parsedPick(getMetaOp, options, { api_revision: true })
    : parsed(getMetaOp, options);
  await expect(pending).rejects.toThrow();
  expect(cancel).toHaveBeenCalledOnce();
});

test('generated bodyless workspace operations accept 204 without inventing a body', async () => {
  seed('secret-1', 'ses_1');
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(null, { status: 204 }));
  await expect(ok(logoutOp, { client: createWorkspaceClient(ORIGIN) })).resolves.toBeUndefined();
});

test('shared wrapper deadline stays active while a remote body stalls', async () => {
  vi.useFakeTimers();
  seed('secret-1', 'ses_1');
  const cancel = vi.fn();
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(new ReadableStream({ cancel }), {
    headers: { 'Content-Type': 'application/json' },
  }));
  const pending = parsed(getMetaOp, { client: createWorkspaceClient(ORIGIN) });
  const refused = expect(pending).rejects.toThrow();
  await vi.advanceTimersByTimeAsync(HANDOFF_REQUEST_TIMEOUT_MS);
  await refused;
  expect(cancel).toHaveBeenCalledOnce();
});

test('shared wrapper bounds chunked error responses before the generated parser', async () => {
  seed('secret-1', 'ses_1');
  const cancel = vi.fn();
  vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(new Uint8Array(WORKSPACE_DATA_RESPONSE_MAX_BYTES));
      controller.enqueue(new Uint8Array(1));
    },
    cancel,
  }), { status: 400 }));
  await expect(parsed(getMetaOp, { client: createWorkspaceClient(ORIGIN) })).rejects.toThrow();
  expect(cancel).toHaveBeenCalledOnce();
});

test('real generated SSE requests preserve bearer and keep healthy streams beyond the handshake deadline', async () => {
  vi.useFakeTimers();
  seed('secret-1', 'ses_1');
  let streamController: ReadableStreamDefaultController<Uint8Array> | undefined;
  let request: Request | undefined;
  const cancelled = vi.fn();
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    if (!(input instanceof Request)) throw new Error('expected a workspace Request');
    request = input;
    return new Response(new ReadableStream<Uint8Array>({
      start(controller) { streamController = controller; },
      cancel: cancelled,
    }), { headers: { 'Content-Type': 'text/event-stream' } });
  });
  const controller = new AbortController();
  const result = await watchProjectEventsOp.call({
    path: { org: 'org_a', project: 'proj_b' },
    client: createWorkspaceClient(ORIGIN),
    signal: controller.signal,
  });
  const first = result.stream.next();
  await vi.advanceTimersByTimeAsync(HANDOFF_REQUEST_TIMEOUT_MS + 1);
  expect(request?.signal.aborted).toBe(false);
  expect(request?.credentials).toBe('omit');
  expect(request?.headers.get('Authorization')).toBe('Bearer secret-1');
  streamController?.enqueue(new TextEncoder().encode('data: {"sequence":1}\r\n\r\n'));
  await expect(first).resolves.toMatchObject({ done: false, value: { sequence: 1 } });
  controller.abort();
  await result.stream.return(undefined);
  expect(cancelled).toHaveBeenCalledOnce();
});
