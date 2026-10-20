# Configurable project key limit

## Task and delivery scope

The requested change makes the original 1,000-key project limit configurable
through Hikyo's existing settings mechanism and documents its rationale. The
approved extension adds per-project inheritance and overrides under a separate
operator-controlled ceiling.
The implementation was approved for a signed, DCO-signed commit, push and PR.
Review and required checks govern subsequent delivery; release and deployment
remain separately scoped.

## Operator workflow

1. Open **Instance settings > Manage Hikyo configuration > Edit configuration project**.
2. Edit `HIKYO_DEFAULT_KEYS_PER_PROJECT` and `HIKYO_MAX_KEYS_PER_PROJECT` in
   the configuration project's Matrix.
3. Publish the draft, return to Hikyo configuration and Apply that exact
   published revision with fresh administrator authentication.
4. Check the acknowledgement from every admitted node.

The inherited default is 1,000 and the operator maximum is 10,000. Both are
integers from 1 to 10,000; the default must not exceed the maximum. They are
shared owner policy for the logical instance. Before adoption, the environment
variables can seed the initial settings. After adoption, the applied managed
revision owns them; stale environment values cannot override them. Removing
a managed value restores its catalogue default after Publish and Apply,
provided the resulting pair remains valid.

In **Project settings > Policy > Key capacity**, users with `project-settings`
authority can choose **Inherit instance default** or a custom limit within the
active maximum. One project can use 5,000 while other projects inherit 1,000.
The panel shows current usage and the effective limit. Actual policy changes
are audited, and resetting inheritance clears the saved override.

The effective limit is the smaller of the saved override (or instance default)
and the instance maximum. Lowering the maximum preserves any larger saved
override while capping its effective limit. Raising the maximum can restore the
saved override up to the new maximum. Stale project revisions or instance bounds
cause a conflict; clients must refresh and review policy before retrying.

CLI parity uses `hikyo project capacity get`, `set --max-keys N` and
`set --inherit`, with ordinary instance/org/project targeting and table or JSON
output. Set reads the current policy, then sends its revision and instance
bounds with the write. It never automatically retries a conflicting update.

The default was a conservative small-installation choice for Pi-class hardware
and Matrix usability, rather than a measured failure threshold. The upper
bound aligns with the existing definitions-entry and per-environment validation
budgets. It is not a benchmark claim for larger projects.

The existing 100,000-cell project budget remains: declared keys times
environments. For example, 2,500 keys fit in 40 environments and 10,000 keys fit
in 10. Creating a key or environment that exceeds this product is refused.
Value sizes, publish deadlines, render sizes, storage and operation-specific
batch limits retain their own bounds. A definitions bundle is separately
limited to 10,000 total keys, environments and groups, and 1 MiB.

Lowering either policy preserves existing keys and delivery. Existing key edits,
deletions and non-growing definitions remain possible. Additional growth above
the active limit is refused. Applying a previously created definitions plan
rechecks the active setting.

## Implementation owners

- `internal/config`: range validation, bootstrap inventory, managed export and
  effective owner configuration. Configuration remains a parsing-only leaf.
- `internal/runtimeconfig`: integer declarations, operator descriptions and
  additive catalogue migrations preserving existing policy.
- `internal/app/generation.go`: captures the instance policy in the replaceable service
  graph used by HTTP and MCP. No mutable process-global cap is introduced.
- `internal/service`: key growth and final definitions validation use the
  effective project policy. Key and environment creation check the resolved-cell
  budget under their existing project write locks. Policy reads and writes use
  project-settings authority and compare policy revisions and instance bounds.
- `internal/store`: nullable project override, policy revision, and durable
  SQLite/PostgreSQL migrations and compare-and-swap writes.
- `internal/server`, `api/openapi.yaml` and generated clients: project capacity
  GET/PUT contract, explicit null to reset inheritance, conflict reporting.
- `web/src/routes/ProjectCapacityPolicy.tsx`: inherited/custom policy editor,
  effective bounds, usage and conflict refresh within Project settings.
- `internal/cli/project_capacity.go`: authenticated project policy get/set,
  inheritance reset, table/JSON output and optimistic concurrency.
- `internal/authz/registry.go`: scopes admission reads and adds project policy
  operations under the existing project-settings capability and human sessions.
  The policy read exemption is explicit and pinned in the audit inventory.
- `api/openapi.yaml` and the generated TypeScript validators: developer
  credential consent can enumerate the full 10,000-key ceiling. Ordinary
  disclosure and independent batch limits are preserved.
- `docs/site/src/content/docs/docs/managed-configuration.mdx` and
  `configuration.mdx`: user workflow, setting reference and capacity examples.
  The structural-bounds section of `docs/adr/ops-spec.md` owns the contract.

## Validation

Completed on the combined implementation:

- SQLite and PostgreSQL 18 policy and admission regressions passed, including
  inheritance, custom overrides, reset, retained ceiling clamps, authorization,
  cross-organization isolation, concurrent writes, stale revisions and owner
  bounds, Definitions Plan/Apply rechecks, and managed configuration protection.
  PostgreSQL used durable commits, with fsync enabled.
- Configuration migration tests passed against both databases, including older
  catalogues and preservation of existing policy.
- Real HTTP handler tests passed across managed owner reloads, custom project
  policy, stale writes, explicit-null reset and lowered instance ceilings.
  Existing key counts remained intact.
- Frontend typecheck, lint and design checks passed. Before the final UI
  consistency pass, all 1,972 frontend unit tests and all 41 TypeScript client
  tests passed. The final UI pass reran the 20 affected unit tests and the
  browser scenarios listed below.
- All 38 Storybook scenarios for project capacity, integrated Project settings
  and shared Buttons passed in Chromium in both dark and light palettes,
  including the shared accessibility checks.
- Collaborative browser checks at desktop and 390-pixel phone width exercised
  inherited and capped states, custom save, reset and invalid numeric input.
  The stale-save scenario retained the editor and disabled saving until policy
  was reloaded. These browser checks used the component's mocked transport;
  the real backend contract was checked separately through HTTP integration.
- Documentation typecheck passed with zero errors, warnings or hints. The
  static documentation build passed.
- Regenerating SQL, Go API, authorization wiring and the TypeScript client
  produced identical bytes. Go import formatting and diff checks passed.
- Refreshed Standards and Spec source reviews found no actionable issues.
  These are source reviews, not hosted CI or a maintainer security approval.

The new database migration's source-owned development compatibility declaration
was regenerated through the canonical tool from actual SQLite and PostgreSQL
schemas. The exhaustive API surface inventory pins both capacity routes as
local-data operations. The audit corpus invokes the real policy service to
prove the new event is emitted, preserving the existing closure checks.

The existing loose-signing-key permission test explicitly sets its unsafe
fixture mode, preserving its refusal check under a restrictive runner umask.
Production signing-key permissions and signing policy are unchanged.

The exhaustive audit corpus, owner-runtime checks, configuration upgrades and
bound registry passed on the combined implementation. The broad architecture
lint run found differing generated SQL parameter identities between engines;
these were aligned in the canonical SQLite queries and regenerated. The SQL
predicate check, both-engine capacity regressions, real HTTP behavior and API
contract checks passed again after the final binding change.

The complete architecture lint suite also reached its default 10-minute limit
in an unrelated disposable-database negative fixture. Its full result is not
claimed as passing. The timeout was not raised and no assertion was removed.
The full negative-fixture architecture suite remains unverified locally.

## CodeQL conversion follow-up

CodeQL flagged the default-key count's uint32-to-int conversion because its
ceiling was expressed only through the already bounded instance maximum.
The parser now checks the absolute 10,000-key ceiling directly before the
conversion, while retaining the separate default-versus-maximum check. Accepted
values and error behavior are unchanged.

Regression coverage includes large signed and unsigned 32-bit boundary inputs
for both settings and successful parsing at the 10,000-key edge. The complete
configuration and runtime-configuration test suites pass after this repair.
Hosted CodeQL passed on `1e30f47820be` and the reported review thread resolved.

## Merge queue integration follow-up

Merge-group run `39255626623` exposed integration gaps in the historical
upgrade fixture, reviewed security inventories and static browser demo.

- The legacy upgrade drill now explicitly admits migration 78, removes its
  columns and ledger entry only after proving every project has no override
  and revision zero, then repeats the existing exact legacy schema inspection.
  New subprocess tests refuse either saved override or revision evidence on
  SQLite and PostgreSQL. The immutable legacy migration prefix is unchanged.
- The operation formula inventory adds capacity read and set under
  `project-settings@project`. Existing operation formulas are unchanged.
- Generic project metadata reads retain their historical projection. They do
  not read capacity columns before migration 78 exists during a legacy restore.
  Capacity still uses its separate proof-gated query. Existing both-engine
  automatic credential-proof tests caught this regression with org-scoped grants
  after the stale fixture guard was repaired.
- Reviewed SQL protocol hashes follow the additive managed configuration
  migrations and their resolved version constants. The canonical updater
  refuses changes to authority, protocol ownership and caller sets.
- The static demo serves schema-valid inherited capacity for its six keys and
  three environments. Capacity writes remain refused and preserve fixture
  state. Browser checks explicitly exercise inspection refresh while all policy
  mutations remain disabled, following the existing credential refresh pattern.

The original legacy fixture failure and missing demo response reproduced
locally before the repair. Frontend typecheck, lint, all 1,978 unit tests and
all 82 static demo browser checks passed after it on desktop and phone.
The actual offline Cosign restore drill passed. Canonical pin regeneration
passed across all four build contexts. The normal reviewed SQL protocol,
predicate and inventory guards passed again against the regenerated pins.
Refreshed Standards and Spec source reviews found no actionable findings.
The first complete app run found the legacy metadata projection regression.
After that repair, the focused legacy regressions passed on both databases,
the complete app suite passed, and the complete configuration upgrade suite
passed. Hosted checks on the new commit remain separate evidence.

## UI consistency pass

The capacity section now uses the same stacked settings-row composition as the
neighboring Definitions bundle, with one shared Edit/Refresh action row instead
of a separate heading and refresh row. Phone editor actions use the existing
single-column action pattern.

A shared mobile-grid issue made a wrapped Refresh label taller than Edit.
The canonical Button stylesheet now stretches sibling grid items to the same
height while retaining centered labels. At a 390-pixel viewport, both summary
buttons measured 156 by 44 pixels on the same row. The three editor actions
measured 357 by 36 pixels each, with no horizontal overflow. Desktop action
alignment follows the existing settings design.

The Storybook alignment helper now skips boxless display:contents ancestors
and keeps its existing one-pixel assertion. A unique capacity scope key also
removes a duplicate React key shared with the definitions panel.

Typecheck, lint, 20 focused unit tests and design checks passed after the UI
pass. All 38 stories covering capacity, integrated Project settings and shared
Buttons passed in Chromium in both dark and light palettes. Integrated desktop
and phone views were also inspected in the collaborative browser.

## Delivery boundaries

The CI prerequisites are merged: #1011 repairs bounded transient reads and
lagging merge-group proof publication; #1009 supplies the backend-inclusive
version-2 feedback compiler seed. The feature branch is rebased onto main
`367ba24d71326655d9815a40eb8b1c4e9e0d9025`. Its three application commits
preserve the original feature patches unchanged. The three obsolete CI
commits were dropped in favour of the merged implementations.

After the rebase, the capacity configuration, runtime reload, service, CLI,
application and API regressions passed. Frontend typechecking, linting and all
1,978 unit tests also passed. Every rewritten commit passed cryptographic
signature and DCO validation. Publish the bounded backend seed from trusted
main before pushing the replacement head; then verify exact-head hosted
checks, protected merge-group proof and actual merge to main separately.

The trusted-main seed was subsequently published successfully. All three
feedback lanes, repository invariants and CodeQL passed on `c75fdff`.
That head's merge-group validation was cancelled after review findings arrived;
its queue admission is not merge proof.

The review follow-up asserts that an override was activated before testing a
lowered ceiling, removes the misleading fixed instance default from uniform
limit-refusal messages, and lets the capacity editor's custom validation handle
out-of-range submissions. A new browser scenario submits a retained 5,000-key
override against a 2,000-key ceiling, checks the accessible inline error, then
corrects it to 1,500 and saves. The same flow was verified at a phone viewport.
The full server suite, runtime reload regression, frontend typecheck and lint,
all 1,978 frontend unit tests, and all 15 capacity stories in each palette pass.
The wire-message regression was observed failing before the message fix.

Implementation and documentation are prepared for pull-request delivery.
Hosted checks on the review follow-up, actual merge, release and deployment
remain separate proof boundaries. A deployed application, real
multi-node HA convergence and performance at the expanded ceiling have not
been verified. The frontend sensitivity inventory retains its required
maintainer security review before merge.
