# Nightly native Apple acceptance transport repair

## Refreshed evidence, 2026-10-30

Canonical main was `4011da3b1c2929124b1b282bc6a3209c01c7e45d` when this
repair began. The latest nightly remained
[41022666494, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/41022666494),
source `45f1608de1fede18fa4f00ca5ba3ec2423e8ca8a`.
Both native Apple jobs failed at `xcrun stapler validate` before installation:
`NSURLErrorDomain Code=-1005`, CloudKit ticket lookup, exit 68. Signing and
notarization completed; native acceptance failed and publication was skipped.
Those outcomes do not establish a passing installation.

The acceptance job called the verifier directly. The separate notarization job
already used the scoped TCP fallback for
[actions/runner-images#14924](https://github.com/actions/runner-images/issues/14924).
The repair invokes the same wrapper around native acceptance on both approved
architectures. It rejects UDP/443 only within the existing child anchor on
disposable hosted runners and releases its own enablement token afterward.
Payload and cleanup failures still fail the job. No retries, timeout changes,
quarantine removal, new credentials or relaxed release gates are introduced.

## Local verification

`TestAppleNativeAcceptanceTicketNetwork` executes the workflow's actual command
with the real wrapper and synthetic privileged tools. Before the repair, the
installation probe reproduces the CloudKit transport refusal. Afterward, both
architecture selections run the installation probe with fallback active,
propagate ticket failure, and fail on cleanup errors. These command fixtures
do not exercise Apple or the host firewall. Existing native-install fixtures
retain signature, ticket, archive equality, architecture, version/help,
quarantine and detach refusal coverage.

From the repository root, run:

```sh
go test -p 2 -parallel 2 -count=1 ./scripts/ci -run 'Apple|Nightly'
scripts/ci/fast-feedback.sh --working-tree --plan
scripts/ci/fast-feedback.sh --working-tree
```

The feedback plan selects checks from the complete diff. Hosted feedback and
native Apple installation of the delivered head are separate gates. A normal
authorized future nightly must establish both native receipts for its exact
finalized artifact. This task does not authorize dispatch, submission or release.

## AWS repair ownership and HOLD

The latest AWS run remained
[40600194878, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/40600194878):
workflow `24e198b32a3ffd8d4013207b2f974d76ec49be6f`, reviewed application
`361e02e3915eef840429d88589c39689f4a58dfb`. Its bounded artifact has 46 terminal
records, 43 passes and three failures. The only failed leaf is PostgreSQL
lifecycle, assertion 227, target conflict. The artifact does not identify the
precise lifecycle point. Evidence JSON SHA-256:
`fc7845e26914f43a0fea943fc8375f9e9d5152c58895e2c42c3c6f689f38e398`.

[PR #969](https://github.com/Hikyo-Org/Hikyo/pull/969) already owns the repair at
`c253ff9b1ccec7408f13366f8c5640c8c7cf3d0a`, tree
`d4332620df1e974470ab0427e6756a5294b64cdd`. It observes the single acknowledged
consent write before draining the queued retry, retaining exact identity and
authorization witnesses, the existing 15-second deadline and three-read bound.
The emulator also models AWSPREVIOUS movement. No AWS changes are duplicated here.
Fresh scoped loopback validation passed 29 terminal isolation records with
SQLite and PostgreSQL 18.6, zero failures/skips, plus the emulator staging test.
These reproduce a concrete fixture defect in the same conflict category; they
do not prove the cause of the hosted failure or current AWS acceptance.

The prior provider attempt is consumed and closed. PR #969 still requires owner
source disposition, a fresh explicit grant for the attempt/resources/reservations
and budget, protected exact-candidate/workflow/run approvals, real expiry and
both-engine acceptance, independent cleanup, current-base CI and the separate
human merge decision. Its current GitHub state was conflicting, with failed
`ci-queue-required` and `ci-required-next`; successful feedback is not merge or
provider proof. See its owning #158 handoff for operational prerequisites.
No provider action, merge, publication or deployment was performed by this repair.
