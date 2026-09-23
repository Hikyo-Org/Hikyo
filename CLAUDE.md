See [AGENTS.md](./AGENTS.md) for the conventions in this repository.

In particular: **every commit must be DCO signed-off (`git commit -s`) — the
`validation / preflight` CI job enforces it before a PR can merge.**

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships. Only `graphify-out/README.md` is committed; the graph is generated locally.

Rules:
- On a fresh clone or worktree `graphify-out/graph.json` is absent — run `graphify update .` once to materialize it (free, AST-only). Community names come out as mechanical hub names; run `graphify label` (LLM) only if you want prose names.
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- Check `built_at_commit` before treating graph relationships as current. The graph is navigational evidence; verify security-sensitive and runtime claims against source.
- After modifying application code, run `graphify update .` to refresh structural relationships (AST-only, no API cost). Documentation and image semantics need a skill-driven `/graphify --update` pass.
- See [graphify-out/README.md](graphify-out/README.md) for installation and why only the README is tracked in Git.
