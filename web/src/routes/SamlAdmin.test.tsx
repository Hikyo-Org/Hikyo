// @vitest-environment happy-dom
import { act } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { renderForm, settleTask } from "../testkit/renderForm.tsx";
import { SamlProvidersPanel } from "./SamlProvidersPanel.tsx";
import { SamlSpKeysPanel } from "./SamlSpKeysPanel.tsx";

afterEach(() => {
  vi.unstubAllGlobals();
});

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function pathOf(input: Parameters<typeof fetch>[0]): {
  method: string;
  path: string;
  request: Request;
} {
  const request = input instanceof Request ? input : new Request(input);
  return {
    method: request.method,
    path: new URL(request.url, "http://localhost").pathname,
    request,
  };
}

function setNativeValue(
  element: HTMLInputElement | HTMLTextAreaElement,
  value: string,
): void {
  const proto =
    element instanceof HTMLTextAreaElement
      ? HTMLTextAreaElement.prototype
      : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, "value")?.set;
  if (setter === undefined) throw new Error("no value setter");
  setter.call(element, value);
  element.dispatchEvent(new Event("input", { bubbles: true }));
}

function button(container: HTMLElement, label: string): HTMLButtonElement {
  const found = [...container.querySelectorAll("button")].find(
    (b) => b.textContent === label,
  );
  if (found === undefined) throw new Error(`button "${label}" is missing`);
  return found;
}

const provider = {
  slug: "acme",
  display_name: "Acme",
  kind: "saml",
  entity_id: "https://idp.example/acme",
  acs_url: "https://sp.example/acs",
  sso_redirect_url: "https://idp.example/acme/sso",
  signing_certificate_fingerprints: ["sha256:AAA"],
  assurance_policy: null,
  allow_email_nameid: false,
  force_sign_requests: false,
  metadata_source: "file",
  metadata_url: null,
  metadata_signed: false,
  metadata_signing_fingerprint: null,
  metadata_valid_until: null,
  warnings: [],
  enabled: true,
  row_version: 1,
  created_at: "2026-09-01T00:00:00Z",
  updated_at: "2026-09-01T00:00:00Z",
} as const;

describe("SamlProvidersPanel metadata ceremony", () => {
  it("previews the trust diff, then applies with the confirmed fingerprints and clears the document", async () => {
    const bodies: string[] = [];
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const { method, path, request } = pathOf(args[0]);
      if (method === "GET" && path === "/api/v1/instance/saml-providers") {
        return Promise.resolve(json({ providers: [] }));
      }
      if (method === "PUT" && path === "/api/v1/instance/saml-providers/acme") {
        return request.text().then((text) => {
          bodies.push(text);
          const parsed: unknown = JSON.parse(text);
          const confirmed =
            typeof parsed === "object" &&
            parsed !== null &&
            "confirmed_fingerprints" in parsed;
          if (!confirmed) {
            return json({
              applied: false,
              provider: null,
              diff: {
                endpoints_added: ["https://idp.example/acme/sso"],
                endpoints_removed: [],
                certs_added_fps: ["sha256:AAA"],
                certs_removed_fps: [],
                metadata_certs_added_fps: [],
                metadata_certs_removed_fps: [],
              },
              required_fingerprints: ["sha256:AAA"],
              required_endpoints: ["https://idp.example/acme/sso"],
            });
          }
          return json({
            applied: true,
            provider,
            diff: {
              endpoints_added: [],
              endpoints_removed: [],
              certs_added_fps: [],
              certs_removed_fps: [],
              metadata_certs_added_fps: [],
              metadata_certs_removed_fps: [],
            },
            required_fingerprints: [],
            required_endpoints: [],
          });
        });
      }
      throw new Error(`unexpected ${method} ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    // The CSRF token the client echoes on mutations.
    document.cookie = "__Host-hikyo-csrf=token";

    const { container, client, unmount } = await renderForm(
      <SamlProvidersPanel />,
    );
    await settleTask();

    await act(async () =>
      button(container, "+ configure SAML provider").click(),
    );
    const inputs =
      container.querySelectorAll<HTMLInputElement>(".saml-editor input");
    const slug = inputs[0];
    const name = inputs[1];
    const entity = inputs[2];
    const textarea = container.querySelector<HTMLTextAreaElement>(
      ".saml-editor textarea",
    );
    if (
      slug === undefined ||
      name === undefined ||
      entity === undefined ||
      textarea === null
    ) {
      throw new Error("provider create fields missing");
    }
    await act(async () => setNativeValue(slug, "acme"));
    await act(async () => setNativeValue(name, "Acme"));
    await act(async () => setNativeValue(entity, "https://idp.example/acme"));
    await act(async () => setNativeValue(textarea, "<md:EntityDescriptor/>"));

    await act(async () => button(container, "Preview and configure").click());
    await settleTask();
    // The diff is shown and nothing is applied yet.
    expect(container.textContent).toContain("changes trust state");
    expect(container.textContent).toContain("sha256:AAA");

    // Every policy field invalidates trust confirmation, not only metadata.
    const assurance = container.querySelectorAll<HTMLTextAreaElement>('.saml-editor textarea')[1];
    if (assurance === undefined) throw new Error('assurance field missing');
    const policyEdits = [
      () => setNativeValue(assurance, 'https://idp.example/mfa'),
      ...[...container.querySelectorAll<HTMLInputElement>('.saml-editor input[type="checkbox"]')].map(
        (checkbox) => () => checkbox.click(),
      ),
    ];
    expect(policyEdits).toHaveLength(4);
    for (const edit of policyEdits) {
      await act(async () => edit());
      expect(container.textContent).not.toContain('Confirm trust and configure provider');
      await act(async () => button(container, 'Preview and configure').click());
      await settleTask();
      expect(container.textContent).toContain('Confirm trust and configure provider');
    }

    await act(async () =>
      button(container, "Confirm trust and configure provider").click(),
    );
    await settleTask();

    // The second request carried the confirmed material copied from required_*.
    const secondBody = bodies.at(-1);
    if (secondBody === undefined)
      throw new Error("confirm request was never sent");
    const confirmBody: unknown = JSON.parse(secondBody);
    expect(confirmBody).toMatchObject({
      confirmed_fingerprints: ["sha256:AAA"],
      confirmed_endpoints: ["https://idp.example/acme/sso"],
      assurance_policy: ['https://idp.example/mfa'],
      allow_email_nameid: true,
      force_sign_requests: true,
      enabled: false,
    });
    expect(container.textContent).toContain("Configured SAML provider acme");
    // The write-only metadata never survives a successful apply in the DOM…
    expect(container.querySelector(".saml-editor")).toBeNull();
    // …nor in React Query's mutation cache: the ceremony resets the mutation so
    // the document is not recoverable from the client afterwards.
    const leaked = client
      .getMutationCache()
      .getAll()
      .some((mutation) =>
        JSON.stringify(mutation.state.variables ?? {}).includes(
          "<md:EntityDescriptor/>",
        ),
      );
    expect(leaked).toBe(false);
    await unmount();
  });

  it("disables editing in flight and refuses stale previews if the draft changes", async () => {
    const bodies: string[] = [];
    let releasePreview: (() => void) | undefined;
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const { method, path, request } = pathOf(args[0]);
      if (method === "GET" && path === "/api/v1/instance/saml-providers") {
        return Promise.resolve(json({ providers: [] }));
      }
      if (method === "PUT" && path === "/api/v1/instance/saml-providers/acme") {
        return request.text().then((text) => {
          bodies.push(text);
          const parsedBody: unknown = JSON.parse(text);
          const confirmed =
            typeof parsedBody === "object" &&
            parsedBody !== null &&
            "confirmed_fingerprints" in parsedBody;
          if (confirmed) {
            return json({
              applied: true,
              provider,
              diff: {
                endpoints_added: [],
                endpoints_removed: [],
                certs_added_fps: [],
                certs_removed_fps: [],
                metadata_certs_added_fps: [],
                metadata_certs_removed_fps: [],
              },
              required_fingerprints: [],
              required_endpoints: [],
            });
          }
          // Hold the preview response so the form can be edited before it lands.
          return new Promise<Response>((resolve) => {
            releasePreview = () =>
              resolve(
                json({
                  applied: false,
                  provider: null,
                  diff: {
                    endpoints_added: [],
                    endpoints_removed: [],
                    certs_added_fps: ["sha256:AAA"],
                    certs_removed_fps: [],
                    metadata_certs_added_fps: [],
                    metadata_certs_removed_fps: [],
                  },
                  required_fingerprints: ["sha256:AAA"],
                  required_endpoints: [],
                }),
              );
          });
        });
      }
      throw new Error(`unexpected ${method} ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    document.cookie = "__Host-hikyo-csrf=token";

    const { container, unmount } = await renderForm(<SamlProvidersPanel />);
    await settleTask();
    await act(async () =>
      button(container, "+ configure SAML provider").click(),
    );
    const inputs =
      container.querySelectorAll<HTMLInputElement>(".saml-editor input");
    const slug = inputs[0];
    const name = inputs[1];
    const entity = inputs[2];
    const textarea = container.querySelector<HTMLTextAreaElement>(
      ".saml-editor textarea",
    );
    if (
      slug === undefined ||
      name === undefined ||
      entity === undefined ||
      textarea === null
    ) {
      throw new Error("provider create fields missing");
    }
    await act(async () => setNativeValue(slug, "acme"));
    await act(async () => setNativeValue(name, "Acme"));
    await act(async () => setNativeValue(entity, "https://idp.example/acme"));
    await act(async () => setNativeValue(textarea, "DOCUMENT_A"));
    await act(async () => button(container, "Preview and configure").click());

    // Cancel is disabled while the request is in flight, so a still-pending
    // mutation can never be reset (and left to settle) with the document cached.
    expect(button(container, "Cancel").disabled).toBe(true);

    // Edit to a different document while the preview response is still pending.
    const liveTextarea = container.querySelector<HTMLTextAreaElement>(
      ".saml-editor textarea",
    );
    if (liveTextarea === null) throw new Error("textarea vanished mid-preview");
    expect(liveTextarea.disabled).toBe(true);
    expect([...container.querySelectorAll<HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement>(
      '.saml-editor input, .saml-editor select, .saml-editor textarea',
    )].every((field) => field.disabled)).toBe(true);
    // A synthetic/programmatic edit must also invalidate the older response.
    await act(async () => setNativeValue(liveTextarea, "DOCUMENT_B"));

    // Now let the preview land; the pending diff is restored despite the edit.
    await act(async () => {
      releasePreview?.();
      await Promise.resolve();
    });
    await settleTask();
    expect(container.textContent).not.toContain('Confirm trust and configure provider');
    expect(button(container, 'Preview and configure').disabled).toBe(false);
    expect(bodies).toHaveLength(1);
    await unmount();
  });
});

describe("SamlSpKeysPanel retirement gating", () => {
  const activeKey = {
    fingerprint: "sha256:ACTIVE",
    state: "active",
    created_at: "2026-09-01T00:00:00Z",
  } as const;
  const retiringKey = {
    fingerprint: "sha256:RETIRING",
    state: "retiring",
    created_at: "2026-08-01T00:00:00Z",
  } as const;

  it("offers compromise-retire only for the active key and ordinary retire only for the retiring key", async () => {
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const { method, path } = pathOf(args[0]);
      if (method === "GET" && path === "/api/v1/instance/saml-sp-keys") {
        return Promise.resolve(json({ keys: [activeKey, retiringKey] }));
      }
      throw new Error(`unexpected ${method} ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);

    const { container, unmount } = await renderForm(<SamlSpKeysPanel />);
    await settleTask();

    const activeRow = container.querySelector<HTMLElement>(
      '[data-sp-key="sha256:ACTIVE"]',
    );
    const retiringRow = container.querySelector<HTMLElement>(
      '[data-sp-key="sha256:RETIRING"]',
    );
    if (activeRow === null || retiringRow === null)
      throw new Error("key rows missing");

    expect(button(activeRow, "Compromise-retire")).toBeTruthy();
    expect(
      [...activeRow.querySelectorAll("button")].some(
        (b) => b.textContent === "Retire",
      ),
    ).toBe(false);
    expect(button(retiringRow, "Retire")).toBeTruthy();
    expect(
      [...retiringRow.querySelectorAll("button")].some(
        (b) => b.textContent === "Compromise-retire",
      ),
    ).toBe(false);

    await unmount();
  });

  it("surfaces the active-key conflict when the server refuses an ordinary retire", async () => {
    const fetchMock = vi.fn((...args: Parameters<typeof fetch>) => {
      const { method, path } = pathOf(args[0]);
      if (method === "GET" && path === "/api/v1/instance/saml-sp-keys") {
        return Promise.resolve(json({ keys: [retiringKey] }));
      }
      if (
        method === "DELETE" &&
        path === "/api/v1/instance/saml-sp-keys/sha256:RETIRING"
      ) {
        return Promise.resolve(json({ code: "conflict" }, 409));
      }
      throw new Error(`unexpected ${method} ${path}`);
    });
    vi.stubGlobal("fetch", fetchMock);
    document.cookie = "__Host-hikyo-csrf=token";

    const { container, unmount } = await renderForm(<SamlSpKeysPanel />);
    await settleTask();

    await act(async () => button(container, "Retire").click());
    const confirmInput =
      container.querySelector<HTMLInputElement>(".danger-zone input");
    if (confirmInput === null) throw new Error("retire confirm input missing");
    await act(async () => setNativeValue(confirmInput, "sha256:RETIRING"));
    await act(async () => button(container, "Retire key").click());
    await settleTask();

    expect(container.textContent).toContain("active signing key");
    await unmount();
  });
});

describe("SAML forward-compatible diagnostics", () => {
  it("shows an unknown warning code using the server message and error severity", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn((...args: Parameters<typeof fetch>) => {
        const { method, path } = pathOf(args[0]);
        if (method !== "GET" || path !== "/api/v1/instance/saml-providers") {
          throw new Error(`unexpected ${method} ${path}`);
        }
        return Promise.resolve(
          json({
            providers: [
              {
                ...provider,
                warnings: [
                  {
                    code: "future-warning",
                    severity: "error",
                    message: "Server diagnostic",
                    effective_at: "2026-09-01T00:00:00Z",
                  },
                ],
              },
            ],
          }),
        );
      }),
    );
    const { container, unmount } = await renderForm(<SamlProvidersPanel />);
    try {
      await settleTask();
      expect(
        container.querySelector('[data-saml-provider="acme"] [role="alert"]')
          ?.textContent,
      ).toBe("error Server diagnostic");
    } finally {
      await unmount();
    }
  });
});
