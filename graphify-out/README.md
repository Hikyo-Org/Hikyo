# Repository knowledge graph

[Interactive graph](graph.html) | [Audit report](GRAPH_REPORT.md) | [Graph JSON](graph.json) | [Coverage](COVERAGE.json)

This snapshot maps application source at commit
`106bc87c22c20ea8e3d084a4f35d06f8ffa09920`. The agent integration files added
alongside it are not part of that source snapshot. It contains 35,330 nodes,
100,989 edges, and 1,281 labeled communities, produced with Graphify 0.9.61.

The HTML is a community overview; individual nodes and source locations remain
in `graph.json`. Serve the directory locally to explore it:

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

## Updating and committing

Run `graphify update .` after application-code changes for an AST-only refresh.
For changed document/image semantics, use the installed skill's
`/graphify . --update` workflow. A fresh clone has no extraction cache or local
manifest, so its first update may process more files. Check coverage, labels,
source revision, and output before committing the refreshed snapshot.

Keep the graph, HTML, report, community labels, coverage, health diagnostics,
and benchmark together. The graph JSON is compacted without dropping fields
to reduce repository size. Local interpreter/root paths, manifests, caches,
token-accounting files, vocabulary, query memory, reflections, and browser
session evidence are ignored. Do not force-add them.

Graphify can regenerate the HTML with `graphify export html`. This snapshot
disables vis-network's improved-layout pass because it could not position the
large community graph; force-directed physics still positions the graph.

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
