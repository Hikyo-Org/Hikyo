# #651 MCP production proof

5 September 2026. Codex-authored. No commit or push by the implementation agent.

## Result and boundary

Actual application source `9827af64962a36e2e31906b48906fd59c7a36c1c`, two distinct HA-enabled containers, dedicated PostgreSQL 18, private verified TLS on ingress and both upstreams. Local Docker image ID `sha256:9a439da272423f4012a5e23f2cd52f8251343392cb7d0f27e8091870429085e1`; binary SHA-256 `a9f3928ce4f004ae96d36e8d1811cd3544fd62ac227970db45d118291063a1ee`. Built the embedded UI from that source and used `Dockerfile.release`. This is a local image ID, not a published OCI RepoDigest, final release candidate or green exact-head CI claim.

All five actual production tools passed with a scoped synthetic identity. Nonempty configuration/revision reads, secret-canary absence, wrong existing scope/ungranted/invalid refusal, stateless cross-process cursor continuation, committed credential revocation and live replacement all passed. First observed post-commit request to each replica denied the revoked token. The shared in-flight table was empty afterward. Pending changes were authenticated but empty because the workload owns no pending draft.

Actual official Go SDK v1.7.0 passed all five tools. Actual Inspector 2.5.0 Modern CLI passed a nonempty definitions call; a transient environment bridge inserted its header inside the process, so no bearer entered OS arguments or saved catalog. Isolated Codex 0.153.2 app-server direct tool invocation failed in client HTTP transport before reaching the proxy; protocol compatibility remains unverified. Isolated Claude 2.1.261 health check reached actual HTTPS but sent legacy initialize without required mirror headers and was refused 400/-32020; this tested handshake is incompatible, and no production tool call passed. Responses has no authorized API key and was not called. No model turns or paid provider requests were made.

Public ingress did not pass. First allocation timeout exposed a bug in the scratch URL parser: it selected the Cloudflare API endpoint from an error log, and a discovery attempt carrying one synthetic fixture bearer got 405 there. This invalid attempt is excluded from pass claims. The synthetic credential was immediately revoked through the application service and replaced. A strict success-banner parser on the single bounded retry rejected the API hostname. The allocated connector then failed outbound network precheck; an unauthenticated exact-route probe returned 530. The replacement token was never sent publicly after that failed preflight. No real tenant/provider/user credentials were involved. Do not publish private endpoint/log/config details.

#651 remains open for public deployment, successful named client calls, live grant mutation across the deployed processes, final candidate identity and the remaining acceptance evidence. Parent independent replay passed all five tools, cursor continuation, scoped refusals and committed credential revocation on both replicas. The local proxy buffers responses; this does not prove streaming cancellation or in-flight graceful drain. Keep the locked modern protocol and TLS/authentication checks.

## Change

The existing `scripts/mcp-public-smoke` checker rejected healthy production responses because its handwritten tests omitted the modern SDK metadata, resultType and cache envelope. `profileResultFields` now validates only the approved public envelope before the exact operation-specific closed payload checks. Unknown metadata and server-info facts, incomplete results and non-public cache profiles remain refused. `TestPublicProfileAcceptsActualProductionHandler` uses the actual handler and production registry and fails against the original checker source, then passes with the fix.

Independent R1 caught duplicate JSON members being collapsed before validation. The fix reuses `definitions.RejectDuplicateMembers` before full RPC and standalone profile map decoding. Raw regressions cover duplicate `_meta`, serverInfo, description and result fields without map reserialization. Normal/race/vet and the rebuilt checker's actual two-process replay all passed afterward. Independent R2 returned CLEAN and reran all 14 checker tests successfully.

`internal/isolation/mcp_deployment_fixture_test.go` is explicitly opt-in. It refuses non-owner-only runtime directories and DSNs outside the dedicated loopback `hikyo_mcp_651` database before migration can write anything. It seeds the existing synthetic isolation fixture and calls real value, revision, workload identity and credential services. Root/key/token files are owner-only. Ordinary CI skips this fixture; skip is not acceptance evidence. `TestMCPFixtureRefusesNonScratchDatastores` exercises the guard, including DSN query overrides.

`scripts/mcp-production-client/main.go` is the official Go SDK manual five-tool probe. It requires exact HTTPS `/mcp`, refuses redirects, injects the environment token only for tools/call, requires the exact catalog, and emits a redacted result. Its fixed synthetic identifiers are deliberately coupled to the opt-in fixture, not a general production-tenant administration interface.

Client documentation and HTML/JSON report explicitly distinguish observed local passes, tested handshake incompatibility and unverified profiles.

## Safe parent replay

The independently owned runtime is temporarily retained at `/tmp/hikyo-mcp-651-runtime` (directory0700, custody files0600). Never print settings/credentials/env/key files or full Docker inspect output. Run:

```sh
rtk proxy python3 /tmp/hikyo-mcp-651-runtime/replay.py
```

This runner refreshes only the synthetic rotating credential via the real opt-in service helper, repeats all five actual calls and cursor/refusal checks, and then commits revocation and checks both replicas. It reads credentials internally; no token arguments or result bodies are printed. The underlying scripts and local logs are diagnostic runtime artifacts, not a portable CI bootstrap harness. The replay uses `/tmp/hikyo-mcp-651` source and never changes the user's global client config. `local-proof.json` and `revocation-proof.json` are redacted; `credentials.json`, settings/env files and client private logs are not publication inputs.

To invoke the official Go probe, load `HIKYO_MCP_URL`, `HIKYO_MCP_TOKEN` and `SSL_CERT_FILE` inside an owned launcher from those private fixture files, then run `GOMAXPROCS=2 go run -p 1 ./scripts/mcp-production-client`. Do not expand the token into a shell command string or process arguments.

Owned resources are precisely `hikyo-mcp-651-pg`, `hikyo-mcp-651-app-0`, `hikyo-mcp-651-app-1`, and the local Python proxy started from this runtime directory. No active tunnel remains. After parent replay, stop/remove these containers and their owned anonymous PostgreSQL volume, stop that proxy, and remove only this rehearsal's private custody files. Parent requested the fixture remain running until independent verification; cleanup is intentionally pending, not claimed complete.

## Verification

- Actual local raw protocol + official Go SDK + Inspector probes passed as scoped above. Revocation replay passed repeatedly with fresh rotating tokens.
- `GOMAXPROCS=2 go test -p 1 -race ./scripts/mcp-public-smoke ./scripts/mcp-production-client`: pass (manual client package compiles and has no unit tests).
- `GOMAXPROCS=2 go test -p 1 -run '^TestMCPFixtureRefusesNonScratchDatastores$' -count=1 ./internal/isolation`: pass.
- Original checker source via a canonical-path Go overlay fails `TestPublicProfileAcceptsActualProductionHandler` with `actual server/discover response rejected: result did not match the exact discovery profile`.
- `GOMAXPROCS=2 go vet -p 1 ./scripts/mcp-public-smoke ./scripts/mcp-production-client ./internal/isolation`: pass.
- Docs package `pnpm run verify` under Node26.7.0/pnpm11.24.0: Astro check zero errors/warnings/hints, build, OSS policy, PWA checks and offline browser test passed.
- `git diff --check`: pass. Exact-candidate CI/merge remains parent-owned.

Report: [HTML](../reports/1.0/mcp-production-proof.html), [JSON](../reports/1.0/mcp-production-proof.json). It includes decisions, rejected options, incident disposition and every remaining evidence gate.
