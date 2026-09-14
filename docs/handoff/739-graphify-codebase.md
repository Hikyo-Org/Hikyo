# PR #739: Graphify agent integration and repository map

PR: https://github.com/Hikyo-Org/Hikyo/pull/739

Entry point: [repository knowledge graph](../../graphify-out/README.md).

## Delivered behavior

`AGENTS.md` and `CLAUDE.md` direct codebase questions to the saved graph first.
Claude's search/read hooks find `graphify` through PATH. OpenCode automatically
loads `.opencode/plugins/graphify.js`, which adds one reminder before the first
bash command when a graph exists, preserving the original command.

The snapshot maps application source at
`106bc87c22c20ea8e3d084a4f35d06f8ffa09920`, before this agent-integration PR.
It contains 35,330 nodes, 100,989 edges, and 1,281 named communities. The HTML
uses the aggregated community view. The graph JSON is compacted without
discarding fields and marked generated for GitHub review.

## Repository boundary

Commit the graph, HTML, report, labels, coverage, diagnostics, and benchmark
together. `.gitignore` excludes local manifests, extraction caches, interpreter
and root-path sidecars, cost tracking, vocabulary, session memory/reflections,
and browser-session evidence. The globally installed skills are user tooling,
not vendored repository dependencies.

Graphify 0.9.61 with the SQL extra produced this snapshot. Read the graph README
for installation and refresh commands. Refresh application-code structure with
`graphify update .`; changed document/image semantics require the installed
skill's `/graphify . --update` workflow.

## Validation and limitations

- Node uniqueness, edge endpoint integrity, and all 1,281 community labels passed.
- Claude guards executed successfully. OpenCode syntax and hook behavior passed
  for absent graphs, non-bash tools, one-time reminders, and command preservation.
- Browser rendering, search, selection, and hide/restore filters passed. CLI
  query smoke checks passed; budget truncation is reported by the CLI.
- The audit records 17 Astro parser gaps, five detector sensitive-name exclusions,
  62 unsupported files, 11,651 unresolved raw edges, and 2,397 collapsed undirected
  edge variants. Documentary references do not prove runtime calls.
- Cross-provider review was skipped by the quota gate: Claude weekly remaining
  28%, model-specific Fable remaining 0%. No CLEAN verdict is claimed.

The benchmark compares estimated context sizes, not actual model charges,
latency, or answer quality. Host-agent token consumption was not measured.

User authorization covers commit, push, and PR creation. Merge remains a
separate decision. Check the PR's current checks and head before further work.
