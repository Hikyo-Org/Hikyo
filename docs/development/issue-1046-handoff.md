# Issue 1046 implementation handoff

The repository-owned successor is
[`scripts/ci/release-admission/`](../../scripts/ci/release-admission/README.md).
It accepts independently reviewed release and configuration data through fixed
policy and code. It does not contact AWS or supply a live original authenticator.

## Entry points

- [Operator workflow](../../scripts/ci/release-admission/README.md) provides
  dependency setup, unit checks, archive-backed qualification and custody gates.
- [Authority and lifecycle contract](release-admission.md) resolves withdrawal
  after a Root safety hold and defines recovery without reviving a reservation.
- [Record API](../../scripts/ci/release-admission/RECORDS-API.md) describes the
  nine closed contracts, external originals and current versus historical admission.
- [Collector API](../../scripts/ci/release-admission/COLLECTOR-API.md) describes
  separate release and candidate/main collection with whole-response replay.
- [Adapter API](../../scripts/ci/release-admission/ADAPTERS-API.md) describes
  preparation, holder, native preflight, paired driver and cleanup consumers.

## Preserved evidence

The original R2 design manifest is identified by raw SHA256
`2701b4e6cb391a3b25aa4f4139d8e2979ff30cb6e892f1a6a7aabacf6f69fe7d`.
The original source package manifest is identified by raw SHA256
`05fcccbaef163485ca131fecd7f688ca0349505eef6e0bd17579164154d8b19f`.
The immutable packages remain external and unchanged. The successor records
relative origin names and hashes rather than publishing original owner/native
payloads. Missing originals fail qualification.

The lifecycle amendment was independently reviewed before implementation. It
permits append-only held-to-revoked escalation, retains the original hold, and
requires a new adoption/genesis for recovery. An unchanged standing owner grant
may support Root-only recovery only with a new single-use recovery decision,
complete current stream and no withdrawal. Reauthorization after withdrawal
must explicitly reference the actual withdrawal and prior adoption/approval.

## Integrity and authority

`EXECUTION-MAP.json` and `IMPLEMENTATION-MANIFEST.json` identify static source.
Root supplies independently reviewed raw pins to the loader. Locally recomputing
pins during tests establishes integrity only. Releases do not regenerate source
pins. Composite configuration precedes source manifest and attempt binding;
the original eight-field resource configuration remains separately bound.

The same attempt identity travels through preparation, reserve, holder/driver,
bind, result, finalization and abort. Current status blocks forward work after
withdrawal. Cleanup reconstructs exact original action-time authority and still
requires current resource, owner/MFA, quiescence and survivor safety proof.

`DecisionJournal` persists CAS head, global stream watermark and decision
consumption together. Its native-original checkpoint callback must authenticate
under the lock. Explicit pending recovery retains the inode, verifies unchanged
bytes and syncs file data before publication. Custody owns exclusive write
access to its journal; local storage cannot authenticate an owner decision.

## Validation boundary

The finite unit suite runs in the existing CI lint job and the local feedback
driver without changing the fixed job inventory. Archive-backed qualification
additionally requires the exact historical archive and reviewed Node executable.
The check runner fails if required tests are skipped. The source closure check
detects stale model/consumer pins and omitted or changed executable files.

Synthetic qualification establishes source behavior only. It does not establish
real hosted CI, real AWS test results, genuine current release approval, Root
adoption, resource authorization, deployment approval or protected merge.
Those operational gates remain separate. Cross-provider review is unavailable
and must not be described as clean.
