# Hikyo GitLab CI/CD variable adapter: deltas over the deployment-module seam (ADR, 2026-09-26)

Context: the MVP boundary ([mvp-boundary.md](./mvp-boundary.md)) kept a GitLab deployment adapter out of 1.0 with the trigger "demand: same seam, same #36-class delta ticket". [#159](https://github.com/Hikyo-Org/Hikyo/issues/159) is that demand, and this ADR is the delta. It adds a third compiled-in provider over the seam fixed in [deployment-adapter.md](./deployment-adapter.md) and the multi-target workflow of [#157](https://github.com/Hikyo-Org/Hikyo/issues/157). Everything not named here transfers unchanged, as it did for [github-adapter.md](./github-adapter.md).

Granularity note: wayfinding-level, like its siblings. Pacing, retry curves, and bounds come from the operations spec.

## What transfers unchanged (cited, not restated)

The four-operation seam and its value-blind `Plan`; full-state converge with writes before prunes; the stale-write fence (target generation plus the per-target provider-write lease); supersede semantics; the durable INTENT/OUTCOME journal; the ownership ledger and its `reserved`/`dispatched`/`owned`/`released` states; `exists, unowned` as a conflict; adoption bound to artifact, generation, and destination; teardown with the `MANAGED_BY_HIKYO` sentinel written first and removed last; the write-only sealed credential; the egress dialer policy, no proxy, no redirects, TLS 1.2 or newer, a response cap, and a request deadline strictly shorter than the provider-write lease.

## Deltas this provider forces

**1. The no-read closure is total, not per-surface.** GitLab's variable `GET` and list endpoints return stored values, and GitLab has no name-only secret list. The client's closed `API` interface is therefore exactly `Version`, `Token`, `ResolveDestination`, `CreateVariable`, `UpdateVariable`, and `DeleteVariable`; no method reads or lists variables, and a route-scan test refuses any registry entry that addresses `variables` with `GET`. Existence and ownership come from the ledger alone. `Plan` reports every unowned key as `unknown-until-sync`, exactly the seam's variable-surface rule. An unowned key is discovered only when a `POST` is refused with GitLab's `(key) has already been taken`, which is recorded as `exists, unowned` and never followed by an overwrite.

**2. One namespace, one sentinel.** GitLab stores secrets and configuration in a single CI/CD variable namespace, so every GitLab ledger row uses the variable surface and there is one sentinel per `(destination, environment scope)`. A classification change is an in-place update of the same owned key, never a delete-and-create across surfaces.

**3. Classification maps to flags, and masking is never silently disabled.** Secret-classified keys are always written `masked`; `masked_and_hidden` on creation when the target asks for hidden (GitLab 17.4 or newer, refused by name below that version rather than downgraded). Configuration keys are plain variables. A target option makes every managed variable `protected`. Values are written `raw` by default so a `$` in a value is delivered byte-exactly; expansion is an explicit target option. GitLab's documented masking rule (a single line of at least 8 characters from `A-Z a-z 0-9 _ @ : . ~ + / = -`) is pinned once in `adapter.GitLabMaskable` and checked before any provider request; a secret value that cannot be masked fails by key name, never with its value, and never ships unmasked. File-type variables are out of scope.

**4. Destinations are projects and groups, and scope is part of identity.** A GitLab project is stored as destination kind `repository` (namespace path plus project path) and a group as `organization` (full group path). Both are pinned by immutable numeric id; every plan, write, and prune re-resolves the id and refuses when its path no longer matches the configured path, so a moved or renamed project is a conflict needing an operator, not a silent redirect. The GitLab `environment_scope` is a target-level field, defaulting to `*`, and it is immutable: owned variables are keyed by it, so changing it in place would orphan them. The ledger's active-name uniqueness includes the scope, so one project can safely receive the same key under `production` and `staging` from two Hikyo environments, while two targets writing one key into one scope are refused at configuration time.

**5. Least-privilege tokens by default.** Project and group access tokens with the `api` scope are accepted at the minimum role GitLab requires to write variables: Maintainer for a project, Owner for a group. The token's type is learned from value-free metadata (`/personal_access_tokens/self` and the token user's `bot` flag). A personal access token is refused unless the adapter was created with `allow_personal_token`, a protected opt-in recorded in the audit payload, because it can act as its human owner everywhere they have access. Tokens carrying `sudo` or `admin_mode` are always refused. Token expiry is surfaced as a target warning within 14 days and as an authentication failure once passed.

**6. Self-hosted trust is part of the adapter.** An adapter may carry a CA bundle (PEM trust anchors added to the system roots) and an SPKI pin (`base64(sha256(SubjectPublicKeyInfo))`, the same construction as the CLI's server pin). The pin is checked after normal chain verification, never instead of it. Both are public material, fixed for the adapter's lifetime, and travel with it through an origin move; changing them means creating a new adapter. They are refused for every other provider rather than ignored.

**7. Rate limits and ambiguity fail loud.** `429` honours `Retry-After` and `RateLimit-Reset` through `RetryAtError`. A definite `4xx` is a failure that keeps the prior ledger state (releasing only a bare reservation); anything ambiguous is recorded as `dispatched` with an unknown outcome and replays as an update. An owned key missing at GitLab is recorded `owned_missing` before it is recreated, so a crash between the two replays as a create. Because the adapter never lists variables, there is no pagination in its provider surface.

## Verification

### Adoption scope repair (2026-10-01)

Explicit adoption records the target's immutable `environment_scope` in the
ledger, just like ordinary reservation. Same-scope claims are excluded globally
across Hikyo tenants; different scopes retain separate ownership. A blank scope
is never a substitute for GitLab's `*` scope.

Migration 72 releases only existing held blank-scope claims on GitLab targets
whose configured scope is nonempty. It preserves provider data and historical
audit/effect outcomes, supersedes pending jobs, advances the target generation,
and pauses the affected target with the warning
`gitlab_scope_claims_released_operator_review`. It does not silently translate
those claims or adopt any remote value. Correctly scoped and non-GitLab claims
are unchanged. An operator must review the actual GitLab variables before
resuming the target. Existing unowned values then follow the ordinary
create-conflict and fresh explicit-adoption workflow; the migration never
authorizes their overwrite or deletion.

If an affected target was moving, its containing unfinished route move becomes
`attention_required`. All pending jobs for that exact move's scoped members are
superseded and only their matching active pointers are detached; sibling
provider fences, ledger custody, pending claims and historical terminal outcomes
are preserved. The supported operator recovery is CancelMove, which restores
the active route without removing the repair pause, followed by an explicit
ResumeTarget with the complete current environment-bound consent. The repair
never silently resumes sibling provider work or infers ownership.

Unit tests pin the closed method set and route table, the masking rule, token policy, scope handling, and the full ledger state machine against a fake API. A gated end-to-end test (`TestGitLabRealLifecycle`) runs the durable store journal on both datastore engines against a real disposable GitLab CE started by `.github/workflows/gitlab-e2e.yml`: project, group, and scoped variables; protected and masked flags; conflict refusal without overwrite; crash-window replay; owned-missing recreation; pruning only owned names; SPKI pin mismatch refusal; and teardown.
