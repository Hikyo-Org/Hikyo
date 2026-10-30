# Handoff: #820 Transit WebUI

Issue: https://github.com/Hikyo-Org/Hikyo/issues/820

The separate #156 governance continuation is now the
[October 29 source-bound amendment closeout](./156-transit-amendment-closeout.md).
It leaves the delivered WebUI and this handoff's historical review limits intact.

## Current delivery record (2026-10-28)

[#965](https://github.com/Hikyo-Org/Hikyo/pull/965) merged on October 18 as
`6da8f57cb8c8aae4cb58b7cc65ec5971c1705e0c`, an ancestor of assessed main
`24e198b32a3ffd8d4013207b2f974d76ec49be6f`; #820 is closed. Existing
[trusted integration run 39137162163](https://github.com/Hikyo-Org/Hikyo/actions/runs/39137162163)
succeeded on that exact merge. Current `api/parity.yaml` has all 15 Transit
operations mapped to `webui: transit-keys`. This establishes delivery and
historical hosted evidence, not a fresh candidate acceptance run.

The [#156 amendment-review obligation](./156-162-589-capability-governance.md)
remains **HOLD** independently. Keep the delivered UI closed while reconciling
that governance debt. No source or UI change is requested by this record.

## Delivered behavior

The project sidebar now opens **Transit keys**, scoped to one environment.
All 15 Transit operations in `api/parity.yaml` have direct `webui: transit-keys`
dispositions. Inspection shows algorithm, custody, lifecycle, retained versions,
public keys, caller rules, version floors, rotation due and deletion deadline.

Management supports creation with immutable allowed operations, configuration,
rotation, disable, enable, retire, compromise, schedule/cancel deletion and trim.
Caller expansions and lower version floors require an explicit acknowledgement
of the affected principals and operations before submission. Trim separately
requires acknowledgement of irreversible live-version erasure.

The interactive data plane supports encryption, decryption, rewrapping, wrapped
and opted-in plaintext data keys, signing, signature verification, HMAC and MAC
verification. All operations use the generated client and existing server
permissions. Neither capability is implied by another role or granted by this
production change. The browser fixture explicitly grants its test principal
`crypto-use` and `crypto-manage` before minting sessions.

## Sensitive data and integration

- Managed private key material is never returned or rendered. Public version
  metadata and public signing keys are inspectable.
- Cryptographic requests bypass query and mutation caches. Inputs clear before
  dispatch; sensitive results remain in component-owned sensitive state.
- Plaintext is displayed once. Dismissal clears it; navigation, focus loss,
  pagehide, session retirement, environment/key replacement and authoritative
  metadata refusals also retire it. Abort and generation checks reject late
  results. Pagehide clears the DOM synchronously before a BFCache snapshot.
- Remote workspaces use the existing workspace boundary and selected transport.
  Transit metadata keys include environment and remote origin. Removed
  environments retire both Transit cache families through the shared topology
  owner; denial tombstones cannot be mistaken for an authoritative empty list.
- Server and schema errors render redacted refusal copy, never raw payloads.
  Disabled, compromised, retired and pending-deletion keys explain their policy.
- The demo excludes this operational surface explicitly. No new dependencies,
  backend API changes or migrations are required.

## Validation and review

Local validation completed:

- Full WebUI unit suite: 212 files, 1,948 passing tests.
- Final focused API, custody, UI and topology checks: 45 passing tests.
- WebUI TypeScript, lint and design-system checks pass.
- All eight Transit Storybook states pass in dark and light, including phone,
  empty, disabled, compromised, pending deletion, create and read refusal.
- Static Storybook build and its documentation/interaction index checks pass.
- Production SPA build, demo isolation and compression checks pass.
- API parity tests pass.
- Collaborative-browser inspection verifies existing typography and page-width
  tokens; a 390px viewport has no page overflow and contains table scrolling.

Real-server browser validation passes: three desktop tests and three mobile
tests, each including the full 15-operation lifecycle and dark/light design
assertions. The tests run against the Go
binary with the production SPA embedded, using independent seeded instances for
each viewport. The lifecycle exercises all 15 operations and cleans up its own
environment. These are focused Transit runs; their full-registry execution
closure is intentionally skipped by the existing harness. Other flow shards
and the complete closure remain part of remote CI.

The initial desktop run reached encryption, decryption, rotation, rewrap and
plaintext data keys, then failed its tab-focus assertion because Playwright's
Chromium adapter forces every page to remain focused. An isolated two-page
reproduction confirmed that a secondary CDP session cannot release the original
override; attempting that also interfered with virtual-authenticator cleanup.
The browser regression explicitly dispatches a synthetic window blur and checks
the product's clearing behavior, alongside the independent unit boundary tests.
It does not claim native operating-system focus proof. Both original failed
runs are retained separately from rerun evidence.

Separate Standards and Spec source reviews found the initial widening-confirmation,
action-layout, story assertion and inventory gaps. Those were repaired and the
Standards rereview found no remaining hard source findings. The nonblocking
algorithm-default duplication was also consolidated into one typed mapping.
These are automated source reviews; they do not establish human approval.

## Merge and execution boundary

This change requires maintainer security review under CONTRIBUTING.md. The
sensitivity inventory describes the new data paths and pins the reviewed source.
Automated review does not replace independent human review.

Cross-provider review was not run under the existing reviewer-availability rule.
The historical #156 Transit amendment review debt is not closed by this WebUI
change. No release, deployment or external custody-provider acceptance is claimed.

The original pre-merge wait is discharged by the verified merge and hosted
integration recorded above. It does not discharge #156's amendment review.

## Repeat the focused checks

From the repository root, with contributor dependencies installed:

```sh
(cd web && node --run typecheck)
(cd web && node --run test)
pnpm --dir web run lint
pnpm --dir web run design:check
pnpm --dir web run build
go test ./api -run TestParity -count=1
pnpm --dir web exec vitest run --project storybook src/routes/TransitKeys.stories.tsx
STORYBOOK_THEME=light pnpm --dir web exec vitest run --project storybook src/routes/TransitKeys.stories.tsx
pnpm --dir web exec playwright test e2e/flows/machine-access.spec.ts --project=desktop --grep 'transit keys'
pnpm --dir web exec playwright test e2e/flows/machine-access.spec.ts --project=mobile --grep 'transit keys'
```

Run viewport projects sequentially because the fixture owns shared files and
sessions. The fixture builds the UI-enabled Go binary when no prebuilt test
binary is supplied. Existing CI executes these tests through the registered
machine-access shard; no workflow or required check was weakened.
