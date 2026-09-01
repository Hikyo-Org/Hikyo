# #496 — Import K8s / SOPS / Infisical / Vault / OpenBao through the WebUI

Parent: #41. Blocked-by: #495 (merged, PR #540). ADR: `docs/adr/import-paths.md`.
Builds on the browser `.env` import wizard (#495) and reuses the same two
server operations (`import.presence`, `value.import`) — **no codegen, no API
change, no server change.**

## Contract decision (approved by the owner)

Acceptance criterion 1 asks that *every* shipped connector complete "through the
browser." The locked import-paths ADR blocks a faithful SOPS or live-mode
browser path in **both** directions:

- **Server-side importers are rejected** ("import stays client-side").
- **A browser credential prompt is the ADR's explicitly-rejected "interactive
  prompt per run"** — live connectors authenticate with "the source's own
  ambient conventions and nothing else," and SOPS uses "the ambient decryption
  keyring exactly as `sops -d` resolves it." A pasted Vault token, an uploaded
  kubeconfig, or an age key in a web form is exactly what the ADR refuses.

So the delivered browser scope is the **file-mode connectors whose parse is pure
and needs no decryption or live network**: Kubernetes Secret manifests, the
pinned Infisical export, and the Vault/OpenBao JSON Lines capture. **SOPS and all
live modes are selectable in the picker but routed to the CLI** with the exact
command and the capture recipe. Widening the browser to those would require an
ADR amendment (governance procedure) — deliberately out of scope. This boundary
was chosen with the owner before any parser was written.

The ADR itself blesses the capture-file fallback: "file mode remains available
everywhere live mode exists — for air-gapped migration and users who prefer the
artifact." A Vault/OpenBao JSONL capture and a K8s manifest export are exactly
that, and Infisical is file-only in v1 regardless.

## What ships

- `web/src/routes/import-sources.ts` — pure, React-free connector layer mirroring
  `internal/importer/{k8s,infisical,vault,names,importer}.go`:
  - `parseSource(connector, text, {envSlug})` → normalized `{key, sourceName,
    value, folderPath}[]` plus `renames` and `skipped`, or one content-free
    refusal. The order of checks mirrors the CLI (file size → parse → per-value
    bounds → name mapping/collisions).
  - K8s: multi-doc YAML/JSON via the `yaml` dep (new dependency, pinned
    `2.9.0`), `data` base64-decoded then `stringData` overlaid (stringData wins),
    one Secret → one folder named after it, single Secret → environment root.
  - Infisical: pinned JSON array; flat object routed to the `.env` path; missing
    `secretPath`/`type` refused; `personal` skipped and listed.
  - Vault/OpenBao: pinned JSONL; common-prefix stripped to folders; deleted/
    destroyed skipped; non-string leaves → `json` via a canonical serialization.
  - **A lossless JSON parser** (numbers kept as their source literal) so a Vault
    non-string leaf's canonical JSON matches the CLI's exact-number fixtures —
    native `JSON.parse` would corrupt `9007199254740993` and long decimals.
  - The `TransformName` rename transform, the `quoteName` safe foreign-name
    renderer (`safeName`, control/DEL/non-ASCII escaped, 128-byte cap), the
    post-transform collision hard-stop, and the uniform bounds block (10 MiB
    file, 50 000 records, 64 KiB value, depth 32).
- `web/src/routes/ImportWizard.tsx` — generalized from the `.env`-only wizard to
  a **source picker** (`pick` step) plus the shared classify → review → apply
  flow, which now threads each connector's `folderPath` into `createKey`
  (already supported by the op), surfaces renames and source-skips, and shows a
  CLI-guidance panel for SOPS and live modes. `.env` remains one journey.
- The matrix trigger button is now **"Import"** (was "Import .env").

## Parity is tested, not asserted

`web/src/routes/import-sources.test.ts` uses the **exact byte content** the Go
connectors pin in `internal/importer/testdata/*` (k8s single/multi/duplicate/
collision/binary/wrong-kind/hostile/unmappable/trim, infisical export/flat/
no-path/no-type/hostile-type, vault capture/numbers/mixed/duplicate). Each
assertion mirrors what the Go connector maps or refuses, so a divergence shows up
here (acceptance criterion 6). E2e: a Kubernetes journey rides `flows/matrix.spec.ts`
beside the `.env` journey (no new registry surface — see
[[hikyo-new-e2e-surface-ci-grouping]]).

## What is deliberately NOT here

- SOPS decryption in the browser, and live k8s/Vault/OpenBao reads (ADR, above).
- The mapping template / run-manifest artifacts. The browser preview funnels
  through `value.import` per environment exactly as #495 did; it does not author
  the CLI's committable artifacts. The occurrence-token precondition is still
  built and checked per environment.
- Cross-environment reconciliation of conflicting per-env sources — a wizard/CLI
  concern; one source read fans onto the selected environments.

## Verification

- `pnpm --dir web typecheck` — clean.
- `pnpm --dir web test` — connector parity + wizard + import-state suites pass.
- `pnpm --dir web build` — clean.
- Targeted Playwright: the `.env` and Kubernetes import journeys in
  `flows/matrix.spec.ts`.
