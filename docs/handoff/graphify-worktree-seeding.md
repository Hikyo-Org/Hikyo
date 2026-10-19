# Graphify worktree seeding

Pull request: [Hikyo #1007](https://github.com/Hikyo-Org/Hikyo/pull/1007).

## Delivered behavior

The Graphify CLI was upgraded from 0.9.61 to 0.9.92, retaining the installed
OpenAI and SQL extras. Its startup refreshed installed agent skills and the
OpenCode reminder plugin. The repository setup pin now names 0.9.92.

`scripts/dev/graphify_worktree.py` builds a reusable seed from committed
`hikyo-upstream/main` in a temporary detached worktree. Seed identity includes
the commit, helper schema, Graphify version, AST schema, and parser fingerprint.
Seed storage is under `hikyo-graphify/` in the common Git directory, outside the
tracked tree. A shared lock serializes seed publication and bootstrap.
Successful publication or reuse retains the selected seed and newest predecessor,
removing superseded helper-owned snapshots under that lock.

Fresh worktrees receive independent graph/cache/manifest/label copies. APFS
clones are attempted on macOS, with normal copies as the portable fallback.
The scan root is reset before a worktree-local update. Existing graphs are
updated in place, preserving local semantic extraction. The helper rejects
symlinked output state and does not copy process markers or session memory.
Graphify's existing shrink guard stays enabled; failures exit nonzero.
SIGTERM/SIGHUP unwind cleanup and stop the Graphify child gracefully. Catchable
interruptions roll back partial worktree-sidecar publication. Uncatchable SIGKILL
recovery is documented in the setup README.
On branch `main`, bootstrap refreshes only the shared seed and never creates a
private checkout graph. After canonical-main is fetched, the next fresh worktree
automatically refreshes the seed for the new commit. Existing worktrees keep
their own graphs and refresh them in place.

The helper installs a standalone copy in the common Git directory. The local
Hikyo T3 project now has a synchronous script named **Graphify: seed worktree
graph**, with **Run on worktree create** enabled. Older branches can therefore
bootstrap without containing the new repository script. Re-run `install` when
the helper changes. No Git hook or signing setting was changed.

## Operator commands

From a checkout containing the helper:

```sh
python3 scripts/dev/graphify_worktree.py install
python3 scripts/dev/graphify_worktree.py bootstrap
```

To select newer canonical-main source explicitly:

```sh
git fetch hikyo-upstream main
python3 scripts/dev/graphify_worktree.py refresh-base
```

The installed command used by T3 is:

```sh
python3 "$(git rev-parse --path-format=absolute --git-common-dir)/hikyo-graphify/bootstrap.py" bootstrap
```

The helper queues heavy work with `agent-heavy` when available and avoids nested
queue submission. It does not fetch on its own. See `graphify-out/README.md` for
requirements, base-ref selection, querying, and the cache lifecycle.

## Validation

- Eighteen focused unit tests passed, including retention, cancellation during
  checkout creation and graph updates, cleanup of late Git registration,
  interrupted publication, the main-checkout boundary, cache isolation, marker filtering,
  version invalidation, existing-state preservation, symlink rejection, and
  failed-build cleanup.
- Ruff lint, Ruff format, Python compilation, OpenCode plugin syntax, and diff
  whitespace checks passed.
- A seed was built from canonical commit
  `f3581e7bb6ec65cb416d5213f7e55819ef19782f` with Graphify 0.9.92.
- In a fresh detached Hikyo worktree, bootstrap reused that seed and reduced
  uncached AST extraction from 4,198 files to 742. Assertions verified a new
  Python symbol, an edited Go symbol, removal of a deleted Go source, preservation
  of the tracked README, independent graph inodes, a local scan root, and an
  unchanged Go cache hit. Worktree-only symbols were absent from the seed.
- The main checkout's branch and its pre-existing uncommitted files were left
  untouched. Temporary test/build worktrees were removed.
- T3 script settings were written and read back through the app API. A new T3
  conversation was not created to exercise the app's worktree-creation callback;
  the installed bootstrap was exercised directly in a fresh Git worktree.
- The exact saved T3 shell command also passed against the existing implementation
  worktree, updating its graph in place. The resulting graph indexes the helper
  and records Graphify 0.9.92 and the expected commit.

## Limits and delivery scope

Graphify still scans and resolves the corpus, clusters it, and writes the full
result. Version 0.9.92 deliberately bypasses AST cache reuse for JavaScript,
TypeScript, and related component files. The observed benefit is reduced parsing,
not a measured total-runtime guarantee or a base-plus-diff storage format.

Code-only updates disable LLM backends and automatic integration rewriting.
Document/image semantics remain a separate explicit skill workflow. Cache/runtime
incompatibilities invalidate seed reuse rather than silently mixing versions.

Implementation and local T3 setup are complete. The authorized delivery is a
cryptographically signed and DCO-signed-off commit, push, and unmerged pull
request. The pull request is open; hosted checks must be evaluated on its exact
current head separately from the local validation above. Merge, release, and
deployment remain outside this delivery.
