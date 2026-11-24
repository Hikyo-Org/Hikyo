# #1057: trusted-source controller lifecycle, restart boundary

Owner: [#1057](https://github.com/Hikyo-Org/Hikyo/issues/1057). Source baseline:
`dev` at `67a4e12ff41b1d1aced02698dfc99675b79bf919` (2026-11-16). This records what
the merged repository contains, what the issue's frozen controller successor adds
that it does not, and what must be supplied before the lifecycle and cleanup
qualification can continue. It is a restart boundary, not a result. No acceptance
item beyond the first is claimed here.

It adds no code, grants no authority and contacts no provider. No live resource,
grant, notification or provider action was used or is authorized by this record.

## Related work

- [#1046](https://github.com/Hikyo-Org/Hikyo/issues/1046) is closed. Its merged PR
  [#1047](https://github.com/Hikyo-Org/Hikyo/pull/1047) (merge commit
  `f5ec085b73df7d179283145c614db87601cff88f`, 2026-10-23) delivered
  `scripts/ci/release-admission/`, which is an ancestor of the baseline above.
- [#158](https://github.com/Hikyo-Org/Hikyo/issues/158) is closed, finished through
  the smaller AWS route. This issue is not a prerequisite of it and does not
  reopen it.
- [#969](https://github.com/Hikyo-Org/Hikyo/pull/969), which CodeRabbit linked, is
  **merged** (2026-10-30), not open. It changed none of the
  `scripts/ci/release-admission/` files, so it does not overlap this scope.

## Source inventory (acceptance item 1)

### Present in the repository

`scripts/ci/release-admission/` as merged by #1047, plus ten later commits that
touch it: the Go 1.27.4 policy acceptance and re-seal (2026-11-13), the retirement
of the hosted AWS acceptance workflow and runner (`58c20d9c2`, 2026-11-12) with its
workflow-contract cleanup, the removal of the retired ci-candidate workflow from
the admission policy (`508656871`, 2026-11-14), the backmerge of main into dev (`266ae0b63`, 2026-11-15)
that refreshed the same pins, and AWS contract-routing and failure-diagnostic
changes (2026-10-26 and 27).
Its static description is `IMPLEMENTATION-MANIFEST.json` (148 file entries) with
`EXECUTION-MAP.json`. Raw SHA-256 of the committed pins at the baseline:

| File | Raw SHA-256 |
|---|---|
| `IMPLEMENTATION-MANIFEST.json` | `b2329f43cad57df7d1fefd1a755284e050bd453c657147ae3634773e7e065b3b` |
| `EXECUTION-MAP.json` | `e09eff7454e4fcd3f84dfeb575ca5016820fef7f158f50d24d4f80c4ee32df67` |
| `HISTORICAL-ARCHIVE-PINS.json` | `c78136200e819b4ff859b85baf26ffc222010d39764a54780740228f05d5386a` |

These are integrity identifiers of committed bytes. They are not source adoption
and not review pins.

The merged cleanup owner is
`release_records.ReleaseAdmission.reconstruct_original_for_cleanup`. Its callers
are `adapters/adapter_context.py`, and through
`ci_original_for_cleanup_checked` the configured model
(`core/next_fixture_model.source-only.py`: `_reconstruct_original_seal`,
`_ci_reconstruct_original`), `core/next_fixture_execution.source-only.py` and
`core/partial_setup_cleanup_inventory.source-only.py`. It runs `attempt_binding_checked`,
`_base_external`, `verify_external('attemptTrustBinding', ...)` and
`_observe(action_at, historical=True)`, under the five-second verifier checks in
`release_records.py` (`external-verifier-five-seconds`, `stream-five-second-verifier`).
Those merged checks compare elapsed time after each call returns. The successor's
`root-authority:native-five-second-deadline` label does not exist in merged source,
so how the successor enforces the deadline inside the native read is unknown here.

### Not present in the repository

The issue's frozen local controller successor is **absent**:

| Retained baseline (from the issue) | In the repository |
|---|---|
| CODE SHA-256 `c67bace64d218e5f0638c5973818d50e3aa1eaf9465ecf6c95416638cf84f30b`, 219 source entries | No. Merged manifest has 148 entries. |
| API index SHA-256 `aae4b3b73cd13d58e3981d9639923840e2e6f1831b4c348ff2bf02a448f0bc9c` | No. |
| `register_phase_prelaunch`, `original_bound_checked`, `native-five-second-deadline` | No match anywhere under `scripts/ci/release-admission/`. |
| Record/transport data optimization (22 parity cases) | No. |
| Retained handoff of 2026-10-26T09:29:58Z (12 artifacts, 50 evidence files) and its verifier | No portable location. |
| Historical archive named by `HISTORICAL-ARCHIVE-PINS.json` | No. Not in the repository. |

So the failing path `register_phase_prelaunch -> original_bound_checked ->
reconstruct_original_for_cleanup -> verify_record -> historical originals -> bounded
native read` cannot be reached from merged source: its first two frames exist only
in the frozen successor. Which local repairs are reviewed, committed or
unqualified cannot be stated from the repository. The issue records them as
frozen and unqualified, and nothing here upgrades that.

The hashes in the issue identify the retained baseline. They are not source
adoption, and no public artifact location is invented for them.

## What was checked on merged source

- `run_checks.py --unit` passes in a fresh virtual environment: 110 tests, 20.4 s,
  run through the shared job queue (job 4951). This is the finite suite CI runs in
  the lint job. It establishes source integrity of the merged package only.
- `--qualify` was **not** run. It requires the historical archive and the reviewed
  Node executable, and a mismatching environment would produce a misleading result.

Environment gaps for a `--qualify` run on the macOS developer machine used for this
record:

- `HIKYO_158_ARCHIVE_ROOT` (the exact retained archive) is unset and the archive is
  not on that machine.
- `HIKYO_158_NODE_PATH` must name the reviewed Node 26.7.0 executable; the adapter
  checks its digest (`NODE_SHA` in `adapters/runtime_binding.py`). The default Node
  there is 24.21.0.
- The README records a post-fork SQLite crash with Python 3.14.4 linked to Apple's
  system SQLite and a checked environment of Python 3.14.9 with SQLite 3.54.1. The
  default Python there is 3.14.4.

## Acceptance status

| # | Item | Status |
|---|---|---|
| 1 | Portable source/evidence inventory | Done by this record, for the repository side. The retained-packet side needs the owner packet. |
| 2 | Reproduce or characterize the provision-registration failure at its owner boundary | **Partly blocked.** Reproducing needs the frozen successor (its failing frames, process/session handles, retained exit status and stage evidence are not in the repository). Describing the merged-source boundary is possible and is recorded above (owner, callers, deadline difference); a timed run of the merged cleanup path needs the archive, because the assembled pipeline refuses without it. |
| 3 | Repair the measured cause at the canonical owner | Blocked on 2. A repair without a measured cause would guess. |
| 4 | Unprofiled affected-owner component and 23 dependent controls on one frozen successor | Blocked on the frozen successor. |
| 5 | Startup-to-result lifecycle and original-cleanup aggregate | Blocked on 4 and the archive. |
| 6 | Cleanup after main movement or release withdrawal | Blocked on 4 and the archive. |
| 7 | Coherent packet binding source/API/delivery, receipts, review, invocation | Blocked on 4 to 6. Later identity, inventory, pending-run and authorization fields stay unbound. |
| 8 | Bounded independent Standards and Spec review of the final composition | Not started. Round 1 of three was used at the 26 October handoff. This record's review is a documentation review and does not use or reset a composition round. |
| 9 | Preserve implementation, document operator workflow | Operator workflow already exists in `scripts/ci/release-admission/README.md` and `docs/development/release-admission.md`. The successor's workflow documentation waits on 4 to 6. |

## What the owner must supply to continue

Only items 1 and 2 need the owner. Items 3 and 4 are environment setup that an
agent can do once the archive exists: the Node digest is pinned in the repository
and Python 3.14.9 with SQLite 3.54.1 is installable. They are listed so the run
environment is not left to chance.

1. The retained completion packet captured at `2026-10-26T09:29:58Z`, including the
   219-entry successor source and its read-only verifier, and separately the
   operator packet holding the 39 retained missing-binding diagnostics, at a path
   the working machine can read.
2. The exact historical archive root for `HIKYO_158_ARCHIVE_ROOT`.
3. The reviewed Node 26.7.0 executable, matching the pinned digest (agent-provisionable).
4. A validated Python 3.14.9 linked to SQLite 3.54.1 (agent-provisionable).

None of these grants provider, spending, source-adoption, release or deployment
authority. Local qualification needs no new AWS fixture or renewed provider access.
If any item later proves to need a live provider run, that item stops and lists
the exact resource and authority for the owner.

## Boundaries to preserve when restarting

- Do not relaunch an unchanged consumed attempt. Establish the failing boundary
  with a minimized diagnostic before choosing a repair.
- Keep the five-second source bounds, the setup-inclusive 180-second target, the
  five-second termination grace and the one-attempt rules. A contract change needs
  its own owner decision and review, never a longer timeout to reach green.
- Keep fresh native-original and source authentication on every admission. Do not
  cache review or action verdicts, substitute internal owners or add fallback
  authority.
- Treat the 83,219,694-call reservation profile as a measurement of repeated work,
  with profiler overhead not isolated and cumulative times overlapping. It is not a
  promised speedup, and the 39 diagnostics are not 39 independent defects.
