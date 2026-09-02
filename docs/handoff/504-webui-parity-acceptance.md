# #504 WebUI parity acceptance: handoff

Closes the full server-capability parity programme (#490 registry, #491,
#183, #492, #493, #494, #495, #496, #464, #497, #498, #499, #500, #501, #502,
#157, #503) with the rows the registry had attributed to this ticket, the two
small follow-ups it had filed (#571, #572), executable browser acceptance of
the whole lifecycle, and the documentation that describes the shipped reality.

## What shipped

| Piece | Where |
| --- | --- |
| Adapter lifecycle in the browser (11 `issue: 504` rows) | `web/src/routes/Adapters.tsx`, `web/src/api/adapters.ts`: origin move with a `?move=` follow-up pane (poll, resume with a new credential, cancel), credential replace and revoke, adapter delete behind the retain-or-prune decision, per-target Plan and Test connection; `showAdapter` and `listAdapterTargets` are `via: [listAdapters]` (the list embeds every adapter's targets) |
| Recovery-code sign-in (#571) | `web/src/routes/EstablishCredential.tsx` (`?mode=recover`), `web/src/api/session.ts`, login lede link; authority held in component state only, one refusal sentence |
| Project and environment audit (#572) | registry surface `project-audit` at `/orgs/:org/projects/:project/audit`; `web/src/routes/Audit.tsx` serves both scopes, environment is a filter; `web/src/api/audit.ts` picks the scoped operation; one export literal per route so the registry's path evidence sees all three |
| Browser-only lifecycle acceptance | `web/e2e/flows/machine-access.spec.ts` › `browser-only lifecycle`: organisation, project, environments, config + secret keys with first values, publish, invitation, service account + display-once credential + read grant, machine-reveal opt-in + reveal grant, the delivery wire fetched with the browser-minted bearer (presence-only before reveal, plaintext after), CI adapter target converging to Healthy, audit, credential rotation (old value refused at the next fetch), history rollback |
| Adapter lifecycle acceptance | same file › `deployment adapters` › `probes, plans, replaces the credential, moves the origin both ways, revokes, and deletes` (the `--dev` fake provider refuses the literal credential `revoked`, which makes `attention_required` deterministic) |
| Project audit acceptance | same file › `project audit`; flow `project-audit` in `web/e2e/registry.ts` rides this file (group 3 on main) |
| Recovery acceptance | `web/e2e/flows/login.spec.ts` › `recovers an account with a code…` plus the recovery entry's pinned set; the invitee is prepared over the API (no CLI, no factor, password as the regeneration proof) |
| Contract fixes found by the browser | `AdapterChange.disposition` now admits `unknown-until-sync` (the Go value the plan already emitted); `AdapterPlan.warnings` is never `null` (`internal/server/adapters.go`); `listAdapters`/`showAdapter` read `state<>'tombstoned'` instead of `state='active'`, so an adapter mid-move stays visible with `state: moving` as the contract's enum promises (`internal/store/repos_adapters_config.go`; mutation reads keep the active-only fence). Regenerated `api/apigen`, `clients/ts/src/generated` |
| Registry | `api/parity.yaml`: no `issue: 504/571/572` rows remain; `issue: 595` (dynamic-secret management, #147 follow-up routed to its own session) is the one open row |
| Ledger and docs | `docs/status/ledger.json` (+ generated README / `docs/status/README.md`): `CAP-BROWSER-SCANNING` implemented (#183 shipped), capability rows name the browser surfaces, new `CAP-WEBUI-PARITY` (partial: #595). Docs site: new `browser-operations.mdx` (lifecycle, low-frequency administration table, closed exemption classes), cross-links from first-project, kubernetes-operator, machine-identities, deployment-adapters, dynamic-secrets, account-security. `docs/spec/README.md` parity row names the acceptance |

## Exemptions, enumerated

Only the four closed classes the registry names (`api/parity.yaml` header,
api-cli-surface ADR § The parity principle): `identity-protocol`,
`client-local-delivery`, `host-local-authority` (no HTTP member by
construction), `k8s-controller-reconciliation` (no HTTP member by
construction). The lifecycle spec starts after `hikyo admin create` for that
reason and proves the Kubernetes prerequisites by fetching the delivery wire
with the browser-minted credential rather than by running the operator.

## Gaps closed on the way

- **Widening grants from Members.** `New grant` ran no reauthentication, so a
  `reveal` (or first `read`) grant to a workload principal was refused `409`
  in the browser: Kubernetes step 5 was CLI-only. The dialog now answers the
  server's reauth-required refusal with the mint-purpose passkey ceremony over
  the environment the refusal names and retries the capabilities that have not
  landed (`web/src/routes/Members.tsx`, `wideningEnvironment`). Completed lines
  stay live, as before.

## Findings for human disposition

- **Route identity survives removal.** `adapter_targets` is
  `UNIQUE (adapter_id, destination_kind, destination_owner, destination_name,
  environment_id)` across tombstoned rows, so re-adding the same route to the
  same adapter after removing it is refused 409 forever (CLI and browser
  alike). The e2e uses a different repository for the re-add. Deliberate
  durable identity or a defect: not changed here; a partial unique index is a
  migration on both engines.
- **Plan dispositions.** The contract listed `refused`, which no Go path emits
  as a disposition (it is an error class), and omitted `unknown-until-sync`,
  which the plan emits. Added the latter; left `refused` in place (removing is
  a contract narrowing).
- **`#595`** stays an `issue:` row by routing: labelled `ready-for-agent` for
  its own session (memory: same-branch collisions). AC1 is therefore met up to
  that one tracked row, and `CAP-WEBUI-PARITY` says so.

## Gotchas for the next reader

- The Projects page is unscoped and creates under the rail's active
  organisation; a full page load forgets an in-memory rail choice, so a flow
  that just created an organisation must choose it through the rail (or deep
  link into an org-scoped route and navigate client-side) before creating a
  project.
- Group 3 order matters: `instance-admin.spec.ts` asserts exactly two
  organisations, so any flow that creates one must ride a file that runs after
  it (`machine-access.spec.ts` does).
- A `.btn--danger` on an always-rendered surface fails the pinned axe contrast
  scan; keep the danger styling on the confirmation inside the dialog.
- The adapter ceremony must cover every environment the server counts, and
  the server counts moving targets. `Adapter.targets` may omit a target
  mid-move, so the move pane unions the move's own target environments; an
  empty set runs no ceremony and the server answers `409` (reauth-required is
  mapped to conflict, not 403). `writeHandlerError` now logs every refusal's
  cause at debug level under `--dev` (`msg=refusal operation=… cause=…`),
  which is how this was found.
- The registry's `reach: path` evidence wants the whole route as one template
  literal per operation; a helper that concatenates segments is invisible to
  it.
- `readonly` project-name uniqueness is per organisation; the lifecycle's
  409 was the rail, not the name.

## Gates run

Filled in at the end of the PR (see the PR body for the exact head and CI
links).
