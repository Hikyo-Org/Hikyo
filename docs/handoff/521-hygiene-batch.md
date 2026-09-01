# Issue 521: hygiene batch

## Outcome

- Split Compose CLI verbs into `compose_run.go`, `compose_render.go`,
  `compose_sync.go`, and `compose_doctor.go`.
- Moved apply-pending and applied-stamp persistence into
  `compose_stamp_state.go`.
- Kept shared stack, delivery, snapshot, cursor, and filesystem helpers in
  `compose.go`; no CLI behavior changed.
- Removed completed landing-page planning residue and tracked Playwright MCP
  captures; future `.playwright-mcp/` output is ignored.
- Replaced the stale pre-implementation marker in `DESIGN.md` with its current
  living-reference role.
- Removed 18 unreachable functions reported by the current pinned analyzer.

## Dead-code audit

The issue's symbol list came from commit `3e45edde`. Re-running the analyzer at
the implementation baseline confirmed that 11 listed functions were still
unreachable; those functions were deleted. The authorization marker remained
intentional. The same pass found seven additional removable functions outside
the audit-era list, which were deleted under the repository's dead-code rule.

The final test-aware analysis reports only these seven intentional seams:

- four `adapter.stubModule` methods used by a compile-time interface assertion;
- `authz.proof.proof`, which closes the proof interface to this package;
- `disclose.fakeTTY.Close`, reached through the test's `io.WriteCloser` seam;
- `operator.WithLeaderElection`, used by the build-tagged kind e2e harness.

Each remaining declaration now carries its justification at the declaration.
Run the pinned analyzer with:

```console
go run golang.org/x/tools/cmd/deadcode@v0.49.0 -test ./...
```

## Verification

Run from repository root:

```console
go build ./...
go test ./...
go run golang.org/x/tools/cmd/deadcode@v0.49.0 -test ./...
git ls-files .playwright-mcp
```

The final command must print nothing.
