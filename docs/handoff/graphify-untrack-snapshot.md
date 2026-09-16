# Handoff: stop tracking the graphify snapshot in Git

PR #754. Reverses the earlier decision to commit the extracted knowledge graph.
Now **only `graphify-out/README.md` is tracked**; everything else graphify
writes is generated locally and gitignored.

## What changed

- `.gitignore`: the `graphify-out/` allowlist collapsed from ten `!` entries to
  one (`README.md`).
- `git rm --cached` for the eight previously tracked derived files: `graph.json`,
  `graph.html`, `GRAPH_REPORT.md`, `.graphify_labels.json`, `GRAPH_HEALTH.json`,
  `GRAPH_HEALTH.txt`, `COVERAGE.json`, `BENCHMARK.json`. They stay on disk,
  now ignored.
- `graphify-out/README.md`, `AGENTS.md`, `CLAUDE.md`: rewritten to the new policy
  and a fresh-clone bootstrap note.

## Why

`graph.json` is **53 MB** (36k nodes / 102k edges). GitHub soft-warns at 50 MB
and hard-blocks at 100 MB per file, so it was already in the warning band and
growing. Every code PR that ran `graphify update` rewrote the whole 53 MB →
unreviewable 1.6M-line diffs, and `git rm --cached` cannot reclaim the two
blobs already in history — this only stops the bleed.

Two empirical findings settled the design (both verified, not assumed):

1. **The graph is non-reproducible.** A clean `graphify update .` (no cached
   graph) produced 36,954 nodes / 1,305 communities vs the committed
   36,523 / 1,415 — same source, different graph. So the committed blob was not
   a source of truth, just one arbitrary snapshot. Not self-scan pollution:
   `grep -c graphify-out graphify-out/graph.json` = 0 (graphify excludes its own
   out-dir).
2. **Community labels do not survive a rebuild.** `.graphify_labels.json` is
   keyed by integer community ID, and a clean rebuild renumbers communities.
   After a from-scratch rebuild the checkbox cluster was named
   `Checkbox.stories.tsx` / `cx` (mechanical hub names), not the pre-rebuild
   prose `Badge Checkbox Primitives` / `Checkbox Stories`. Since every worktree
   is a fresh checkout, committing the labels file buys nothing cross-clone —
   so it is not tracked either.

Net: the only thing worth committing was the hand-written README.

## Bootstrap (fresh clone / worktree)

```sh
graphify update .          # free, AST-only; works with no graph present
```

Community names come out as mechanical hub names. Run `graphify label` (LLM,
needs an API key) only if you want prose names — they will not survive the next
rebuild, so there is nothing to commit.

The `graphify hook-guard search|read` hooks in `.claude/settings.json` self-check
for `graph.json` and no-op when it is absent, so agents degrade to grep on a
fresh clone rather than erroring.

## Not done here (deliberate)

- History rewrite to purge the two 53 MB blobs already committed — out of scope,
  would need a coordinated force-push. The blobs are inert; they just sit in
  history.
- The graphify non-determinism (node/community counts drift between cached and
  clean runs) is a tool behaviour, not this repo's to fix.
