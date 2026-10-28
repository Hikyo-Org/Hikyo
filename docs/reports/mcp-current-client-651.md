# Current native Codex evidence for #651

28 October preparation: the
[external acceptance packet](../handoff/651-mcp-external-acceptance.md)
records current availability and the owner's approved option A: Go v1.8.0,
Inspector 2.11.0 and Codex 0.160.1 require final-candidate proof; Claude and
Responses remain not verified for 1.0, outside the mandatory matrix. Inspector
is now pinned to 2.11.0 in source; the 16 October observations and table below retain
their historical scope. Neither this report nor preparation is final external
acceptance.

16 October 2026. [Issue #651](https://github.com/Hikyo-Org/Hikyo/issues/651)
remains open. These measurements cover actual `codex-cli 0.160.1` against local
in-process HTTP handlers on SQLite and PostgreSQL. They do not establish a
published candidate, public HTTPS deployment, or two application processes.

## Evidence identity

| Input | Recorded identity |
| --- | --- |
| Source baseline | `c4c626a105d0139fcc8a6f5444e585e7a4e0a761` |
| Tested source patch SHA-256 | `e42923a52abab6133b6a76d17023768876a4825f004286dc628d8f8843e8d95c` |
| Native test source SHA-256 | `a8b6ff94a9771bde24e4cb707c87142d2a3a644e5327d2f8c8da1fb0f7160fbe` |
| Compiled isolation test binary SHA-256 | `7d8346bd51747db186b40739ca034b1aef298b879f274afde4c0672a1c8f222a` |
| Toolchain | Go 1.27.2, darwin/arm64 |
| Client | Actual `codex-cli 0.160.1`, isolated `codex app-server` |
| PostgreSQL image | `postgres:18@sha256:06cad38a5d9f5d24b4d83d86def30795d5e4b757fedbf5281172b576dedcd941` |

The patch hash identifies the test-only working diff when the binary was built,
before this documentation was added. It is not a commit or a release image.
Source and binary identities were checked before execution and after the final
run. Private raw logs, the tested patch and local binary are retained by the
operator outside Git; they are not distributed by this report.

## Results and exact coverage

| Test | Engine | Result | Wire observations | Exercised tool calls |
| --- | --- | --- | --- | --- |
| `TestMCPCodexNativeReadOnly` | SQLite | PASS, 0.69 s | 10 | Definitions, inspect configuration |
| `TestMCPCodexNativeReadOnly` | PostgreSQL | PASS, 1.50 s | 10 | Definitions, inspect configuration |
| `TestMCPCodexNative` | SQLite | PASS, 0.67 s | 12 | Definitions, inspect configuration, validate, stage |
| `TestMCPCodexNative` | PostgreSQL | PASS, 1.47 s | 12 | Definitions, inspect configuration, validate, stage |

The read-only test verifies exactly five advertised tools. The original test
verifies the exact seven-tool profile with four exercised calls; it does not
invoke all seven tools. Every run reads a real published config value and
secret metadata through typed result fields. The secret must be marked as set
with the correct classification and no value. Every observed client frame,
including notifications and errors, is checked for the synthetic canary and
credential. Successful read-only calls create neither a caller draft nor an
audit event. The original test retains validation without staging, exactly one
inert staged draft, one corresponding validation/staging audit event each with
`origin=mcp`, and no publication.

Every observed initialization requested and negotiated `2025-06-18` on
`/mcp/codex`. Subsequent requests carried that protocol header. Actual tool calls
returned HTTP 200 with a bearer present; no request or response carried an MCP
session ID. The client's optional `resources/list` and
`resources/templates/list` requests returned HTTP 404, consistent with the
closed tools-only profile. Both initializations per engine are retained in the
[redacted machine-readable evidence](mcp-current-client-651.json).

The client used an isolated configuration containing the bearer environment
variable name. No model turn or provider API request was made. PostgreSQL used
a newly owned loopback datastore with `sslmode=disable`, so these results do
not prove database TLS. All owned containers were removed after their run.

## Preserved failed attempt

The first PostgreSQL read-only attempt failed before any MCP call with a
database connection unexpected EOF. The launcher had used socket readiness,
which could observe the official image's temporary initializer. Source copied
from the exact pinned image showed that initializer listens only on the socket,
then stops before the final PostgreSQL process starts. The failed log was
preserved separately.

The diagnosed rerun used TCP readiness and passed. The later seven-tool profile
run used authenticated TCP `SELECT 1`, passing the password only through an
environment-variable reference. The readiness budget remained 30 seconds.
This corrected the disposable launcher, not application behavior. A passing
rerun does not erase the original failure.

## Operator support table

| Exact client/profile | Classification | Evidence boundary |
| --- | --- | --- |
| Codex CLI 0.160.1, `/mcp/codex`, `2025-06-18` | Verified locally | Actual native client on both engines; five-tool read-only profile and seven-tool profile with four exercised calls as above. Final public candidate remains unverified. |
| Codex CLI 0.155.0 and T3 transport | Historical local evidence | Separate [#781 report](mcp-codex-781.md); no current T3 desktop or final-candidate claim. |
| Official Go SDK v1.8.0, modern `/mcp` | Current source pin; final candidate unverified | Existing official client probe calls all five read tools. Historical v1.7.0 success is not a v1.8.0 deployed pass. |
| Inspector 2.10.1, Modern `/mcp` | Current source pin; unexecuted in this run | `scripts/mcp-conformance/package.json` requires 2.10.1. Historical Inspector 2.5.0 success does not establish current-version acceptance. |
| Codex CLI 0.153.2 | Historical inconclusive attempt | Transport failed before the old local proxy; no compatibility verdict. |
| Claude Code 2.1.261, historical modern-endpoint attempt | Tested handshake incompatible | Historical initialize lacked the required modern mirrored headers and was refused. No successful tool invocation; no claim about a newer client or the Codex adapter. |
| Current Claude Code | Unverified; execution unavailable | No authorized subscription or paid substitute. Requires actual authorized proof or an explicit release/ADR disposition. |
| OpenAI Responses remote MCP | Unverified; no provider request | No authorized API credential/provider-recipient approval. Requires actual authorized proof or an explicit release/ADR disposition. |

Historical results are described in the
[production-proof handoff](../handoff/651-mcp-production-proof.md). Unsupported
means a measured incompatibility within the stated profile; unavailable or
unexecuted means unverified. Neither is a silent release waiver. The accepted
#631 no-OAuth decision does not waive #651's runtime evidence requirements.

## Reproduction and remaining gate

From the repository root, with Codex installed and a private disposable
`HIKYO_TEST_POSTGRES_DSN` loaded by an owned launcher:

```sh
GOMAXPROCS=2 HIKYO_TEST_CODEX_NATIVE=1 go test -p 1 ./internal/isolation \
  -run '^TestMCPCodexNative(ReadOnly)?$' -parallel=1 -count=1 -timeout=5m -v
```

The harness resets the disposable PostgreSQL isolation schema. Without the
DSN the PostgreSQL leg skips; a skip is not acceptance evidence. Ordinary test
runs also skip native Codex unless `HIKYO_TEST_CODEX_NATIVE=1` is set. Preserve
private logs and inspect only redacted observations and test outcomes.

Still required: a source-bound published candidate RepoDigest and matching
verified admission bundle; a recorded isolated public HTTPS deployment;
current actual Go, Inspector and claimed client flows; two real PostgreSQL-backed
application processes with cross-process cursor/current-authority proof,
admitted cancellation through the public proxy, bounded drain and released
shared claims; all applicable checks and explicit profile dispositions. This
test/report change references #651 and must not close it.
