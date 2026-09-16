# Repository knowledge graph

Only this README is tracked in Git. The graph itself (`graph.json`,
`graph.html`, `GRAPH_REPORT.md`, community labels, coverage, health, benchmark)
is a large, non-reproducible derived artifact and is **not** committed —
`graph.json` alone is ~53 MB and a clean rebuild never reproduces the same
community numbering, so pinning it in history bought unreviewable 50 MB+ diffs
with no stable source of truth. Regenerate it locally instead:

```sh
graphify update .
```

This is AST-only and free (no LLM). It works from a fresh checkout with no
graph present. Community names come out as mechanical hub names (e.g.
`Checkbox.stories.tsx`); run `graphify label` (LLM, needs an API key) if you
want prose names — they do not survive a rebuild, so there is nothing to
commit. The HTML is a community overview; individual nodes and source
locations remain in `graph.json`. Serve the directory locally to explore it:

```sh
python3 -m http.server 8766 --bind 127.0.0.1 --directory graphify-out
```

Open `http://127.0.0.1:8766/graph.html`. Search for a community, such as
`Authorization Proof Verification`, then select the result. The checkboxes
control visible communities.

## Agent setup

Install Graphify and make its executable available on the agent process's PATH:

```sh
uv tool install 'graphifyy[sql]==0.9.61'
graphify --help
```

Install the Graphify skill separately for your agent. `AGENTS.md` and `CLAUDE.md`
describe query-first navigation. `.claude/settings.json` registers the native
search/read guards. OpenCode automatically discovers
`.opencode/plugins/graphify.js`; no additional plugin registration is needed.

```sh
graphify query "authorization proof" --budget 1500
graphify path "Proof" "DB"
graphify explain "OrgID"
```

The CLI prints truncation notices when the budget omits results. Narrow the
question or raise the budget before drawing conclusions from an incomplete result.

## Generating and updating

Run `graphify update .` after application-code changes for an AST-only refresh.
For changed document/image semantics, use the installed skill's
`/graphify . --update` workflow. A fresh clone has no extraction cache or local
manifest, so its first update processes every file and takes longer.

Nothing here is committed except this README, so there is no snapshot to keep
in sync and no diff to review — `.gitignore` allowlists only
`graphify-out/README.md`; everything else the tool writes (graph, HTML, report,
labels, coverage, health, benchmark, caches, manifests, session evidence) is
ignored. Do not force-add any of it.

Graphify can regenerate the HTML with `graphify export html`. It disables
vis-network's improved-layout pass because it could not position the large
community graph; force-directed physics still positions the graph.

## Limits

The graph is undirected. Documentary citations and shared test references do
not establish runtime calls. Seventeen Astro files have parser recovery gaps;
five sensitive-name matches and 62 unsupported files were not extracted.
Document semantics cover selected substantive passages, not every clause.
See [health diagnostics](GRAPH_HEALTH.txt) for unresolved endpoints and collapsed
parallel relationships.

[BENCHMARK.json](BENCHMARK.json) records a heuristic context-size comparison
using the detector's word count. It does not measure billing, latency, or answer
quality. Host-agent token usage was not measured.
