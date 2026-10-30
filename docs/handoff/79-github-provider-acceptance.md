# #79: GitHub provider acceptance packet

## Disposition and ownership

**HOLD for final-candidate GH-E2E/GH-CONTRACT.** This packet prepares a repeatable
run. It does not certify the 1.0 candidate. It owns the dedicated GitHub API and
workflow-consumption contract only. The release-plan, ledger, roadmap, AWS #158,
controller #1057, MCP, signing, publication and production environments belong to
other work. No provider mutation or new spending is authorized by this document.

The final qualification owner selects a clean integrated candidate **after** the
required source/trust changes merge. Run this packet at that exact commit. Any
subsequent source movement needs a new binding and disposition from that owner.
The test exercises the adapter directly, not a released binary or deployed server.

## Current evidence, 28 October 2026

Inspection started at main `24e198b32a3ffd8d4013207b2f974d76ec49be6f`, also returned
by GitHub's main-commit API. Read-only GitHub API inspection found:

| Resource | Observed identity/state |
| --- | --- |
| Fixture organization | `Hikyo-Org`, ID `316726515` |
| Fixture repository | `Hikyo-Org/hikyo-adapter-acceptance`, ID `1358512924`, public, active |
| Protected environment | `contract`, ID `21319818820`, custom branch policy allows `main`, no required reviewers |
| Fixture main | `3756144981f0fc7ae58da1cefb3bcf7fc18e011f` |
| Installed workflow SHA-256 | `ba9219d757f499b201f6002e0593c03c880e960ee01d22c37fbf7b7b15df6e3a` |
| Candidate source fixture SHA-256 | `cf02244263caaeec014c463558e56c25621beb1671b3c5e00e86eeff5c1a7f4b` |
| Last observed dispatch | September 5; latest run `33992743845`, success |

**The installed workflow differs from the source fixture.** A newly authorized
fixture update must install the exact candidate file before acceptance. Do not
change the test to accept the stale workflow or infer consumption from storage.
Recheck all identities and branch protections before approval and execution.
The organization above also owns the product repository; organization permissions
are organization-wide even when repository access selects only the fixture.
Reusing it needs explicit approval covering only this fixture's synthetic names
and selected recipients. Otherwise supply an approved isolated organization and
refresh the numeric identities. Never use the product repository or a production
environment for this run.

[Historical acceptance](../reports/1.0/github-live-acceptance.json) records a
September 5 PASS in 574.203 seconds, 18 successful workflow/hash comparisons,
cleanup, and revocation of all seven tokens. Its source was an uncommitted,
separately hashed historical input set. It cannot certify today's source.
[The historical handoff](github-environment-api-paths.md) describes that proof's
limits. No current seven-token set or resource-specific renewed authority was
found in the task environment, documented custody or recovered release guidance.
The existing GitHub CLI OAuth session is neither a minimum-permission PAT nor
permission to substitute one credential for the seven required roles.

## Repairs in this packet

- Ordinary CI keeps the explicitly disabled external skip. Explicit opt-in with
  partial, malformed or reused credentials now fails before provider access.
- Configuration requires full source/fixture commit pins, authorization reference,
  positive immutable repository/org/environment IDs, and seven distinct
  fine-grained PATs. The product repository is refused.
- Read-only preflight checks a public active fixture, the numeric repository and
  owner IDs, exact ref commit and byte-identical installed workflow. The three
  destination identity lookups must match approved IDs before any mutation.
- Every consumed run must match the fixture commit, repository ID,
  `workflow_dispatch` event and first attempt. Drift fails visibly.
- Owned names are registered before writes, including missing-PATCH and denied
  sentinel probes, selected-recipient failures, and ambiguous create responses.
  Auto-created environments have cleanup registered before PUT and deletion is
  followed by a GET-404 check.
- Dispatch intent is recorded before POST. Cleanup discovers the nonce even after
  a lost response, cancels pending runs, removes only their artifacts and checks
  an empty artifact inventory. Unobserved dispatch outcomes remain failures with
  the nonce retained for reconciliation. Other fixture runs are untouched.
- Artifact download uses the fixed GitHub artifact endpoint. Redirects carry no
  harness Authorization header and must use HTTPS.
- Hash-only structured evidence records candidate/workflow pins, UTC run context,
  owned names/environments, dispatch nonces, run/artifact IDs, consumed byte
  lengths, SHA-256 comparisons and cleanup. No token or provider value is logged.

## Human prerequisites and credential custody

Obtain explicit, current resource-specific approval for the selected candidate,
fixture repository/org/environment, source workflow installation, and **one run
of at most 18 standard public GitHub-hosted workflow dispatches**, with no new
spending. The approval covers synthetic `HIKYO_CONTRACT_*` and
`HIKYO_PERMISSION_*` secrets/variables, selected organization recipients limited
to the fixture's immutable repository ID, the settings-free PUT on `contract`,
one temporary `hikyo_contract_*_autocreate` environment, and deletion/cancellation
of this run's resources. It does not permit changing existing environment
protections, production resources or unrelated org names.

Confirm the public fixture's standard Ubuntu runners and account entitlements
permit these executions without new spend. Do not buy capacity, add a paid
runner, change the billing plan or create a paid organization under this packet.
The exact source workflow uses `ubuntu-latest` and the pinned artifact action.

Use seven separately minted, short-lived, approved fine-grained PATs. Every PAT
has the approved organization as resource owner, fixture-only repository access
and automatic Metadata read. Separate token records are required, not seven
GitHub user accounts. Preserve protected custody plus a **non-secret** inventory
of token record identifiers, approved grants, owner/repository selection,
expiration and approval state. Review that inventory before setting
`credential_grants_attested` true. Distinct token strings alone cannot prove
that positive grants are minimal or negative grants omit exactly one permission.

| JSON credential key | Repository permissions beyond Metadata | Organization permissions | Negative control |
| --- | --- | --- | --- |
| `REPOSITORY_TOKEN` | Secrets: write; Variables: write | None | None |
| `REPOSITORY_DENIED_TOKEN` | Secrets: write | None | Omit Variables: write |
| `ORGANIZATION_TOKEN` | None | Secrets: write; Variables: write | None |
| `ORGANIZATION_DENIED_TOKEN` | None | Secrets: write | Omit Variables: write |
| `ENVIRONMENT_TOKEN` | Environments: write; Actions: read | None | None |
| `ENVIRONMENT_DENIED_TOKEN` | Actions: read | None | Omit Environments: write |
| `HARNESS_TOKEN` | Contents: read; Actions: write; Administration: write | None | None |

The harness token does not need secret/variable delivery grants. Do not broaden
it to replace delivery or negative-control tokens. Updating workflow source
requires a separately authorized maintainer operation with workflow-write
capability; none of these seven tokens should gain that capability as a shortcut.
Keep all credentials out of Git, shell tracing/history, logs, reports and chat.
Use the protected `request_secret` card with file delivery for the credential
JSON, or an approved protected store. Never source the JSON as shell code.
Revoke all seven tokens after confirmed cleanup, including failed acceptance.
Record revocation separately; the runner cannot revoke PATs for the owner.

## Executable run from the repository root

Prerequisites: candidate's pinned Go version (`go.mod`), Bash, Git, jq, shasum,
working DNS/HTTPS to GitHub and artifact storage, and the shared `agent-heavy`
queue where required. These commands do not install or upgrade dependencies.

1. Select and check out the final candidate through the qualification owner.
   Preserve the current task's worktree/branch binding. In the **candidate
   checkout**, keep evidence/custody outside Git. If `.local/` is not already
   ignored, add `/.local/` to that clone's Git exclude file before using it.

   ```sh
   git status --short
   git rev-parse HEAD
   go version
   git check-ignore .local/79-github/authorization.json
   ```

   Expected: clean checkout, exact selected SHA, pinned Go, and an ignored local
   path. Use an absolute evidence directory outside the checkout if preferred.

2. After separate approval, have an authorized maintainer install the candidate's
   `internal/adapter/githubactions/testdata/hikyo-contract-consume.yml` verbatim
   at `.github/workflows/hikyo-contract-consume.yml` on fixture `main`. Keep the
   environment's existing `main` branch policy. Record the resulting **full fixture
   commit SHA**, re-fetch the file and compare bytes with `cmp`. Do not treat
   identical YAML semantics as identical bytes. This installation is not done by
   the acceptance runner and is not authorized by the blank example file.

3. Copy [authorization.example.json](../../scripts/acceptance/github-contract/authorization.example.json)
   into protected ignored storage. Fill `approved_by`, the durable human
   `approval_ref`, UTC `expires_at` in `YYYY-MM-DDTHH:MM:SSZ` form, final
   `candidate_sha`, the updated fixture `workflow_sha`, and all rechecked resource
   fields. Set the grant attestation only after the separate inventory is reviewed.
   The example deliberately fails validation. It grants no authority.
   Put only the seven table keys and their token strings into the protected
   credentials JSON; require owner-only permissions (`chmod 600`). Do not reuse
   revoked historical values or a single broad token.

4. Set these paths to the protected files and a **new** evidence directory, then
   execute once. Paths are operator inputs, not machine-specific committed paths.

   ```sh
   candidate=$(git rev-parse HEAD)
   authorization=.local/79-github/authorization.json
   credentials=.local/79-github/credentials.json
   evidence=.local/79-github/attempt-1
   agent-heavy run --label 'GitHub candidate contract' -- \
     bash scripts/acceptance/github-contract/run.sh \
     "$candidate" "$authorization" "$credentials" "$evidence"
   ```

   Expected: `github contract: PASS; receipt .../receipt.json`, exit zero,
   the exact candidate/tree still clean, all six live subtests plus parent PASS,
   no skips/failures, 18 distinct consumed runs/artifacts, six per destination,
   and paired ownership/cleanup evidence. Any failure, partial output, candidate
   movement, missing cleanup or incomplete consumption yields HOLD/nonzero.
   Each attempt needs a new directory; never overwrite failure evidence.
   Overall timeout is 45 minutes; historical runtime is not a deadline promise.

## Receipt interpretation and interrupted cleanup

The runner retains `go-test.jsonl`, stderr, parsed `events.json`, the approved
non-secret authorization copy and `receipt.json`. Receipt fields bind the exact
source SHA/tree, fixture commit/file digest, numeric destination IDs, authorization,
start/end UTC times, Go exit and log digest, actual workflow/artifact IDs,
per-pair byte lengths/hashes, and cleanup events. The raw log retains per-subtest
assertion results, including exact provider status contract failures. The validator
rejects explicit skips, missing subtests, missing scope evidence, duplicate
run/artifact IDs, malformed identifiers/hashes and unmatched cleanup.

A contract PASS still has `credential_revocation: NOTRUN` until the owner supplies
the seven-token revocation record. No stable release, deployed application,
whole-program E2E or GHES proof is implied. This fixture covers provider API
contracts and byte consumption. The broader locked ADR's lifecycle/identity/
adoption/generation and both-engine integration requirements retain their own
existing test/acceptance owners. NUL and non-UTF-8 remain named client-side refusal
classes; this run does not claim they can be delivered through workflow env.

On interruption or HOLD, preserve all files and credentials needed for cleanup.
Use `owned_name`, `owned_environment`, `dispatch_intent` and `owned_run` entries
in the raw JSON log to reconcile **only this attempt**. If a POST response was
lost and no run was observed, list workflow-dispatch runs on the pinned fixture,
match its exact `hikyo-contract-<nonce>` title and UTC start, verify the numeric
repository ID, then cancel if pending and delete its artifacts. If listing was
lagging, recheck later before declaring cleanup complete. Do not retry the whole
destructive contract to find resources from a failed attempt.

For names, issue only DELETE against the recorded destination and exact name:
repository/org use `/actions/secrets/{name}` or `/actions/variables/{name}`;
environment uses `/environments/{environment}/secrets/{name}` or `/variables/{name}`.
Use the corresponding minimum delivery token. Verify HTTP 204 or 404; never GET
or list variables to obtain their values. For each owned run, use the harness
token to list `/repos/{owner}/{repo}/actions/runs/{run_id}/artifacts`, delete only
those artifact IDs, then require `total_count: 0`. For the auto-created environment,
DELETE its exact recorded name and require a subsequent GET 404. Keep `contract`
and all its protections. Use bounded requests and retain every refusal.

A forced kill can prevent `testing.T.Cleanup`; unresolved custody remains HOLD.
Revoke all seven PATs after cleanup verification. If a credential must be revoked
before cleanup for another reason, record orphaned resource IDs and escalate for
separate scoped owner cleanup rather than substituting a production credential.

## Local validation and next owner action

Regression coverage includes all 21 token-reuse pairs, missing/malformed pins,
ordinary skip versus explicit enabled failure, fixture/ref/repository/run drift,
artifact credential stripping, lost dispatch responses, invalid artifact download,
cancellation before scrub, incomplete inventories, deletion errors, and receipt
rejection for skipped/partial/misbound evidence. The historical loader was restored
locally for the opt-in regression: it returned SKIP/PASS and the regression failed.
The repaired loader passes the same regression. These synthetic checks are local
source evidence and never provider proof. No persistence behavior changes here;
SQLite/PostgreSQL release acceptance remains owned by the final qualification lane.

Run complete-diff `scripts/ci/fast-feedback.sh --working-tree`, focused package and
receipt tests, adapter race tests and vet through the shared heavy queue when
required. Hosted PR checks, review, merge, live execution and token revocation
remain separate evidence classes. Cross-provider review is SKIPPED if unavailable,
never CLEAN. Maintainer merge approval remains required.

The next human-only input is one resource-specific approval for this packet,
including workflow installation, plus protected delivery of seven distinct,
minimum/negative/harness PATs and the reviewed grant inventory. Candidate selection
can follow required merges; credentials can be prepared without running mutations.
Keep GH-E2E/GH-CONTRACT HOLD until final-candidate execution and cleanup/revocation
are recorded. Do not close #79 from this preparation PR.

## First-party API references

- [Fine-grained permission mapping](https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens)
- [Workflow dispatch](https://docs.github.com/en/rest/actions/workflows#create-a-workflow-dispatch-event)
- [Environment settings](https://docs.github.com/en/rest/deployments/environments#create-or-update-an-environment)
- [Workflow artifact download and deletion](https://docs.github.com/en/rest/actions/artifacts)

The test retains the adapter's pinned API version rather than silently changing
production behavior to follow the documentation's latest example version.
