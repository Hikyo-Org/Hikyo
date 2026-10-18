# Stable installer and updater safety: #979 and #980

Scope: stable archive delivery and stable verification-state advancement.
Implementation baseline: `cb78b6be111029335e744aa3e4dcc956d5194ba1`.
Implementation commit: `fe46f23b7ecc86e7edd9dfbd488faeaad7f7613e`.
PR: [#991](https://github.com/Hikyo-Org/Hikyo/pull/991), linked to the
implementation and parent coordination threads.
The live issues were open with no comments and no matching open PR when work
started on 2026-10-18. This work authorizes a PR only. Merge, auto-merge, enqueueing, release and deployment remain on hold.

## #979: validate authenticated archive members

The stable installer verifies the signed bundle before decompressing or
inspecting the selected archive. It now bounds decompression with the shell's
file-size limit, accepts an uncompressed tar stream of at most 512 MiB, and
bounds member-list output. The file-size ceiling permits at most 1 GiB of
intermediate decompression across the supported 512/1024-byte shell block
conventions. Inventory validation requires one exact `hikyo` member, at most
10,000 member names, and rejects unsafe or ambiguous path spellings.

GNU/BSD tar metadata must identify the selected member as a nonempty regular
executable of at most 256 MiB. Links, special files and duplicate selected
members fail. Extraction writes only the selected payload through stdout to a
private regular file; the archive never chooses an extraction destination.
Publication retains the existing same-directory temporary file and atomic
rename. The existing installer/updater lock gap and platform-wide `sync`
limitation remain unchanged.

The pre-fix fixture accepted a selected symlink. New harmless synthetic archive
fixtures cover symlink, hardlink, duplicate, empty, nonexecutable, directory,
FIFO, oversized member, oversized gzip expansion, malformed gzip/tar and
truncation. Every refusal checks that the installed executable is byte-identical
and no staged publication occurred. A verifier-failure case proves that
unauthenticated bytes never reach decompression. The fixture records `sync`
without flushing unrelated host workloads.

## #980: preserve historical keyed updates with authenticated catalog evidence

The [stable workflow signing ADR](../adr/stable-workflow-signing.md) retains
historical primary-key range and revocation semantics. Accordingly, keyed
updates remain supported. They now fetch and authenticate the current catalog
and its signature with the shared snapshot verifier. The catalog must bind the
exact already-authenticated metadata bytes. If it binds a stable delegation,
that delegation and its pinned trust material must also verify.

Both keyed and workflow paths advance state from the authenticated snapshot
floor using the same decoder and monotonic comparison. The unreachable
variadic branch and the metadata-only canonical branch that reused a remembered
catalog digest are removed. Successful legacy migration writes canonical state.
Catalog, metadata and highest-release floors cannot decrease; equal-height
conflicting digests fail. Failure leaves state bytes unchanged.

Regression fixtures include legacy state with zero/nonzero catalog floors,
canonical state, valid keyed updates, workflow migration, catalog/metadata
rollback and equivocation, highest-release rollback, invalid catalog signature,
unbound catalog and missing catalog. Both legacy and canonical states exercise
catalog-authentication failures. Existing promoted-stable installation coverage
now supplies a real test-key-signed catalog.

If current trust files are temporarily inconsistent or older than locally
remembered evidence, retain the state file and retry after matching authenticated
metadata/catalog become available. Missing or invalid catalog evidence is an
intentional refusal, never permission to copy a remembered digest, lower a
floor, delete state, or bypass revocation. Recovery beyond retry follows the
existing authenticated signing/rebootstrap procedures.

## Validation

Passed on the implementation:

- `GOMAXPROCS=2 go test -p 2 ./internal/selfupdate ./internal/releasetrust ./internal/releaseidentity -count=1`
- `GOMAXPROCS=2 go vet -p 2 ./internal/selfupdate ./internal/releasetrust ./internal/releaseidentity`
- ShellCheck 0.11.0 on `install/install.sh.in` and
  `scripts/release/render-installer_test.sh`.
- Installer fixtures with macOS BSD tar 3.5.3 and GNU tar 1.35, including the
  added refusal and verification-ordering checks.
- Repository-wide pinned goimports 0.50.0 check passed for 2,034 handwritten
  files; changed Go files pass gofmt.
- `git diff --check`.

The first repository analyzer run (`go test -p 2 ./internal/lint -run 'Repo$'
-count=1`, with `GOMAXPROCS=2`) timed out after ten minutes in
`TestProofSignaturesRepo/default`, waiting on the `go/packages` driver. The
repository compiler-cache preparation completed its default context in 647.2
seconds, then was gracefully interrupted in the UI context at the user
resource-coordination boundary. Its processes exited and logs/cache were
preserved. The analyzer retry and static isolation invariants are queued as a
finite sequential batch through the host-wide validation queue, without a
nested manual mutex. The batch uses `GOMAXPROCS=2`, `GOMEMLIMIT=512MiB`,
`GOGC=50` and Go parallelism two. Queued work is not a passing result. The first
clustered graph refresh was
stopped after AST extraction stalled; bounded structural retries also timed
out (120/240 seconds). The AST pass reported 17 pre-existing Astro syntax
warnings and no completed graph was produced. Source inspection, rather than
graph output, supports this change.

No database, query, Go generator input, generated API or storage-engine behavior
changed. Existing Go generated-output paths have no diff; Go generators were not
rerun because their inputs were unchanged. Installer fixtures render and execute
the changed installer template. No
PostgreSQL integration, full repository suite, race suite, native Linux runtime,
privileged installer, production release, provider acceptance or performance
benchmark is claimed. The installer delivery fixtures stub network and bundle
verification; actual trust signatures are exercised separately by the Go suites.
Exact implementation-head Go/frontend feedback passed. Full-suite merge proof
and maintainer security review remain required before any later merge decision.
An unexpected merge-queue entry was observed and removed at
2026-10-18 19:00:55 UTC to enforce the hold. A fresh GitHub read showed the PR
open with both `mergeQueueEntry` and `autoMergeRequest` null. The enqueue
timeline names account `Dunky13` but does not identify the initiating session.
CodeRabbit reported a rate limit despite its success status, Qodo was
billing-blocked, and MergeWatch reported action-required. There were no review
threads at inspection. Cross-provider review was not run; it is not CLEAN.
