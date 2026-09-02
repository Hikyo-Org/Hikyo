# Handoff: #147 Dynamic secrets — leased PostgreSQL credentials end to end

Issue: https://github.com/Hikyo-Org/Hikyo/issues/147 (blocked-by #146, now merged).
ADR amendments (operative banners in this PR): `mvp-boundary.md` §4.1 (Dynamic DB
credentials In), §4.2 (agent daemon stays Out, confirmed against the trigger),
`system-architecture.md` § Jobs (a second domain-specific outbox).

Dynamic secrets as a self-hostable lifecycle: an authorized workload requests a
short-lived PostgreSQL credential, Hikyo mints it at the provider, discloses it
once, and durably renews/revokes the lease without retaining plaintext. First
provider: PostgreSQL. Provider boundary stays closed and explicit.

## ADR gate

Three locked places bear on this ticket:

- mvp-boundary §4.1 "Dynamic DB credentials — Out. Trigger: repeated user demand
  naming a concrete engine set." The trigger names PostgreSQL. Flipped to **In**
  by the operative banner in this PR (reopen -> cross-model review -> banner, per
  oss-mechanics governance).
- mvp-boundary §4.2 "Local agent daemon / resident watcher — Out. Trigger:
  dynamic secrets only." **Confirmed Out.** Decision for this ticket: **no
  agent.** Renewal is a client call (`hikyo lease renew`, or the workload's own
  loop); `hikyo run --` gets no resident renewer. Stated in the banner.
- system-architecture § Jobs. This adds a **second** domain-specific outbox
  (`dynamic_effects` + per-row lease fences on `dynamic_leases`), not a generic
  job queue. Banner note added.

## Decisions (taken; divergences from the issue handoff comment flagged)

1. **Provider kind is a closed enum** `postgres`. Provider rows are
   project-scoped like adapters: `dynamic_providers(id, org_id, project_id,
   kind, origin, tls_mode='verify-full', grant_role, admin_credential_ciphertext,
   credential_set_at, authority_principal_id, state)`. Credential sealed with
   `ProjectFieldAAD{OwnerTable:"dynamic_providers", FieldTag:"credential"}`,
   `PurposeProject`. Configure formula `manage-identities@project` + human
   session (no env-bound ceremony: configuring standing authority is a project
   act, not a per-environment disclosure). **Divergence from the issue comment,
   which proposed `reveal@env`-class ceremony** — dropped because a provider is
   project-scoped and has no environment; the human-session gate plus
   `manage-identities` is the same shape `sa` config uses. Restore invalidates
   provider credentials (mirror `InvalidateRestoredAdapterCredentials`).

   **grant_role (advisor finding 1):** a bare `CREATE ROLE ... LOGIN PASSWORD
   VALID UNTIL` has no privileges. The provider carries a required parent role;
   lease roles are created `IN ROLE <grant_role>` so they inherit exactly the
   access the operator granted that parent. This also keeps `DROP ROLE` clean.

2. **Lease object** `dynamic_leases(id, org_id, project_id, environment_id,
   provider_id, principal_id, principal_class, provider_handle (role name),
   state, issued_at, expires_at, max_ttl_seconds, last_transition_at,
   lease_owner, lease_expires_at, attempt_count, next_attempt_at)`. **No sealed
   column:** the password is generated, delivered once, never stored (not even a
   hash). Provider handle is public metadata. States: `minting, active,
   renewing, revoking, revoked, expired, unknown, failed`. **`failed` added
   (advisor finding 7)** for a definite mint failure (connect refused before any
   send) — an honest terminal distinct from `unknown` (ambiguous, needs
   reconcile). `principal_class` stored so the worker gate rebuilds the exact
   caller identity (advisor finding 2 — the adapter precedent hardcodes
   ClassHuman, which would bypass `machineRevealWithdrawn` on renew).

3. **Mint op** `OpLeaseMint`: `ClassTenant`, `LevelEnv`, formula
   **`read@environment` only** — machine-holdable, exactly like `delivery.fetch`
   (the contract refuses a machine-credential-eligible op whose formula carries
   an atom no machine class can hold, and `reveal` is such an atom). The
   disclosure decision is applied in the SERVICE, per caller class, mirroring
   delivery: a machine mints only under the project's machine-reveal opt-in
   (`MachineRevealOptIn`; a withdrawn opt-in answers the uniform nonexistent
   response); a human mints only holding `reveal@environment` AND after a fresh
   mint reauthentication ceremony (`RequireDisclosureAuthority`, which checks the
   grant and consumes the mint window). **This replaced the initial
   `read ∧ reveal` formula, which the contract test `machineSatisfiable`
   rejected.** Response discloses the credential ONCE with `lease_id`,
   `expires_at`; a retry is a new lease, never the old secret. Events:
   `dynamic.lease_transition_intent` (kind=mint), `dynamic.lease_transition_outcome`,
   and one `dynamic.lease_disclosed` (there is no per-key event — a PostgreSQL
   lease is a role, not a Hikyo key set, so the disclosure unit is the lease).

4. **Provider adapter** `internal/dynamic/postgres`: closed `API` interface
   `CreateRole/ExtendRole/DropRole/RoleStatus` over pgx `sslmode=verify-full`,
   custom dialer through `netpolicy` + allowed CIDRs, statement timeout < lease.
   A reflection test pins the interface (no SELECT of secrets, no arbitrary
   SQL). Role name `hikyo_<lease-id-prefix>`; `VALID UNTIL expires_at` so
   **natural expiry is enforced by PostgreSQL even if Hikyo is down.**
   `RoleStatus` returns `(exists, validUntil, err)` from `pg_roles` so reconcile
   settles a renew, not only mint/revoke (advisor finding 5). DDL takes no
   params: identifiers via `pgx.Identifier{}.Sanitize()`, password charset
   restricted to alphanumerics and validated before SQL (advisor finding 6).

   **Sync vs async (locked):** mint is **synchronous** in the request (the
   password is display-once in the HTTP response and is never stored, so it
   cannot be worker-delivered). Renew, revoke, expire, and reconcile are
   **worker-driven** and durable (the lease row carries the retry/lease
   columns). Human mint consumes `NewMintReauthIntent(env, nil)` (the existing
   env-bound keyless mint ceremony, satisfied by any unbound #54 window);
   machines skip it (`skipsCeremony`, no session). There is no per-key ceremony
   or per-key disclosure event: a PostgreSQL lease is a role, not a Hikyo key
   set, so the disclosure unit is the lease. One `dynamic.lease_disclosed`
   event per mint.

   **Revoke/expire never re-check grants (divergence from the adapter Gate,
   which re-authorizes every transition):** revoking a compromised workload
   must succeed AFTER its grants are removed, so the worker Gate re-checks
   `read ∧ reveal@env` only for mint and renew (creating/extending access);
   revoke and expire re-assert only the lease row + provider existence. This is
   the fail-safe direction and is the whole point of cascade-on-principal-
   revocation.

5. **Lifecycle transitions are INTENT/OUTCOME effects**: `dynamic_effects`
   mirroring `adapter_effects`; the worker writes INTENT before every provider
   call, then OUTCOME atomically. **Renew re-authorizes** the lease's recorded
   principal (rebuilt from `principal_id` + `principal_class`) for `OpLeaseRenew`
   (`read@environment`) before extending — a principal that lost `read` cannot
   renew. **Revoke and expire re-check no grants** (fail-safe: a compromised
   workload must be revocable AFTER its grants are pulled); they re-assert only
   the lease row's crash fence. A crash or ambiguous outcome leaves
   `state=unknown` + `hikyo lease settle` (re-probes via the latest effect kind,
   settles). Renew = `ExtendRole` (new expiry = now + `max_ttl`); revoke/expire =
   `DropRole` (idempotent: absent role = success), expire additionally drops the
   already-dead role; principal/provider deletion cascades to `revoking`;
   provider deletion refuses while active leases exist unless `--revoke-all`, and
   keeps the sealed admin credential so the worker can still drop those roles.

6. **Worker** is a continuous per-node loop (`app.dynamicWorker`), NOT the #146
   singleton scheduler lease and NOT a new queue. It is the adapter-outbox
   composition, verbatim: each due lease is claimed under the lease ROW's own
   crash fence (`lease_owner` + `lease_expires_at` via `SELECT ... FOR UPDATE
   SKIP LOCKED`, a per-org cap of 4, and an exact-row re-assertion on every
   settling write), so it runs safely on every node under #146 HA — each
   transition has one current owner and a stale worker's writes affect zero
   rows. It claims `minting/renewing/revoking/unknown` rows past their
   `next_attempt_at`, and `active` rows past `expires_at` (also gated by
   `next_attempt_at`, so an unreachable provider backs off instead of spinning),
   dropping the now-dead role and flipping to `expired`. **Divergence from the
   initial advisor suggestion of a fenced `dynamic-lease` scheduler lease:** the
   singleton scheduler lease exists for work that is NOT row-fenced (retention,
   backups); every dynamic transition IS row-fenced, so it needs no singleton
   lease, exactly as the merged adapter outbox needs none. No `SiteScheduler`
   growth — the authority is the recorded lease principal, not a system site.

7. **Surfaces**: API `.../projects/{project}/dynamic-providers...`,
   `.../environments/{env}/leases` (POST mint, GET list/status),
   `/leases/{lease}/renew|revoke`, `/leases/{lease}/reconcile`. CLI
   `dynamic-provider create|list|show|credential set|revoke|delete`,
   `lease mint|renew|revoke|list|show|reconcile` (print triad for the secret).
   SPA: status/metadata only on the machine-access "Leases" tab, never the
   secret. Metrics label-free: `hikyo_dynamic_leases_active`,
   `hikyo_dynamic_effects_unknown`. `hikyo doctor`: unknown effects > 0 = warning.

## Egress (advisor finding 3)

`HIKYO_ADAPTER_EGRESS_POLICY_FILE`'s loader requires `https://host` keys. A
self-hoster's Postgres is a private IP. Dynamic providers get their own policy
`HIKYO_DYNAMIC_EGRESS_POLICY_FILE` keyed `postgres://host:port` (or the origin
as stored), so a private-IP target is reachable by explicit operator allow-list
while the default-deny public rule still governs anything unlisted.

## Tests / gates

Go both-engine (sqlite + `HIKYO_TEST_POSTGRES_DSN`) for Hikyo's own state; a
**second** DSN `HIKYO_TEST_DYNAMIC_PG_DSN` for the target Postgres the provider
mints into (CI fails loud when unset). Roles are cluster-level: tests
`t.Cleanup` drop their roles. Reflection test on the provider `API`. Fuzz the
role-name/password generator charset. Lifecycle end-to-end evidence is the Go
isolation suite on both Hikyo engines (the web e2e job has no Postgres target,
so browser e2e covers status/empty-state/refusal copy only).

## Progress

- [x] P1 ADR banners + this doc
- [x] P2 migration 00039 (both engines) + reencrypt coverage + hikyo:table dirs
- [x] P3 openapi + codegen + TS + pinnedContractSurface (contract freeze)
- [x] P4 provider (internal/dynamic/postgres) + reflection/fuzz tests
- [x] P5 store repo + runtime + authz ops/store-ops + audit events + budget class
- [x] P6 service (providers, leases, worker) + fence + restore invalidation
- [x] P7 server handlers + app wiring (scheduler lease, egress) + metrics + doctor
- [x] P8 CLI verbs + spellings + goldens
- [x] P9 SPA Leases tab + e2e (rides machine-access.spec.ts)
- [x] P10 isolation fixtures (formulas, audit lifecycle, annotated queries) + docs page
- [x] P11 full dual-engine test, vet all build tags, GOOS=linux build
- [x] P12 Codex R1-R3 review (R3 CLEAN: A2 claim-token fence, A4 FinishMint
      lease_owner IS NULL, B2 in-tx disclosure recheck all VERIFIED), rebase
      onto main (migration renumber 00037 -> 00039, squash to one commit,
      sqlc/apigen/TS regenerate), parity dispositions

## WebUI parity dispositions (#490 registry)

The dynamic surface predates the parity registry. `#147` scoped the browser to
lease *status* (the read-only Leases tab on `machine-access`), not management,
mirroring how adapters (#157) ship CLI/API first with a tracked WebUI follow-up.

- `listLeases` -> `{webui: machine-access}` (the Leases tab imports `listLeasesOp`)
- `showLease` -> `{webui: machine-access, via: [listLeases]}` (the list shows the row)
- provider CRUD (`listDynamicProviders`, `createDynamicProvider`,
  `showDynamicProvider`, `deleteDynamicProvider`, `setDynamicProviderCredential`,
  `revokeDynamicProviderCredential`) and lease write ops (`mintLease`,
  `renewLease`, `revokeLease`, `settleLease`) -> `{issue: 595}` tracking the
  management WebUI (human mint keeps the reauth ceremony + display-once).

**Resolved by #595** (`docs/handoff/595-dynamic-secrets-webui.md`): all ten rows
above flipped to `{webui: machine-access}` — `showDynamicProvider` as
`{webui: machine-access, via: [listDynamicProviders]}`. The `machine-access` page
gained a **Providers** tab (provider CRUD + write-only credential set/revoke) and
lease write actions on the **Leases** tab (display-once mint with the `mint`
reauth ceremony, plus queued renew/revoke/settle). WebUI-only: no server change.

## CI wiring for the provider integration test (needs a workflow-scoped push)

`scripts/ci/start-dynamic-pg.sh` is committed but the `ci.yml` step that invokes
it is NOT (the automation token here lacks the `workflow` OAuth scope). To make
the provider integration test run and fail loud in CI, add to the core test job
(`Test all packages except the sharded isolation suite`), immediately before it:

```yaml
      - name: Start dynamic-secret TLS PostgreSQL target
        run: ./scripts/ci/start-dynamic-pg.sh
```

The script generates a verify-full cert (SAN IP:127.0.0.1), runs a second
PostgreSQL on 127.0.0.1:55432 with TLS, creates the `app_reader` grant role, and
exports `HIKYO_TEST_DYNAMIC_PG_{DSN,PASSWORD,CA,GRANT_ROLE}` + the
`HIKYO_DYNAMIC_PG_REQUIRED=1` fail-loud flag into `$GITHUB_ENV`. Until that step
lands, the integration test SKIPS in CI (it still runs locally and was verified
green against a real TLS target); it never vacuously passes because the flag,
when set, turns an unset DSN into a hard failure. The unit tests
(`postgres_internal_test.go`) and the isolation lifecycle cover the logic
engine-independently in the meantime.
