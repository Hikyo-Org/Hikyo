# #153: Repository and CI secret scanning

Status: implemented on `claude/awesome-dijkstra-9slvj8`.

## ADR gate

The work needed two declared amendments, and both are written as banners on
the ADRs they amend:

- [api-cli-surface.md](../adr/api-cli-surface.md): the closed v1 verb set gains
  one client-local verb, `scan`. It follows the `definitions scaffold`
  precedent: no context, no session, no request. Its exit codes are a
  verb-scoped contract on the `definitions check` precedent.
- [secret-scanning.md](../adr/secret-scanning.md): §7's rejection of a CLI
  pre-check of *server writes* stands. Repository scanning is a different act
  on different content, and it runs the same compiled ruleset. §8's "no
  operator custom rules" holds, so **the ticket's custom-rules acceptance item
  is out of scope**. The configuration only narrows: path and rule exclusions,
  and fingerprint suppressions. At least one rule must remain.

[ops-spec.md](../adr/ops-spec.md) row 15a records the local bounds.
[api-cli-spellings.md](../spec/api-cli-spellings.md) records the spellings, the
`hikyo.scan/v1` JSON shape and the configuration grammar. The user guide is
`docs/site/.../repository-scanning.mdx`.

## Shape

- `internal/scanning`: a single addition, `(*Ruleset).Subset(ids)`. It narrows
  the compiled ruleset without changing rules, digests or the snapshot. It
  refuses an unknown, duplicate or empty selection. There is still no hash
  import (the SS4 boundary test still covers the package).
- `internal/repscan` (new): the engine.
  - `Run` selects a mode (`paths`, `staged`, `unstaged`, `history`, `range`),
    loads the config, narrows the ruleset, and scans under a `budget`.
  - `detect.go`: the ruleset's own whole-content verdict decides *which*
    rules matched. Localization only assigns lines. It re-runs the single
    matched rule (a `Subset`) over line-aligned windows: a binary search for
    the shortest matching prefix (the end line), then for the latest start
    line. Windows align to line boundaries, so boundary-sensitive rules
    (`\b`, `$`) see the same neighbouring bytes as in the whole content. A
    rule that matches but cannot be localized is reported with line `0` and
    never dropped.
  - Fingerprint: `hex(sha256(digest ‖ 0 ‖ path ‖ 0 ‖ line ‖ 0 ‖ scope))`, where
    scope is the commit id or `worktree` (the working tree and the index share
    it). It contains no matched bytes. It lives here, not in
    `internal/scanning`, because of the SS4 hash-import ban.
  - `git.go`: shells out to `git` with `--no-optional-locks`,
    `core.fsmonitor=false`, `core.quotepath=off` and
    `GIT_TERMINAL_PROMPT=0`. It reads blob contents only (`cat-file
    --batch`, and oversize blobs are drained, never buffered). History uses
    `rev-list --topo-order --reverse` (bounded while streaming), then
    `diff-tree --stdin -r -z --root -m --no-renames --diff-filter=AMT`. Each
    path and blob pair is reported once, against the first commit that
    introduced it, so a merge does not report it again. The range is passed
    after `--end-of-options` and must not look like an option.
  - `content.go`: `readWithin` reads through an `os.Root`. It Lstat-refuses
    symlinks, re-checks the opened descriptor (regular file, `SameFile`), and
    reads one byte past the budget so growth is counted as oversize, never
    truncated. A root the user names (or the working directory) is resolved
    to its physical path first, so a symlinked checkout is scanned instead of
    being skipped as one symlink and reported clean. An untracked embedded
    repository (`dir/` from `ls-files`) is counted and never descended. `classify` skips archives (by extension and magic) and binaries
    (a NUL in the first 8 KiB).
  - `config.go`: BurntSushi TOML with `Undecoded()` refusal. `version = 1` is
    required. Globs are repository-relative, with no `..` and no absolute
    paths, and `**` must be a whole segment. Suppressions need a reason, and
    the optional `expires` is an inclusive TOML date.
  - `sarif.go`: a hand-rolled SARIF 2.1.0 encoder. Each rule carries
    `properties.semanticDigest`, and each result carries
    `partialFingerprints["hikyo/v1"]`. It has no snippet, no column and no
    content-derived message, and URIs are percent-encoded.
- `internal/cli/scanlocal.go`: the verb. It maps `repscan.Kind` to 2, 4 or 6,
  returns `silentExit{1}` for unsuppressed findings, and quotes untrusted file
  names in table output.
- `cmd/hikyo/main.go`: `scan` is excluded from the pre-dispatch update check.
  It runs in pre-commit hooks and CI, and must stay offline and prompt-free.
- Wire registry: `cli:scan` is `ClassUnauthenticated` (client-local), and the
  auth-kind table lists `scan` as unauthenticated.
- CI: `scan-xplat` runs `go test ./internal/scanning/... ./internal/repscan`
  on `macos-latest` and `windows-latest`. It is an **indirect** gate:
  `test_core` needs it, so a failure skips `test_core` and `ci-required`
  reports the mismatch. The trusted base-branch checker compares
  `ci-required.needs` against the base registry, so this avoids editing
  `needs` in the same PR. The cache/runner policy fixture allows exactly this
  job's closed OS matrix and nothing else.

## Decisions taken against the handoff comment

- `--reveal` was not built. A finding carries no match text by construction,
  and printing the line would be a new plaintext path for no gain over opening
  `path:line`.
- `--ci` was not built. Exit codes and output are identical everywhere, and
  there is no colour or prompt to disable, so a no-op flag would only suggest
  otherwise.
- Every scanner failure maps to `4`, not `1`, so `1` means findings and nothing
  else.
- A file over 1 MiB is skipped and counted (not a refusal). Exceeding the file
  count, total bytes, findings, commits or wall-clock budget refuses the scan.
- SARIF is validated structurally in `TestSARIFShape`. The 2.1.0 JSON schema is
  not vendored, because validating against it needs a validator dependency.

## Validation

- `go build ./... && go vet ./...` and `gofmt -l` on the touched packages: clean.
- `go test ./internal/repscan ./internal/scanning/... ./internal/cli
  ./internal/authz ./internal/boundary ./cmd/hikyo ./scripts/ci`: passed.
- `go test ./internal/isolation -run 'TestInvariant01|TestInvariant02|Scanning'`:
  passed.
- `scripts/ci/check-cache-policy_test.sh`, `check-required-jobs_test.sh`,
  `classify-changed-paths_test.sh`, `analysis-shards_test.sh`: passed.
