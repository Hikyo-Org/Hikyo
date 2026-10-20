# Repository knowledge graph

Only this README is tracked in Git. Graphify's graph, cache, manifest, reports,
and visualizations are ignored local artifacts. The graph is large and community
numbering can change between builds, so generated snapshots do not belong in
code review.

## Install and bootstrap

Install Graphify with the SQL extractor and make it available on PATH:

```sh
uv tool install 'graphifyy[sql]==0.9.92'
python3 scripts/dev/graphify_worktree.py install
python3 scripts/dev/graphify_worktree.py bootstrap
```

Python 3.10+, Git, and a POSIX host are required. The helper uses `agent-heavy` when installed
and runs directly when that queue is unavailable, with an explicit notice.
It does not nest queue jobs. Graphify runs with two AST workers, automatic
integration rewriting disabled, and all LLM backends disabled.

## Reuse the canonical-main graph

The helper builds a seed from the committed `hikyo-upstream/main` revision in
a temporary detached worktree. It leaves the main checkout, its uncommitted
files, and its branch untouched. Refresh the tracking ref when you want a newer
base:

```sh
git fetch hikyo-upstream main
python3 scripts/dev/graphify_worktree.py refresh-base
```

The main checkout has no private graph. Running `bootstrap` on branch `main`
refreshes only the shared seed. Generated graphs and caches stay outside tracked
files. The next new worktree checks the fetched canonical-main revision and
automatically builds a new seed if needed, reusing compatible extraction caches.
Existing worktrees continue updating their own graphs. The helper does not fetch
the remote itself; refresh the tracking ref through normal Git fetching.

`--base-ref` selects another existing ref explicitly when the clone uses a
different remote name. A missing ref is an error; the helper does not fetch or
silently substitute another branch.

The reusable seed lives inside the repository's common Git directory under
`hikyo-graphify/`. Seed identity includes the base commit, Graphify version,
AST cache schema, and parser versions. A lock serializes seed publication and
copies. A changed base reuses compatible extraction caches; a changed Graphify
or parser version gets a new cache. Failed builds do not publish a partial seed.
After a successful build or reuse, only the selected seed and newest predecessor
are retained. Superseded helper-owned snapshots are removed under the same lock.

On a new worktree's first bootstrap, the helper copies the seed's graph,
extraction cache, manifest, and relevant sidecars into `graphify-out/`, resets
its saved scan root, then updates the worktree's graph. Copies are independent;
macOS uses filesystem clones when available, with ordinary copies otherwise.
Worktrees never share writable files with the seed or one another. Process
locks, pending requests, and session memory are not copied.

Run bootstrap again after source changes, pulls, rebases, or branch switches.
An existing graph is updated in place rather than replaced by the seed, so its
local semantic extraction is retained. Graphify reuses cached extraction for
unchanged files and handles additions, edits, and deletions. It still scans
files, resolves relationships, clusters, and writes a complete resulting graph;
this is not a base-plus-diff graph storage format.

Graphify 0.9.92 deliberately bypasses its AST cache for JavaScript, TypeScript,
and related component files. Those files are reparsed even when unchanged;
Go and other cacheable languages still benefit from the copied cache.

## T3 worktree setup

The `install` command copies the helper into the common Git directory so it is
available even on branches that predate this script. Re-run it after changing
the helper. Configure this command as a synchronous T3 project script with
**Run on worktree create** enabled:

```sh
python3 "$(git rev-parse --path-format=absolute --git-common-dir)/hikyo-graphify/bootstrap.py" bootstrap
```

Worktree setup completes after the private graph has been refreshed. No Git
hooks are installed, and signature verification hooks remain in place.

SIGTERM and SIGHUP unwind the helper and let Graphify stop its workers before
temporary-checkout cleanup. Catchable interruptions roll back partially installed
worktree sidecars. SIGKILL cannot run cleanup. If it leaves a helper-owned
`hikyo-graphify/build-*/checkout` registered in `git worktree list`, verify that
the build is no longer running before removing that specific abandoned checkout
with `git worktree remove --force <checkout>`.

## Query and refresh

```sh
graphify query "authorization proof" --budget 1500
graphify path "Proof" "DB"
graphify explain "OrgID"
python3 scripts/dev/graphify_worktree.py bootstrap
```

`AGENTS.md` and `CLAUDE.md` guide agents toward graph queries. Claude's
`.claude/settings.json` search/read guards and OpenCode's
`.opencode/plugins/graphify.js` add reminders. These reminders query the current
worktree's `graphify-out/graph.json`; they do not build the seed themselves.

Code extraction is local and does not call an LLM. Document and image semantics
need a separate skill-driven `/graphify . --update` pass. Community labels from
code-only builds are mechanical names; optional `graphify label` uses an LLM.

Check `built_at_commit` before treating graph relationships as current. That
stamp alone does not establish freshness for uncommitted edits. Check truncation
notices and narrow queries before drawing conclusions from omitted results.
Verify important relationships against source: the graph is navigation evidence,
not runtime proof.
