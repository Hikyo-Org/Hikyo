# Issue #807: bounded developer credential implementation

Read the owner-locked [developer credential ADR](../adr/developer-credentials.md)
for the normative lifecycle and security contract. This branch implements the
credential lane of [#807](https://github.com/Hikyo-Org/Hikyo/issues/807).
Documentation ask A previously merged through #811.

The authorized endpoint is implementation, signed commits, push and PR creation.
Merge, release and deployment are outside this authorization. The task baseline
is `36c323c417d1c29b20431a8c4bdb539882b9136c` on `dunky13/fix-issue-807`.

## Accepted decision and lock

The owner selected a separate developer principal for one unprotected
environment, current human read and reveal authority on every fetch, recorded
human audit authority, current and future key consent, and fresh scope-bound
self-delegation. Ordinary logout and login expiry preserve the fixed lifetime;
security invalidation and access or protection changes prevent the next fetch.
The concrete eight-hour maximum, lowerable instance ceiling and four-live cap
were approved with "implement, commit, push, pr" on 2026-10-09. Owning ADR
amendments are recorded in this changeset and become operative together on merge.

Two ordinary OpenAI Standards and Spec draft reviews found a ceiling-change
expiry-revival flaw. Both follow-ups confirmed the durable monotonic expiry
clamp correction. Cross-provider review is **SKIPPED by explicit owner
instruction**, not CLEAN. The owner no longer has a Claude subscription;
Anthropic availability is recorded as unavailable in the shared reviewer cache
and working-style guidance. No Claude call or replacement paid review was made.

## Implementation

- Migration 00074 introduces durable delegation metadata and the shared ceiling
  for SQLite and PostgreSQL. Verifiers are hashed, never recoverable bearers.
- The developer artifact is admitted only to latest online delivery. It has
  immutable scope and no separately grantable authority, renewal, history,
  dynamic leases, file delivery or offline reconciliation.
- Fresh developer reauthentication binds scope, current published key set,
  lifetime and future-key consent. It consumes one decision. Generic reveal
  windows do not satisfy mint. CLI TOTP and browser passkey/TOTP use this intent.
- CLI private custody uses the owner-only state file primitives outside the
  repository, exact trusted origin and immutable IDs. Mint outputs metadata
  only and attempts server revocation if local custody fails.
- Non-TTY `run` discovers the matching credential when no explicit machine
  credential is supplied, strips Hikyo credential environment variables and
  retains loader-control, collision and execution checks. It never reads or
  writes offline snapshots, cursors or pending audit material.
- Session security revocation, account restrictions, provider/identity security
  mutation, environment protection and scoped SCIM access loss terminally revoke
  affected credentials in the mutation transaction with human audit attribution.
- Instance policy lowering durably clamps existing expiry; raising cannot revive
  it. Own lifecycle, project incident administration and instance policy APIs are
  generated into the Go and TypeScript contract.

## Validation and delivery evidence

Local validation covers the complete Go package set, the repository-planned
four isolation shards, and targeted developer race tests. New lifecycle,
security invalidation, concurrent mint/disclosure, forged archive epoch and
canonical audit-emission regressions also run against SQLite and PostgreSQL.
The full PostgreSQL isolation suite remains a hosted CI responsibility.

Web validation passed typecheck, lint, design checks, 167 unit-test files with
1,512 tests, and eight CLI reauthentication story tests. The generated TypeScript
client passed typecheck and its 36 tests. API generation, authorization registry,
SQL policy, Go lint, import formatting, vet, documentation status ledger and the
76-page documentation build were checked. Structural graph relationships were
refreshed.

Review repaired refused-mint audit coverage, restore epoch scanning, exact
pristine policy seed admission, shared launch safety, credential custody outside
all enclosing repositories, preauthentication rate admission before durable
refusal audits, consent bounds matching the supported 1,000-key catalogue, and zero-window
passkey enforcement at both ceremony opening and decision consumption.
Final ordinary OpenAI Standards and Spec closure reviews are **CLEAN**.
Cross-provider review remains **SKIPPED by explicit owner instruction**.

The browser consent story was inspected at 1280 x 800 and 390 x 844. It shows
scope, fixed lifetime, current/future consent and fresh factor controls. The
local Storybook URL is temporary and is not a published environment.

The own lifecycle and mint operations have a declared CLI-only parity exception.
Project incident and instance policy WebUI parity remain tracked by #807; this
PR references the issue rather than closing it prematurely. No remote CI,
merge, artifact publication, deployment or production runtime proof is claimed
by local tests.

## Operator entry points

After selecting a trusted instance, organization and project using immutable IDs:

```sh
hikyo dev session --env <env-id> --ttl 8h
hikyo run --env <env-id> -- pnpm dev
hikyo dev session list
hikyo dev session revoke --id <credential-id>
hikyo dev session revoke --all
hikyo instance-config developer-credential-policy get
hikyo instance-config developer-credential-policy set --max-lifetime 4h
```

Mint runs in a controlling terminal. `run` may be launched by a coding agent for
that exact scope after mint. Development environments must contain only
development values: an agent controlling the child can read every delivered
value. Fixed expiry and next-fetch revocation cannot erase values already held
by a child process.

## PR #868 CI security follow-up

The first hosted run passed the isolation, race and browser matrices, but
`validation / test_core` and `validation / web-go` failed their release-shaped
binary vulnerability scans. The pinned Go 1.27.0 compiler retained affected
standard-library code; `golang.org/x/net` v0.58.0 also retained vulnerable HTTP/2
symbols. Both gate failures were inspected independently.

The repair raises the compiler minimum to Go 1.27.2 and `x/net` to v0.60.0, with
only related module versions required by Go's minimum version selection.
`go mod tidy` updates the checksums. Scanner policies and CI gates are preserved.
Both rebuilt binary shapes pass the existing pinned binary-mode `govulncheck`
with zero reachable vulnerabilities. Fresh crypto, authorization, service, API,
transport and UI-tagged serving tests pass; `go vet ./...` passes. Ordinary
Standards and Spec repair reviews are CLEAN. Fresh hosted CI must validate the
repair commit before readiness is claimed.

Upstream evidence: [Go 1.27.2 release](https://go.dev/doc/devel/release#go1.27.2),
[HTTP/2 security advisory](https://pkg.go.dev/vuln/GO-2026-6612).

## PR #868 main integration follow-up

The `ci-required` failure on cdf8da6 reported merge conflicts, rather than a
new test failure. Main independently landed the Go security pins and refreshed
the guides in #865. Integrating main preserves the developer credential recipe,
its online-only boundary, and main's accurate plaintext wording. Checksums are
regenerated with `go mod tidy`; main's updated Go tooling and SSH certificate
checks are retained.

The merged result passes documentation typechecking with zero diagnostics,
the full documentation build/test suite (76 pages including offline delivery),
the 35-entry status ledger and its rejection fixtures, module verification,
SSH certificate tests, developer credential CLI tests, and Go import formatting.
Fresh hosted checks must validate the integration commit before readiness is
claimed. The PR remains unmerged.

## PR #868 automated review repairs

All three Qodo findings on 3ee3273d8 were reproduced against source and repaired:

- Mint custody validates positive server-relative lifetime against the exact
  confirmed duration. A slow laptop clock cannot reject an otherwise valid
  eight-hour mint, and an oversized server response is revoked.
- Before consent, CLI mint resolves the current ceiling through the existing
  environment-read-authorized reveal-window surface. Both ceremony and mint
  require that same concrete positive duration. A policy raise cannot enlarge
  the decision; a policy reduction is rechecked under the mint transaction lock.
  The affordance uses a nonlocking policy read so PostgreSQL read-only requests
  remain valid. Generated API contracts, clients and review pins are updated.
- Policy reductions only clamp and count live credentials, excluding revoked,
  expired and epoch/generation-invalidated rows. Historical expiry stays intact.

Validation passes: full CLI and service suites, API and server suites, the
both-engine developer credential flows, new default/clock/race/audit regressions,
query and authorizer pin checks, Go vet/import formatting and all four lint
build contexts. Web typechecking/lint and all 1,512 unit tests pass; its refreshed
sensitivity inventory follows an explicit source review. Client typechecking
and all 37 tests pass. Documentation typechecking/build and the 35-entry ledger
pass. Ordinary Standards and Spec repair reviews are CLEAN, including closure
of the review-pin finding. Cross-provider review remains explicitly skipped.

CodeRabbit skipped review because this PR exceeds its 100-file limit and lacks
review capacity. That skip is not a CLEAN review. The benchmark notice is
optional; no benchmark or paid capacity was requested. Fresh hosted CI must
validate the repair head. The PR remains unmerged.

## PR #868 requested rebase

Rebased onto main ac417ef00, including #866's focus-loss secret remasking and
late-response protection. Conflict resolution retains the development guide,
security dependency pins, handoff history, and both #861 and #807 sensitivity
review notes. Comparing the final tree with the previously green 03351b5 head
shows only main's remasking files and the combined review notes; credential
implementation and its consent repairs are unchanged.

Post-rebase verification passes all 1,534 web unit tests, web typechecking/lint,
documentation typechecking, module verification, and focused developer tests
across CLI, service, server and API. Every rewritten commit is cryptographically
signed and DCO signed off. Fresh hosted CI is required for the rewritten head;
previous-head green CI does not establish this head. No merge is authorized.
