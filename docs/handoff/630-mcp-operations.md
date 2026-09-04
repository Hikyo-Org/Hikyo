# Issue 630: MCP deployment and interoperability proof

## Delivered boundary

Issue #630 adds the deployment and operator proof around the phase-1 MCP
server. It does not add tools, widen authorization, expose secret material, or
add automatic OAuth.

The server remains:

- MCP `2026-07-28` over stateless JSON `POST /mcp`;
- disabled by default;
- limited to workload and automation service-account bearers;
- limited to the five read-only tools from #629;
- safe under multiple replicas with no stickiness.

## Executable evidence

| Proof | Executable evidence |
| --- | --- |
| Official Go client | `TestOfficialGoClientInteroperability` using SDK `v1.7.0` |
| Upstream conformance | `scripts/ci/check-mcp-conformance.sh`, pinned to `0.2.0-alpha.11` |
| Inspector | The conformance script runs Inspector `2.5.0` against the tenant-free fixture |
| Public HTTPS and token lifecycle | `go run ./scripts/mcp-public-smoke` proves a still-live credential, waits for its revocation, then observes the first exact denial |
| Replica portability | `TestMCPToolsEndToEndCanaryAndDenial` alternates independently loaded keyrings, services, registries, and handlers and continues a cursor |
| Immediate revocation | The isolation test and public checker both prove a successful credential is denied on the next observed request exactly like an invalid token |
| Cancellation | `TestCancellationReachesRegisteredOperation`, `TestMCPGracefulShutdownLetsActiveCallComplete`, and `TestMCPGracefulShutdownCancelsCallWhenDrainExpires` |
| Secret-safe telemetry | `TestMCPMetricsAndAccessLogsUseOnlyClosedLabels` and the metrics registry budget test |
| Helm and Compose | `scripts/ci/check-chart.sh` renders enabled MCP; `scripts/ci/check-mcp-deployment.sh` validates the digest-pinned Compose and exact HTTPS proxy fixtures |

The upstream expected-failure file is per-check, not a scenario skip. A
conformance-only fixture supplies the upstream diagnostic tools, so every
server-stateless MUST check passes. The baseline names only prompts/resources
capabilities Hikyo does not advertise. Header, metadata, statelessness,
protocol-version, and tool-list failures are not baselined.

## Client profiles

The operator runbook pins the exact configuration shape for the current Codex
CLI, Claude Code, Inspector, official Go SDK, and OpenAI Responses API. Every
committed example references an environment variable or transient secret
field. No bearer value is stored in a client configuration.

Managed bearer means the operator creates, scopes, mints, distributes, rotates,
and revokes a Hikyo service-account credential. Automatic MCP OAuth remains
unavailable because Hikyo intentionally publishes no authorization-server
metadata or registration/token endpoints in this phase.

## Deployment proof record

After the committed head is deployed, record these facts on the issue or pull
request without copying response bodies:

```text
image digest:
deployment revision:
public MCP URL:
ready replicas:
public smoke UTC timestamp:
public smoke exit status:
Codex version and result:
Claude Code version and result:
Inspector version and result:
OpenAI Responses request ID and result:
old credential revoked UTC timestamp:
first denied request UTC timestamp:
```

The local implementation cannot produce this external record before its image
is built and deployed. The public checker is fail-closed and ready for that
post-deployment step.
