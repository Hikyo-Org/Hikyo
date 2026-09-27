# #161 Cloudflare Workers and Pages synchronization adapter

One-way synchronization from Hikyo into encrypted Cloudflare Workers secrets and
Cloudflare Pages encrypted environment variables, as a third compiled-in module
over the #65/#157 deployment seam.

## What landed

- Provider `cloudflare` and destination kinds `workers-script` and
  `pages-project` (`internal/adapter/provider.go`, `internal/adapter/adapter.go`).
  `ValidateCloudflareManifest` applies the binding-name rule
  (`^[A-Za-z_][A-Za-z0-9_]*$`, at most 64 bytes, case-insensitively unique
  because the ledger normalizes to upper case), the 5 KiB value limit and UTF-8.
- `internal/adapter/cloudflare`: hand-rolled `net/http` client with the closed
  `API` interface and `operationRegistry`, public-egress dialer, no proxy, no
  redirects, TLS 1.2+, 4 MiB response cap, 15 s deadline under
  `adapter.LeaseTime`. `Module` implements the four seam operations.
- Registration: app provider registry, store `Create` provider check,
  `validateTargetMutation`/`validatePendingTarget`, a provider/kind pairing
  guard in `targetManifest`, migration `00062_cloudflare_adapter.sql` on both
  engines widening every provider and destination-kind `CHECK`, the OpenAPI
  `AdapterProvider` and `AdapterDestinationKind` enums (apigen and
  `clients/ts` regenerated), CLI flags and help, the web create/add-target form.
- `internal/buildcompat/development.json` regenerated on PostgreSQL 18; the
  diff is exactly the new version-62 entry and `schema_sha256` per engine.
- `internal/app/backup_upgrade_drill_test.go`: the legacy upgrade drill fixture
  reverses 00062 (PostgreSQL restores the 00025 CHECKs by name; SQLite
  recreates each rebuilt table from its stored declaration minus the added
  kinds, and the catalog inspection proves the legacy text byte-for-byte).
- `web/src/api/sensitiveInventory.json` re-pinned for `api/adapters.ts` and
  `routes/Adapters.tsx`. Reviewed: the credential still flows only through
  `useSensitiveState` into the existing create mutation.
- ADR gate: `mvp-boundary.md` declared amendment 6 plus the section 4.1 row, and
  a banner on `deployment-adapter.md` naming the destination class and its
  no-read closure.

## Provider facts and the must-verify decision

- Workers: `PUT /accounts/{a}/workers/scripts/{s}/secrets` with
  `{name, text, type: "secret_text"}`; `DELETE .../secrets/{name}`. Presence
  is read from `GET .../settings`, whose `bindings` array covers every binding
  type: it decodes binding names only (plain_text values are never decoded,
  the buffer is cleared), so a secret upsert cannot land on an unowned
  plain_text binding. `GET .../secrets` is not linked: it lists secrets only. Every write or delete deploys a new script
  version. Script identity is the `tag` from `GET /accounts/{a}/workers/scripts`.
- Pages: `PATCH /accounts/{a}/pages/projects/{p}` merges `env_vars` per key
  (the pattern `wrangler pages secret put/delete` relies on: a single-key body
  sets that key; a key set to `null` is deleted; other keys are untouched).
  Hikyo therefore sends one key per request and never needs a read-modify-write.
  `GET /accounts/{a}/pages/projects/{p}` returns `plain_text` values and omits
  `secret_text` values. It is the only read that can carry a value: it decodes
  into `ProjectShape` (project id plus names and types of the selected
  environment), which structurally cannot hold a value, and the transport
  buffer is cleared. Names of every type count as present, so an unowned
  `plain_text` variable is a conflict, never overwritten.
- Tokens: `GET /accounts/{a}/tokens/verify`, falling back to
  `GET /user/tokens/verify` for user-owned tokens (status and expiry only).
  `GET /accounts` must list exactly the configured account. Global API Keys
  (37 lowercase hex) and `email:key` pairs are refused by shape. There is no
  `--allow-broad-token` override: multi-account tokens are always refused.
- Rate limits: 429 becomes `adapter.RetryAtError` from `Retry-After`, or five
  minutes (Cloudflare's 1200 requests per five minutes window).

## Design decisions

1. **One surface.** Cloudflare has no plaintext surface Hikyo will use, so every
   row, including config-classified keys and the sentinel, is owned on the
   secret surface. Only the secret sentinel is desired. Route-move claims still
   derive surface from classification; they only reserve names, and both sides
   of each collision query use the same derivation.
2. **Identity.** `destination_id` is a positive 63-bit SHA-256 fingerprint of
   kind, account, immutable provider id (script tag or Pages project id) and
   Pages environment. Preview and production of one project are distinct
   ledger destinations. A recreated or renamed destination fails loud with
   `ErrDestinationID` or a named not-found.
3. **No batching.** The outbox holds one provider-write lease per effect, so a
   Pages multi-key `PATCH` cannot be journaled per name. Per-name writes keep
   every INTENT/OUTCOME exact; batching would trade replay safety for request
   count.
4. **Capture window.** Both writes are upserts. Before each first write of a
   name the module re-reads destination names and refuses an unowned one. A name
   created externally in the window between that read and the write is the
   residual risk; Cloudflare's response does not distinguish create from update.
5. **Activation writes nothing.** `TestConnection` verifies token, account scope,
   destination identity and name listing. The first sync writes the sentinel
   before any key, so a permission gap fails before any value is written.
6. **Ambiguity.** A 2xx whose envelope lacks `success: true` is
   `ErrAmbiguousResponse`: OUTCOME `unknown`, ledger `dispatched`, replayed as an
   update. Provider bodies are never surfaced.

## Validation

```sh
go test ./internal/adapter/... ./internal/cli/ -count=1
HIKYO_TEST_POSTGRES_DSN=postgres://.../postgres?sslmode=disable \
  go test ./internal/store/ ./internal/buildcompat/ -count=1
go test ./internal/service/ ./internal/app/ -run 'Adapter|Provider' -count=1
(cd web && node --run typecheck && node --run lint && node --run test)
```

Real provider (destructive, opt-in; skips locally, fails when
`HIKYO_TEST_CLOUDFLARE_REQUIRED=1`):

```sh
HIKYO_TEST_CLOUDFLARE_ACCOUNT=... HIKYO_TEST_CLOUDFLARE_TOKEN=... \
HIKYO_TEST_CLOUDFLARE_SCRIPT=... HIKYO_TEST_CLOUDFLARE_PAGES_PROJECT=... \
  go test ./internal/adapter/cloudflare -run TestCloudflareRealLifecycle -count=1 -v
```

It covers both kinds: sentinel and keys land, `secret_text` on every Pages
write, preview/production isolation, replay, conflict refusal on an unowned
name, and teardown. It has not been run against a live account in this change;
no sandbox credentials were available.

## Out of scope

Wrangler or `wrangler.toml` generation, KV/D1/R2 bindings, zone settings,
reading Cloudflare values, creating scripts or projects.


## PR #822 integration review, 2026-09-27

Merged main after sealed-webhook and SSH certificates. The Cloudflare migration
is now 00062 on both engines and retains the sealed-webhook provider CHECK.
A both-engine regression creates a sealed-webhook target after the Cloudflare
migration. The CLI, provider registry, API and WebUI retain both provider sets.
Generated Go/TypeScript clients and the empirically generated development
compatibility declaration were refreshed. Web credential state and uncached
mutation handling were reviewed before updating sensitivity pins.

Local evidence: Cloudflare/adapter/CLI tests, SQLite and PostgreSQL Cloudflare
store tests, Web typecheck/lint and all 1170 unit tests (1169 in the broad run,
then the refreshed sensitivity-inventory regression). Broader Go checks and
remote CI must be verified on the final pushed head before merge.
Native cross-provider review was skipped because session quota remained
unknown after the policy's three-minute response window; ordinary adversarial
inspection found and fixed the provider-preservation migration defect above.
