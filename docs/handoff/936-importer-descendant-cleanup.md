# Issue 936: failed importer helper descendants

## Behavior and ownership

`RunInternalSubprocess` owns cleanup for a failed helper invocation, including
read/wait errors, expired context after waiting, and failed or incomplete output
delivery. It kills the owned process group or job before returning the existing
content-free failure code. Output from an unsuccessful command is not delivered.
Deadline selection, the output cap, and `subprocessWaitDelay` remain unchanged.

Unix ownership is the process group created with `Setpgid`. Successful Unix
helpers retain the existing behavior: background agents may survive. This is
accidental process-lifecycle cleanup, not containment of deliberately daemonized
helpers that leave the group. The runner reaps its direct child; the operating
system's orphan reaper collects terminated grandchildren.

Windows retains its existing policy: `release` closes a
`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` job on both success and failure. The new
explicit error cleanup terminates that job before release. Microsoft documents
that closing the last handle terminates associated processes, and children
created after job association normally inherit membership:
[Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects).
The existing window between `Start` and `AssignProcessToJobObject` remains:
descendants created before adoption are not guaranteed to join. This change
does not claim Windows/Unix parity or resolve that separate Windows limitation.

## Acceptance evidence

- A real `/bin/sh` helper backgrounds a redirected `sleep`, records its PID, and
  exits 7. The original audit regression fails on base `c4c626a10` on macOS arm64
  and Linux arm64 because `kill(pid, 0)` still succeeds after return. The test
  cleans up its own child after recording the failure.
- New Unix regressions cover nonzero exit, output write error, partial write
  error, complete write with error, short write without error, output overflow,
  and successful-helper survival. Cleanup assertions wait at most three seconds
  for the grandchild to disappear. All fixture children have registered cleanup.
- Existing expired-before-start and timeout/grandchild tests remain covered.
- The complete importer suite passes on macOS arm64 and Linux arm64: 110
  top-level tests/fuzz seed groups, zero skips on each platform. This includes
  Kubernetes exec credentials, credential refresh, plugin policy, deadline and
  overflow behavior, and Vault external token-helper sanitization and bounds.
- `go test -race ./internal/importer/... -count=1` and
  `go vet ./internal/importer/...` pass on macOS.
- Windows amd64 importer tests cross-compile with `CGO_ENABLED=0`; Windows
  process lifecycle semantics were inspected in source and official API
  documentation, not executed on a Windows host.

## Reproduction commands

From the repository root:

```sh
go test ./internal/importer/... -count=1 -v
go test -race ./internal/importer/... -count=1
go vet ./internal/importer/...
```

For Linux execution from another host, set `IMPORTER_TEST_BINARY` to a local
output filename, cross-compile, and run the resulting binary on Linux with the
package's `testdata` available as its working directory:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c \
  -o "$IMPORTER_TEST_BINARY" ./internal/importer
```

The recorded Linux run used Docker's `--init` so an orphan reaper existed;
otherwise a container's non-reaping PID 1 can leave killed children as zombies,
which correctly prevents an `ESRCH` assertion from passing. These are real
Linux subprocess tests, not a cross-compilation-only result. Hosted CI and the
protected merge group's full proof remain separate from this local evidence.
