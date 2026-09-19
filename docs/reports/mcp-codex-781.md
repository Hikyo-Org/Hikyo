# Native Codex and T3 MCP evidence (#781)

[Connection instructions](../site/src/content/docs/docs/mcp.mdx#codex-and-t3) · [Issue #781](https://github.com/Hikyo-Org/Hikyo/issues/781) · [Transport decision](../adr/mcp-server.md)

Verified 19 September 2026, against this change's `/mcp/codex` handler and
production tool registrations. All credentials and tenant data were disposable.
No production deployment, model turn or provider request was used.

## Results

| Client/path | SQLite | PostgreSQL 18 | Verified behavior |
| --- | --- | --- | --- |
| Native `codex-cli 0.155.0`, direct `codex app-server` | PASS | PASS | Initialization, seven tools, authorized definition read, validate, stage, audit, no publish |
| T3 `effect-codex-app-server` client library driving native Codex | PASS | PASS | Same assertions through T3's actual subprocess transport and initialization profile |
| Modern MCP `2026-07-28` conformance suite | PASS | N/A | Existing pinned baseline retained |
| Both wire profiles, domain/security suite | PASS | PASS | Wrong-scope and invalid/revoked-token refusal, redaction, audit origin, inert drafts, cancellation |

The native test calls `initialize`, `thread/start`, `mcpServerStatus/list`, and
`mcpServer/tool/call` on the actual Codex app-server. Codex itself performs the
HTTP MCP handshake and tool invocation. It does not substitute an HTTP client
for Codex. The test creates a fresh Codex home, stores only an environment
variable name in TOML, and passes the disposable bearer through that variable.

The assertions require exactly one caller-owned pending draft after stage,
zero drafts after validation, one `value.staged` and one
`value.change_validated` event with `origin=mcp`, and no MCP publication.

## Reproduce standalone Codex

With Codex installed on PATH:

```sh
HIKYO_TEST_CODEX_NATIVE=1 go test ./internal/isolation \
  -run '^TestMCPCodexNative$' -count=1 -v
```

Set `HIKYO_TEST_POSTGRES_DSN` to a **disposable** PostgreSQL database to run the
second engine. The harness resets its schema: do not point it at a real
database or run two test processes against the same database simultaneously.
Without the variable, the PostgreSQL leg explicitly skips.

## Separate T3 verification

T3 source: [pingdotgg/t3code, `97333813f85381313b41eb98fe859ed41c195b74`](https://github.com/pingdotgg/t3code/tree/97333813f85381313b41eb98fe859ed41c195b74).
The local transport probe imported that checkout's
`packages/effect-codex-app-server/src/client.ts`, used its
`layerChildProcess` and `raw.request`/`raw.notify` APIs, and launched the native
`codex app-server` through Effect's child-process spawner. It ran the same
committed Go fixture and assertions on both engines.

Initialization matched `CodexAdapterV2.ts`: `clientInfo.name=t3code_desktop`,
title `T3 Code Desktop`, version `0.1.0`, `experimentalApi=true`, and
`optOutNotificationMethods=["turn/diff/updated"]`. The environment carried the
isolated `CODEX_HOME` and disposable bearer variable into the subprocess.

Source inspection separately verified T3's launch behavior: provider
`homePath` overrides `CODEX_HOME`; configured provider environment overlays the
launching process environment. An unrelated terminal export is therefore
insufficient for an already-running desktop process. Operators must select the
correct Codex home and supply the bearer variable to that provider, then start
a fresh provider session.

This is a real T3 client-library and launch-profile check, not a claim of a
clicked-through desktop UI or a production deployment. No global Codex/T3
configuration or production credentials were changed.

## Security and protocol checks

The compatibility adapter keeps the modern endpoint's bearer and service
chokepoints. Tests run the existing read/write security proofs through both
profiles, including live revocation and cross-replica grant changes. Separate
transport tests cover initialization fields, pinned version refusal, session
refusal, malformed JSON/IDs, request/response bounds, host/origin admission,
notifications, JSON tool errors, and ID preservation.

`/mcp` still runs the pinned modern conformance suite. New lifecycle metadata
methods have named unauthenticated audit exemptions; no tenant service operation
is exempted. Stage remains an inert draft. Publish, Apply and secret reveal
remain absent from the tool catalog.
